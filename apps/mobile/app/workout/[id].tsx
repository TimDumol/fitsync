import { useEffect, useReducer } from 'react';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { Alert, Button, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useSQLiteContext } from 'expo-sqlite';
import { useAuth } from '../../src/auth/AuthProvider';
import { activeDrafts, deleteDraft, saveDraft } from '../../src/db/repository';
import { enqueueWorkout } from '../../src/sync/outbox';
import { idleWorkoutState, workoutReducer } from '../../src/workout/types';
import type { WorkoutEntry } from '../../src/api/generated';
import { v7 as uuidv7 } from 'uuid';

export default function ActiveWorkout(): JSX.Element {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const db = useSQLiteContext();
  const { selectedAthleteID, me } = useAuth();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? '';
  const [state, dispatch] = useReducer(workoutReducer, idleWorkoutState);
  useEffect(() => { void activeDrafts(db, athleteID).then((drafts) => { const workout = drafts.find((item) => item.workout_id === id); if (workout) { const elapsed = workout.started_at ? Math.max(0, Math.floor((Date.now() - Date.parse(workout.started_at)) / 1000)) : 0; dispatch({ type: 'restore', workout, elapsedSeconds: elapsed }); } }); }, [athleteID, db, id]);
  useEffect(() => { if (state.status !== 'active' || !state.workout || !state.dirty) return; const timer = setTimeout(() => { void saveDraft(db, state.workout!).then(() => enqueueWorkout(db, 'save', state.workout!)); }, 250); return () => clearTimeout(timer); }, [db, state]);
  useEffect(() => { if (state.status !== 'active') return; const timer = setInterval(() => dispatch({ type: 'tick' }), 1000); return () => clearInterval(timer); }, [state.status]);
  if (!state.workout) return <View style={styles.center}><Text>Loading workout…</Text></View>;
  const finish = async () => { dispatch({ type: 'finish' }); const completed = { ...state.workout!, status: 'completed' as const, completed_at: new Date().toISOString(), duration_minutes: Math.round(state.elapsedSeconds / 60) }; await saveDraft(db, completed); await enqueueWorkout(db, 'complete', completed); router.replace({ pathname: '/workout/[id]/summary', params: { id: completed.workout_id } }); };
  const discard = () => { Alert.alert('Discard workout?', 'This removes the local workout draft.', [{ text: 'Keep workout', style: 'cancel' }, { text: 'Discard', style: 'destructive', onPress: () => { void deleteDraft(db, state.workout!.workout_id).then(() => router.back()); } }]); };
  const addSet = () => { const last = state.workout!.entries.filter((entry) => entry.entry_type === 'set').at(-1); dispatch({ type: 'add_entry', entry: { entry_id: uuidv7(), sequence_no: state.workout!.entries.length + 1, entry_type: 'set', exercise_key: last?.exercise_key, exercise_name: last?.exercise_name, planned_step_id: last?.planned_step_id, set_number: (last?.set_number ?? 0) + 1, reps: last?.reps ?? null, load_kg: last?.load_kg ?? null, rir: last?.rir ?? null } }); };
  const addUnplanned = () => dispatch({ type: 'add_entry', entry: { entry_id: uuidv7(), sequence_no: state.workout!.entries.length + 1, entry_type: 'set', exercise_name: 'Unplanned exercise', reps: null, load_kg: null, rir: null } });
  const restLabel = state.restSeconds > 0 ? `Rest ${Math.floor(state.restSeconds / 60)}:${String(state.restSeconds % 60).padStart(2, '0')}` : 'Start 90-second rest';
  return <ScrollView contentContainerStyle={styles.content}><Text style={styles.title}>{state.workout.title}</Text><Text>Elapsed {Math.floor(state.elapsedSeconds / 60)}:{String(state.elapsedSeconds % 60).padStart(2, '0')}</Text>{state.restSeconds > 0 ? <Button title={restLabel} onPress={() => dispatch({ type: 'stop_rest' })} /> : <Button title={restLabel} onPress={() => dispatch({ type: 'start_rest', seconds: 90 })} />}{state.workout.entries.map((entry) => <EntryEditor key={entry.entry_id} entry={entry} onChange={(patch) => dispatch({ type: 'edit_entry', entryID: entry.entry_id, patch })} onRemove={() => dispatch({ type: 'remove_entry', entryID: entry.entry_id })} />)}<Button title="Add set" onPress={addSet} /><Button title="Add unplanned exercise" color="#667085" onPress={addUnplanned} /><TextInput style={styles.notes} placeholder="Workout notes" value={state.workout.notes ?? ''} onChangeText={(value) => dispatch({ type: 'set_notes', notes: value || null })} multiline/><Button title="Finish workout" onPress={() => void finish()} /><Button title="Discard" color="#D92D20" onPress={discard} /></ScrollView>;
}

function EntryEditor({ entry, onChange, onRemove }: { entry: WorkoutEntry; onChange: (patch: Partial<WorkoutEntry>) => void; onRemove: () => void }): JSX.Element {
  return <View style={styles.entry}><Text style={styles.entryTitle}>{entry.exercise_name ?? entry.entry_type} {entry.set_number ? `· Set ${entry.set_number}` : ''}</Text>{entry.entry_type === 'set' ? <View style={styles.inputs}><TextInput style={styles.input} keyboardType="decimal-pad" placeholder="Reps" value={entry.reps == null ? '' : String(entry.reps)} onChangeText={(value) => onChange({ reps: value === '' ? null : Number(value) })}/><TextInput style={styles.input} keyboardType="decimal-pad" placeholder="Load kg" value={entry.load_kg == null ? '' : String(entry.load_kg)} onChangeText={(value) => onChange({ load_kg: value === '' ? null : Number(value) })}/><TextInput style={styles.input} keyboardType="decimal-pad" placeholder="RIR" value={entry.rir == null ? '' : String(entry.rir)} onChangeText={(value) => onChange({ rir: value === '' ? null : Number(value) })}/></View> : <Text>{entry.duration_seconds ?? 0} seconds</Text>}<Button title="Remove" color="#667085" onPress={onRemove}/></View>;
}
const styles = StyleSheet.create({ content: { padding: 20, gap: 12 }, center: { flex: 1, alignItems: 'center', justifyContent: 'center' }, title: { fontSize: 28, fontWeight: '700' }, entry: { padding: 14, backgroundColor: '#F2F4F7', gap: 8 }, entryTitle: { fontWeight: '600' }, inputs: { flexDirection: 'row', gap: 8 }, input: { flex: 1, minWidth: 70, borderWidth: 1, borderColor: '#D0D5DD', borderRadius: 8, padding: 10, backgroundColor: 'white' }, notes: { minHeight: 80, borderWidth: 1, borderColor: '#D0D5DD', borderRadius: 8, padding: 10, textAlignVertical: 'top' } });
