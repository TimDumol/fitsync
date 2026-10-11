package com.fitsync.nativeapp.data

import androidx.room.withTransaction
import com.fitsync.nativeapp.domain.*
import org.json.JSONObject

class Repository(val db: FitDatabase) {
    val store = db.store()
    /** Called only after the server confirms this rejected workout does not exist. */
    suspend fun repairRejectedIds(item: Pending) = db.withTransaction {
        if (store.pending(item.athlete, item.id)?.generation != item.generation) return@withTransaction
        val ids = mutableMapOf<String, String>()
        fun replacement(id: String): String = if (java.util.UUID.fromString(id).version() == 7) id else ids.getOrPut(id) { newId() }
        fun repair(raw: String): JSONObject = JSONObject(raw).apply {
            put("workout_id", replacement(getString("workout_id")))
            objects("entries").forEach { it.put("entry_id", replacement(it.getString("entry_id"))) }
        }
        val payload = repair(item.json)
        val id = payload.getString("workout_id")
        val draft = store.find(item.athlete, "draft", item.id)
        val timer = store.meta("active_timer:${item.id}")?.let { Timer.parse(it) }
        store.removeWorkout(item.athlete, item.id)
        if (draft != null) store.put(draft.copy(id = id, json = repair(draft.json).toString()))
        if (timer != null) {
            store.removeMeta("active_timer:${item.id}")
            store.meta(Metadata("active_timer:$id", timer.copy(restAnchorEntryID = ids[timer.restAnchorEntryID] ?: timer.restAnchorEntryID).json()))
        }
        val generation = (store.meta("generation")?.toLongOrNull() ?: 0) + 1
        store.meta(Metadata("generation", generation.toString()))
        store.acknowledge(item.athlete, item.id, item.generation)
        store.enqueue(item.copy(id = id, json = payload.toString(), generation = generation, retries = 0, nextAttempt = 0, error = null, blocked = false))
    }
    suspend fun saveDraft(workout: JSONObject, operation: String = "save") = db.withTransaction {
        val athlete = workout.getString("athlete_id"); val id = workout.getString("workout_id")
        val existing = store.pending(athlete, id)
        check(existing?.operation != "delete") { "This workout is pending deletion." }
        check(existing?.operation != "complete" || operation == "complete") { "This workout is already finished." }
        val generation = (store.meta("generation")?.toLongOrNull() ?: 0) + 1
        store.meta(Metadata("generation", generation.toString()))
        store.put(Aggregate(athlete, "draft", id, workout.toString(), generation))
        store.enqueue(Pending(athlete, id, operation, workout.toString(), generation))
    }
    suspend fun delete(workout: JSONObject) = db.withTransaction {
        val athlete = workout.getString("athlete_id"); val id = workout.getString("workout_id")
        val generation = (store.meta("generation")?.toLongOrNull() ?: 0) + 1
        store.meta(Metadata("generation", generation.toString()))
        store.removeWorkout(athlete, id); store.removeMeta("active_timer:$id")
        store.enqueue(Pending(athlete, id, "delete", workout.toString(), generation))
    }
    suspend fun timer(id: String): Timer = Timer.parse(store.meta("active_timer:$id"))
    suspend fun timer(id: String, value: Timer) = store.meta(Metadata("active_timer:$id", value.json()))
    suspend fun cache(athlete: String, kind: String, id: String, value: JSONObject) = db.withTransaction {
        if (kind != "workout" || store.pending(athlete, id)?.operation != "delete") store.put(Aggregate(athlete, kind, id, value.toString()))
    }
    suspend fun activePlan(athlete: String, plan: JSONObject?) = db.withTransaction {
        if (plan != null) {
            check(plan.getString("athlete_id") == athlete && plan.text("status") == "active")
            store.put(Aggregate(athlete, "plan", plan.getString("plan_id"), plan.toString()))
        }
        // An empty successful response is authoritative, even if stale plans remain cached.
        store.meta(Metadata("active_plan:$athlete", plan?.getString("plan_id") ?: ""))
    }
    suspend fun applyPage(athlete: String, page: JSONObject) = db.withTransaction {
        val current = store.meta("sync_cursor:$athlete")?.toLongOrNull() ?: 0
        check(page.getLong("next_after") >= current) { "Sync cursor moved backwards" }
        page.objects("changes").forEach { change ->
            require(change.getString("kind") in listOf("training_plan", "workout"))
            require(change.getString("operation") in listOf("upsert", "delete"))
            val kind = if (change.getString("kind") == "training_plan") "plan" else "workout"
            val id = change.getString("entity_id")
            if (kind != "workout" || store.pending(athlete, id) == null) {
                if (change.getString("operation") == "delete") {
                    store.remove(athlete, kind, id)
                    if (kind == "plan" && store.meta("active_plan:$athlete") == id) store.meta(Metadata("active_plan:$athlete", ""))
                }
                else {
                    val payload = change.getJSONObject("entity")
                    check(payload.getString("athlete_id") == athlete)
                    store.put(Aggregate(athlete, kind, id, payload.toString()))
                    if (kind == "plan") {
                        if (payload.text("status") == "active") store.meta(Metadata("active_plan:$athlete", id))
                        else if (store.meta("active_plan:$athlete") == id) store.meta(Metadata("active_plan:$athlete", ""))
                    }
                }
            }
        }
        store.meta(Metadata("sync_cursor:$athlete", page.getLong("next_after").toString()))
    }
    suspend fun acknowledge(item: Pending, response: JSONObject?) = db.withTransaction {
        // Network requests are outside transactions. Only acknowledge the exact payload sent.
        if (store.pending(item.athlete, item.id)?.generation == item.generation) {
            if (response != null) store.put(Aggregate(item.athlete, "workout", item.id, response.toString()))
            // A successful autosave must remain resumable until finish/discard.
            if (item.operation != "save") {
                store.remove(item.athlete, "draft", item.id)
                store.removeMeta("active_timer:${item.id}")
            }
            store.acknowledge(item.athlete, item.id, item.generation)
        }
    }
    suspend fun clear() = db.withTransaction { store.clearAggregates(); store.clearQueue(); store.clearMetadata() }
}
