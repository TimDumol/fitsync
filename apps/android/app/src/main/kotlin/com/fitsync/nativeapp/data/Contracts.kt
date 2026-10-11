package com.fitsync.nativeapp.data

import com.fitsync.nativeapp.domain.*
import org.json.JSONObject
import java.time.LocalDate

/** Validate known transport fields while retaining unknown fields for lossless offline replay. */
object Contracts {
    fun workout(value: JSONObject, athlete: String? = null, id: String? = null): JSONObject = value.also {
        require(!it.getString("workout_id").isBlank())
        require(!it.getString("athlete_id").isBlank())
        if (athlete != null) require(it.getString("athlete_id") == athlete) { "Workout response belongs to another athlete" }
        if (id != null) require(it.getString("workout_id") == id) { "Workout response ID mismatch" }
        require(it.getString("status") in listOf("in_progress", "completed", "discarded")) { "Unsupported workout status" }
        LocalDate.parse(it.getString("date")); it.getString("title"); it.getString("source")
        it.getJSONArray("entries")
        it.objects("entries").forEach { entry ->
            entry.getString("entry_id"); require(entry.getInt("sequence_no") > 0)
            require(entry.getString("entry_type") in listOf("set", "rest", "note"))
            for (field in listOf("reps", "load_kg", "rir", "duration_seconds")) if (!entry.isNull(field)) require(entry.getDouble(field).isFinite() && entry.getDouble(field) >= 0)
        }
    }
    fun plan(value: JSONObject, athlete: String): JSONObject = value.also {
        require(it.getString("athlete_id") == athlete) { "Plan response belongs to another athlete" }
        it.getString("plan_id"); it.getString("title")
        require(it.getString("status") in listOf("draft", "active", "completed", "archived"))
        it.getJSONArray("sessions")
        it.objects("sessions").forEach { session ->
            session.getString("session_id"); session.getString("title"); LocalDate.parse(session.getString("date"))
            require(session.getString("status") in listOf("planned", "completed", "skipped", "cancelled"))
            session.getJSONArray("steps")
            session.objects("steps").forEach { step -> step.getString("step_id"); step.getInt("sequence_no"); step.getJSONObject("prescription") }
        }
    }
}
