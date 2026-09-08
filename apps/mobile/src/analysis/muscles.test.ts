import { describe, expect, it } from 'vitest';
import type { Workout } from '../api/generated';
import { dateRangeForDays, muscleGroupsForEntry, summarizeMuscles } from './muscles';

const workout = (overrides: Partial<Workout> = {}): Workout => ({
  workout_id: '00000000-0000-4000-8000-000000000001',
  athlete_id: '00000000-0000-4000-8000-000000000002',
  date: '2026-09-03',
  title: 'Strength',
  source: 'mobile',
  status: 'completed',
  entries: [],
  ...overrides,
});

describe('muscle history analysis', () => {
  it('uses tagged groups once per working set and ignores warmups', () => {
    const result = summarizeMuscles(workout({ entries: [
      { entry_id: '00000000-0000-4000-8000-000000000003', sequence_no: 1, entry_type: 'set', exercise_key: 'bench_press', is_warmup: false },
      { entry_id: '00000000-0000-4000-8000-000000000004', sequence_no: 2, entry_type: 'set', muscle_groups: ['chest', 'chest', 'Triceps'] },
      { entry_id: '00000000-0000-4000-8000-000000000005', sequence_no: 3, entry_type: 'set', exercise_key: 'bench_press', is_warmup: true },
    ] }));

    expect(result.map((item) => [item.group, item.sets])).toEqual([['chest', 2], ['triceps', 2], ['shoulders', 1]]);
    expect(result[0]?.percentage).toBeCloseTo(40);
  });

  it('combines muscle focus across the selected history window', () => {
    const second = workout({ workout_id: '00000000-0000-4000-8000-000000000006', date: '2026-09-02', entries: [
      { entry_id: '00000000-0000-4000-8000-000000000007', sequence_no: 1, entry_type: 'set', exercise_key: 'pull_up' },
    ] });
    const result = summarizeMuscles([workout({ entries: [{ entry_id: '00000000-0000-4000-8000-000000000003', sequence_no: 1, entry_type: 'set', exercise_key: 'bench_press' }] }), second]);

    expect(result.map((item) => item.group)).toEqual(['back', 'biceps', 'chest', 'shoulders', 'triceps']);
    expect(result.find((item) => item.group === 'back')?.workouts).toBe(1);
  });

  it('falls back to the explicit exercise catalog for offline entries', () => {
    expect(muscleGroupsForEntry({ entry_id: '00000000-0000-4000-8000-000000000003', sequence_no: 1, entry_type: 'set', exercise_key: 'goblet_squat' })).toEqual(['quads', 'glutes']);
  });

  it('returns an inclusive calendar range', () => {
    expect(dateRangeForDays(7, new Date(2026, 8, 3))).toEqual({ from: '2026-08-28', to: '2026-09-03' });
  });
});
