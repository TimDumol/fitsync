package com.fitsync.nativeapp.auth

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import com.fitsync.nativeapp.BuildConfig
import com.fitsync.nativeapp.LogBook
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import net.openid.appauth.*
import org.json.JSONObject
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

class TokenVault(context: Context) {
    private val file = File(context.noBackupFilesDir, "native-auth.json")
    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey("fitsync.native.auth.v1", null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder("fitsync.native.auth.v1", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
    }
    fun read(): String? {
        if (!file.exists()) return null
        val envelope = JSONObject(file.readText())
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, Base64.decode(envelope.getString("iv"), Base64.NO_WRAP)))
        return String(cipher.doFinal(Base64.decode(envelope.getString("data"), Base64.NO_WRAP)), Charsets.UTF_8)
    }
    fun write(value: String) {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding"); cipher.init(Cipher.ENCRYPT_MODE, key())
        val data = cipher.doFinal(value.toByteArray(Charsets.UTF_8))
        val tmp = File(file.parentFile, file.name + ".tmp")
        tmp.writeText(JSONObject().put("iv", Base64.encodeToString(cipher.iv, Base64.NO_WRAP)).put("data", Base64.encodeToString(data, Base64.NO_WRAP)).toString())
        check(tmp.renameTo(file)) { "Could not persist sign-in" }
    }
    fun clear() { file.delete(); File(file.parentFile, file.name + ".tmp").delete() }
}

class SessionExpired: Exception("Your session expired. Sign in again; offline workouts and pending changes are preserved.")

class Auth(context: Context) {
    private val vault = TokenVault(context)
    private val service = AuthorizationService(context)
    private var state: AuthState? = try { vault.read()?.let { AuthState.jsonDeserialize(it) } } catch (_: Exception) {
        LogBook.add("auth", "Saved sign-in could not be decrypted. Please sign in again; local workouts are preserved.", "warn")
        null
    }
    private val lock = Mutex()
    private val expired = MutableStateFlow(false)
    val requiresSignIn = expired.asStateFlow()
    private fun expireLocked() { vault.clear(); state = null; expired.value = true }
    suspend fun expire() = lock.withLock { expireLocked() }
    val hasSession: Boolean get() = state?.isAuthorized == true
    suspend fun intent(): Intent {
        val configuration = suspendCancellableCoroutine<AuthorizationServiceConfiguration> { continuation ->
            AuthorizationServiceConfiguration.fetchFromIssuer(Uri.parse(BuildConfig.ISSUER)) { config, error ->
                if (continuation.isActive) { if (config != null) continuation.resume(config) else continuation.resumeWithException(error ?: Exception("OIDC discovery failed")) }
            }
        }
        val request = AuthorizationRequest.Builder(configuration, BuildConfig.CLIENT_ID, ResponseTypeValues.CODE, Uri.parse(BuildConfig.REDIRECT_URI))
            .setScopes("fitness:read", "fitness:write", "openid", "offline_access")
            .setAdditionalParameters(mapOf("resource" to BuildConfig.RESOURCE)).build()
        LogBook.add("auth", "Pocket ID sign-in started")
        return service.getAuthorizationRequestIntent(request)
    }
    suspend fun accept(intent: Intent?) = lock.withLock {
        requireNotNull(intent) { "Sign-in was cancelled" }
        val response = AuthorizationResponse.fromIntent(intent)
        val error = AuthorizationException.fromIntent(intent)
        if (response == null) throw error ?: Exception("Sign-in was closed before receiving a callback")
        val next = AuthState(response, error)
        val request = response.createTokenExchangeRequest(mapOf("resource" to BuildConfig.RESOURCE))
        val tokens = exchange(request)
        next.update(tokens, null)
        check(next.isAuthorized) { "Pocket ID returned no access token" }
        vault.write(next.jsonSerializeString()); state = next; expired.value = false
    }
    private suspend fun exchange(request: TokenRequest): TokenResponse = suspendCancellableCoroutine { continuation ->
        service.performTokenRequest(request) { response, error ->
            if (continuation.isActive) { if (response != null) continuation.resume(response) else continuation.resumeWithException(error ?: Exception("Token exchange failed")) }
        }
    }
    suspend fun token(force: Boolean = false): String = lock.withLock {
        val current = state ?: run { expireLocked(); throw SessionExpired() }
        if (!force && current.accessToken != null && (current.accessTokenExpirationTime ?: Long.MAX_VALUE) > System.currentTimeMillis() + 60_000) return@withLock current.accessToken!!
        val refresh = current.refreshToken ?: return@withLock current.accessToken ?: run { expireLocked(); throw SessionExpired() }
        LogBook.add("auth", "Refreshing FitSync sign-in", "debug")
        val request = TokenRequest.Builder(requireNotNull(current.authorizationServiceConfiguration), BuildConfig.CLIENT_ID)
            .setGrantType(GrantTypeValues.REFRESH_TOKEN).setRefreshToken(refresh).setAdditionalParameters(mapOf("resource" to BuildConfig.RESOURCE)).build()
        try {
            val response = exchange(request); current.update(response, null); vault.write(current.jsonSerializeString())
            current.accessToken ?: error("Pocket ID returned no access token")
        } catch (error: AuthorizationException) {
            if (error.error == "invalid_grant") { expireLocked(); throw SessionExpired() }
            throw error
        }
    }
    suspend fun clear() = lock.withLock { vault.clear(); state = null; expired.value = false }
}
