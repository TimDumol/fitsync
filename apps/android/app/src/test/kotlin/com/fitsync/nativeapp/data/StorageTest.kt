package com.fitsync.nativeapp.data

import android.app.Application
import android.database.sqlite.SQLiteDatabase
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.fitsync.nativeapp.domain.*
import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class)
class StorageTest {
    private lateinit var context: Application
    private lateinit var db: FitDatabase
    private lateinit var repo: Repository
    @Before fun setup() {
        context = ApplicationProvider.getApplicationContext()
        db = Room.inMemoryDatabaseBuilder(context, FitDatabase::class.java).build()
        repo = Repository(db)
    }
    @After fun close() { db.close() }
    private fun workout() = JSONObject().put("workout_id", "w").put("athlete_id", "a").put("status", "in_progress").put("date", "2026-10-03").put("title", "Workout").put("source", "mobile").put("entries", jsonArray(emptyList()))
    @Test fun rejectedUuidV4WorkoutRecoversWithoutLosingDraftOrTimer() = runBlocking {
        val oldId = java.util.UUID.randomUUID().toString()
        val oldEntry = java.util.UUID.randomUUID().toString()
        val value = workout().put("workout_id", oldId).put("notes", "keep me").put("entries", jsonArray(listOf(
            JSONObject().put("entry_id", oldEntry).put("sequence_no", 1).put("entry_type", "set").put("reps", 12))))
        repo.saveDraft(value)
        repo.timer(oldId, Timer(elapsedSeconds = 40).start(120, oldEntry, 1000))
        val item = repo.store.pending("a", oldId)!!
        repo.store.enqueue(item.copy(blocked = true, error = "invalid_input: new IDs must be UUIDv7 (HTTP 400)"))
        var saved: JSONObject? = null
        val api = Api(object: Transport {
            override suspend fun request(path: String, method: String, payload: JSONObject?): JSONObject {
                if (path.startsWith("/api/v2/sync/pull")) return JSONObject().put("changes", jsonArray(emptyList())).put("next_after", 1).put("has_more", false)
                if (method == "GET") throw ApiError(404, "Missing")
                saved = payload!!.copyJson(); return saved!!
            }
        })
        Sync(repo, api).run("a")
        val repaired = saved!!
        val id = repaired.getString("workout_id")
        val entry = repaired.objects("entries").single().getString("entry_id")
        assertEquals(7, java.util.UUID.fromString(id).version())
        assertEquals(7, java.util.UUID.fromString(entry).version())
        assertEquals("keep me", repaired.getString("notes"))
        assertEquals(12, repaired.objects("entries").single().getInt("reps"))
        assertNotNull(repo.store.find("a", "draft", id))
        assertNull(repo.store.find("a", "draft", oldId))
        assertEquals(entry, repo.timer(id).restAnchorEntryID)
        assertEquals(40L, repo.timer(id).elapsedSeconds)
        assertTrue(repo.store.queue("a").isEmpty())
    }
    @Test fun newerDraftSurvivesAnOlderNetworkAcknowledgement() = runBlocking {
        repo.saveDraft(workout())
        val sent = repo.store.pending("a", "w")!!
        repo.saveDraft(workout().put("notes", "new edit"))
        repo.acknowledge(sent, workout())
        assertEquals("new edit", JSONObject(repo.store.find("a", "draft", "w")!!.json).getString("notes"))
        assertNotNull(repo.store.pending("a", "w"))
        assertNull(repo.store.find("a", "workout", "w"))
    }
    @Test fun acknowledgedAutosaveKeepsResumableDraftAndTimer() = runBlocking {
        repo.saveDraft(workout())
        repo.timer("w", Timer(elapsedSeconds = 40).start(120, "entry", 1000))
        val sent = repo.store.pending("a", "w")!!
        repo.acknowledge(sent, workout())
        assertNotNull(repo.store.find("a", "draft", "w"))
        assertTrue(repo.timer("w").restActive)
        assertTrue(repo.store.queue("a").isEmpty())
    }
    @Test fun deleteWinsAgainstInFlightSaveAndPulledCopy() = runBlocking {
        repo.saveDraft(workout())
        val sent = repo.store.pending("a", "w")!!
        repo.delete(workout())
        repo.acknowledge(sent, workout())
        val change = JSONObject().put("kind", "workout").put("operation", "upsert").put("entity_id", "w").put("entity", workout())
        repo.applyPage("a", JSONObject().put("changes", jsonArray(listOf(change))).put("next_after", 2))
        assertNull(repo.store.find("a", "draft", "w")); assertNull(repo.store.find("a", "workout", "w"))
        assertEquals("delete", repo.store.queue("a").single().operation)
    }
    @Test fun failedPullPageRollsBackItsCursorAndEarlierRows() = runBlocking {
        val valid = JSONObject().put("kind", "workout").put("operation", "upsert").put("entity_id", "w").put("entity", workout())
        val invalid = valid.copyJson().put("entity_id", "bad").put("entity", workout().put("athlete_id", "other"))
        assertTrue(runCatching { repo.applyPage("a", JSONObject().put("changes", jsonArray(listOf(valid, invalid))).put("next_after", 10)) }.isFailure)
        assertNull(repo.store.find("a", "workout", "w")); assertNull(repo.store.meta("sync_cursor:a"))
        repo.applyPage("a", JSONObject().put("changes", jsonArray(emptyList())).put("next_after", 12))
        assertEquals("12", repo.store.meta("sync_cursor:a"))
    }
    @Test fun activePlanSelectionTracksPublicationArchivalAndEmptyResponses() = runBlocking {
        fun plan(id: String, status: String) = JSONObject().put("plan_id", id).put("athlete_id", "a").put("status", status)
        repo.cache("a", "plan", "draft", plan("draft", "draft"))
        repo.activePlan("a", plan("old", "active"))
        repo.activePlan("a", plan("new", "active"))
        suspend fun state() = com.fitsync.nativeapp.AppState(athlete = "a", rows = repo.store.list("a", "plan"),
            activePlanIDs = mapOf("a" to repo.store.meta("active_plan:a")!!))
        assertEquals("new", state().plan()!!.getString("plan_id"))
        val archived = JSONObject().put("kind", "training_plan").put("operation", "upsert").put("entity_id", "new").put("entity", plan("new", "archived"))
        repo.applyPage("a", JSONObject().put("changes", jsonArray(listOf(archived))).put("next_after", 1))
        assertNull(state().plan())
        repo.activePlan("a", null)
        assertNull(state().plan())
        assertEquals("draft", JSONObject(repo.store.find("a", "plan", "draft")!!.json).getString("status"))
        assertEquals(3, repo.store.list("a", "plan").size)
        val draftOnly = com.fitsync.nativeapp.AppState(athlete = "a", rows = listOf(Aggregate("a", "plan", "draft", plan("draft", "draft").toString())))
        assertNull(draftOnly.plan())
    }
    private fun legacy(corrupt: Boolean = false): File {
        val file = File(context.filesDir, "SQLite/fitsync.db")
        file.parentFile!!.mkdirs(); file.delete()
        SQLiteDatabase.openOrCreateDatabase(file, null).use { legacy ->
            legacy.execSQL("CREATE TABLE local_meta(key TEXT PRIMARY KEY, value TEXT)")
            legacy.execSQL("INSERT INTO local_meta VALUES('schema_version','3'),('selected_athlete_id','a'),('sync_cursor:a','42'),('active_timer:w','30')")
            legacy.execSQL("CREATE TABLE cached_plans(plan_id TEXT, athlete_id TEXT, json TEXT)")
            legacy.execSQL("CREATE TABLE cached_workouts(workout_id TEXT, athlete_id TEXT, json TEXT)")
            legacy.execSQL("CREATE TABLE active_workout_drafts(workout_id TEXT, athlete_id TEXT, json TEXT)")
            legacy.execSQL("INSERT INTO active_workout_drafts VALUES(?,?,?)", arrayOf("w", "a", if (corrupt) "broken" else workout().put("laterality", "left").toString()))
            legacy.execSQL("CREATE TABLE outbox(id INTEGER, athlete_id TEXT, operation TEXT, aggregate_id TEXT, request_json TEXT, retry_count INTEGER, next_attempt_at INTEGER)")
            legacy.execSQL("INSERT INTO outbox VALUES(?,?,?,?,?,?,?)", arrayOf<Any>(5, "a", "save", "w", workout().toString(), 2, 9000))
            legacy.execSQL("INSERT INTO outbox VALUES(?,?,?,?,?,?,?)", arrayOf<Any>(6, "a", "complete", "w", workout().put("status", "completed").toString(), 3, 10000))
        }
        return file
    }
    @Test fun upgradePreservesPendingCompletionTimerOwnershipAndSource() = runBlocking {
        val file = legacy()
        val importer = LegacyImporter(context, db)
        importer.import()
        assertTrue(file.isFile)
        assertEquals("left", JSONObject(repo.store.find("a", "draft", "w")!!.json).getString("laterality"))
        assertEquals("complete", repo.store.queue("a").single().operation)
        assertEquals(3, repo.store.queue("a").single().retries)
        assertEquals(10000L, repo.store.queue("a").single().nextAttempt)
        assertEquals(30L, repo.timer("w").elapsedSeconds)
        assertEquals("42", repo.store.meta("sync_cursor:a"))
        assertEquals("a", repo.store.meta("legacy_athletes"))
        importer.import(); assertEquals(1, repo.store.queue("a").size)
    }
    @Test fun malformedLegacyDataDoesNotMarkImportCompleteOrResetSource() = runBlocking {
        val file = legacy(corrupt = true)
        assertTrue(runCatching { LegacyImporter(context, db).import() }.isFailure)
        assertTrue(file.isFile); assertNull(repo.store.meta("legacy_import")); assertTrue(repo.store.queue("a").isEmpty())
    }
    @Test fun syncCompletionCreatesAbsentWorkoutAndAcceptsCompletedOnReplay() = runBlocking {
        val calls = mutableListOf<String>()
        var saved: JSONObject? = null
        val api = Api(object: Transport {
            override suspend fun request(path: String, method: String, payload: JSONObject?): JSONObject {
                calls += method + " " + path
                if (path.startsWith("/api/v2/sync/pull")) return JSONObject().put("changes", jsonArray(emptyList())).put("next_after", 1).put("has_more", false)
                if (method == "GET") return saved ?: throw ApiError(404, "Missing")
                saved = payload!!.copyJson(); return saved!!
            }
        })
        val completed = workout().put("status", "completed")
        repo.saveDraft(completed, "complete")
        val sync = Sync(repo, api)
        sync.run("a")
        assertTrue(calls.any { it.startsWith("PUT") }); assertTrue(calls.any { it.startsWith("POST") })
        assertTrue(repo.store.queue("a").isEmpty())
        calls.clear(); repo.saveDraft(completed, "complete"); sync.run("a")
        assertFalse(calls.any { it.startsWith("PUT") || it.startsWith("POST") })
    }
    @Test fun permanentConflictRetainsPayloadForUserAndDoesNotLoop() = runBlocking {
        val api = Api(object: Transport {
            override suspend fun request(path: String, method: String, payload: JSONObject?): JSONObject {
                if (path.startsWith("/api/v2/sync/pull")) return JSONObject().put("changes", jsonArray(emptyList())).put("next_after", 1).put("has_more", false)
                throw ApiError(409, "Conflict")
            }
        })
        repo.saveDraft(workout()); Sync(repo, api).run("a")
        assertTrue(repo.store.queue("a").single().blocked)
        assertNotNull(repo.store.find("a", "draft", "w"))
    }
}
