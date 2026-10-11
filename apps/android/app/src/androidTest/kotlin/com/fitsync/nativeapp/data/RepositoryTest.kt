package com.fitsync.nativeapp.data

import androidx.room.Room
import androidx.test.platform.app.InstrumentationRegistry
import com.fitsync.nativeapp.domain.*
import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class RepositoryTest {
    private fun workout() = JSONObject().put("workout_id", "w").put("athlete_id", "a").put("status", "in_progress").put("entries", jsonArray(emptyList()))
    @Test fun staleResponseDoesNotAcknowledgeNewEditAndDeleteSupersedesCompletion() = runBlocking {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val db = Room.inMemoryDatabaseBuilder(context, FitDatabase::class.java).build()
        try {
            val repo = Repository(db)
            repo.saveDraft(workout())
            val sent = repo.store.pending("a", "w")!!
            repo.saveDraft(workout().put("notes", "new edit"))
            repo.acknowledge(sent, workout())
            assertEquals("new edit", JSONObject(repo.store.find("a", "draft", "w")!!.json).getString("notes"))
            assertNotNull(repo.store.pending("a", "w"))
            repo.saveDraft(workout().put("status", "completed"), "complete")
            repo.delete(workout())
            assertEquals("delete", repo.store.queue("a").single().operation)
            assertNull(repo.store.find("a", "draft", "w"))
        } finally { db.close() }
    }
    @Test fun malformedPageRollsBackAggregatesAndCursorAndEmptyPageAdvances() = runBlocking {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val db = Room.inMemoryDatabaseBuilder(context, FitDatabase::class.java).build()
        try {
            val repo = Repository(db)
            val valid = JSONObject().put("kind", "workout").put("operation", "upsert").put("entity_id", "w").put("entity", workout())
            val invalid = valid.copyJson().put("entity_id", "bad").put("entity", workout().put("athlete_id", "another-athlete"))
            val page = JSONObject().put("next_after", 10).put("changes", jsonArray(listOf(valid, invalid)))
            assertTrue(runCatching { repo.applyPage("a", page) }.isFailure)
            assertNull(repo.store.find("a", "workout", "w")); assertNull(repo.store.meta("sync_cursor:a"))
            repo.applyPage("a", JSONObject().put("next_after", 11).put("changes", jsonArray(emptyList())))
            assertEquals("11", repo.store.meta("sync_cursor:a"))
        } finally { db.close() }
    }
}
