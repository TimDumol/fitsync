@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package com.fitsync.nativeapp

import android.media.MediaPlayer
import android.os.Bundle
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.LocalActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.ui.Alignment
import androidx.compose.foundation.background
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.fitsync.nativeapp.domain.*
import kotlinx.coroutines.launch
import org.json.JSONObject

class MainActivity: ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent { FitSync() }
    }
}
@Composable
fun FitSync(model: AppModel = viewModel()) {
    val state by model.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var signInError by remember { mutableStateOf<String?>(null) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { model.authResult(it.data) }
    val lifecycle = LocalLifecycleOwner.current
    val activity = LocalActivity.current
    DisposableEffect(lifecycle) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) model.foreground(true)
            if (event == Lifecycle.Event.ON_PAUSE) model.foreground(false)
        }
        lifecycle.lifecycle.addObserver(observer)
        onDispose { lifecycle.lifecycle.removeObserver(observer) }
    }
    DisposableEffect(state.garminBusy) {
        if (state.garminBusy) activity?.window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        onDispose { activity?.window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON) }
    }
    BackHandler(state.signedIn && state.screen.name !in listOf("today", "plan", "history", "settings")) { model.back() }
    FitSyncTheme {
        Surface(Modifier.fillMaxSize().safeDrawingPadding().semantics { testTagsAsResourceId = true }, color = MaterialTheme.colorScheme.background) {
            if (!state.ready) Box(Modifier.padding(32.dp)) { CircularProgressIndicator() }
            else if (!state.signedIn) Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(28.dp).testTag("sign-in-screen"), verticalArrangement = Arrangement.Center) {
                Image(painterResource(R.drawable.fitsync_logo), contentDescription = null, modifier = Modifier.size(108.dp).padding(bottom = 16.dp))
                Text("FitSync", style = MaterialTheme.typography.headlineLarge)
                Text("TRAINING, IN FOCUS", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(top = 8.dp))
                Text("Sign in to review your training and record workouts.", modifier = Modifier.padding(vertical = 16.dp))
                (signInError ?: state.error)?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                Action("Sign in with Pocket ID", "sign-in-button", !state.busy) {
                    signInError = null
                    if (BuildConfig.FIXTURES) model.fixtureSignIn() else scope.launch {
                        try { launcher.launch(model.signInIntent()) } catch (error: Exception) { signInError = error.message ?: "Sign-in failed" }
                    }
                }
                if (state.error != null) Action("Retry loading account") { model.retryAccount() }
            } else Scaffold(
                bottomBar = {
                    if (state.screen.name in listOf("today", "plan", "history", "settings")) NavigationBar(containerColor = MaterialTheme.colorScheme.surface, tonalElevation = 0.dp) {
                        for (tab in listOf("today", "plan", "history", "settings")) NavigationBarItem(selected = state.screen.name == tab, onClick = { model.tab(tab) },
                            icon = { TabIcon(tab) }, label = { Text(tab.replaceFirstChar { it.uppercase() }) })
                    }
                },
            ) { insets ->
                Column(Modifier.padding(insets).fillMaxSize()) {
                    if (state.screen.name !in listOf("today", "plan", "history", "settings")) TextButton(onClick = { model.back() }) { Text("Back") }
                    state.error?.let { error ->
                        Column(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.errorContainer).padding(12.dp)) {
                            SelectionContainer { Text(error, color = MaterialTheme.colorScheme.error) }
                            Row { TextButton(onClick = { model.clearError() }) { Text("Dismiss") }; TextButton(onClick = { if (state.screen.name == "history") model.history() else model.refresh() }) { Text("Try again") } }
                        }
                    }
                    when (state.screen.name) {
                        "today" -> Today(state, model)
                        "plan" -> Plan(state, model)
                        "session" -> Session(state, model)
                        "workout" -> Workout(state, model)
                        "history" -> History(state, model)
                        "summary" -> Summary(state, model)
                        "settings" -> Settings(state, model)
                        "logs" -> Logs()
                    }
                }
            }
        }
    }
}
@Composable
private fun Page(tag: String, content: @Composable ColumnScope.() -> Unit) {
    Column(Modifier.fillMaxSize().testTag(tag).verticalScroll(rememberScrollState()).padding(20.dp), verticalArrangement = Arrangement.spacedBy(16.dp), content = content)
}
@Composable
private fun Header(title: String, subtitle: String? = null, compact: Boolean = false) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(title, style = if (compact) MaterialTheme.typography.titleLarge else MaterialTheme.typography.headlineLarge)
        subtitle?.let { Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}
@Composable
private fun Action(label: String, tag: String = "", enabled: Boolean = true, secondary: Boolean = false, onClick: () -> Unit) {
    val modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp).testTag(tag)
    if (secondary) OutlinedButton(onClick = onClick, enabled = enabled, modifier = modifier, shape = MaterialTheme.shapes.small) { Text(label) }
    else Button(onClick = onClick, enabled = enabled, modifier = modifier, shape = MaterialTheme.shapes.small) { Text(label) }
}
@Composable
private fun CardBlock(tag: String = "", content: @Composable ColumnScope.() -> Unit) {
    Card(Modifier.fillMaxWidth().testTag(tag), shape = MaterialTheme.shapes.large,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface)) {
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp), content = content)
    }
}
@Composable
private fun Detail(label: String, value: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        Text(label, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, textAlign = androidx.compose.ui.text.style.TextAlign.End)
    }
}
@Composable
private fun ItemLink(title: String, tag: String, onClick: () -> Unit) {
    TextButton(onClick = onClick, modifier = Modifier.fillMaxWidth().testTag(tag), contentPadding = PaddingValues(vertical = 4.dp)) {
        Text(title, modifier = Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
        Text("›", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(start = 12.dp))
    }
}
@Composable
private fun SyncStatus(state: AppState, model: AppModel) {
    val pending = state.queue.filter { it.athlete == state.athlete }
    Surface(color = MaterialTheme.colorScheme.surfaceContainer, shape = MaterialTheme.shapes.medium) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(7.dp).background(MaterialTheme.colorScheme.primary, androidx.compose.foundation.shape.CircleShape))
                Text(if (state.syncing) "Syncing…" else if (pending.isNotEmpty()) "${pending.size} pending changes" else "Up to date",
                    modifier = Modifier.weight(1f).padding(start = 8.dp), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                TextButton(onClick = { model.refresh() }, enabled = !state.syncing) { Text("Sync now") }
            }
            pending.firstOrNull { it.error != null }?.let { SelectionContainer { Text(it.error!!, color = MaterialTheme.colorScheme.error) } }
        }
    }
}
@Composable
private fun Today(state: AppState, model: AppModel) = Page("today-screen") {
    Header("Today", state.today())
    SyncStatus(state, model)
    state.drafts().firstOrNull { it.text("status") == "in_progress" }?.let { draft -> CardBlock("resume-workout-card") {
        Text(draft.getString("title"), style = MaterialTheme.typography.titleMedium); Action("Resume workout", "resume-workout-button") { model.go("workout", draft.getString("workout_id")) }
    } }
    val session = state.sessions().find { it.getString("date") == state.today() }
    if (session == null) CardBlock("today-rest-card") { Header("Rest day", "No session planned for today.", compact = true) }
    else CardBlock("today-session-card") {
        Header(session.getString("title"), session.getString("status"), compact = true)
        session.text("notes")?.let { Text(it) }
        Action(if (session.text("status") == "completed") "View result" else "Start workout", "start-workout-button") { model.go("session", session.getString("session_id")) }
    }
}
@Composable
private fun Plan(state: AppState, model: AppModel) = Page("plan-screen") {
    Header("Plan", "Your training schedule")
    SyncStatus(state, model)
    val plan = state.plan()
    if (plan == null) Text("No active training plan.") else {
        Header(plan.getString("title"), plan.text("description"), compact = true)
        state.sessions().sortedBy { it.getString("date") }.forEach { session -> CardBlock {
            Text("${session.getString("date")} · ${session.getString("status")}")
            ItemLink(session.getString("title"), "planned-session-row") { model.go("session", session.getString("session_id")) }
        } }
    }
    state.plans().filter { it.text("status") == "draft" }.forEach { draft -> CardBlock {
        Text(draft.getString("title"), style = MaterialTheme.typography.titleMedium); Text("Draft")
        Action("Publish plan", enabled = !state.busy) { model.publish(draft.getString("plan_id")) }
    } }
}
@Composable
private fun Session(state: AppState, model: AppModel) = Page("planned-session-screen") {
    val session = state.sessions().find { it.text("session_id") == state.screen.id }
    if (session == null) Text("Planned session not found.") else {
        Header(session.getString("title"), "${session.getString("date")} · ${session.getString("status")}")
        session.text("notes")?.let { Text(it) }
        val emitted = mutableSetOf<String>()
        session.objects("steps").sortedBy { it.getInt("sequence_no") }.forEach { step ->
            val group = session.objects("groups").find { group -> group.getJSONArray("step_sequence_nos").let { order -> (0 until order.length()).any { order.getInt(it) == step.getInt("sequence_no") } } }
            if (group != null && emitted.add(group.getString("group_id"))) {
                Text("${groupLabel(group)} · ${group.getInt("rounds")} rounds", style = MaterialTheme.typography.titleLarge)
                group.text("notes")?.let { Text(it) }
            }
            CardBlock {
                Text(step.text("exercise_name") ?: step.getString("step_type"), style = MaterialTheme.typography.titleMedium)
                step.text("notes")?.let { Text(it) }
                val prescription = step.getJSONObject("prescription")
                prescription.objects("sets").forEachIndexed { index, set -> Text("Set ${index + 1}: " + listOfNotNull(
                    set.text("reps_min")?.let { "$it${set.text("reps_max")?.let { max -> "–$max" } ?: ""} reps" },
                    set.text("load_kg")?.let { "$it kg" }, set.text("rir")?.let { "$it RIR" }, set.text("rest_seconds")?.let { "$it seconds rest" },
                    set.text("duration_seconds")?.let { "$it seconds" }, set.text("tempo")?.let { "Tempo $it" }, set.text("rpe")?.let { "$it RPE" },
                ).joinToString(" · ")) }
                listOf("rounds", "distance_meters", "duration_seconds", "pace_min_per_km_min", "pace_min_per_km_max", "heart_rate_zone").forEach { key -> prescription.text(key)?.let { Text("${key.replace('_', ' ')}: $it") } }
            }
        }
        if (session.text("status") == "planned") {
            Action("Start workout", "session-start-workout-button", !state.busy) { model.start(session) }
            Action("Skip session", "session-skip-button", !state.busy, secondary = true) { model.skip(session.getString("session_id")) }
        }
        session.text("completed_workout_id")?.let { id -> Action("View result", "session-view-result-button") { model.go("summary", id) } }
    }
}
@Composable
private fun Workout(state: AppState, model: AppModel) {
    val workout = state.workout
    if (workout == null) { Text("Loading workout…"); return }
    val focus = remember(workout.getString("workout_id")) { mutableMapOf<String, FocusRequester>() }
    var discard by remember { mutableStateOf(false) }
    var addingExercise by rememberSaveable { mutableStateOf(false) }
    var exerciseName by rememberSaveable { mutableStateOf("") }
    val context = LocalContext.current
    val chime = remember { MediaPlayer.create(context, R.raw.rest_chime) }
    DisposableEffect(chime) { onDispose { chime?.release() } }
    var chimed by rememberSaveable { mutableStateOf<Long?>(null) }
    LaunchedEffect(state.timer.restStartedAtMs, state.timer.restElapsedSeconds) {
        if (state.timer.restActive && state.timer.restElapsedSeconds >= state.timer.restTargetSeconds && chimed != state.timer.restStartedAtMs) {
            chime?.seekTo(0); chime?.start(); chimed = state.timer.restStartedAtMs
        }
    }
    Column(Modifier.fillMaxSize()) {
        Column(Modifier.weight(1f).testTag("active-workout-screen").verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Header(workout.getString("title"), "Elapsed ${duration(state.timer.elapsedSeconds)} · ${workout.objects("entries").count { it.text("entry_type") == "set" && it.optBoolean("completed") }}/${workout.objects("entries").count { it.text("entry_type") == "set" }} sets done")
            val sets = workout.objects("entries").filter { it.text("entry_type") == "set" }
            Surface(color = MaterialTheme.colorScheme.primaryContainer, shape = MaterialTheme.shapes.medium) {
                Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("${sets.count { it.optBoolean("completed") }} of ${sets.size} sets complete", style = MaterialTheme.typography.titleMedium)
                    LinearProgressIndicator(progress = { if (sets.isEmpty()) 0f else sets.count { it.optBoolean("completed") }.toFloat() / sets.size }, modifier = Modifier.fillMaxWidth())
                    Text("Log each set below. RIR = reps left in reserve.", style = MaterialTheme.typography.bodyMedium, modifier = Modifier.testTag("workout-entry-column-headers"))
                }
            }
            val entries = workout.objects("entries")
            entries.forEachIndexed { index, entry ->
                val id = entry.getString("entry_id")
                CardBlock("workout-entry-${entry.getInt("sequence_no")}") {
                    groupFor(workout, entry)?.let { group ->
                        val step = workout.getJSONObject("planned_session").objects("steps").find { it.text("step_id") == entry.text("planned_step_id") }
                        val order = group.getJSONArray("step_sequence_nos")
                        val member = (0 until order.length()).indexOfFirst { order.getInt(it) == step?.optInt("sequence_no") }
                        Text("${groupLabel(group)} · Round ${entry.optInt("set_number", 1)} · ${('A'.code + member).toChar()}${entry.optInt("set_number", 1)}", modifier = Modifier.testTag("workout-entry-${entry.getInt("sequence_no")}-group"))
                    }
                    Text("${entry.text("exercise_name") ?: entry.getString("entry_type")}${entry.text("set_number")?.let { " · Set $it" } ?: ""}", style = MaterialTheme.typography.titleMedium)
                    val step = workout.optJSONObject("planned_session")?.objects("steps")?.find { it.text("step_id") == entry.text("planned_step_id") }
                    step?.text("notes")?.let { Text(it) }
                    if (entry.text("entry_type") == "set") {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            for ((field, label) in listOf("reps" to "Reps", "load_kg" to "Load kg", "rir" to "RIR")) {
                                val key = "$id:$field"
                                val requester = focus.getOrPut(key) { FocusRequester() }
                                val nextField = when (field) { "reps" -> "$id:load_kg"; "load_kg" -> "$id:rir"; else -> entries.drop(index + 1).firstOrNull { it.text("entry_type") == "set" }?.text("entry_id")?.let { "$it:reps" } }
                                Numeric(entry, field, label, Modifier.weight(1f).focusRequester(requester), "workout-entry-${entry.getInt("sequence_no")}-${if (field == "load_kg") "load" else field}", onNext = { nextField?.let { focus[it]?.requestFocus() } }) { model.edit(id, field, it) }
                            }
                        }
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(checked = entry.optBoolean("completed"), onCheckedChange = { model.edit(id, "completed", it) }, modifier = Modifier.testTag("workout-entry-${entry.getInt("sequence_no")}-complete-button"))
                            Text(if (entry.optBoolean("completed")) "Completed" else "Complete set", style = MaterialTheme.typography.bodyMedium)
                        }
                        Action("Start ${duration((restSeconds(workout, entry).takeIf { it > 0 } ?: 120).toLong())} rest", "workout-entry-${entry.getInt("sequence_no")}-rest-button") { model.rest(entry) }
                    } else entry.text("duration_seconds")?.let { Text("$it seconds") }
                    var showNotes by rememberSaveable(id) { mutableStateOf(!entry.text("notes").isNullOrBlank()) }
                    if (showNotes) OutlinedTextField(entry.text("notes") ?: "", onValueChange = { model.edit(id, "notes", it.ifBlank { null }) }, label = { Text("Set notes (optional)") }, modifier = Modifier.fillMaxWidth().testTag("workout-entry-${entry.getInt("sequence_no")}-notes"))
                    else TextButton(onClick = { showNotes = true }) { Text("Add a note") }
                    TextButton(onClick = { model.remove(id) }, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) { Text("Remove") }
                }
            }
            OutlinedTextField(workout.text("notes") ?: "", onValueChange = { model.notes(it) }, label = { Text("Workout notes") }, modifier = Modifier.fillMaxWidth().testTag("workout-notes-input"))
            Action("Finish workout", "finish-workout-button", !state.busy) { model.finish() }
            TextButton(onClick = { discard = true }, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error), modifier = Modifier.testTag("discard-workout-button")) { Text("Discard workout") }
        }
        if (!state.timer.restActive) Surface(shadowElevation = 8.dp) {
            Column(Modifier.fillMaxWidth().imePadding().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                val lastSet = workout.objects("entries").lastOrNull { it.text("entry_type") == "set" }
                lastSet?.text("exercise_name")?.let { Text("Next set for $it", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Box(Modifier.weight(1f)) { Action("Add set", "add-set-button", enabled = lastSet != null && !state.busy, secondary = true) { model.add(false) } }
                    Box(Modifier.weight(1f)) { Action("Add exercise", "add-unplanned-button", enabled = !state.busy) { exerciseName = ""; addingExercise = true } }
                }
            }
        } else CardBlock("rest-timer") {
            val timer = state.timer
            Text(if (timer.restElapsedSeconds >= timer.restTargetSeconds) "REST TARGET REACHED" else "RESTING")
            Text("Rest ${duration(timer.restElapsedSeconds)} / ${duration(timer.restTargetSeconds)}", style = MaterialTheme.typography.titleLarge)
            val next = nextSet(workout, timer.restAnchorEntryID)
            val nextGroup = next?.let { groupFor(workout, it) }
            val anchor = workout.objects("entries").find { it.text("entry_id") == timer.restAnchorEntryID }
            val nextRound = nextGroup != null && anchor != null && groupFor(workout, anchor)?.text("group_id") == nextGroup.text("group_id") && next.optInt("set_number") != anchor.optInt("set_number")
            Text(if (next == null) "No more sets — finish when ready" else if (nextRound) "Next: ${groupLabel(nextGroup!!)} · Round ${next.optInt("set_number")}" else "Next: ${next.text("exercise_name") ?: "Set"} · Set ${next.optInt("set_number", 1)}", modifier = Modifier.testTag("rest-next-target"))
            Box(Modifier.testTag("rest-progress")) { LinearProgressIndicator(progress = { (timer.restElapsedSeconds.toFloat() / timer.restTargetSeconds.coerceAtLeast(1)).coerceIn(0f, 1f) }, modifier = Modifier.fillMaxWidth().testTag("rest-progress-fill")) }
            Text(if (timer.restElapsedSeconds > timer.restTargetSeconds) "Over by ${duration(timer.restElapsedSeconds - timer.restTargetSeconds)}" else if (timer.restElapsedSeconds == timer.restTargetSeconds) "Rest target reached" else "${duration(timer.restTargetSeconds - timer.restElapsedSeconds)} remaining", modifier = Modifier.testTag("rest-target-status"))
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Action(if (next == null) "End rest" else if (nextRound) "Start next round" else "Start next set", "start-next-action-button") { model.endRest(); next?.text("entry_id")?.let { focus["$it:reps"]?.requestFocus() } }
                TextButton(onClick = { model.endRest() }, modifier = Modifier.testTag("stop-rest-button")) { Text("End rest") }
            }
        }
        if (state.endedRest != null) Row(Modifier.fillMaxWidth().padding(8.dp)) { Text("Rest ended", modifier = Modifier.weight(1f)); TextButton(onClick = { model.undoRest() }, modifier = Modifier.testTag("undo-rest-button")) { Text("Undo") } }
    }
    if (addingExercise) AlertDialog(
        onDismissRequest = { addingExercise = false },
        title = { Text("Add an exercise") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("Add an exercise to this workout. You can log reps and weight after adding it.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(exerciseName, onValueChange = { exerciseName = it }, label = { Text("Exercise name") }, placeholder = { Text("e.g. Dumbbell shoulder press") }, singleLine = true, modifier = Modifier.fillMaxWidth().testTag("add-exercise-name"), keyboardOptions = KeyboardOptions(capitalization = androidx.compose.ui.text.input.KeyboardCapitalization.Words, imeAction = ImeAction.Done))
            }
        },
        confirmButton = { TextButton(enabled = exerciseName.isNotBlank() && !state.busy, onClick = { model.add(true, exerciseName.trim()); addingExercise = false }, modifier = Modifier.testTag("add-exercise-confirm")) { Text("Add exercise") } },
        dismissButton = { TextButton(onClick = { addingExercise = false }) { Text("Cancel") } },
    )
    if (discard) Confirm("Discard workout?", "Remove this workout and any queued saves?", "Discard", { discard = false }) { discard = false; model.discard() }
}
@Composable
private fun Numeric(entry: JSONObject, field: String, label: String, modifier: Modifier, tag: String, onNext: () -> Unit, change: (Double?) -> Unit) {
    val value = entry.text(field)?.let { if (it.endsWith(".0")) it.dropLast(2) else it } ?: ""
    var raw by remember(entry.getString("entry_id"), field) { mutableStateOf(TextFieldValue(value)) }
    LaunchedEffect(value) { if (raw.text.toDoubleOrNull() != value.toDoubleOrNull()) raw = TextFieldValue(value) }
    OutlinedTextField(raw, onValueChange = { proposed ->
        val text = proposed.text
        val normalized = text.replace(',', '.')
        if (normalized.isEmpty() || normalized.matches(Regex("\\d*\\.?\\d*"))) {
            raw = proposed
            if (normalized.isEmpty()) change(null) else normalized.toDoubleOrNull()?.takeIf { it.isFinite() && it >= 0 }?.let(change)
        }
    }, label = { Text(label) }, singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal, imeAction = ImeAction.Next), keyboardActions = KeyboardActions(onNext = { onNext() }), modifier = modifier.testTag(tag).onFocusChanged { if (it.isFocused) raw = raw.copy(selection = TextRange(0, raw.text.length)) })
}
@Composable
private fun MuscleOverview(workouts: List<JSONObject>) {
    val muscles = summarizeMuscles(workouts)
    CardBlock {
        Text("Muscle balance", style = MaterialTheme.typography.titleLarge)
        if (muscles.isEmpty()) Text("Complete a workout with tagged exercises to see your muscle balance.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        muscles.forEach { muscle ->
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(muscle.group.replaceFirstChar { it.uppercase() }, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
                    Text("${muscle.sets} ${if (muscle.sets == 1) "set" else "sets"} · ${muscle.workouts} ${if (muscle.workouts == 1) "workout" else "workouts"}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                LinearProgressIndicator(progress = { muscle.percentage }, modifier = Modifier.fillMaxWidth())
            }
        }
    }
}
@Composable
private fun History(state: AppState, model: AppModel) {
    var deleting by remember { mutableStateOf<JSONObject?>(null) }
    Page("history-screen") {
        Header("History", "Your recorded workouts")
        SyncStatus(state, model)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) { for (days in listOf(7L, 14L, 30L)) FilterChip(selected = state.historyDays == days, onClick = { model.history(days) }, modifier = Modifier.weight(1f).testTag("history-range-$days"), label = { Text("$days days") }) }
        MuscleOverview(state.history())
        if (state.historyLoading) CircularProgressIndicator(Modifier.testTag("history-loading"))
        if (state.history().isEmpty() && !state.historyLoading) Text("No workouts recorded yet.", modifier = Modifier.testTag("history-empty-state"))
        state.history().forEach { workout -> CardBlock {
            ItemLink(workout.getString("title"), "history-workout-row-${workout.getString("workout_id")}") { model.go("summary", workout.getString("workout_id")) }
            Text("${workout.getString("date")} · ${workout.getString("status")} · ${workout.objects("entries").size} entries · ${if (workout.text("planned_session_id") != null) "Planned" else "Unplanned"}")
            Text("Muscles: " + summarizeMuscles(listOf(workout)).joinToString(" · ") { it.group })
            TextButton(onClick = { deleting = workout }, modifier = Modifier.testTag("history-delete-workout-${workout.getString("workout_id")}")) { Text("Delete", color = MaterialTheme.colorScheme.error) }
        } }
        if (state.historyCursor != null) Action("Load more", "history-load-more", !state.historyLoading) { model.history(more = true) }
    }
    deleting?.let { workout -> Confirm("Delete workout?", "Remove “${workout.getString("title")}” from your history?", "Delete", { deleting = null }) { deleting = null; model.delete(workout) } }
}
@Composable
private fun Summary(state: AppState, model: AppModel) {
    val workout = state.workouts().find { it.text("workout_id") == state.screen.id } ?: state.workout?.takeIf { it.text("workout_id") == state.screen.id }
    var deleting by remember { mutableStateOf(false) }
    Page("workout-summary-screen") {
        if (workout == null) Text("Workout not found.") else {
            Header(workout.getString("title"), "${workout.getString("date")} · ${workout.getString("status")}")
            workout.text("duration_minutes")?.let { Text("Duration: $it minutes") }
            if (state.queue.any { it.id == workout.getString("workout_id") }) Text("Workout is pending sync.")
            Action("Delete workout", "delete-workout-button", !state.busy, secondary = true) { deleting = true }
            MuscleOverview(listOf(workout))
            workout.text("notes")?.let { Text(it) }
            workout.objects("entries").sortedBy { it.getInt("sequence_no") }.forEach { entry -> CardBlock("workout-summary-entry-${entry.getInt("sequence_no")}") {
                Text(entry.text("exercise_name") ?: entry.getString("entry_type"), style = MaterialTheme.typography.titleMedium)
                Text(listOfNotNull(entry.text("reps")?.let { "$it reps" }, entry.text("load_kg")?.let { "$it kg" }, entry.text("rir")?.let { "$it RIR" }, entry.text("duration_seconds")?.let { "$it seconds" }).joinToString(" · ").ifBlank { "No measurements recorded" })
                if (entry.optBoolean("completed")) Text("Completed")
                entry.text("notes")?.let { Text(it) }
                Text(muscleGroups(entry).joinToString(" · "))
            } }
        }
    }
    if (deleting && workout != null) Confirm("Delete workout?", "Remove “${workout.getString("title")}” from your history?", "Delete", { deleting = false }) { deleting = false; model.delete(workout) }
}
@Composable
private fun Settings(state: AppState, model: AppModel) = Page("settings-screen") {
    Header("Settings", "Your account and connected devices")
    SyncStatus(state, model)
    CardBlock {
        Header("Preferences", compact = true)
        Detail("Timezone", state.selectedProfile()?.text("timezone") ?: "Not set")
        Detail("Units", "Metric")
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        Detail("Version", BuildConfig.VERSION_NAME)
        Detail("Built", BuildConfig.BUILD_TIMESTAMP.substringBefore("T"))
    }
    Header("Athlete", "Choose the profile to sync", compact = true)
    state.account?.objects("accessible_athletes")?.forEach { athlete -> Action(athlete.getString("handle") + if (athlete.text("athlete_id") == state.athlete) " ✓" else "", enabled = !state.garminBusy && !state.busy, secondary = athlete.text("athlete_id") != state.athlete) { model.select(athlete.getString("athlete_id")) } }
    GarminPanel(state, model)
    Action("View app logs", "view-app-logs-button", secondary = true) { model.go("logs") }
    var logout by remember { mutableStateOf(false) }
    Action("Log out", "logout-button", !state.garminBusy && !state.busy, secondary = true) { if (state.queue.isNotEmpty()) logout = true else model.logout() }
    if (logout) Confirm("Log out?", "Logging out clears offline workouts and pending changes on this device.", "Log out", { logout = false }) { logout = false; model.logout() }
}
@Composable
private fun GarminPanel(state: AppState, model: AppModel) {
    var email by rememberSaveable(state.athlete) { mutableStateOf("") }
    var password by remember(state.athlete) { mutableStateOf("") }
    var code by remember(state.loginId) { mutableStateOf("") }
    var days by rememberSaveable { mutableStateOf(1) }
    var extended by rememberSaveable { mutableStateOf(true) }
    var disconnect by remember { mutableStateOf(false) }
    CardBlock("garmin-import") {
        Header("Garmin import", "Bring your health data into FitSync.", compact = true)
        if (state.loginId != null) {
            Text("Verify Garmin")
            OutlinedTextField(code, { code = it }, label = { Text("Verification code") }, modifier = Modifier.fillMaxWidth().testTag("garmin-mfa-code"), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
            Action("Verify Garmin", "verify-garmin-button", !state.garminBusy && code.isNotBlank()) { model.verifyGarmin(code); code = "" }
            TextButton(onClick = { model.cancelGarmin() }, enabled = !state.garminBusy) { Text("Cancel") }
        } else if (state.garminConnection != "connected") {
            if (state.garminConnection == "needs_reconnect") Text("Reconnect Garmin to continue.")
            OutlinedTextField(email, { email = it }, label = { Text("Garmin email") }, modifier = Modifier.fillMaxWidth().testTag("garmin-email"), singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email))
            OutlinedTextField(password, { password = it }, label = { Text("Garmin password") }, modifier = Modifier.fillMaxWidth().testTag("garmin-password"), singleLine = true, visualTransformation = PasswordVisualTransformation())
            Action("Connect Garmin", "connect-garmin-button", !state.garminBusy && email.isNotBlank() && password.isNotBlank()) { model.connectGarmin(email, password); password = "" }
        } else {
            Text("Connected · Garmin Connect", modifier = Modifier.testTag("garmin-connected"))
            Detail("Last upload", state.garminStatus?.text("last_ingested_at")?.substringBefore("T") ?: "Never")
            Detail("Latest record", state.garminStatus?.text("latest_record_date") ?: "None")
            Text("Days to import")
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) { for (option in listOf(1, 3, 7)) FilterChip(selected = days == option, onClick = { days = option }, enabled = !state.garminBusy, label = { Text("$option day${if (option == 1) "" else "s"}") }) }
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) { Text("Include extended data", modifier = Modifier.weight(1f)); Switch(extended, { extended = it }, enabled = !state.garminBusy, modifier = Modifier.testTag("garmin-extended")) }
            Action(if (state.garminBusy) "Syncing Garmin…" else "Sync Garmin now", "sync-garmin-button", !state.garminBusy) { model.syncGarmin(days, extended) }
            state.garminProgress?.let { Text(it, modifier = Modifier.testTag("garmin-progress")) }
            state.garminResult?.let { Text("Uploaded ${it.optInt("recordsSent")} records in ${it.optInt("batchesSent")} batch.", modifier = Modifier.testTag("garmin-result")) }
            Action("Disconnect Garmin", "disconnect-garmin-button", !state.garminBusy, secondary = true) { disconnect = true }
        }
        if (state.garminBusy) CircularProgressIndicator()
        state.garminError?.let { SelectionContainer { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.testTag("garmin-error")) }; Action("View detailed app logs", "view-garmin-logs-button") { model.go("logs") } }
    }
    if (disconnect) Confirm("Disconnect Garmin?", "Remove the locally stored Garmin session and pending data?", "Disconnect", { disconnect = false }) { disconnect = false; model.disconnectGarmin() }
}
@Composable
private fun Logs() {
    val live by LogBook.entries.collectAsStateWithLifecycle()
    var entries by remember { mutableStateOf(LogBook.entries.value) }
    LaunchedEffect(live) { entries = live }
    Page("logs-screen") {
        Header("Diagnostic report", "Version ${BuildConfig.VERSION_NAME} · Built ${BuildConfig.BUILD_TIMESTAMP}")
        Text("${entries.size} entries · credentials and tokens are redacted")
        Row { TextButton(onClick = { entries = LogBook.entries.value }) { Text("Refresh") }; TextButton(onClick = { LogBook.clear() }) { Text("Clear") } }
        SelectionContainer { Column(verticalArrangement = Arrangement.spacedBy(12.dp)) { entries.forEach { Text(it, style = MaterialTheme.typography.bodySmall) } } }
    }
}
@Composable
private fun Confirm(title: String, message: String, label: String, dismiss: () -> Unit, confirm: () -> Unit) {
    AlertDialog(onDismissRequest = dismiss, title = { Text(title) }, text = { Text(message) }, confirmButton = { TextButton(onClick = confirm) { Text(label) } }, dismissButton = { TextButton(onClick = dismiss) { Text("Cancel") } })
}
