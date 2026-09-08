import { describe, expect, it } from 'vitest';
import { E2EApiClient, e2eMe } from './e2e';
import { idleWorkoutState, prescribedRestSeconds, prescriptionToActual, workoutReducer } from '../workout/types';

describe('mobile fixture core flows', () => {
  it('loads the signed-in athlete, reviews the plan, and skips a session', async () => {
    const api = new E2EApiClient();
    const me = await api.me();
    expect(me).toEqual(e2eMe);

    const plan = await api.activePlan();
    const session = plan.sessions[0]!;
    expect(plan.title).toBe('Fixture strength plan');
    expect(session.title).toBe('Fixture strength session');
    expect(session.session_type).toBe('superset');
    expect(session.steps).toHaveLength(4);
    expect(session.steps.map((step) => step.exercise_name)).toEqual(['Goblet squat', 'Bench press', 'Romanian deadlift', 'Barbell row']);
    expect(session.groups?.map((group) => [group.label, group.rounds, group.step_sequence_nos])).toEqual([['Superset A', 2, [1, 2]], ['Superset B', 2, [3, 4]]]);
    expect(session.steps.every((step) => !step.notes?.includes('Superset'))).toBe(true);

    const skipped = await api.skipSession(me.athlete.athlete_id, session.session_id);
    expect(skipped.status).toBe('skipped');
    expect((await api.plannedSessions())[0]!.status).toBe('skipped');
  });

  it('edits, rests, saves, and completes a planned workout', async () => {
    const api = new E2EApiClient();
    const plan = await api.activePlan();
    const session = plan.sessions[0]!;
    const athleteID = e2eMe.athlete.athlete_id;
    const workout = prescriptionToActual(session, athleteID);
    expect(workout.entries.map((entry) => entry.exercise_name)).toEqual(['Goblet squat', 'Bench press', 'Goblet squat', 'Bench press', 'Romanian deadlift', 'Barbell row', 'Romanian deadlift', 'Barbell row']);
    expect(prescribedRestSeconds(workout, workout.entries[0])).toBe(0);
    expect(prescribedRestSeconds(workout, workout.entries[1])).toBe(120);
    expect(prescribedRestSeconds(workout, workout.entries[7])).toBe(120);

    let state = workoutReducer(idleWorkoutState, { type: 'start', workout });
    state = workoutReducer(state, { type: 'activate' });
    state = workoutReducer(state, {
      type: 'edit_entry',
      entryID: workout.entries[0]!.entry_id,
      patch: { reps: 10, load_kg: 18, rir: 1 },
    });
    state = workoutReducer(state, { type: 'start_rest', seconds: 120 });
    state = workoutReducer(state, { type: 'tick', seconds: 15 });
    state = workoutReducer(state, { type: 'stop_rest' });
    state = workoutReducer(state, { type: 'set_notes', notes: 'Fixture workout note' });
    state = workoutReducer(state, { type: 'finish' });
    const finishedWorkout = state.workout;
    if (!finishedWorkout) throw new Error('Workout state unexpectedly became empty');

    expect(state.status).toBe('finishing');
    expect(finishedWorkout.entries[0]).toMatchObject({ reps: 10, load_kg: 18, rir: 1 });
    expect(finishedWorkout.notes).toBe('Fixture workout note');
    expect(state.elapsedSeconds).toBe(15);

    const saved = await api.saveWorkout(finishedWorkout);
    const completed = await api.completeWorkout({ ...saved, status: 'completed' });
    expect(completed.status).toBe('completed');
    expect(completed.comparison?.steps.map((step) => step.actual_sets)).toEqual([2, 2, 2, 2]);
    expect((await api.workout(athleteID, completed.workout_id)).status).toBe('completed');
  });

  it('lists the completed workout for history and summary', async () => {
    const api = new E2EApiClient();
    const workouts = await api.workouts();
    expect(workouts.items).toHaveLength(2);
    const workout = workouts.items.find((item) => item.title === 'Fixture completed workout');
    expect(workout).toMatchObject({ title: 'Fixture completed workout', status: 'completed' });
    if (!workout) throw new Error('Fixture workout unexpectedly missing');

    const summary = await api.workout(e2eMe.athlete.athlete_id, workout.workout_id);
    expect(summary.entries[0]).toMatchObject({ exercise_name: 'Goblet squat', reps: 10, load_kg: 16 });
  });
});
