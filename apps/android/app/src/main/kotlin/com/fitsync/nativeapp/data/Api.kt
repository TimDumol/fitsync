package com.fitsync.nativeapp.data

import com.fitsync.nativeapp.auth.SessionExpired
import com.fitsync.nativeapp.BuildConfig
import com.fitsync.nativeapp.LogBook
import com.fitsync.nativeapp.domain.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.net.URLEncoder
import java.util.concurrent.TimeUnit

class ApiError(val status: Int, message: String, val requestId: String? = null): Exception("$message (HTTP $status)${requestId?.let { "; Request ID: $it" } ?: ""}")
fun encode(value: String): String = URLEncoder.encode(value, "UTF-8").replace("+", "%20")
interface Transport { suspend fun request(path: String, method: String = "GET", payload: JSONObject? = null): JSONObject }
class HttpTransport(private val token: suspend (Boolean) -> String): Transport {
    private val client = OkHttpClient.Builder().connectTimeout(20, TimeUnit.SECONDS).readTimeout(60, TimeUnit.SECONDS).build()
    override suspend fun request(path: String, method: String, payload: JSONObject?): JSONObject {
        var access = token(false)
        repeat(2) { attempt ->
            val result = withContext(Dispatchers.IO) {
                val body = if (method in listOf("PUT", "POST")) (payload ?: JSONObject()).toString().toRequestBody("application/json".toMediaType()) else null
                LogBook.add("api", "$method ${path.substringBefore('?')}", "debug")
                client.newCall(Request.Builder().url(BuildConfig.API_URL.trimEnd('/') + path).header("Authorization", "Bearer $access").header("Accept", "application/json").method(method, body).build()).execute().use { response ->
                    Triple(response.code, response.body?.string().orEmpty(), response.header("x-request-id"))
                }
            }
            if (result.first == 401 && attempt == 0) { access = token(true); return@repeat }
            if (result.first == 401) throw SessionExpired()
            if (result.first !in 200..299) {
                val error = runCatching { JSONObject(result.second) }.getOrNull()
                val message = error?.text("error_description") ?: error?.text("error") ?: error?.text("message") ?: "Request failed"
                LogBook.add("api", "$method ${path.substringBefore('?')} HTTP ${result.first}; Request ID: ${result.third ?: "unknown"}", "warn")
                throw ApiError(result.first, message, result.third)
            }
            return if (result.first == 204) JSONObject() else JSONObject(result.second)
        }
        throw SessionExpired()
    }
}
class Api(private val transport: Transport) {
    private fun root(athlete: String) = "/api/v2/athletes/${encode(athlete)}"
    suspend fun me(): JSONObject = transport.request("/api/v2/me").also { value ->
        value.getJSONObject("athlete").getString("athlete_id"); value.getJSONArray("accessible_athletes")
    }
    suspend fun activePlan(athlete: String): JSONObject? = transport.request(root(athlete) + "/training-plans/active").optJSONObject("plan")?.let { Contracts.plan(it, athlete) }
    suspend fun plan(athlete: String, id: String): JSONObject = Contracts.plan(transport.request(root(athlete) + "/training-plans/${encode(id)}").getJSONObject("plan"), athlete)
    suspend fun publish(athlete: String, id: String): JSONObject = Contracts.plan(transport.request(root(athlete) + "/training-plans/${encode(id)}/publish", "POST").getJSONObject("plan"), athlete)
    suspend fun skip(athlete: String, id: String): JSONObject = transport.request(root(athlete) + "/planned-sessions/${encode(id)}/skip", "POST")
    suspend fun sessions(athlete: String, from: String, to: String): List<JSONObject> = transport.request(root(athlete) + "/planned-sessions?from=${encode(from)}&to=${encode(to)}").objects("sessions")
    suspend fun workouts(athlete: String, from: String? = null, to: String? = null, cursor: String? = null): JSONObject {
        val query = listOfNotNull("limit=50", from?.let { "from=${encode(it)}" }, to?.let { "to=${encode(it)}" }, cursor?.let { "cursor=${encode(it)}" }).joinToString("&")
        return transport.request(root(athlete) + "/workouts?$query").also { page -> page.getJSONArray("items"); page.objects("items").forEach { Contracts.workout(it, athlete) } }
    }
    suspend fun workout(athlete: String, id: String): JSONObject = Contracts.workout(transport.request(root(athlete) + "/workouts/${encode(id)}"), athlete, id)
    suspend fun save(workout: JSONObject): JSONObject = Contracts.workout(transport.request(root(workout.getString("athlete_id")) + "/workouts/${encode(workout.getString("workout_id"))}", "PUT", workout), workout.getString("athlete_id"), workout.getString("workout_id"))
    suspend fun complete(workout: JSONObject): JSONObject = Contracts.workout(transport.request(root(workout.getString("athlete_id")) + "/workouts/${encode(workout.getString("workout_id"))}/complete", "POST", workout), workout.getString("athlete_id"), workout.getString("workout_id"))
    suspend fun delete(workout: JSONObject) { transport.request(root(workout.getString("athlete_id")) + "/workouts/${encode(workout.getString("workout_id"))}", "DELETE") }
    suspend fun pull(athlete: String, after: Long): JSONObject = transport.request("/api/v2/sync/pull?athlete_id=${encode(athlete)}&after=$after&limit=50")
    suspend fun garminStatus(athlete: String): JSONObject = transport.request(root(athlete) + "/garmin/sync-status")
}
