import type { GarminSyncStatus, MeResponse, SyncPage, TrainingPlan, Workout } from './generated';
import { ApiClient } from './client';

const athleteID = '00000000-0000-4000-8000-000000000001';
const planID = '00000000-0000-4000-8000-000000000002';
const sessionID = '00000000-0000-4000-8000-000000000003';
const stepID = '00000000-0000-4000-8000-000000000004';
const secondStepID = '00000000-0000-4000-8000-000000000007';
const thirdStepID = '00000000-0000-4000-8000-000000000008';
const fourthStepID = '00000000-0000-4000-8000-000000000009';
const firstGroupID = '00000000-0000-4000-8000-000000000013';
const secondGroupID = '00000000-0000-4000-8000-000000000014';
const historyWorkoutID = '00000000-0000-4000-8000-000000000005';
const summaryWorkoutID = '00000000-0000-4000-8000-000000000015';

function today(): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'UTC', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
}

function fixturePlan(): TrainingPlan {
  return {
    plan_id: planID,
    athlete_id: athleteID,
    title: 'Fixture strength plan',
    description: 'Deterministic data for mobile UI tests',
    goal: { summary: 'Build consistent strength' },
    status: 'active',
    start_date: today(),
    end_date: today(),
    source: 'e2e',
    sync_seq: 1,
    sessions: [{
      session_id: sessionID,
      plan_id: planID,
      athlete_id: athleteID,
      date: today(),
      title: 'Fixture strength session',
      sport: 'strength',
      session_type: 'superset',
      priority: 'high',
      targets: { duration_minutes: 45 },
      notes: 'Use controlled reps and leave one clean repetition in reserve.',
      status: 'planned',
      steps: [
        {
          step_id: stepID,
          sequence_no: 1,
          step_type: 'exercise',
          exercise_key: 'goblet_squat',
          exercise_name: 'Goblet squat',
          prescription: { sets: [{ reps_min: 8, reps_max: 10, load_kg: 16, rir: 2, rest_seconds: 0 }, { reps_min: 8, reps_max: 10, load_kg: 16, rir: 2, rest_seconds: 0 }] },
          notes: 'Keep your chest tall.',
        },
        {
          step_id: secondStepID,
          sequence_no: 2,
          step_type: 'exercise',
          exercise_key: 'bench_press',
          exercise_name: 'Bench press',
          prescription: { sets: [{ reps_min: 8, reps_max: 10, load_kg: 40, rir: 2, rest_seconds: 120 }, { reps_min: 8, reps_max: 10, load_kg: 40, rir: 2, rest_seconds: 120 }] },
          notes: 'Pause briefly at the chest.',
        },
        {
          step_id: thirdStepID,
          sequence_no: 3,
          step_type: 'exercise',
          exercise_key: 'romanian_deadlift',
          exercise_name: 'Romanian deadlift',
          prescription: { sets: [{ reps_min: 8, reps_max: 10, load_kg: 36, rir: 2, rest_seconds: 0 }, { reps_min: 8, reps_max: 10, load_kg: 36, rir: 2, rest_seconds: 0 }] },
          notes: 'Control the hinge.',
        },
        {
          step_id: fourthStepID,
          sequence_no: 4,
          step_type: 'exercise',
          exercise_key: 'barbell_row',
          exercise_name: 'Barbell row',
          prescription: { sets: [{ reps_min: 8, reps_max: 10, load_kg: 30, rir: 2, rest_seconds: 120 }, { reps_min: 8, reps_max: 10, load_kg: 30, rir: 2, rest_seconds: 120 }] },
          notes: 'Keep your back neutral.',
        },
      ],
      groups: [
        { group_id: firstGroupID, label: 'Superset A', mode: 'rounds', rounds: 2, step_sequence_nos: [1, 2], notes: 'Move directly between the two exercises.' },
        { group_id: secondGroupID, label: 'Superset B', mode: 'rounds', rounds: 2, step_sequence_nos: [3, 4] },
      ],
    }],
  };
}

function fixtureHistory(workoutID: string, title: string): Workout {
  return {
    workout_id: workoutID,
    athlete_id: athleteID,
    date: today(),
    title,
    source: 'e2e',
    workout_type: 'strength',
    status: 'completed',
    started_at: new Date(Date.now() - 45 * 60 * 1000).toISOString(),
    completed_at: new Date().toISOString(),
    duration_minutes: 45,
    entries: [
      {
        entry_id: '00000000-0000-4000-8000-000000000006',
        planned_step_id: stepID,
        sequence_no: 1,
        entry_type: 'set',
        exercise_key: 'goblet_squat',
        exercise_name: 'Goblet squat',
        set_number: 1,
        reps: 10,
        load_kg: 16,
        rir: 2,
        muscle_groups: ['quads', 'glutes'],
      },
      {
        entry_id: '00000000-0000-4000-8000-000000000010',
        planned_step_id: secondStepID,
        sequence_no: 2,
        entry_type: 'set',
        exercise_key: 'bench_press',
        exercise_name: 'Bench press',
        set_number: 1,
        reps: 10,
        load_kg: 40,
        rir: 2,
        muscle_groups: ['chest', 'triceps', 'shoulders'],
      },
      {
        entry_id: '00000000-0000-4000-8000-000000000011',
        planned_step_id: thirdStepID,
        sequence_no: 3,
        entry_type: 'set',
        exercise_key: 'romanian_deadlift',
        exercise_name: 'Romanian deadlift',
        set_number: 1,
        reps: 10,
        load_kg: 36,
        rir: 2,
        muscle_groups: ['hamstrings', 'glutes'],
      },
      {
        entry_id: '00000000-0000-4000-8000-000000000012',
        planned_step_id: fourthStepID,
        sequence_no: 4,
        entry_type: 'set',
        exercise_key: 'barbell_row',
        exercise_name: 'Barbell row',
        set_number: 1,
        reps: 10,
        load_kg: 30,
        rir: 2,
        muscle_groups: ['back', 'biceps'],
      },
    ],
  };
}

export const e2eMe: MeResponse = {
  athlete: { athlete_id: athleteID, handle: 'e2e-athlete', timezone: 'UTC', units: 'metric' },
  accessible_athletes: [{ athlete_id: athleteID, handle: 'e2e-athlete', timezone: 'UTC', units: 'metric' }],
  capabilities: { plans: true, workouts: true, sync: true },
};

export class E2EApiClient extends ApiClient {
  private planState = fixturePlan();
  private readonly workoutState = new Map<string, Workout>([
    [historyWorkoutID, fixtureHistory(historyWorkoutID, 'Fixture completed workout with an intentionally long training session name for layout coverage')],
    [summaryWorkoutID, fixtureHistory(summaryWorkoutID, 'Fixture completed workout')],
  ]);

  constructor() {
    super(async () => 'e2e-fixture-token');
  }

  override async me(): Promise<MeResponse> { return e2eMe; }

  override async garminSyncStatus(): Promise<GarminSyncStatus> { return { last_ingested_at: null, latest_record_date: null, datasets: {} }; }

  override async activePlan(): Promise<TrainingPlan> { return this.planState; }

  override async plan(): Promise<TrainingPlan> { return this.planState; }

  override async plannedSessions(): Promise<TrainingPlan['sessions']> { return this.planState.sessions; }

  override async publishPlan(): Promise<TrainingPlan> { return this.planState; }

  override async skipSession(_athleteID: string, requestedSessionID: string): Promise<TrainingPlan['sessions'][number]> {
    const session = this.planState.sessions.find((item) => item.session_id === requestedSessionID);
    if (!session) throw new Error('Fixture session not found');
    const skipped = { ...session, status: 'skipped' as const };
    this.planState = { ...this.planState, sessions: this.planState.sessions.map((item) => item.session_id === requestedSessionID ? skipped : item) };
    return skipped;
  }

  override async workouts(): Promise<{ items: Workout[]; next_cursor?: string | null }> {
    return { items: [...this.workoutState.values()], next_cursor: null };
  }

  override async workout(_athleteID: string, workoutID: string): Promise<Workout> {
    const workout = this.workoutState.get(workoutID);
    if (!workout) throw new Error('Fixture workout not found');
    return workout;
  }

  override async saveWorkout(workout: Workout): Promise<Workout> {
    this.workoutState.set(workout.workout_id, workout);
    return workout;
  }

  override async completeWorkout(workout: Workout): Promise<Workout> {
    const comparison = workout.planned_session ? { steps: workout.planned_session.steps.map((step) => {
      const entries = workout.entries.filter((entry) => entry.planned_step_id === step.step_id && entry.entry_type === 'set').sort((a, b) => a.sequence_no - b.sequence_no);
      return { planned_step_id: step.step_id, exercise_key: step.exercise_key, exercise_name: step.exercise_name, planned_sets: step.prescription.sets?.length ?? 0, actual_sets: entries.length, planned_reps: step.prescription.sets?.map((set) => set.reps_min ?? 0), actual_reps: entries.flatMap((entry) => entry.reps == null ? [] : [entry.reps]), planned_load_kg: step.prescription.sets?.map((set) => set.load_kg ?? 0), actual_load_kg: entries.flatMap((entry) => entry.load_kg == null ? [] : [entry.load_kg]) };
    }) } : undefined;
    const completed = { ...workout, status: 'completed' as const, comparison };
    this.workoutState.set(completed.workout_id, completed);
    return completed;
  }

  override async deleteWorkout(workout: Workout): Promise<void> {
    this.workoutState.delete(workout.workout_id);
  }

  override async pull(): Promise<SyncPage> { return { changes: [], next_after: 1, has_more: false }; }
}
