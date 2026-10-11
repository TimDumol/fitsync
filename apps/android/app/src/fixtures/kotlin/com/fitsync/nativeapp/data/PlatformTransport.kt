package com.fitsync.nativeapp.data

import android.content.Context
import com.fitsync.nativeapp.domain.*
import org.json.JSONObject
import java.time.LocalDate
import java.time.ZoneOffset

fun createTransport(context: Context, token: suspend (Boolean) -> String): Transport = FixtureTransport(context)
class FixtureTransport(context: Context): Transport {
    private val baseline = context.assets.open("baseline.json").bufferedReader().use { it.readText() }
    private var data = JSONObject(baseline)
    private fun currentPlan() = data.getJSONObject("plan").apply {
        put("start_date", LocalDate.now(ZoneOffset.UTC).toString()); put("end_date", LocalDate.now(ZoneOffset.UTC).toString())
        objects("sessions").forEach { it.put("date", LocalDate.now(ZoneOffset.UTC).toString()) }
    }
    private var plan = currentPlan()
    private fun currentWorkouts() = data.objects("workouts").associateBy { it.getString("workout_id") }.toMutableMap().apply { values.forEach { it.put("date", LocalDate.now(ZoneOffset.UTC).toString()) } }
    private var workouts = currentWorkouts()
    override suspend fun request(path: String, method: String, payload: JSONObject?): JSONObject {
        if (path == "/api/v2/me") return data.getJSONObject("me").copyJson()
        if (path.startsWith("/api/v2/sync/pull")) return JSONObject().put("changes", jsonArray(listOf(JSONObject().put("kind", "training_plan").put("operation", "upsert").put("entity_id", plan.getString("plan_id")).put("entity", plan)))).put("next_after", 1).put("has_more", false)
        if (path.endsWith("garmin/sync-status")) return JSONObject().put("last_ingested_at", JSONObject.NULL).put("latest_record_date", JSONObject.NULL).put("datasets", JSONObject())
        if (path.contains("/training-plans/")) return JSONObject().put("plan", plan.copyJson())
        if (path.endsWith("/skip")) {
            val id = path.substringBeforeLast('/').substringAfterLast('/')
            return plan.objects("sessions").first { it.getString("session_id") == id }.put("status", "skipped").copyJson()
        }
        if (path.contains("/planned-sessions?")) return JSONObject().put("sessions", plan.getJSONArray("sessions"))
        if (path.contains("/workouts?")) return JSONObject().put("items", jsonArray(workouts.values)).put("next_cursor", JSONObject.NULL)
        if (path.contains("/workouts/")) {
            val id = path.substringAfter("/workouts/").substringBefore('/')
            if (method == "DELETE") { workouts.remove(id); return JSONObject() }
            if (method in listOf("POST", "PUT")) { workouts[id] = requireNotNull(payload).copyJson(); return workouts[id]!!.copyJson() }
            return workouts[id]?.copyJson() ?: throw ApiError(404, "Workout not found")
        }
        throw ApiError(404, "Fixture route not implemented")
    }
}
