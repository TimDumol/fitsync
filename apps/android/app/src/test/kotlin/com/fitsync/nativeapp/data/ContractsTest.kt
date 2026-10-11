package com.fitsync.nativeapp.data

import com.fitsync.nativeapp.domain.*
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ContractsTest {
    private fun workout() = JSONObject().put("workout_id", "w").put("athlete_id", "a").put("status", "completed").put("title", "Strength").put("date", "2026-10-03").put("source", "mobile").put("entries", jsonArray(emptyList()))
    @Test fun rejectsWrongAthleteUnknownStatusAndMissingEntries() {
        assertTrue(runCatching { Contracts.workout(workout(), "other") }.isFailure)
        assertTrue(runCatching { Contracts.workout(workout().put("status", "unknown")) }.isFailure)
        assertTrue(runCatching { Contracts.workout(workout().apply { remove("entries") }) }.isFailure)
    }
    @Test fun validatesWithoutDroppingFutureFields() {
        val value = workout().put("future", JSONObject().put("value", 1))
        assertEquals(1, Contracts.workout(value, "a", "w").getJSONObject("future").getInt("value"))
    }
    @Test fun pathEncodingEscapesSeparatorsAndSpaces() {
        assertEquals("a%2Fb%20c", encode("a/b c"))
    }
}
