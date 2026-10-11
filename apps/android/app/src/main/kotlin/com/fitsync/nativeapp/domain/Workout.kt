package com.fitsync.nativeapp.domain

import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.util.Locale
import java.util.UUID

fun JSONObject.text(key: String): String? = if (isNull(key)) null else optString(key).takeIf { it.isNotBlank() }
fun JSONObject.objects(key: String): List<JSONObject> = optJSONArray(key)?.objects() ?: emptyList()
fun JSONArray.objects(): List<JSONObject> = (0 until length()).map { getJSONObject(it) }
fun JSONObject.copyJson(): JSONObject = JSONObject(toString())
fun jsonArray(values: Iterable<Any?>) = JSONArray().apply { values.forEach { put(it) } }
fun newId(): String {
    val random = UUID.randomUUID()
    val timestamp = System.currentTimeMillis() and 0xffffffffffffL
    return UUID((timestamp shl 16) or 0x7000L or (random.mostSignificantBits and 0xfffL), random.leastSignificantBits).toString()
}
fun duration(seconds: Long): String = "${seconds.coerceAtLeast(0) / 60}:${(seconds.coerceAtLeast(0) % 60).toString().padStart(2, '0')}"

data class Execution(val step: JSONObject, val setIndex: Int?)
fun executions(session: JSONObject): List<Execution> {
    val steps = session.objects("steps").sortedBy { it.getInt("sequence_no") }
    val groups = session.objects("groups")
    val emitted = mutableSetOf<String>()
    return buildList {
        for (step in steps) {
            val group = groups.find { g -> g.getJSONArray("step_sequence_nos").let { a -> (0 until a.length()).any { a.getInt(it) == step.getInt("sequence_no") } } }
            if (group != null) {
                if (!emitted.add(group.getString("group_id"))) continue
                val order = group.getJSONArray("step_sequence_nos")
                repeat(group.getInt("rounds")) { round ->
                    for (i in 0 until order.length()) steps.find { it.getInt("sequence_no") == order.getInt(i) }?.let { add(Execution(it, round)) }
                }
            } else {
                val sets = step.getJSONObject("prescription").objects("sets")
                if (sets.isEmpty() && step.getString("step_type") in listOf("rest", "interval")) add(Execution(step, null))
                else sets.indices.forEach { add(Execution(step, it)) }
            }
        }
    }
}

private fun identities(entry: JSONObject): List<String> = listOfNotNull(
    entry.text("exercise_key")?.let { "key:$it" },
    entry.text("exercise_name")?.let { "name:${it.trim().lowercase(Locale.ROOT).replace(Regex("\\s+"), " ")}" },
)

fun startWorkout(session: JSONObject, athlete: String, history: List<JSONObject>): JSONObject {
    val previous = mutableMapOf<String, Map<Int, JSONObject>>()
    history.filter { it.text("status") == "completed" && it.getString("date") <= session.getString("date") }
        .sortedWith(compareByDescending<JSONObject> { it.getString("date") }.thenByDescending { it.text("completed_at") ?: it.text("started_at") ?: "" })
        .forEach { workout ->
            val implicit = mutableMapOf<String, Int>()
            val values = mutableMapOf<String, MutableMap<Int, JSONObject>>()
            workout.objects("entries").filter { it.text("entry_type") == "set" }.sortedBy { it.getInt("sequence_no") }.forEach { entry ->
                identities(entry).forEach { key ->
                    val number = if (!entry.isNull("set_number")) entry.getInt("set_number") else (implicit[key] ?: 0) + 1
                    implicit[key] = maxOf(implicit[key] ?: 0, number)
                    values.getOrPut(key) { mutableMapOf() }.putIfAbsent(number, entry)
                }
            }
            values.forEach { (key, sets) -> previous.putIfAbsent(key, sets) }
        }
    val entries = executions(session).mapNotNull { execution ->
        val step = execution.step
        val prescription = step.getJSONObject("prescription")
        val set = execution.setIndex?.let { prescription.objects("sets").getOrNull(it) }
        if (execution.setIndex != null && set == null) return@mapNotNull null
        val past = identities(step).firstNotNullOfOrNull { previous[it]?.get((execution.setIndex ?: 0) + 1) }
        JSONObject().apply {
            put("entry_id", newId()); put("planned_step_id", step.getString("step_id"))
            put("entry_type", if (step.text("step_type") == "rest") "rest" else "set")
            put("exercise_key", step.opt("exercise_key")); put("exercise_name", step.opt("exercise_name"))
            put("is_warmup", step.text("step_type") == "warmup"); put("completed", false)
            if (set != null) {
                put("set_number", execution.setIndex!! + 1)
                for ((field, fallback) in listOf("reps" to "reps_min", "load_kg" to "load_kg", "rir" to "rir")) {
                    put(field, past?.opt(field)?.takeUnless { it == JSONObject.NULL } ?: set.opt(fallback) ?: JSONObject.NULL)
                }
                put("duration_seconds", set.opt("duration_seconds"))
            } else put("duration_seconds", prescription.opt("duration_seconds"))
        }
    }.mapIndexed { index, entry -> entry.put("sequence_no", index + 1) }
    return JSONObject().apply {
        put("workout_id", newId()); put("athlete_id", athlete); put("planned_session_id", session.getString("session_id"))
        put("date", session.getString("date")); put("title", session.getString("title")); put("source", "mobile")
        put("workout_type", session.getString("sport")); put("status", "in_progress"); put("started_at", Instant.now().toString())
        put("entries", jsonArray(entries)); put("planned_session", session.copyJson())
    }
}

fun groupFor(workout: JSONObject, entry: JSONObject): JSONObject? {
    val session = workout.optJSONObject("planned_session") ?: return null
    val step = session.objects("steps").find { it.text("step_id") == entry.text("planned_step_id") } ?: return null
    return session.objects("groups").find { group ->
        val order = group.getJSONArray("step_sequence_nos")
        (0 until order.length()).any { order.getInt(it) == step.getInt("sequence_no") } && entry.optInt("set_number", 1) <= group.getInt("rounds")
    }
}
fun groupLabel(group: JSONObject): String = group.text("label") ?: if (group.getJSONArray("step_sequence_nos").length() == 2) "Superset" else "Circuit"
fun restSeconds(workout: JSONObject, entry: JSONObject): Int {
    val step = workout.optJSONObject("planned_session")?.objects("steps")?.find { it.text("step_id") == entry.text("planned_step_id") }
    val set = step?.getJSONObject("prescription")?.objects("sets")?.getOrNull((entry.optInt("set_number", 1) - 1).coerceAtLeast(0))
    return if (set != null && !set.isNull("rest_seconds")) set.getInt("rest_seconds").coerceAtLeast(0) else if (groupFor(workout, entry) != null) 0 else 120
}
fun nextSet(workout: JSONObject, anchor: String?): JSONObject? {
    val entries = workout.objects("entries").sortedBy { it.getInt("sequence_no") }
    return entries.drop(entries.indexOfFirst { it.text("entry_id") == anchor } + 1).find { it.text("entry_type") == "set" && !it.optBoolean("completed") }
}

data class Timer(val elapsedSeconds: Long = 0, val restElapsedSeconds: Long = 0, val restTargetSeconds: Long = 0,
    val restActive: Boolean = false, val restStartedAtMs: Long? = null, val restAnchorEntryID: String? = null) {
    fun restNow(now: Long): Timer = if (restActive && restStartedAtMs != null) copy(restElapsedSeconds = maxOf(restElapsedSeconds, ((now - restStartedAtMs) / 1000).coerceAtLeast(0))) else this
    fun start(seconds: Int, anchor: String?, now: Long) = copy(restElapsedSeconds = 0, restTargetSeconds = seconds.toLong(), restActive = seconds > 0, restStartedAtMs = if (seconds > 0) now else null, restAnchorEntryID = anchor)
    fun stop() = copy(restElapsedSeconds = 0, restTargetSeconds = 0, restActive = false, restStartedAtMs = null, restAnchorEntryID = null)
    fun json(): String = JSONObject().put("elapsedSeconds", elapsedSeconds).put("restElapsedSeconds", restElapsedSeconds).put("restTargetSeconds", restTargetSeconds)
        .put("restActive", restActive).put("restStartedAtMs", restStartedAtMs ?: JSONObject.NULL).put("restAnchorEntryID", restAnchorEntryID ?: JSONObject.NULL).toString()
    companion object {
        fun parse(raw: String?): Timer {
            if (raw == null) return Timer()
            raw.toLongOrNull()?.let { return Timer(elapsedSeconds = it.coerceAtLeast(0)) }
            val value = JSONObject(raw)
            val start = if (value.isNull("restStartedAtMs")) null else value.getLong("restStartedAtMs")
            return Timer(value.optLong("elapsedSeconds").coerceAtLeast(0), value.optLong("restElapsedSeconds").coerceAtLeast(0), value.optLong("restTargetSeconds").coerceAtLeast(0),
                value.optBoolean("restActive", value.optLong("restTargetSeconds") > 0) && start != null, start, value.text("restAnchorEntryID"))
        }
    }
}

private val muscles = mapOf(
    "back_squat" to listOf("quads", "glutes", "hamstrings"), "front_squat" to listOf("quads", "glutes", "hamstrings"),
    "goblet_squat" to listOf("quads", "glutes"), "deadlift" to listOf("hamstrings", "glutes", "back"),
    "bench_press" to listOf("chest", "triceps", "shoulders"), "overhead_press" to listOf("shoulders", "triceps"),
    "pull_up" to listOf("back", "biceps"), "barbell_row" to listOf("back", "biceps"), "one_arm_dumbbell_row" to listOf("back", "biceps"),
    "romanian_deadlift" to listOf("hamstrings", "glutes"),
)
fun muscleGroups(entry: JSONObject): List<String> {
    fun strings(key: String) = entry.optJSONArray(key)?.let { array -> (0 until array.length()).map { array.getString(it) } } ?: emptyList()
    return (strings("direct_muscle_groups") + strings("indirect_muscle_groups")).ifEmpty { strings("muscle_groups") }.ifEmpty { muscles[entry.text("exercise_key")] ?: emptyList() }
        .map { it.trim().lowercase(Locale.ROOT) }.filter { it.isNotBlank() }.distinct()
}
data class Muscle(val group: String, val sets: Int, val workouts: Int, val percentage: Float)
fun summarizeMuscles(workouts: List<JSONObject>): List<Muscle> {
    val counts = mutableMapOf<String, Int>(); val touched = mutableMapOf<String, MutableSet<String>>()
    workouts.filter { it.text("status") != "discarded" }.forEach { workout -> workout.objects("entries").filter { it.text("entry_type") == "set" && !it.optBoolean("is_warmup") }.forEach { entry ->
        muscleGroups(entry).forEach { group -> counts[group] = (counts[group] ?: 0) + 1; touched.getOrPut(group) { mutableSetOf() }.add(workout.getString("workout_id")) }
    } }
    val total = counts.values.sum().coerceAtLeast(1)
    return counts.map { (group, count) -> Muscle(group, count, touched[group]!!.size, count.toFloat() / total) }.sortedWith(compareByDescending<Muscle> { it.sets }.thenBy { it.group })
}
