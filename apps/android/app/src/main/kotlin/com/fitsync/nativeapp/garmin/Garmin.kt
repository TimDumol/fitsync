package com.fitsync.nativeapp.garmin

import org.json.JSONObject

data class GarminLogin(val connected: Boolean, val loginId: String? = null)
class GarminError(val code: String, message: String, val retryable: Boolean): Exception(message)
interface Garmin {
    suspend fun connection(athlete: String): String
    suspend fun login(athlete: String, email: String, password: String): GarminLogin
    suspend fun resume(athlete: String, loginId: String, code: String)
    suspend fun cancel(loginId: String)
    suspend fun disconnect(athlete: String)
    suspend fun sync(athlete: String, days: Int, extended: Boolean, access: suspend (Boolean) -> String, progress: (String) -> Unit): JSONObject
}
