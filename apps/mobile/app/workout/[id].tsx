import { useEffect, useReducer, useRef, useState } from 'react';
import { setAudioModeAsync, useAudioPlayer } from 'expo-audio';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { AppState, Alert, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useSQLiteContext } from 'expo-sqlite';
import { useAuth } from '../../src/auth/AuthProvider';
import { AppButton } from '../../src/components/AppButton';
import { AppIcon } from '../../src/components/AppIcon';
import { activeDrafts, activeTimerSeconds, deleteActiveTimer, deleteDraft, saveActiveTimer, saveDraft } from '../../src/db/repository';
import { enqueueWorkout } from '../../src/sync/outbox';
import { REST_TARGET_CHIME_URI } from '../../src/workout/chime';
import { DEFAULT_REST_SECONDS, formatDuration, groupForEntry, groupLabel, idleWorkoutState, newUUID, prescribedRestSeconds, workoutReducer } from '../../src/workout/types';
import type { PlannedStepGroup, WorkoutEntry } from '../../src/api/generated';
import { colors, radii, spacing } from '../../src/ui/theme';

export default function ActiveWorkout(): JSX.Element {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const db = useSQLiteContext();
  const { selectedAthleteID, me } = useAuth();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? '';
  const [state, dispatch] = useReducer(workoutReducer, idleWorkoutState);
  const [isFocused, setIsFocused] = useState(AppState.currentState === 'active');
  const latestState = useRef(state);
  const inputRefs = useRef(new Map<string, TextInput>());
  const insets = useSafeAreaInsets();
  const chimePlayer = useAudioPlayer(REST_TARGET_CHIME_URI);
  const chimePlayedForRest = useRef(false);
  useEffect(() => { latestState.current = state; }, [state]);
  useEffect(() => {
    const subscription = AppState.addEventListener('change', (nextState) => {
      const focused = nextState === 'active';
      setIsFocused(focused);
      if (!focused && latestState.current.status === 'active' && latestState.current.workout) {
        void saveActiveTimer(db, latestState.current.workout.workout_id, latestState.current.elapsedSeconds);
      }
    });
    return () => subscription.remove();
  }, [db]);
  useEffect(() => {
    void activeDrafts(db, athleteID).then(async (drafts) => {
      const workout = drafts.find((item) => item.workout_id === id);
      if (!workout) return;
      const savedElapsed = await activeTimerSeconds(db, workout.workout_id);
      const elapsed = savedElapsed ?? 0;
      dispatch({ type: 'restore', workout, elapsedSeconds: elapsed });
    });
  }, [athleteID, db, id]);
  useEffect(() => { if (state.status !== 'active' || !state.workout || !state.dirty) return; const timer = setTimeout(() => { void saveDraft(db, state.workout!).then(() => enqueueWorkout(db, 'save', state.workout!)); }, 250); return () => clearTimeout(timer); }, [db, state]);
  useEffect(() => { if (state.status !== 'active' || !isFocused) return; const timer = setInterval(() => dispatch({ type: 'tick' }), 1000); return () => clearInterval(timer); }, [isFocused, state.status]);
  useEffect(() => {
    if (state.status !== 'active' || !state.workout || !isFocused) return;
    const timer = setInterval(() => {
      const current = latestState.current;
      if (current.status === 'active' && current.workout) void saveActiveTimer(db, current.workout.workout_id, current.elapsedSeconds);
    }, 5000);
    return () => clearInterval(timer);
  }, [db, isFocused, state.status]);
  useEffect(() => { void setAudioModeAsync({ playsInSilentMode: true, interruptionMode: 'mixWithOthers', interruptionModeAndroid: 'duckOthers' }).catch(() => undefined); }, []);
  useEffect(() => {
    if (!state.restActive) {
      chimePlayedForRest.current = false;
      return;
    }
    if (chimePlayedForRest.current || state.restTargetSeconds <= 0 || state.restElapsedSeconds < state.restTargetSeconds) return;
    chimePlayedForRest.current = true;
    void chimePlayer.seekTo(0).then(() => chimePlayer.play()).catch(() => chimePlayer.play());
  }, [chimePlayer, state.restActive, state.restElapsedSeconds, state.restTargetSeconds]);
  if (!state.workout) return <View style={styles.center}><AppIcon name="loading" size={28} color={colors.primary} /><Text style={styles.muted}>Loading workout…</Text></View>;
  const finish = async () => { dispatch({ type: 'finish' }); const completed = { ...state.workout!, status: 'completed' as const, completed_at: new Date().toISOString(), duration_minutes: Math.round(state.elapsedSeconds / 60) }; await deleteActiveTimer(db, completed.workout_id); await saveDraft(db, completed); await enqueueWorkout(db, 'complete', completed); router.replace({ pathname: '/workout/[id]/summary', params: { id: completed.workout_id } }); };
  const discard = () => { Alert.alert('Discard workout?', 'This removes the local workout draft.', [{ text: 'Keep workout', style: 'cancel' }, { text: 'Discard', style: 'destructive', onPress: () => { void deleteActiveTimer(db, state.workout!.workout_id).then(() => deleteDraft(db, state.workout!.workout_id)).then(() => router.back()); } }]); };
  const nextSequenceNo = () => state.workout!.entries.reduce((highest, entry) => Math.max(highest, entry.sequence_no), 0) + 1;
  const addSet = () => { const last = state.workout!.entries.filter((entry) => entry.entry_type === 'set').at(-1); dispatch({ type: 'add_entry', entry: { entry_id: newUUID(), sequence_no: nextSequenceNo(), entry_type: 'set', exercise_key: last?.exercise_key, exercise_name: last?.exercise_name, planned_step_id: last?.planned_step_id, set_number: (last?.set_number ?? 0) + 1, reps: last?.reps ?? null, load_kg: last?.load_kg ?? null, rir: last?.rir ?? null } }); };
  const addUnplanned = () => dispatch({ type: 'add_entry', entry: { entry_id: newUUID(), sequence_no: nextSequenceNo(), entry_type: 'set', exercise_name: 'Unplanned exercise', reps: null, load_kg: null, rir: null } });
  const startRest = (entry?: WorkoutEntry) => { const seconds = prescribedRestSeconds(state.workout!, entry) || DEFAULT_REST_SECONDS; dispatch({ type: 'start_rest', seconds }); };
  const progress = state.restTargetSeconds > 0 ? Math.min(1, state.restElapsedSeconds / state.restTargetSeconds) : 1;
  return <View style={[styles.screen, { paddingTop: process.env.EXPO_OS === 'android' ? insets.top : 0 }]}>
    <ScrollView testID="active-workout-screen" contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
    <View style={styles.workoutHeader}><View style={styles.titleRow}><View style={styles.workoutIcon}><AppIcon name="dumbbell" size={22} color={colors.primary} /></View><View style={styles.titleCopy}><Text style={styles.title}>{state.workout.title}</Text><Text style={styles.muted}>Elapsed {formatDuration(state.elapsedSeconds)}</Text></View></View><View style={styles.livePill}><View style={styles.liveDot} /><Text style={styles.liveText}>LIVE</Text></View></View>
    <View testID="workout-entry-column-headers" style={styles.tableHeader}>
      <Text style={[styles.tableHeaderText, styles.exerciseColumn]}>Exercise</Text>
      <View style={styles.inputs}>
        <Text style={styles.tableHeaderText}>Reps</Text>
        <Text style={styles.tableHeaderText}>Load (kg)</Text>
        <Text style={styles.tableHeaderText}>RIR</Text>
      </View>
    </View>
    {state.workout.entries.map((entry, index, entries) => {
      const group = groupForEntry(state.workout!, entry);
      const plannedStep = entry.planned_step_id ? state.workout!.planned_session?.steps.find((step) => step.step_id === entry.planned_step_id) : undefined;
      const memberIndex = group && plannedStep ? group.step_sequence_nos.indexOf(plannedStep.sequence_no) : -1;
      const groupMemberLabel = group && memberIndex >= 0 ? `${String.fromCharCode(65 + memberIndex)}${entry.set_number ?? 1}` : undefined;
      const next = entries[index + 1];
      const nextGroup = next ? groupForEntry(state.workout!, next) : undefined;
      const roundComplete = Boolean(group && nextGroup?.group_id === group.group_id && next?.set_number !== entry.set_number);
      const nextSet = entries.slice(index + 1).find((candidate) => candidate.entry_type === 'set');
      return <EntryEditor key={entry.entry_id} entry={entry} group={group} groupMemberLabel={groupMemberLabel} roundComplete={roundComplete} coachingNote={plannedStep?.notes} restTargetSeconds={prescribedRestSeconds(state.workout!, entry)} nextSetEntryID={nextSet?.entry_id} inputRefs={inputRefs} onStartRest={() => startRest(entry)} onChange={(patch) => dispatch({ type: 'edit_entry', entryID: entry.entry_id, patch })} onRemove={() => dispatch({ type: 'remove_entry', entryID: entry.entry_id })} />;
    })}
    <Text style={styles.notesLabel}>Workout notes</Text>
    <TextInput testID="workout-notes-input" style={styles.notes} accessibilityLabel="Workout notes" placeholder="Add notes" placeholderTextColor={colors.subtle} value={state.workout.notes ?? ''} onChangeText={(value) => dispatch({ type: 'set_notes', notes: value || null })} multiline />
    <AppButton testID="finish-workout-button" label="Finish workout" icon="check" onPress={() => void finish()} />
    <AppButton testID="discard-workout-button" label="Discard workout" icon="trash-can-outline" variant="danger" onPress={discard} />
    </ScrollView>
    {!state.restActive ? <View style={[styles.stickyActions, { bottom: insets.bottom + spacing.sm }]}><AppButton testID="add-set-button" label="Add set" icon="plus" fullWidth={false} onPress={addSet} /><AppButton testID="add-unplanned-button" label="Add unplanned exercise" icon="plus-circle-outline" variant="quiet" fullWidth={false} onPress={addUnplanned} /></View> : null}
    {state.restActive ? <View testID="rest-timer" style={[styles.floatingRestTimer, { bottom: insets.bottom + spacing.md }]}>
      <View style={styles.restHeader}><View><Text style={styles.restLabel}>Recovery timer</Text><Text style={styles.restTitle}>{`Rest ${formatDuration(state.restElapsedSeconds)} / ${formatDuration(state.restTargetSeconds)}`}</Text></View><View style={styles.timerIcon}><AppIcon name="timer-outline" size={22} color={colors.primary} /></View></View>
      <View testID="rest-progress" accessibilityRole="progressbar" accessibilityValue={{ min: 0, max: state.restTargetSeconds, now: Math.min(state.restElapsedSeconds, state.restTargetSeconds) }} style={styles.progressTrack}>
        <View testID="rest-progress-fill" style={[styles.progressFill, { width: `${progress * 100}%` }]} />
      </View>
      <Text testID="rest-target-status" style={styles.restMeta}>{state.restElapsedSeconds >= state.restTargetSeconds ? state.restElapsedSeconds > state.restTargetSeconds ? `Over by ${formatDuration(state.restElapsedSeconds - state.restTargetSeconds)}` : 'Rest target reached' : `${formatDuration(state.restTargetSeconds - state.restElapsedSeconds)} remaining`}</Text>
      <AppButton testID="stop-rest-button" label="Stop rest" icon="stop" variant="secondary" onPress={() => dispatch({ type: 'stop_rest' })} />
    </View> : null}
  </View>;
}

function EntryEditor({ entry, group, groupMemberLabel, roundComplete, coachingNote, restTargetSeconds, nextSetEntryID, inputRefs, onStartRest, onChange, onRemove }: { entry: WorkoutEntry; group?: PlannedStepGroup; groupMemberLabel?: string; roundComplete: boolean; coachingNote?: string | null; restTargetSeconds: number; nextSetEntryID?: string; inputRefs: { current: Map<string, TextInput> }; onStartRest: () => void; onChange: (patch: Partial<WorkoutEntry>) => void; onRemove: () => void }): JSX.Element {
  const repsRef = useRef<TextInput>(null);
  const loadRef = useRef<TextInput>(null);
  const rirRef = useRef<TextInput>(null);
  const memberLabel = group && groupMemberLabel ? `${groupLabel(group)} · Round ${entry.set_number ?? 1} of ${group.rounds} · ${groupMemberLabel}` : undefined;
  const restButtonSeconds = restTargetSeconds > 0 ? restTargetSeconds : DEFAULT_REST_SECONDS;
  const register = (field: string, input: TextInput | null) => { const key = `${entry.entry_id}:${field}`; if (input) inputRefs.current.set(key, input); else inputRefs.current.delete(key); };
  const focusNextSet = () => { if (nextSetEntryID) inputRefs.current.get(`${nextSetEntryID}:reps`)?.focus(); else rirRef.current?.blur(); };
  const laterality = entry.laterality ?? 'unknown';
  const loadBasis = entry.load_basis ?? 'unknown';
  const updateLoad = (raw: string) => {
    const value = raw === '' ? null : Number(raw);
    const patch: Partial<WorkoutEntry> = { load_kg: value };
    if (loadBasis === 'per_limb') patch.load_per_limb_kg = value;
    if (loadBasis === 'total_external') patch.total_external_load_kg = value;
    if (loadBasis === 'bodyweight_added') patch.added_load_kg = value;
    onChange(patch);
  };
  const lateralityOptions: NonNullable<WorkoutEntry['laterality']>[] = ['bilateral', 'unilateral', 'left', 'right', 'alternating', 'unknown'];
  const loadBasisOptions: NonNullable<WorkoutEntry['load_basis']>[] = ['total_external', 'per_limb', 'bodyweight_added', 'assistance', 'unknown'];
  return <View testID={`workout-entry-${entry.sequence_no}`} style={styles.entry}>
    {memberLabel ? <Text testID={`workout-entry-${entry.sequence_no}-group`} style={styles.groupMeta}>{memberLabel}</Text> : null}
    <View style={styles.entryRow}>
      <Text style={[styles.entryTitle, styles.exerciseColumn]}>{entry.exercise_name ?? entry.entry_type} {entry.set_number ? `· Set ${entry.set_number}` : ''}</Text>
      {entry.entry_type === 'set' ? <View style={styles.inputs}>
        <TextInput ref={(input) => { repsRef.current = input; register('reps', input); }} testID={`workout-entry-${entry.sequence_no}-reps`} style={styles.input} accessibilityLabel="Reps" keyboardType="decimal-pad" returnKeyType="next" blurOnSubmit={false} selectTextOnFocus placeholder="Reps" placeholderTextColor={colors.subtle} value={entry.reps == null ? '' : String(entry.reps)} onChangeText={(value) => onChange({ reps: value === '' ? null : Number(value) })} onSubmitEditing={() => loadRef.current?.focus()} />
        <TextInput ref={(input) => { loadRef.current = input; register('load', input); }} testID={`workout-entry-${entry.sequence_no}-load`} style={styles.input} accessibilityLabel="Load (kg)" keyboardType="decimal-pad" returnKeyType="next" blurOnSubmit={false} selectTextOnFocus placeholder="Load kg" placeholderTextColor={colors.subtle} value={entry.load_kg == null ? '' : String(entry.load_kg)} onChangeText={updateLoad} onSubmitEditing={() => rirRef.current?.focus()} />
        <TextInput ref={(input) => { rirRef.current = input; register('rir', input); }} testID={`workout-entry-${entry.sequence_no}-rir`} style={styles.input} accessibilityLabel="RIR" keyboardType="decimal-pad" returnKeyType={nextSetEntryID ? "next" : "done"} blurOnSubmit={false} selectTextOnFocus placeholder="RIR" placeholderTextColor={colors.subtle} value={entry.rir == null ? '' : String(entry.rir)} onChangeText={(value) => onChange({ rir: value === '' ? null : Number(value) })} onSubmitEditing={focusNextSet} />
      </View> : <Text>{entry.duration_seconds ?? 0} seconds</Text>}
    </View>
    {entry.entry_type === 'set' ? <>
      <Text style={styles.semanticLabel}>Repetition and load basis</Text>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chipRow}>
        {lateralityOptions.map((option) => <Pressable key={option} accessibilityRole="button" accessibilityState={{ selected: laterality === option }} style={[styles.semanticChip, laterality === option && styles.semanticChipSelected]} onPress={() => onChange({ laterality: option, reps_per_side: option === 'unilateral' || option === 'alternating' ? entry.reps_per_side : false })}><Text style={[styles.semanticChipText, laterality === option && styles.semanticChipTextSelected]}>{option.replace('_', ' ')}</Text></Pressable>)}
      </ScrollView>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chipRow}>
        {loadBasisOptions.map((option) => <Pressable key={option} accessibilityRole="button" accessibilityState={{ selected: loadBasis === option }} style={[styles.semanticChip, loadBasis === option && styles.semanticChipSelected]} onPress={() => onChange({ load_basis: option })}><Text style={[styles.semanticChipText, loadBasis === option && styles.semanticChipTextSelected]}>{option.replace('_', ' ')}</Text></Pressable>)}
        {(laterality === 'unilateral' || laterality === 'alternating') ? <Pressable accessibilityRole="switch" accessibilityState={{ checked: Boolean(entry.reps_per_side) }} style={[styles.semanticChip, entry.reps_per_side && styles.semanticChipSelected]} onPress={() => onChange({ reps_per_side: !entry.reps_per_side })}><Text style={[styles.semanticChipText, entry.reps_per_side && styles.semanticChipTextSelected]}>per side</Text></Pressable> : null}
      </ScrollView>
      <Text style={styles.restPrescription}>{restTargetSeconds > 0 ? `Rest ${formatDuration(restTargetSeconds)} prescribed` : `Optional rest · ${formatDuration(restButtonSeconds)}`}</Text>
      <AppButton testID={`workout-entry-${entry.sequence_no}-rest-button`} label={`Start ${formatDuration(restButtonSeconds)} rest`} icon="timer-outline" variant="quiet" onPress={onStartRest} />
      {coachingNote ? <Text style={styles.coachingNote}>{coachingNote}</Text> : null}
    </> : null}
    {roundComplete ? <Text style={styles.roundBoundary}>Round complete · next round</Text> : null}
    <AppButton label="Remove" icon="trash-can-outline" variant="quiet" onPress={onRemove} />
  </View>;
}
const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  content: { padding: spacing.xl, paddingBottom: spacing.xxxl + 148, gap: spacing.lg, backgroundColor: colors.background },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.md, backgroundColor: colors.background },
  workoutHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: spacing.md, minWidth: 0 },
  titleRow: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  workoutIcon: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center', borderRadius: radii.sm, backgroundColor: colors.primarySoft },
  titleCopy: { flex: 1, gap: spacing.xs },
  title: { flexShrink: 1, color: colors.ink, fontSize: 25, fontWeight: '800', letterSpacing: -0.4 },
  muted: { color: colors.muted, fontSize: 13 },
  livePill: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs, paddingVertical: 6, paddingHorizontal: 9, borderRadius: radii.pill, backgroundColor: colors.dangerSoft },
  liveDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: colors.danger },
  liveText: { color: colors.danger, fontSize: 10, fontWeight: '800', letterSpacing: 0.8 },
  tableHeader: { flexDirection: 'row', alignItems: 'center', gap: 8, minWidth: 0, paddingHorizontal: 14 },
  tableHeaderText: { flex: 1, color: colors.muted, fontSize: 12, fontWeight: '800', textAlign: 'center' },
  exerciseColumn: { flex: 1.4, minWidth: 0 },
  entry: { minWidth: 0, padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, gap: spacing.md },
  entryRow: { flexDirection: 'row', alignItems: 'center', gap: 8, minWidth: 0 },
  entryTitle: { flexShrink: 1, color: colors.ink, fontWeight: '800' },
  inputs: { flex: 2.6, minWidth: 0, flexDirection: 'row', gap: 8 },
  input: { flex: 1, minWidth: 0, minHeight: 44, borderWidth: 1, borderColor: colors.line, borderRadius: radii.sm, paddingHorizontal: spacing.sm, backgroundColor: colors.background, color: colors.ink, textAlign: 'center' },
  floatingRestTimer: { position: 'absolute', left: spacing.lg, right: spacing.lg, padding: spacing.lg, borderRadius: radii.lg, backgroundColor: colors.primarySoft, borderWidth: 1, borderColor: '#BFE9D7', gap: spacing.md, zIndex: 10, boxShadow: '0 4px 16px rgba(16, 24, 40, 0.18)' },
  restHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  restLabel: { color: colors.primaryDark, fontSize: 12, fontWeight: '800', textTransform: 'uppercase', letterSpacing: 0.7 },
  restTitle: { color: colors.ink, fontSize: 16, fontWeight: '800', marginTop: 2 },
  timerIcon: { width: 40, height: 40, alignItems: 'center', justifyContent: 'center', borderRadius: radii.sm, backgroundColor: colors.white },
  progressTrack: { height: 10, overflow: 'hidden', borderRadius: 5, backgroundColor: '#BFE9D7' },
  progressFill: { height: '100%', borderRadius: 5, backgroundColor: colors.primary },
  restMeta: { color: colors.primaryDark },
  restPrescription: { color: colors.muted, fontSize: 13 },
  groupMeta: { color: colors.primaryDark, fontSize: 12, fontWeight: '800' },
  coachingNote: { color: colors.warmInk, fontSize: 13, lineHeight: 19 },
  roundBoundary: { color: colors.primaryDark, fontSize: 12, fontWeight: '800', paddingTop: spacing.xs },
  stickyActions: { position: 'absolute', left: spacing.lg, right: spacing.lg, flexDirection: 'row', gap: spacing.sm, minWidth: 0, padding: spacing.sm, borderRadius: radii.lg, backgroundColor: colors.background, borderWidth: 1, borderColor: colors.line, zIndex: 5 },
  semanticLabel: { color: colors.muted, fontSize: 12, fontWeight: '800' },
  chipRow: { flexDirection: 'row', gap: spacing.xs, paddingRight: spacing.sm },
  semanticChip: { borderWidth: 1, borderColor: colors.line, borderRadius: radii.pill, paddingVertical: 6, paddingHorizontal: 9, backgroundColor: colors.background },
  semanticChipSelected: { borderColor: colors.primary, backgroundColor: colors.primarySoft },
  semanticChipText: { color: colors.muted, fontSize: 11, fontWeight: '700' },
  semanticChipTextSelected: { color: colors.primaryDark },
  notesLabel: { color: colors.ink, fontSize: 13, fontWeight: '800' },
  notes: { minHeight: 88, borderWidth: 1, borderColor: colors.line, borderRadius: radii.md, padding: spacing.md, backgroundColor: colors.surface, color: colors.ink, textAlignVertical: 'top' },
});
