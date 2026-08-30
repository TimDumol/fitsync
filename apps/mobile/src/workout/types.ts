import { v7 as uuidv7 } from 'uuid';
import { PlannedSession, Workout, WorkoutEntry } from '../api/generated';

export type WorkoutState =
  | { status: 'idle'; workout: null; elapsedSeconds: number; restSeconds: number; dirty: false }
  | { status: 'starting' | 'active' | 'finishing'; workout: Workout; elapsedSeconds: number; restSeconds: number; dirty: boolean }
  | { status: 'completed' | 'discarded'; workout: Workout; elapsedSeconds: number; restSeconds: number; dirty: false };

export type WorkoutAction =
  | { type: 'start'; workout: Workout }
  | { type: 'activate' }
  | { type: 'edit_entry'; entryID: string; patch: Partial<WorkoutEntry> }
  | { type: 'add_entry'; entry: WorkoutEntry }
  | { type: 'remove_entry'; entryID: string }
  | { type: 'set_notes'; notes: string | null }
  | { type: 'tick'; seconds?: number }
  | { type: 'start_rest'; seconds: number }
  | { type: 'stop_rest' }
  | { type: 'finish' }
  | { type: 'completed'; workout: Workout }
  | { type: 'discarded' }
  | { type: 'restore'; workout: Workout; elapsedSeconds?: number };

export const idleWorkoutState: WorkoutState = { status: 'idle', workout: null, elapsedSeconds: 0, restSeconds: 0, dirty: false };

export function workoutReducer(state: WorkoutState, action: WorkoutAction): WorkoutState {
  switch (action.type) {
    case 'start': return { status: 'starting', workout: action.workout, elapsedSeconds: 0, restSeconds: 0, dirty: true };
    case 'activate': return state.status === 'starting' ? { ...state, status: 'active' } : state;
    case 'edit_entry': return updateWorkout(state, (workout) => ({ ...workout, entries: workout.entries.map((entry) => entry.entry_id === action.entryID ? { ...entry, ...action.patch } : entry) }));
    case 'add_entry': return updateWorkout(state, (workout) => ({ ...workout, entries: [...workout.entries, action.entry] }));
    case 'remove_entry': return updateWorkout(state, (workout) => ({ ...workout, entries: workout.entries.filter((entry) => entry.entry_id !== action.entryID) }));
    case 'set_notes': return updateWorkout(state, (workout) => ({ ...workout, notes: action.notes }));
    case 'tick': return state.status === 'active' || state.status === 'finishing' ? { ...state, elapsedSeconds: state.elapsedSeconds + (action.seconds ?? 1), restSeconds: Math.max(0, state.restSeconds - (action.seconds ?? 1)) } : state;
    case 'start_rest': return state.status === 'active' ? { ...state, restSeconds: Math.max(0, Math.round(action.seconds)) } : state;
    case 'stop_rest': return state.status === 'active' ? { ...state, restSeconds: 0 } : state;
    case 'finish': return state.status === 'active' ? { ...state, status: 'finishing' } : state;
    case 'completed': return { status: 'completed', workout: action.workout, elapsedSeconds: state.elapsedSeconds, restSeconds: 0, dirty: false };
    case 'discarded': return state.workout ? { status: 'discarded', workout: { ...state.workout, status: 'discarded' }, elapsedSeconds: state.elapsedSeconds, restSeconds: 0, dirty: false } : state;
    case 'restore': return { status: 'active', workout: { ...action.workout, status: 'in_progress' }, elapsedSeconds: action.elapsedSeconds ?? 0, restSeconds: 0, dirty: false };
  }
}

function updateWorkout(state: WorkoutState, update: (workout: Workout) => Workout): WorkoutState {
  if (!state.workout || (state.status !== 'active' && state.status !== 'starting')) return state;
  return { ...state, workout: update(state.workout), dirty: true };
}

export function prescriptionToActual(session: PlannedSession, athleteID: string, date = session.date): Workout {
  let sequence = 1;
  const entries: WorkoutEntry[] = [];
  for (const step of [...session.steps].sort((a, b) => a.sequence_no - b.sequence_no)) {
    const sets = step.prescription.sets ?? [];
    if (sets.length === 0 && (step.step_type === 'interval' || step.step_type === 'rest')) {
      entries.push({ entry_id: uuidv7(), planned_step_id: step.step_id, sequence_no: sequence++, entry_type: step.step_type === 'rest' ? 'rest' : 'set', exercise_key: step.exercise_key, exercise_name: step.exercise_name, duration_seconds: step.prescription.duration_seconds ?? null, is_warmup: false });
      continue;
    }
    for (let index = 0; index < sets.length; index += 1) {
      const set = sets[index];
      if (!set) continue;
      entries.push({ entry_id: uuidv7(), planned_step_id: step.step_id, sequence_no: sequence++, entry_type: 'set', exercise_key: step.exercise_key, exercise_name: step.exercise_name, set_number: index + 1, reps: set.reps_min ?? null, load_kg: set.load_kg ?? null, rir: set.rir ?? null, duration_seconds: set.duration_seconds ?? null, is_warmup: step.step_type === 'warmup' });
    }
  }
  return { workout_id: uuidv7(), athlete_id: athleteID, planned_session_id: session.session_id, date, title: session.title, source: 'mobile', workout_type: session.sport === 'strength' ? 'strength' : session.sport, status: 'in_progress', started_at: new Date().toISOString(), entries };
}
