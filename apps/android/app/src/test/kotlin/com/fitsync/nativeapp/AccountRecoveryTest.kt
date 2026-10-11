package com.fitsync.nativeapp

import androidx.lifecycle.ViewModelStore
import androidx.test.core.app.ApplicationProvider
import com.fitsync.nativeapp.auth.SessionExpired
import com.fitsync.nativeapp.data.*
import com.fitsync.nativeapp.domain.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = FitSyncApplication::class)
class AccountRecoveryTest {
    private lateinit var app: FitSyncApplication
    private lateinit var model: AppModel
    private val models = ViewModelStore()
    private var reject = false
    private var emptyPlan = false
    private var unavailable = false

    @Before fun setup(): Unit = runBlocking {
        assumeTrue(BuildConfig.FIXTURES)
        Dispatchers.setMain(Dispatchers.Unconfined)
        app = ApplicationProvider.getApplicationContext()
        val fixture = createTransport(app) { error("Fixture transport must not request credentials") }
        val api = Api(object: Transport {
            override suspend fun request(path: String, method: String, payload: JSONObject?): JSONObject {
                if (reject) throw SessionExpired()
                if (unavailable) throw java.io.IOException("Offline")
                if (emptyPlan && path.endsWith("/training-plans/active")) return JSONObject().put("plan", JSONObject.NULL)
                return fixture.request(path, method, payload)
            }
        })
        model = AppModel(app, api)
        models.put("model", model)
        withTimeout(10_000) { model.state.first { it.ready && it.signedIn && it.plan() != null } }
    }
    @After fun close() {
        if (::model.isInitialized) models.clear()
        Dispatchers.resetMain()
    }
    @Test fun expiredSessionPreservesDraftQueueTimerAndCanSignInAgain() = runBlocking {
        val session = model.state.value.sessions().first()
        model.start(session).join()
        withTimeout(10_000) { model.state.first { it.workout != null } }
        val id = model.state.value.workout!!.getString("workout_id")
        val athlete = model.state.value.athlete
        model.rest(model.state.value.workout!!.objects("entries").first())
        val timer = model.state.value.timer
        val pending = app.repository.store.pending(athlete, id)!!
        reject = true
        model.refresh().join()
        assertFalse(model.state.value.signedIn)
        assertTrue(model.state.value.error!!.contains("Sign in again"))
        assertEquals(pending.json, app.repository.store.pending(athlete, id)!!.json)
        assertNotNull(app.repository.store.find(athlete, "draft", id))
        assertEquals(timer.elapsedSeconds, app.repository.timer(id).elapsedSeconds)
        assertTrue(app.repository.timer(id).restActive)
        reject = false
        model.fixtureSignIn().join()
        assertTrue(model.state.value.signedIn)
        withTimeout(10_000) { model.state.first { it.drafts().any { draft -> draft.text("workout_id") == id } } }
        model.go("workout", id)
        withTimeout(10_000) { model.state.first { it.workout?.text("workout_id") == id } }
        assertEquals(timer.elapsedSeconds, model.state.value.timer.elapsedSeconds)
    }
    @Test fun emptyActivePlanClearsSelectionButOfflineRefreshKeepsIt() = runBlocking {
        val session = model.state.value.sessions().first().copyJson()
        unavailable = true
        model.refresh().join()
        assertNotNull(model.state.value.plan())
        unavailable = false
        emptyPlan = true
        model.refresh().join()
        withTimeout(10_000) { model.state.first { it.activePlanIDs[it.athlete] == "" } }
        assertNull(model.state.value.plan())
        assertTrue(model.state.value.sessions().isEmpty())
        model.start(session).join()
        assertTrue(model.state.value.error!!.contains("no longer in the active plan"))
        assertTrue(app.repository.store.queue(model.state.value.athlete).isEmpty())
    }
    @Test fun backgroundExpiryAlsoReturnsToSignInWithoutClearingData() = runBlocking {
        val athlete = model.state.value.athlete
        val before = app.repository.store.list(athlete, "plan")
        app.auth.expire()
        withTimeout(10_000) { model.state.first { !it.signedIn } }
        assertEquals(before, app.repository.store.list(athlete, "plan"))
    }
}
