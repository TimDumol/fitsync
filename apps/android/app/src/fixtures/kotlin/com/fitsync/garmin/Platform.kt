package com.fitsync.garmin

import android.content.Context
import com.fitsync.nativeapp.garmin.*
import kotlinx.coroutines.delay
import org.json.JSONObject

fun createGarmin(context: Context): Garmin = FixtureGarmin()
class FixtureGarmin: Garmin {
    private val connected = mutableSetOf<String>()
    private val pending = mutableMapOf<String, String>()
    override suspend fun connection(athlete: String) = if (athlete in connected) "connected" else "disconnected"
    override suspend fun login(athlete: String, email: String, password: String): GarminLogin {
        if (email.contains("fail", true)) throw GarminError("GARMIN_AUTH_FAILED", "Garmin authentication failed", false)
        if (email.contains("mfa", true)) { pending[athlete] = athlete; return GarminLogin(false, athlete) }
        connected.add(athlete); return GarminLogin(true)
    }
    override suspend fun resume(athlete: String, loginId: String, code: String) {
        if (pending[loginId] != athlete) throw GarminError("GARMIN_MFA_EXPIRED", "Garmin verification expired", false)
        if (code != "123456") throw GarminError("GARMIN_AUTH_FAILED", "The Garmin verification code was not accepted", false)
        pending.remove(loginId); connected.add(athlete)
    }
    override suspend fun cancel(loginId: String) { pending.remove(loginId) }
    override suspend fun disconnect(athlete: String) { connected.remove(athlete) }
    override suspend fun sync(athlete: String, days: Int, extended: Boolean, access: suspend (Boolean) -> String, progress: (String) -> Unit): JSONObject {
        if (athlete !in connected) throw GarminError("GARMIN_AUTH_FAILED", "Reconnect Garmin before syncing", true)
        listOf("authenticating", "fetching_activities", "fetching_sleep", "fetching_recovery", "fetching_activity_details", "fetching_measurements", "uploading", "cleaning_up", "complete").forEach { progress(it); delay(80) }
        return JSONObject().put("recordsSent", 3).put("batchesSent", 1).put("archivesSent", 0)
    }
}
