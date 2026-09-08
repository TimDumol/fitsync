import { v7 as uuidv7 } from 'uuid';
import { PlannedSession, PlannedStep, PlannedStepGroup, Workout, WorkoutEntry } from '../api/generated';

export type WorkoutState =
  | { status: 'idle'; workout: null; elapsedSeconds: number; restElapsedSeconds: number; restTargetSeconds: number; restActive: boolean; dirty: false }
  | { status: 'starting' | 'active' | 'finishing'; workout: Workout; elapsedSeconds: number; restElapsedSeconds: number; restTargetSeconds: number; restActive: boolean; dirty: boolean }
  | { status: 'completed' | 'discarded'; workout: Workout; elapsedSeconds: number; restElapsedSeconds: number; restTargetSeconds: number; restActive: boolean; dirty: false };

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

export const DEFAULT_REST_SECONDS = 120;

export interface ExerciseHistoryValues {
  reps: number | null;
  load_kg: number | null;
  rir: number | null;
}

export const idleWorkoutState: WorkoutState = { status: 'idle', workout: null, elapsedSeconds: 0, restElapsedSeconds: 0, restTargetSeconds: 0, restActive: false, dirty: false };

export function formatDuration(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds));
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`;
}

export function newUUID(): string {
  return uuidv7({ rng: randomBytes });
}

function randomBytes(): Uint8Array {
  const bytes = new Uint8Array(16);
  if (typeof globalThis.crypto?.getRandomValues === 'function') {
    return globalThis.crypto.getRandomValues(bytes);
  }
  // Hermes does not expose Web Crypto in every supported configuration. IDs
  // are identifiers rather than secrets, so retain the UUID shape and avoid
  // failing a workout start when that optional API is unavailable.
  for (let index = 0; index < bytes.length; index += 1) {
    bytes[index] = Math.floor(Math.random() * 256);
  }
  return bytes;
}

export function workoutReducer(state: WorkoutState, action: WorkoutAction): WorkoutState {
  switch (action.type) {
    case 'start': return { status: 'starting', workout: action.workout, elapsedSeconds: 0, restElapsedSeconds: 0, restTargetSeconds: 0, restActive: false, dirty: true };
    case 'activate': return state.status === 'starting' ? { ...state, status: 'active' } : state;
    case 'edit_entry': return updateWorkout(state, (workout) => ({ ...workout, entries: workout.entries.map((entry) => entry.entry_id === action.entryID ? { ...entry, ...action.patch } : entry) }));
    case 'add_entry': return updateWorkout(state, (workout) => ({ ...workout, entries: [...workout.entries, action.entry] }));
    case 'remove_entry': return updateWorkout(state, (workout) => ({ ...workout, entries: workout.entries.filter((entry) => entry.entry_id !== action.entryID) }));
    case 'set_notes': return updateWorkout(state, (workout) => ({ ...workout, notes: action.notes }));
    case 'tick': {
      if (state.status !== 'active' && state.status !== 'finishing') return state;
      const seconds = action.seconds ?? 1;
      return { ...state, elapsedSeconds: state.elapsedSeconds + seconds, restElapsedSeconds: state.restActive ? state.restElapsedSeconds + seconds : state.restElapsedSeconds };
    }
    case 'start_rest': {
      if (state.status !== 'active') return state;
      const seconds = Math.max(0, Math.round(action.seconds));
      return seconds === 0 ? { ...state, restElapsedSeconds: 0, restTargetSeconds: 0, restActive: false } : { ...state, restElapsedSeconds: 0, restTargetSeconds: seconds, restActive: true };
    }
    case 'stop_rest': return state.status === 'active' ? { ...state, restElapsedSeconds: 0, restTargetSeconds: 0, restActive: false } : state;
    case 'finish': return state.status === 'active' ? { ...state, status: 'finishing', restActive: false } : state;
    case 'completed': return { status: 'completed', workout: action.workout, elapsedSeconds: state.elapsedSeconds, restElapsedSeconds: 0, restTargetSeconds: 0, restActive: false, dirty: false };
    case 'discarded': return state.workout ? { status: 'discarded', workout: { ...state.workout, status: 'discarded' }, elapsedSeconds: state.elapsedSeconds, restElapsedSeconds: 0, restTargetSeconds: 0, restActive: false, dirty: false } : state;
    case 'restore': return { status: 'active', workout: { ...action.workout, status: 'in_progress' }, elapsedSeconds: action.elapsedSeconds ?? 0, restElapsedSeconds: 0, restTargetSeconds: 0, restActive: false, dirty: false };
  }
}

function updateWorkout(state: WorkoutState, update: (workout: Workout) => Workout): WorkoutState {
  if (!state.workout || (state.status !== 'active' && state.status !== 'starting')) return state;
  return { ...state, workout: update(state.workout), dirty: true };
}

export function prescriptionToActual(session: PlannedSession, athleteID: string, date = session.date, previousWorkouts: Workout[] = []): Workout {
  let sequence = 1;
  const entries: WorkoutEntry[] = [];
  const previousValues = firstValuesFromPreviousWorkouts(previousWorkouts, date);
  const currentValues = new Map<string, ExerciseHistoryValues>();
  for (const execution of expandSessionExecution(session)) {
    const step = execution.step;
    const setIndex = execution.setIndex;
    const sets = step.prescription.sets ?? [];
    if (sets.length === 0 && (step.step_type === 'interval' || step.step_type === 'rest')) {
      entries.push({ entry_id: newUUID(), planned_step_id: step.step_id, sequence_no: sequence++, entry_type: step.step_type === 'rest' ? 'rest' : 'set', exercise_key: step.exercise_key, exercise_name: step.exercise_name, duration_seconds: step.prescription.duration_seconds ?? null, is_warmup: false });
      continue;
    }
    const set = setIndex == null ? undefined : sets[setIndex];
    if (!set) continue;
    const keys = exerciseIdentities(step.exercise_key, step.exercise_name);
    const previous = keys.map((key) => currentValues.get(key) ?? previousValues.get(key)).find(Boolean);
    const values: ExerciseHistoryValues = {
      reps: previous?.reps ?? set.reps_min ?? null,
      load_kg: previous?.load_kg ?? set.load_kg ?? null,
      rir: previous?.rir ?? set.rir ?? null,
    };
    for (const key of keys) currentValues.set(key, values);
    entries.push({ entry_id: newUUID(), planned_step_id: step.step_id, sequence_no: sequence++, entry_type: 'set', exercise_key: step.exercise_key, exercise_name: step.exercise_name, set_number: (setIndex ?? 0) + 1, reps: values.reps, load_kg: values.load_kg, rir: values.rir, duration_seconds: set.duration_seconds ?? null, is_warmup: step.step_type === 'warmup' });
  }
  return { workout_id: newUUID(), athlete_id: athleteID, planned_session_id: session.session_id, date, title: session.title, source: 'mobile', workout_type: session.sport === 'strength' ? 'strength' : session.sport, status: 'in_progress', started_at: new Date().toISOString(), entries, planned_session: session };
}

export function exerciseIdentity(exerciseKey?: string | null, exerciseName?: string | null): string | null {
  return exerciseIdentities(exerciseKey, exerciseName)[0] ?? null;
}

function exerciseIdentities(exerciseKey?: string | null, exerciseName?: string | null): string[] {
  const identities: string[] = [];
  if (exerciseKey) identities.push(`key:${exerciseKey}`);
  if (exerciseName) identities.push(`name:${exerciseName.trim().toLocaleLowerCase().replace(/\s+/g, ' ')}`);
  return identities;
}

function exerciseEntries(workout: Workout): WorkoutEntry[] {
  return workout.entries.filter((entry) => entry.entry_type === 'set').sort((left, right) => left.sequence_no - right.sequence_no);
}

function firstValuesFromPreviousWorkouts(previousWorkouts: Workout[], date: string): Map<string, ExerciseHistoryValues> {
  const result = new Map<string, ExerciseHistoryValues>();
  const history = previousWorkouts
    .filter((workout) => workout.status === 'completed' && workout.date <= date)
    .sort((left, right) => right.date.localeCompare(left.date) || (right.completed_at ?? right.started_at ?? '').localeCompare(left.completed_at ?? left.started_at ?? ''));
  for (const workout of history) {
    for (const entry of exerciseEntries(workout)) {
      const keys = exerciseIdentities(entry.exercise_key, entry.exercise_name);
      const values = { reps: entry.reps ?? null, load_kg: entry.load_kg ?? null, rir: entry.rir ?? null };
      for (const key of keys) if (!result.has(key)) result.set(key, values);
    }
  }
  return result;
}

export interface PlannedExecution {
  step: PlannedStep;
  setIndex?: number;
  group?: PlannedStepGroup;
  round?: number;
}

/** Expand a planned session into the order in which its actual entries run. */
export function expandSessionExecution(session: PlannedSession): PlannedExecution[] {
  const steps = [...session.steps].sort((a, b) => a.sequence_no - b.sequence_no);
  const stepBySequence = new Map(steps.map((step) => [step.sequence_no, step]));
  const groupBySequence = new Map<number, PlannedStepGroup>();
  for (const group of session.groups ?? []) {
    for (const sequenceNo of group.step_sequence_nos) groupBySequence.set(sequenceNo, group);
  }
  const result: PlannedExecution[] = [];
  const emittedGroups = new Set<string>();
  for (const step of steps) {
    const group = groupBySequence.get(step.sequence_no);
    if (!group) {
      appendStepExecution(result, step);
      continue;
    }
    if (emittedGroups.has(group.group_id) || group.step_sequence_nos[0] !== step.sequence_no) continue;
    emittedGroups.add(group.group_id);
    for (let round = 0; round < group.rounds; round += 1) {
      for (const sequenceNo of group.step_sequence_nos) {
        const member = stepBySequence.get(sequenceNo);
        if (member) result.push({ step: member, setIndex: round, group, round: round + 1 });
      }
    }
  }
  return result;
}

function appendStepExecution(result: PlannedExecution[], step: PlannedStep): void {
  const sets = step.prescription.sets ?? [];
  if (sets.length === 0 && (step.step_type === 'interval' || step.step_type === 'rest')) {
    result.push({ step });
    return;
  }
  for (let setIndex = 0; setIndex < sets.length; setIndex += 1) result.push({ step, setIndex });
}

export function groupForEntry(workout: Workout, entry: WorkoutEntry): PlannedStepGroup | undefined {
  const step = entry.planned_step_id ? workout.planned_session?.steps.find((candidate) => candidate.step_id === entry.planned_step_id) : undefined;
  return step ? (workout.planned_session?.groups ?? []).find((group) => group.step_sequence_nos.includes(step.sequence_no) && (entry.set_number ?? 1) <= group.rounds) : undefined;
}

export function groupLabel(group: PlannedStepGroup): string {
  if (group.label) return group.label;
  return group.step_sequence_nos.length === 2 ? 'Superset' : 'Circuit';
}

export function prescribedRestSeconds(workout: Workout, entry?: WorkoutEntry): number {
  const step = entry?.planned_step_id ? workout.planned_session?.steps.find((item) => item.step_id === entry.planned_step_id) : undefined;
  const setIndex = Math.max(0, (entry?.set_number ?? 1) - 1);
  const prescribed = step?.prescription.sets?.[setIndex]?.rest_seconds;
  if (step && workout.planned_session?.groups?.some((group) => group.step_sequence_nos.includes(step.sequence_no)) && prescribed == null) return 0;
  return prescribed == null ? DEFAULT_REST_SECONDS : Math.max(0, Math.round(prescribed));
}
