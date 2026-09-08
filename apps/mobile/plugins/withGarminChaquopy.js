const { withAndroidManifest, withAppBuildGradle, withGradleProperties, withMainApplication, withProjectBuildGradle } = require('@expo/config-plugins');

const CHAQUOPY_VERSION = '17.0.0';
const CHAQUOPY_PYTHON_VERSION = '3.13';
// The Android wheel is usable with Chaquopy's cffi 1.17.x at runtime, but its
// published metadata currently requires cffi 2.x, which has no Chaquopy wheel.
const CURL_CFFI_VERSION = '0.15.0';

function groovyString(value) {
  return value.replace(/\\/g, '\\\\').replace(/'/g, "\\'");
}

function withGarminChaquopy(config) {
  // The E2E fixture uses the JavaScript Garmin double and runs on the
  // repository's x86_64 emulator. Chaquopy's Garmin wheel is ARM64-only, so
  // leave the native integration out of that build while keeping production
  // phone builds unchanged.
  if (process.env.EXPO_PUBLIC_E2E === '1') return config;

  config = withMainApplication(config, (mod) => {
    if (mod.modResults.language === 'kt') {
      const imports = 'import com.chaquo.python.Python\nimport com.chaquo.python.android.AndroidPlatform\n';
      if (!mod.modResults.contents.includes('import com.chaquo.python.Python')) {
        mod.modResults.contents = mod.modResults.contents.replace(/(package [^\n]+\n)/, `$1\n${imports}`);
      }
      if (!mod.modResults.contents.includes('Python.start(AndroidPlatform(this))')) {
        mod.modResults.contents = mod.modResults.contents.replace(
          /(  override fun onCreate\(\) \{\n    super\.onCreate\(\)\n)/,
          '$1    if (!Python.isStarted()) {\n      Python.start(AndroidPlatform(this))\n    }\n',
        );
      }
    } else if (mod.modResults.language === 'java') {
      const imports = 'import com.chaquo.python.Python;\nimport com.chaquo.python.android.AndroidPlatform;\n';
      if (!mod.modResults.contents.includes('import com.chaquo.python.Python;')) {
        mod.modResults.contents = mod.modResults.contents.replace(/(package [^;]+;\n)/, `$1\n${imports}`);
      }
      if (!mod.modResults.contents.includes('Python.start(new AndroidPlatform(this))')) {
        mod.modResults.contents = mod.modResults.contents.replace(
          /(super\.onCreate\(\);\n)/,
          '$1    if (!Python.isStarted()) {\n      Python.start(new AndroidPlatform(this));\n    }\n',
        );
      }
    }
    return mod;
  });
  config = withAndroidManifest(config, (mod) => {
    const application = mod.modResults.manifest.application?.[0];
    if (application) {
      application.$['android:allowBackup'] = 'false';
      application.$['android:fullBackupContent'] = '@xml/garmin_full_backup_rules';
      application.$['android:dataExtractionRules'] = '@xml/garmin_backup_rules';
    }
    return mod;
  });
  config = withGradleProperties(config, (mod) => {
    const existing = mod.modResults.find((item) => item.type === 'property' && item.key === 'reactNativeArchitectures');
    if (existing) {
      existing.value = 'arm64-v8a';
    } else {
      mod.modResults.push({ type: 'property', key: 'reactNativeArchitectures', value: 'arm64-v8a' });
    }

    const jvmArgs = mod.modResults.find((item) => item.type === 'property' && item.key === 'org.gradle.jvmargs');
    if (jvmArgs) {
      jvmArgs.value = jvmArgs.value.replace(/-XX:MaxMetaspaceSize=\S+/g, '') + ' -XX:MaxMetaspaceSize=1024m';
      jvmArgs.value = jvmArgs.value.trim().replace(/\s+/g, ' ');
    } else {
      mod.modResults.push({ type: 'property', key: 'org.gradle.jvmargs', value: '-Xmx2048m -XX:MaxMetaspaceSize=1024m' });
    }
    return mod;
  });
  config = withProjectBuildGradle(config, (mod) => {
    if (mod.modResults.language !== 'groovy') return mod;
    const dependency = `classpath "com.chaquo.python:gradle:${CHAQUOPY_VERSION}"`;
    if (!mod.modResults.contents.includes(dependency)) {
      mod.modResults.contents = mod.modResults.contents.replace(
        /classpath\(['\"]com\.android\.tools\.build:gradle['\"]\)/,
        (match) => `${match}\n    ${dependency}`,
      );
    }
    return mod;
  });
  return withAppBuildGradle(config, (mod) => {
    if (mod.modResults.language !== 'groovy') return mod;
    const nativeCompileJobs = process.env.FITSYNC_NATIVE_COMPILE_JOBS || '2';
    if (!/^[1-9]\d*$/.test(nativeCompileJobs)) {
      throw new Error('FITSYNC_NATIVE_COMPILE_JOBS must be a positive integer.');
    }
    if (!/apply plugin:\s*["']com\.chaquo\.python["']/.test(mod.modResults.contents)) {
      const chaquopy = "apply plugin: 'com.chaquo.python'";
      const reactPlugin = /(apply plugin:\s*[\"']com\.facebook\.react[\"']\s*)/;
      if (reactPlugin.test(mod.modResults.contents)) {
        mod.modResults.contents = mod.modResults.contents.replace(reactPlugin, `$1\n${chaquopy}\n`);
      } else {
        mod.modResults.contents = `${chaquopy}\n\n${mod.modResults.contents}`;
      }
    }
    if (!mod.modResults.contents.includes('sourceSets {')) {
      const buildPython = process.env.FITSYNC_CHAQUOPY_PYTHON
        ? `        buildPython '${groovyString(process.env.FITSYNC_CHAQUOPY_PYTHON)}'\n`
        : '';
      const chaquopyBlock = `

def fitsyncPythonSource = new File(project.buildDir, 'generated/fitsync-python')
def prepareFitsyncPython = tasks.register('prepareFitsyncPython', Sync) {
    from rootProject.file('../../../fitsync_garmin')
    into new File(fitsyncPythonSource, 'fitsync_garmin')
    exclude '**/__pycache__/**', '**/*.pyc'
}
tasks.configureEach { task ->
    if (task.name.endsWith('PythonSources')) {
        task.dependsOn(prepareFitsyncPython)
    }
}

chaquopy {
    defaultConfig {
${buildPython}        version '${CHAQUOPY_PYTHON_VERSION}'
        pip {
            // curl_cffi's Android wheel currently overstates its cffi runtime
            // requirement. Install the compatible Android cffi explicitly and
            // prevent pip from replacing it with the unavailable cffi 2.x.
            options '--no-deps'
            install 'certifi'
            install 'charset-normalizer'
            install 'idna'
            install 'urllib3'
            install 'pycparser'
            // cffi's native backend links against libffi. Chaquopy ships that
            // shared library as a separate Android wheel, so include it
            // explicitly because this block intentionally uses --no-deps.
            install 'chaquopy-libffi'
            install 'cffi==1.17.1'
            install 'requests==2.34.2'
            install 'curl_cffi==${CURL_CFFI_VERSION}'
        }
    }
    sourceSets {
        main {
            // Stage the package under build/ so Gradle does not treat the whole
            // repository as a Python source/output directory.
            srcDir fitsyncPythonSource
            srcDir rootProject.file('../modules/garmin-sync/android/src/main/python')
        }
    }
}

// Chaquopy 17 ships Python 3.13 for the ARM64 phone target.
android {
    defaultConfig {
        ndk {
            // curl_cffi publishes an Android ARM64 wheel; the Garmin phone
            // client targets the physical ARM64 device.
            abiFilters 'arm64-v8a'
        }
    }
}
`;
      mod.modResults.contents += chaquopyBlock;
    }
    if (!mod.modResults.contents.includes('fitsync_native_compile')) {
      mod.modResults.contents += `

// React Native's CMake build invokes Ninja independently of Gradle's worker
// limit. Cap compile concurrency so high-core machines don't exhaust memory.
android {
    defaultConfig {
        externalNativeBuild {
            cmake {
                arguments '-DCMAKE_JOB_POOLS=fitsync_native_compile=${nativeCompileJobs}',
                          '-DCMAKE_JOB_POOL_COMPILE=fitsync_native_compile'
            }
        }
    }
}
`;
    }
    return mod;
  });
}

module.exports = withGarminChaquopy;
