package com.fitsync.nativeapp

import android.app.Application
import android.content.Intent
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.fitsync.nativeapp.auth.SessionExpired
import androidx.work.WorkManager
import com.fitsync.nativeapp.data.*
import com.fitsync.nativeapp.domain.*
import com.fitsync.nativeapp.garmin.GarminError
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.json.JSONObject
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import kotlin.math.roundToInt

data class Screen(val name: String = "today", val id: String? = null)
data class AppState(
    val ready: Boolean = false, val busy: Boolean = false, val signedIn: Boolean = false,
    val error: String? = null, val account: JSONObject? = null, val athlete: String = "", val screen: Screen = Screen(),
    val activePlanIDs: Map<String, String> = emptyMap(),
    val rows: List<Aggregate> = emptyList(), val queue: List<Pending> = emptyList(), val syncing: Boolean = false,
    val workout: JSONObject? = null, val timer: Timer = Timer(), val endedRest: Timer? = null,
    val historyDays: Long = 7, val historyCursor: String? = null, val historyLoading: Boolean = false,
    val garminConnection: String = "disconnected", val loginId: String? = null, val garminBusy: Boolean = false,
    val garminError: String? = null, val garminProgress: String? = null, val garminResult: JSONObject? = null, val garminStatus: JSONObject? = null,
) {
    fun plans() = rows.filter { it.athlete == athlete && it.kind == "plan" }.map { JSONObject(it.json) }
    fun plan() = plans().firstOrNull { it.text("status") == "active" && (athlete !in activePlanIDs || it.text("plan_id") == activePlanIDs[athlete]) }
    fun sessions() = plan()?.objects("sessions") ?: emptyList()
    fun drafts() = rows.filter { it.athlete == athlete && it.kind == "draft" }.map { JSONObject(it.json) }
    fun workouts(): List<JSONObject> {
        val deleted = queue.filter { it.athlete == athlete && it.operation == "delete" }.map { it.id }.toSet()
        val values = rows.filter { it.athlete == athlete && it.kind in listOf("workout", "draft") }.groupBy { it.id }
        return values.filterKeys { it !in deleted }.map { (_, copies) -> JSONObject((copies.find { it.kind == "draft" } ?: copies.first()).json) }
            .sortedWith(compareByDescending<JSONObject> { it.getString("date") }.thenByDescending { it.text("completed_at") ?: it.text("started_at") ?: "" })
    }
    fun selectedProfile() = account?.objects("accessible_athletes")?.find { it.text("athlete_id") == athlete }
    fun today() = LocalDate.now(ZoneId.of(selectedProfile()?.text("timezone") ?: "UTC")).toString()
    fun history() = workouts().filter { it.getString("date") >= LocalDate.now().minusDays(historyDays - 1).toString() && it.getString("date") <= LocalDate.now().toString() }
}
class AppModel @JvmOverloads constructor(application: Application, private val api: Api = (application as FitSyncApplication).api): AndroidViewModel(application) {
    private val app = application as FitSyncApplication
    private val repository = app.repository
    private val sync = if (api === app.api) app.sync else Sync(repository, api)
    private val mutable = MutableStateFlow(AppState())
    val state = mutable.asStateFlow()
    private val navigation = mutableListOf<Screen>()
    private val writes = Mutex()
    private var timerJob: Job? = null
    private var foreground = true
    private var historyJob: Job? = null
    private var profileJob: Job? = null
    private var undoJob: Job? = null
    private var draftSyncJob: Job? = null
    init {
        viewModelScope.launch {
            try {
                app.initialize()
                launch { repository.store.observe().collect { rows -> mutable.value = mutable.value.copy(rows = rows) } }
                launch { repository.store.observeQueue().collect { queue -> mutable.value = mutable.value.copy(queue = queue) } }
                launch { repository.store.observeActivePlans().collect { values ->
                    mutable.update { it.copy(activePlanIDs = values.associate { value -> value.key.removePrefix("active_plan:") to value.value }) }
                } }
                launch { app.auth.requiresSignIn.collect { expired -> if (expired) requireSignIn() } }
                if (BuildConfig.FIXTURES || app.auth.hasSession) loadAccount()
            } catch (error: Exception) { failure(error) }
            mutable.value = mutable.value.copy(ready = true)
        }
    }
    private suspend fun failure(error: Exception) {
        if (error is CancellationException) throw error
        if (error is SessionExpired || (error as? ApiError)?.status == 401) {
            app.auth.expire()
            requireSignIn()
            return
        }
        LogBook.add("app", error.message ?: "Action failed", "error")
        mutable.value = mutable.value.copy(error = error.message ?: "Action failed")
    }
    private suspend fun requireSignIn() {
        timerJob?.cancel(); undoJob?.cancel(); draftSyncJob?.cancel()
        writes.withLock {
            val current = mutable.value
            if (current.signedIn) current.workout?.takeIf { it.text("status") == "in_progress" }?.let {
                repository.saveDraft(it)
                repository.timer(it.getString("workout_id"), current.timer)
            }
            navigation.clear()
            mutable.update { it.copy(signedIn = false, account = null,
                screen = Screen(), error = SessionExpired().message, syncing = false) }
        }
    }
    fun clearError() { mutable.value = mutable.value.copy(error = null) }
    private suspend fun loadAccount() {
        val account = try { api.me() } catch (error: Exception) {
            if (error is CancellationException) throw error
            if (error is SessionExpired || (error as? ApiError)?.status in listOf(401, 403)) throw error
            if (!BuildConfig.FIXTURES && !app.auth.hasSession) throw error
            repository.store.meta("account")?.let { JSONObject(it) } ?: throw error
        }
        val allowed = account.objects("accessible_athletes").map { it.getString("athlete_id") }.toSet()
        val legacy = repository.store.meta("legacy_athletes")?.split(',')?.filter { it.isNotBlank() } ?: emptyList()
        check(allowed.containsAll(legacy)) { "Sign in with the account that owns the imported offline workouts. They have been preserved." }
        val previousAccount = repository.store.meta("account")?.let { JSONObject(it).getJSONObject("athlete").getString("athlete_id") }
        check(previousAccount == null || previousAccount == account.getJSONObject("athlete").getString("athlete_id")) { "The saved offline data belongs to another account. Sign in with that account or log out to clear it." }
        repository.store.meta(Metadata("account", account.toString()))
        val selected = repository.store.meta("selected_athlete_id")?.takeIf { it in allowed } ?: account.getJSONObject("athlete").getString("athlete_id")
        mutable.value = mutable.value.copy(signedIn = true, account = account, athlete = selected, error = null)
        select(selected)
    }
    suspend fun signInIntent(): Intent = app.auth.intent()
    fun authResult(intent: Intent?) = action { app.auth.accept(intent); loadAccount() }
    fun fixtureSignIn() = action { loadAccount() }
    fun retryAccount() = action { loadAccount() }
    private fun action(block: suspend () -> Unit): Job = viewModelScope.launch {
        clearError(); mutable.value = mutable.value.copy(busy = true)
        try { block() } catch (error: Exception) { failure(error) } finally { mutable.value = mutable.value.copy(busy = false) }
    }
    fun select(athlete: String) {
        require(mutable.value.account?.objects("accessible_athletes")?.any { it.text("athlete_id") == athlete } == true)
        profileJob?.cancel(); historyJob?.cancel(); timerJob?.cancel(); undoJob?.cancel()
        navigation.clear()
        mutable.value = mutable.value.copy(athlete = athlete, screen = Screen(), workout = null, timer = Timer(), endedRest = null, historyCursor = null,
            garminConnection = "disconnected", loginId = null, garminResult = null, garminProgress = null, garminError = null)
        profileJob = viewModelScope.launch {
            repository.store.meta(Metadata("selected_athlete_id", athlete))
            refresh(athlete)
            try { if (mutable.value.signedIn) loadGarmin(athlete) } catch (error: Exception) { failure(error) }
        }
    }
    private suspend fun refresh(athlete: String) {
        if (!mutable.value.signedIn) return
        mutable.value = mutable.value.copy(syncing = true)
        try {
            sync.run(athlete)
            repository.activePlan(athlete, api.activePlan(athlete))
        } catch (error: Exception) { failure(error) }
        finally { mutable.value = mutable.value.copy(syncing = false) }
        if (!BuildConfig.FIXTURES && mutable.value.signedIn) Sync.schedule(app, athlete)
    }
    fun refresh() = action { refresh(mutable.value.athlete) }
    fun logout() = action {
        profileJob?.cancelAndJoin(); historyJob?.cancelAndJoin(); timerJob?.cancelAndJoin(); undoJob?.cancelAndJoin()
        draftSyncJob?.cancelAndJoin()
        if (!BuildConfig.FIXTURES) WorkManager.getInstance(app).cancelAllWorkByTag("fitsync-sync")
        sync.exclusive { writes.withLock { app.auth.clear(); repository.clear(); mutable.value = AppState(ready = true) } }
        navigation.clear()
    }
    fun go(name: String, id: String? = null) {
        navigation.add(mutable.value.screen); mutable.value = mutable.value.copy(screen = Screen(name, id), error = null)
        if (name == "history") history()
        if (name == "summary") action {
            val athlete = mutable.value.athlete
            if (repository.store.find(athlete, "draft", id!!) == null) {
                try { repository.cache(athlete, "workout", id, api.workout(athlete, id)) }
                catch (error: Exception) { if (error is CancellationException || error is SessionExpired || (error as? ApiError)?.status == 401 || repository.store.find(athlete, "workout", id) == null) throw error }
            }
        }
        if (name == "workout") action { restore(id!!) }
    }
    fun tab(name: String) { navigation.clear(); mutable.value = mutable.value.copy(screen = Screen(name), error = null); if (name == "history") history() }
    fun back() { mutable.value = mutable.value.copy(screen = if (navigation.isEmpty()) Screen() else navigation.removeAt(navigation.lastIndex), error = null) }
    fun publish(id: String) = action {
        val athlete = mutable.value.athlete
        val canonical = api.plan(athlete, id)
        repository.cache(athlete, "plan", id, canonical)
        check(canonical.text("status") == "draft") { "That draft is no longer available. The plan list was refreshed." }
        val published = api.publish(athlete, id); repository.cache(athlete, "plan", id, published); refresh(athlete)
    }
    fun skip(id: String) = action {
        val athlete = mutable.value.athlete
        api.skip(athlete, id)
        repository.activePlan(athlete, api.activePlan(athlete))
    }
    fun start(session: JSONObject) = action {
        val athlete = mutable.value.athlete
        check(mutable.value.sessions().any { it.text("session_id") == session.text("session_id") && it.text("status") == "planned" }) { "This session is no longer in the active plan." }
        val previous = try { api.workouts(athlete, to = session.getString("date")).objects("items") } catch (error: Exception) {
            if (error is CancellationException || error is SessionExpired || (error as? ApiError)?.status == 401) throw error
            mutable.value.workouts()
        }
        val workout = startWorkout(session, athlete, previous)
        writes.withLock { repository.saveDraft(workout); repository.timer(workout.getString("workout_id"), Timer()) }
        go("workout", workout.getString("workout_id"))
    }
    private suspend fun restore(id: String) {
        val workout = repository.store.find(mutable.value.athlete, "draft", id)?.let { JSONObject(it.json) } ?: error("Workout draft not found")
        check(workout.text("status") == "in_progress") { "Workout is already finished" }
        mutable.value = mutable.value.copy(workout = workout, timer = repository.timer(id).restNow(System.currentTimeMillis()), endedRest = null)
        startTicks()
    }
    private fun startTicks() {
        timerJob?.cancel()
        timerJob = viewModelScope.launch {
            while (isActive) {
                delay(1000)
                val current = mutable.value
                if (current.workout?.text("status") != "in_progress") break
                var timer = current.timer.restNow(System.currentTimeMillis())
                if (foreground && current.screen.name == "workout") timer = timer.copy(elapsedSeconds = timer.elapsedSeconds + 1)
                mutable.value = mutable.value.copy(timer = timer)
                writes.withLock { if (mutable.value.workout?.text("status") == "in_progress") repository.timer(current.workout.getString("workout_id"), mutable.value.timer) }
            }
        }
    }
    fun foreground(active: Boolean) { foreground = active; if (active && mutable.value.signedIn) refresh() }
    fun edit(entryId: String, field: String, value: Any?) {
        val current = mutable.value
        val workout = current.workout?.copyJson()?.takeIf { it.text("status") == "in_progress" } ?: return
        val entry = workout.objects("entries").find { it.getString("entry_id") == entryId } ?: return
        val completesNext = field == "completed" && value == true && nextSet(workout, current.timer.restAnchorEntryID)?.text("entry_id") == entryId
        entry.put(field, value ?: JSONObject.NULL)
        // Preserve explicit load-basis semantics when editing a recorded load.
        if (field == "load_kg") when (entry.text("load_basis")) {
            "per_limb" -> { entry.put("load_per_limb_kg", value ?: JSONObject.NULL); entry.put("total_external_load_kg", JSONObject.NULL) }
            "total_external" -> entry.put("total_external_load_kg", value ?: JSONObject.NULL)
            "bodyweight_added" -> entry.put("added_load_kg", value ?: JSONObject.NULL)
        }
        mutable.value = current.copy(workout = workout)
        if (completesNext) endRest()
        persist()
    }
    fun notes(value: String) { mutable.value.workout?.copyJson()?.takeIf { it.text("status") == "in_progress" }?.let { mutable.value = mutable.value.copy(workout = it.put("notes", value.ifBlank { null } ?: JSONObject.NULL)); persist() } }
    fun add(unplanned: Boolean, exerciseName: String = "Unplanned exercise") {
        val workout = mutable.value.workout?.copyJson()?.takeIf { it.text("status") == "in_progress" } ?: return
        val last = workout.objects("entries").lastOrNull { it.text("entry_type") == "set" }
        val entry = if (!unplanned && last != null) last.copyJson() else JSONObject().put("entry_type", "set").put("exercise_name", exerciseName.trim().ifBlank { "Unplanned exercise" }).put("set_number", 1)
        entry.put("entry_id", newId()).put("sequence_no", (workout.objects("entries").maxOfOrNull { it.getInt("sequence_no") } ?: 0) + 1).put("completed", false)
        if (!unplanned) entry.put("set_number", (last?.optInt("set_number") ?: 0) + 1)
        workout.getJSONArray("entries").put(entry); mutable.value = mutable.value.copy(workout = workout); persist()
    }
    fun remove(id: String) {
        val workout = mutable.value.workout?.copyJson()?.takeIf { it.text("status") == "in_progress" } ?: return
        workout.put("entries", jsonArray(workout.objects("entries").filter { it.getString("entry_id") != id }))
        mutable.value = mutable.value.copy(workout = workout); persist()
    }
    private fun persist() = viewModelScope.launch {
        try { writes.withLock { mutable.value.workout?.takeIf { it.text("status") == "in_progress" }?.let { workout ->
            repository.saveDraft(workout)
            if (!BuildConfig.FIXTURES) {
                draftSyncJob?.cancel()
                draftSyncJob = viewModelScope.launch { delay(1500); Sync.schedule(app, workout.getString("athlete_id")) }
            }
        } } }
        catch (error: Exception) { failure(error) }
    }
    fun rest(entry: JSONObject) {
        val workout = mutable.value.workout?.takeIf { it.text("status") == "in_progress" } ?: return
        val seconds = restSeconds(workout, entry).takeIf { it > 0 } ?: 120
        mutable.value = mutable.value.copy(timer = mutable.value.timer.start(seconds, entry.getString("entry_id"), System.currentTimeMillis()), endedRest = null)
        saveTimer()
    }
    fun endRest() {
        val timer = mutable.value.timer.restNow(System.currentTimeMillis())
        if (!timer.restActive) return
        mutable.value = mutable.value.copy(timer = timer.stop(), endedRest = timer)
        saveTimer(); undoJob?.cancel(); undoJob = viewModelScope.launch { delay(5000); mutable.value = mutable.value.copy(endedRest = null) }
    }
    fun undoRest() {
        val ended = mutable.value.endedRest ?: return
        mutable.value = mutable.value.copy(timer = ended.restNow(System.currentTimeMillis()), endedRest = null); undoJob?.cancel(); saveTimer()
    }
    private fun saveTimer() = action { writes.withLock { mutable.value.workout?.takeIf { it.text("status") == "in_progress" }?.let { repository.timer(it.getString("workout_id"), mutable.value.timer) } } }
    fun finish() = action {
        writes.withLock {
            val workout = mutable.value.workout?.copyJson() ?: error("Workout not loaded")
            check(workout.text("status") == "in_progress")
            mutable.value = mutable.value.copy(workout = workout.copyJson().put("status", "finishing"))
            timerJob?.cancel()
            workout.put("status", "completed").put("completed_at", Instant.now().toString()).put("duration_minutes", (mutable.value.timer.elapsedSeconds / 60.0).roundToInt())
            try { repository.saveDraft(workout, "complete") } catch (error: Exception) {
                mutable.value = mutable.value.copy(workout = workout.copyJson().put("status", "in_progress").put("completed_at", JSONObject.NULL))
                startTicks(); throw error
            }
            repository.store.removeMeta("active_timer:${workout.getString("workout_id")}")
            timerJob?.cancel(); mutable.value = mutable.value.copy(workout = workout, timer = mutable.value.timer.stop(), screen = Screen("summary", workout.getString("workout_id")))
            if (!BuildConfig.FIXTURES) Sync.schedule(app, workout.getString("athlete_id"), 2000)
        }
    }
    fun delete(workout: JSONObject) = action {
        writes.withLock {
            if (mutable.value.workout?.text("workout_id") == workout.text("workout_id")) mutable.value = mutable.value.copy(workout = workout.copyJson().put("status", "deleting"))
            repository.delete(workout); timerJob?.cancel()
            mutable.value = mutable.value.copy(workout = null, timer = Timer(), screen = Screen("history"))
        }
        if (BuildConfig.FIXTURES) sync.run(workout.getString("athlete_id")) else Sync.schedule(app, workout.getString("athlete_id"))
    }
    fun discard() { mutable.value.workout?.let { delete(it) } }
    fun history(days: Long = mutable.value.historyDays, more: Boolean = false) {
        historyJob?.cancel()
        mutable.value = mutable.value.copy(historyDays = days, historyLoading = true, historyCursor = if (more) mutable.value.historyCursor else null)
        val athlete = mutable.value.athlete; val cursor = mutable.value.historyCursor
        historyJob = viewModelScope.launch {
            try {
                val page = api.workouts(athlete, LocalDate.now().minusDays(days - 1).toString(), LocalDate.now().toString(), cursor)
                page.objects("items").forEach { repository.cache(athlete, "workout", it.getString("workout_id"), it) }
                if (mutable.value.athlete == athlete) mutable.value = mutable.value.copy(historyCursor = page.text("next_cursor"))
            } catch (error: Exception) { failure(error) } finally { mutable.value = mutable.value.copy(historyLoading = false) }
        }
    }
    private suspend fun loadGarmin(athlete: String) {
        val connection = app.garmin.connection(athlete)
        val status = try { api.garminStatus(athlete) } catch (error: Exception) {
            if (error is CancellationException || error is SessionExpired || (error as? ApiError)?.status == 401) throw error
            null
        }
        if (mutable.value.athlete == athlete) mutable.value = mutable.value.copy(garminConnection = connection, garminStatus = status)
    }
    private fun garminAction(block: suspend (String) -> Unit) = viewModelScope.launch {
        if (mutable.value.garminBusy) return@launch
        val athlete = mutable.value.athlete
        mutable.value = mutable.value.copy(garminBusy = true, garminError = null)
        try { block(athlete); loadGarmin(athlete) }
        catch (error: Exception) {
            if (error is CancellationException) throw error
            if (error is SessionExpired || (error as? ApiError)?.status == 401 || (error as? GarminError)?.code == "FITNESSD_UNAUTHORIZED") {
                failure(SessionExpired()); return@launch
            }
            val message = if (error is GarminError) "${error.message}\nDiagnostic code: ${error.code}\n${if (error.retryable) "You can try again." else "Please reconnect Garmin if needed."}" else error.message ?: "Garmin action failed"
            LogBook.add("garmin", message, "error"); mutable.value = mutable.value.copy(garminError = message)
        } finally { mutable.value = mutable.value.copy(garminBusy = false) }
    }
    fun connectGarmin(email: String, password: String) = garminAction { athlete ->
        val login = app.garmin.login(athlete, email, password)
        mutable.value = mutable.value.copy(loginId = login.loginId)
    }
    fun verifyGarmin(code: String) = garminAction { athlete -> app.garmin.resume(athlete, requireNotNull(mutable.value.loginId), code); mutable.value = mutable.value.copy(loginId = null) }
    fun cancelGarmin() = garminAction { mutable.value.loginId?.let { app.garmin.cancel(it) }; mutable.value = mutable.value.copy(loginId = null) }
    fun disconnectGarmin() = garminAction { athlete -> app.garmin.disconnect(athlete); mutable.value = mutable.value.copy(loginId = null, garminResult = null) }
    fun syncGarmin(days: Int, extended: Boolean) = garminAction { athlete ->
        val result = app.garmin.sync(athlete, days, extended, { force -> if (BuildConfig.FIXTURES) "fixture" else app.auth.token(force) }) { stage ->
            mutable.update { it.copy(garminProgress = stage) }
        }
        mutable.value = mutable.value.copy(garminResult = result)
    }
}
