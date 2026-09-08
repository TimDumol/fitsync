import { describe, expect, it } from 'vitest';
import type { PlannedSession } from '../api/generated';
import { DEFAULT_REST_SECONDS, expandSessionExecution, idleWorkoutState, prescribedRestSeconds, prescriptionToActual, workoutReducer } from './types';
import { plannedSessionSchema } from '../api/schemas';

const session: PlannedSession = { session_id: '11111111-1111-4111-8111-111111111111', plan_id: '22222222-2222-4222-8222-222222222222', athlete_id: '33333333-3333-4333-8333-333333333333', date: '2026-08-29', title: 'Strength', sport: 'strength', session_type: 'full_body', priority: 'normal', targets: {}, status: 'planned', steps: [{ step_id: '44444444-4444-4444-8444-444444444444', sequence_no: 1, step_type: 'exercise', exercise_name: 'Squat', prescription: { sets: [{ reps_min: 5, load_kg: 80 }] } }] };

describe('workout state machine', () => {
  it('copies prescribed sets and uses a two-minute default rest timer', () => {
    const workout = prescriptionToActual(session, session.athlete_id);
    expect(prescribedRestSeconds(workout, workout.entries[0])).toBe(DEFAULT_REST_SECONDS);
    let state = workoutReducer(idleWorkoutState, { type: 'start', workout });
    expect(state.status).toBe('starting');
    state = workoutReducer(state, { type: 'activate' });
    state = workoutReducer(state, { type: 'edit_entry', entryID: workout.entries[0]!.entry_id, patch: { reps: 6 } });
    state = workoutReducer(state, { type: 'start_rest', seconds: DEFAULT_REST_SECONDS });
    state = workoutReducer(state, { type: 'tick', seconds: DEFAULT_REST_SECONDS + 5 });
    expect(state.restElapsedSeconds).toBe(DEFAULT_REST_SECONDS + 5);
    expect(state.restTargetSeconds).toBe(DEFAULT_REST_SECONDS);
    expect(state.restActive).toBe(true);
    state = workoutReducer(state, { type: 'stop_rest' });
    expect(state.workout?.entries[0]!.reps).toBe(6);
    state = workoutReducer(state, { type: 'finish' });
    expect(state.status).toBe('finishing');
  });

  it('expands a round group in declared member order and retains per-round prescriptions', () => {
    const grouped: PlannedSession = {
      ...session,
      steps: [
        { ...session.steps[0]!, step_id: '44444444-4444-4444-8444-444444444444', sequence_no: 1, exercise_name: 'A', prescription: { sets: [{ reps_min: 5, load_kg: 40, rir: 2, rest_seconds: 0 }, { reps_min: 6, load_kg: 42.5, rir: 1, rest_seconds: 0 }] } },
        { ...session.steps[0]!, step_id: '55555555-5555-4555-8555-555555555555', sequence_no: 2, exercise_name: 'B', prescription: { sets: [{ reps_min: 8, load_kg: 20, rir: 2, rest_seconds: 90 }, { reps_min: 9, load_kg: 22, rir: 1, rest_seconds: 90 }] } },
        { ...session.steps[0]!, step_id: '66666666-6666-4666-8666-666666666666', sequence_no: 3, exercise_name: 'After', prescription: { sets: [{ reps_min: 10, load_kg: 10 }] } },
      ],
      groups: [{ group_id: '77777777-7777-4777-8777-777777777777', label: 'Superset A', mode: 'rounds', rounds: 2, step_sequence_nos: [1, 2] }],
    };
    expect(expandSessionExecution(grouped).map((item) => `${item.step.exercise_name}${item.setIndex == null ? '' : item.setIndex + 1}`)).toEqual(['A1', 'B1', 'A2', 'B2', 'After1']);
    const workout = prescriptionToActual(grouped, grouped.athlete_id);
    expect(workout.entries.map((entry) => [entry.sequence_no, entry.planned_step_id, entry.set_number, entry.reps, entry.load_kg, entry.rir])).toEqual([
      [1, grouped.steps[0]!.step_id, 1, 5, 40, 2],
      [2, grouped.steps[1]!.step_id, 1, 8, 20, 2],
      [3, grouped.steps[0]!.step_id, 2, 5, 40, 2],
      [4, grouped.steps[1]!.step_id, 2, 8, 20, 2],
      [5, grouped.steps[2]!.step_id, 1, 10, 10, null],
    ]);
    expect(prescribedRestSeconds(workout, workout.entries[0])).toBe(0);
    expect(prescribedRestSeconds(workout, workout.entries[1])).toBe(90);
    expect(prescribedRestSeconds(workout, workout.entries[3])).toBe(90);
  });

  it('does not activate a timer for zero rest and accepts legacy sessions without groups', () => {
    const workout = prescriptionToActual(session, session.athlete_id);
    let state = workoutReducer(idleWorkoutState, { type: 'start', workout });
    state = workoutReducer(state, { type: 'activate' });
    state = workoutReducer(state, { type: 'start_rest', seconds: 0 });
    expect(state.restActive).toBe(false);
    expect(plannedSessionSchema.parse(session).groups).toEqual([]);
    expect(() => plannedSessionSchema.parse({ ...session, groups: [{ group_id: '77777777-7777-4777-8777-777777777777', mode: 'timed', rounds: 2, step_sequence_nos: [1, 2] }] })).toThrow();
  });

  it('copies the previous session first set, then carries the last set forward within the new session', () => {
    const repeated: PlannedSession = {
      ...session,
      steps: [{ ...session.steps[0]!, prescription: { sets: [{ reps_min: 5, load_kg: 80, rir: 2 }, { reps_min: 5, load_kg: 80, rir: 2 }] } }],
    };
    const previous: import('../api/generated').Workout = {
      workout_id: '77777777-7777-4777-8777-777777777777', athlete_id: session.athlete_id, date: '2026-08-28', title: 'Other routine', source: 'mobile', status: 'completed', entries: [
        { entry_id: '88888888-8888-4888-8888-888888888888', sequence_no: 1, entry_type: 'set', exercise_name: 'Squat', set_number: 1, reps: 7, load_kg: 90, rir: 1 },
      ],
    };
    const workout = prescriptionToActual(repeated, session.athlete_id, session.date, [previous]);
    expect(workout.entries.map((entry) => [entry.reps, entry.load_kg, entry.rir])).toEqual([[7, 90, 1], [7, 90, 1]]);
  });
});
