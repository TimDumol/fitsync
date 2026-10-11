package com.fitsync.nativeapp.domain

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class WorkoutTest {
    @Test fun generatedIdsAreUniqueUuidV7WithCurrentTimestamp() {
        val before = System.currentTimeMillis()
        val ids = (1..1000).map { java.util.UUID.fromString(newId()) }
        val after = System.currentTimeMillis()
        assertEquals(ids.size, ids.toSet().size)
        ids.forEach {
            assertEquals(7, it.version()); assertEquals(2, it.variant())
            assertTrue((it.mostSignificantBits ushr 16) in before..after)
        }
    }
    private fun step(id: String, sequence: Int, key: String) = JSONObject().put("step_id", id).put("sequence_no", sequence).put("step_type", "exercise").put("exercise_key", key).put("exercise_name", key)
        .put("prescription", JSONObject().put("sets", jsonArray(listOf(JSONObject().put("reps_min", 8).put("load_kg", 10).put("rir", 2), JSONObject().put("reps_min", 6).put("load_kg", 12)))))
    private fun session() = JSONObject().put("session_id", "session").put("title", "Strength").put("date", "2026-10-03").put("sport", "strength")
        .put("steps", jsonArray(listOf(step("a", 1, "goblet_squat"), step("b", 2, "bench_press"))))
        .put("groups", jsonArray(listOf(JSONObject().put("group_id", "g").put("rounds", 2).put("step_sequence_nos", jsonArray(listOf(1, 2))))))
    @Test fun roundsInterleaveAndRetainPrescription() {
        val workout = startWorkout(session(), "athlete", emptyList())
        assertEquals(listOf("a", "b", "a", "b"), workout.objects("entries").map { it.text("planned_step_id") })
        assertEquals(listOf(1, 1, 2, 2), workout.objects("entries").map { it.getInt("set_number") })
        assertEquals(6, workout.objects("entries")[2].getInt("reps"))
        assertEquals("Superset", groupLabel(groupFor(workout, workout.objects("entries")[0])!!))
    }
    @Test fun mostRecentWorkoutProvidesEachExerciseWithoutMixingMissingSets() {
        val older = JSONObject().put("workout_id", "old").put("status", "completed").put("date", "2026-10-01").put("entries", jsonArray(listOf(
            JSONObject().put("sequence_no", 1).put("entry_type", "set").put("exercise_key", "goblet_squat").put("set_number", 1).put("reps", 99),
            JSONObject().put("sequence_no", 2).put("entry_type", "set").put("exercise_key", "goblet_squat").put("set_number", 2).put("reps", 99),
        )))
        val recent = older.copyJson().put("date", "2026-10-02").put("entries", jsonArray(listOf(JSONObject().put("sequence_no", 1).put("entry_type", "set").put("exercise_key", "goblet_squat").put("set_number", 1).put("reps", 11).put("load_kg", 18))))
        val entries = startWorkout(session(), "athlete", listOf(older, recent)).objects("entries")
        assertEquals(11, entries[0].getInt("reps")); assertEquals(18, entries[0].getInt("load_kg"))
        assertEquals(6, entries[2].getInt("reps"))
    }
    @Test fun historyMatchesNormalizedNameWhenCanonicalKeyWasAdded() {
        val history = JSONObject().put("status", "completed").put("date", "2026-10-02").put("entries", jsonArray(listOf(JSONObject().put("sequence_no", 1).put("entry_type", "set").put("exercise_name", "  GOBLET_SQUAT ").put("reps", 12))))
        assertEquals(12, startWorkout(session(), "athlete", listOf(history)).objects("entries")[0].getInt("reps"))
    }
    @Test fun futureAndDiscardedHistoryCannotPrefill() {
        val history = JSONObject().put("status", "discarded").put("date", "2026-10-02").put("entries", jsonArray(listOf(JSONObject().put("sequence_no", 1).put("entry_type", "set").put("exercise_key", "goblet_squat").put("reps", 99))))
        assertEquals(8, startWorkout(session(), "athlete", listOf(history, history.copyJson().put("status", "completed").put("date", "2026-10-04"))).objects("entries")[0].getInt("reps"))
    }
    @Test fun restTargetUsesPerRoundPrescriptionAndFindsNextOpenSet() {
        val plan = session()
        plan.objects("steps")[1].getJSONObject("prescription").objects("sets")[0].put("rest_seconds", 75)
        val workout = startWorkout(plan, "athlete", emptyList())
        val entries = workout.objects("entries")
        assertEquals(0, restSeconds(workout, entries[0])); assertEquals(75, restSeconds(workout, entries[1]))
        entries[1].put("completed", true)
        assertEquals(entries[2].getString("entry_id"), nextSet(workout, entries[0].getString("entry_id"))!!.getString("entry_id"))
    }
    @Test fun timerSurvivesBackgroundAndNeverMovesBackwards() {
        val timer = Timer(elapsedSeconds = 20).start(120, "entry", 1000).restNow(31_000)
        val restored = Timer.parse(timer.json()).restNow(151_000)
        assertEquals(150L, restored.restElapsedSeconds); assertEquals(20L, restored.elapsedSeconds)
        assertEquals(150L, restored.restNow(10_000).restElapsedSeconds)
        assertFalse(restored.stop().restActive)
        assertEquals(30L, Timer.parse("30").elapsedSeconds)
    }
    @Test fun muscleCountsExcludeWarmupsAndDiscardedAndDeduplicateGroups() {
        val entry = JSONObject().put("entry_type", "set").put("exercise_key", "bench_press").put("direct_muscle_groups", jsonArray(listOf("chest", "chest"))).put("indirect_muscle_groups", jsonArray(listOf("triceps")))
        val workout = JSONObject().put("workout_id", "w").put("status", "completed").put("entries", jsonArray(listOf(entry, entry.copyJson().put("is_warmup", true))))
        val summary = summarizeMuscles(listOf(workout, workout.copyJson().put("status", "discarded")))
        assertEquals(2, summary.size); assertTrue(summary.all { it.sets == 1 && it.workouts == 1 && it.percentage == 0.5f })
    }
    @Test fun snapshotsPreserveUneditedWireFields() {
        val entry = JSONObject().put("entry_id", "e").put("laterality", "unilateral").put("load_basis", "per_limb").put("custom_future_field", JSONObject().put("value", 1))
        val copy = entry.copyJson().put("reps", 10)
        assertEquals("unilateral", copy.getString("laterality")); assertEquals(1, copy.getJSONObject("custom_future_field").getInt("value")); assertFalse(entry.has("reps"))
    }
}
