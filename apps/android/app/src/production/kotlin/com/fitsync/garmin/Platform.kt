package com.fitsync.garmin

import android.content.Context
import com.chaquo.python.Python
import com.chaquo.python.android.AndroidPlatform
import com.fitsync.nativeapp.BuildConfig
import com.fitsync.nativeapp.LogBook
import com.fitsync.nativeapp.garmin.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.File
import java.util.UUID

fun createGarmin(context: Context): Garmin = PhoneGarmin(context)
class PhoneGarmin(private val context: Context): Garmin {
    private val vault = GarminTokenVault(context)
    private val runner = GarminSyncRunner()
    private val mutex = Mutex()
    private fun initialize() { if (!Python.isStarted()) Python.start(AndroidPlatform(context)) }
    private fun work(athlete: String): File { UUID.fromString(athlete); return File(context.noBackupFilesDir, "garmin-work/$athlete") }
    private suspend fun call(name: String, vararg args: Any?): Map<String, Any?> = mutex.withLock { withContext(Dispatchers.IO) {
        initialize()
        try {
            runner.run {
                val raw = Python.getInstance().getModule("native_phone_bridge").callAttr(name, *args).toJava(String::class.java)
                val envelope = JSONObject(raw)
                val value = mutableMapOf<String, Any?>("ok" to envelope.optBoolean("ok"), "logs" to envelope.optString("logs"))
                envelope.optJSONObject("value")?.let { payload -> payload.keys().forEach { value[it] = payload.opt(it) } }
                envelope.optJSONObject("error")?.let { error -> value["error"] = error.keys().asSequence().associateWith { error.opt(it) } }
                (value["logs"] as? String)?.lineSequence()?.forEach { LogBook.add("garmin", it, "debug") }
                if (value["ok"] != true) {
                    val error = value["error"] as? Map<*, *>
                    throw GarminError(error?.get("code") as? String ?: "INTERNAL", error?.get("message") as? String ?: "Garmin failed", error?.get("retryable") as? Boolean ?: false)
                }
                value
            }
        } catch (error: GarminNativeException) { LogBook.add("garmin", error.safeMessage, "error"); throw GarminError(error.code, error.safeMessage, error.retryable) }
    } }
    override suspend fun connection(athlete: String): String = withContext(Dispatchers.IO) {
        UUID.fromString(athlete)
        if (!vault.has(athlete)) "disconnected" else if (vault.read(athlete) == null) "needs_reconnect" else "connected"
    }
    override suspend fun login(athlete: String, email: String, password: String): GarminLogin {
        UUID.fromString(athlete)
        val value = call("start_login", email, password)
        if (value["status"] == "needs_mfa") return GarminLogin(false, value["login_id"] as String)
        vault.write(athlete, requireNotNull(value["token_json"] as? String)); return GarminLogin(true)
    }
    override suspend fun resume(athlete: String, loginId: String, code: String) {
        UUID.fromString(athlete)
        val value = call("resume_login", loginId, code)
        vault.write(athlete, requireNotNull(value["token_json"] as? String))
    }
    override suspend fun cancel(loginId: String) { call("cancel_login", loginId) }
    override suspend fun disconnect(athlete: String) = mutex.withLock { withContext(Dispatchers.IO) { vault.delete(athlete); work(athlete).deleteRecursively(); Unit } }
    override suspend fun sync(athlete: String, days: Int, extended: Boolean, access: suspend (Boolean) -> String, progress: (String) -> Unit): JSONObject {
        val token = vault.read(athlete) ?: throw GarminError("GARMIN_AUTH_FAILED", "Reconnect Garmin before syncing", true)
        val config = JSONObject().put("athlete_id", athlete).put("api_base_url", BuildConfig.API_URL).put("days", days).put("extended", extended).put("work_dir", work(athlete).absolutePath)
        progress("Fetching Garmin data and uploading to FitSync…")
        var bearer = access(false)
        repeat(2) { attempt ->
            try {
                val result = call("sync", config.toString(), token, bearer, ProgressSink(progress))
                vault.write(athlete, requireNotNull(result["refreshed_garmin_token_json"] as? String))
                progress("Garmin sync complete")
                return JSONObject().put("recordsSent", result["records_sent"]).put("batchesSent", result["batches_sent"]).put("archivesSent", result["archives_sent"])
            } catch (error: GarminError) {
                if (attempt == 0 && error.code == "FITNESSD_UNAUTHORIZED") bearer = access(true) else throw error
            }
        }
        error("Garmin sync failed")
    }
}

class ProgressSink(private val callback: (String) -> Unit) {
    // Public method invoked by Chaquopy from Python's progress callback.
    fun emit(raw: String) {
        val value = JSONObject(raw)
        callback("${value.optString("message")}: ${value.optString("stage")} (${value.optInt("completed")}/${value.optInt("total")})")
    }
}
