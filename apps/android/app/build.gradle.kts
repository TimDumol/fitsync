import java.time.Instant
import java.net.URI

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.kapt")
}
// Do not resolve or package Python in emulator fixtures.
val fixtures = providers.gradleProperty("fixtures").orNull == "true"
layout.buildDirectory.set(layout.projectDirectory.dir(if (fixtures) "build/fixtures" else "build/production"))
if (!fixtures) apply(plugin = "com.chaquo.python")
fun env(name: String, fallback: String) = System.getenv(name) ?: fallback
fun literal(value: String) = "\"" + value.replace("\\", "\\\\").replace("\"", "\\\"") + "\""
android {
    namespace = "com.fitsync.nativeapp"
    compileSdk = 35
    defaultConfig {
        applicationId = if (fixtures) "com.fitsync.mobile.nativefixtures" else env("FITSYNC_APPLICATION_ID", "com.fitsync.mobile")
        require(fixtures || applicationId != "com.fitsync.mobile.nativefixtures") { "The fixture app ID is reserved for tests." }
        minSdk = 26
        targetSdk = 35
        versionCode = providers.gradleProperty("versionCode").orNull?.toInt() ?: 2
        versionName = "2.0.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        val redirect = env("FITSYNC_OAUTH_REDIRECT_URI", "fitsync:///oauthredirect")
        manifestPlaceholders["appAuthRedirectScheme"] = URI(redirect).scheme
        buildConfigField("String", "REDIRECT_URI", literal(redirect))
        buildConfigField("boolean", "FIXTURES", fixtures.toString())
        buildConfigField("String", "API_URL", literal(env("FITSYNC_API_BASE_URL", "https://api.aries.timdumol.com")))
        buildConfigField("String", "ISSUER", literal(env("FITSYNC_POCKET_ID_ISSUER", "https://auth.aries.timdumol.com")))
        buildConfigField("String", "CLIENT_ID", literal(env("FITSYNC_OAUTH_CLIENT_ID", "fitsync-mobile")))
        buildConfigField("String", "RESOURCE", literal(env("FITSYNC_OAUTH_RESOURCE", env("FITSYNC_API_BASE_URL", "https://api.aries.timdumol.com") + "/mcp")))
        buildConfigField("String", "BUILD_TIMESTAMP", literal(env("FITSYNC_BUILD_TIMESTAMP", Instant.now().toString())))
        if (!fixtures) ndk { abiFilters += "arm64-v8a" }
    }
    signingConfigs {
        create("distribution") {
            val path = System.getenv("FITSYNC_SIGNING_STORE")
            if (path != null) {
                storeFile = file(path)
                storePassword = System.getenv("FITSYNC_SIGNING_STORE_PASSWORD")
                keyAlias = System.getenv("FITSYNC_SIGNING_ALIAS")
                keyPassword = System.getenv("FITSYNC_SIGNING_KEY_PASSWORD")
            }
        }
    }
    buildTypes {
        getByName("release") {
            signingConfig = if (System.getenv("FITSYNC_SIGNING_STORE") != null) signingConfigs.getByName("distribution") else signingConfigs.getByName("debug")
            isMinifyEnabled = false
        }
    }
    buildFeatures { compose = true; buildConfig = true }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    kotlinOptions { jvmTarget = "17" }
    testOptions { unitTests.isIncludeAndroidResources = true }
    sourceSets.getByName("main").java.srcDir(if (fixtures) "src/fixtures/kotlin" else "src/production/kotlin")
    if (fixtures) sourceSets.getByName("main").assets.srcDir("src/fixtures/assets")
    packaging { resources.excludes += setOf("META-INF/AL2.0", "META-INF/LGPL2.1") }
}
if (!fixtures) {
    extensions.configure<com.chaquo.python.ChaquopyExtension> {
        defaultConfig {
            version = "3.13"
            System.getenv("FITSYNC_CHAQUOPY_PYTHON")?.let { buildPython(it) }
            pip {
                options("--no-deps")
                install("certifi"); install("charset-normalizer"); install("idna"); install("urllib3==2.8.0"); install("pycparser")
                install("chaquopy-libffi"); install("cffi==1.17.1"); install("requests==2.34.2"); install("curl_cffi==0.15.0")
            }
        }
        sourceSets.getByName("main") {
            srcDir("src/production/python")
            srcDir(layout.buildDirectory.dir("generated/python"))
        }
    }
    val preparePython by tasks.registering(Sync::class) {
        from("../../../fitsync_garmin") { into("fitsync_garmin"); exclude("**/__pycache__/**", "**/*.pyc") }
        into(layout.buildDirectory.dir("generated/python"))
    }
    tasks.configureEach { if (name.endsWith("PythonSources")) dependsOn(preparePython) }
}
kapt { arguments { arg("room.schemaLocation", "$projectDir/schemas") } }
dependencies {
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("androidx.compose.ui:ui:1.7.6")
    implementation("androidx.compose.foundation:foundation:1.7.6")
    implementation("androidx.compose.material3:material3:1.3.1")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("androidx.room:room-runtime:2.7.2")
    implementation("androidx.room:room-ktx:2.7.2")
    kapt("androidx.room:room-compiler:2.7.2")
    implementation("androidx.work:work-runtime-ktx:2.9.1")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("net.openid:appauth:0.11.1")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.9.0")
    testImplementation("org.robolectric:robolectric:4.14.1")
    testImplementation("androidx.test:core:1.6.1")
    testImplementation("androidx.compose.ui:ui-test-junit4:1.7.6")
    debugImplementation("androidx.compose.ui:ui-test-manifest:1.7.6")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.room:room-testing:2.7.2")
}
