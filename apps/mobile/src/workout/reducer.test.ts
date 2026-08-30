import { describe, expect, it } from 'vitest';
import type { PlannedSession } from '../api/generated';
import { idleWorkoutState, prescriptionToActual, workoutReducer } from './types';

const session: PlannedSession = { session_id: '11111111-1111-4111-8111-111111111111', plan_id: '22222222-2222-4222-8222-222222222222', athlete_id: '33333333-3333-4333-8333-333333333333', date: '2026-08-29', title: 'Strength', sport: 'strength', session_type: 'full_body', priority: 'normal', targets: {}, status: 'planned', steps: [{ step_id: '44444444-4444-4444-8444-444444444444', sequence_no: 1, step_type: 'exercise', exercise_name: 'Squat', prescription: { sets: [{ reps_min: 5, load_kg: 80 }] } }] };

describe('workout state machine', () => {
  it('copies prescribed sets and moves through finish', () => {
    const workout = prescriptionToActual(session, session.athlete_id);
    let state = workoutReducer(idleWorkoutState, { type: 'start', workout });
    expect(state.status).toBe('starting');
    state = workoutReducer(state, { type: 'activate' });
    state = workoutReducer(state, { type: 'edit_entry', entryID: workout.entries[0]!.entry_id, patch: { reps: 6 } });
    expect(state.workout?.entries[0]!.reps).toBe(6);
    state = workoutReducer(state, { type: 'finish' });
    expect(state.status).toBe('finishing');
  });
});
