import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Workout } from './generated';
import { ApiClient } from './client';

const athleteID = '00000000-0000-4000-8000-000000000001';
const workoutID = '00000000-0000-4000-8000-000000000005';
const workout: Workout = {
  workout_id: workoutID,
  athlete_id: athleteID,
  date: '2026-08-31',
  title: 'Upper body',
  source: 'mobile',
  status: 'completed',
  duration_minutes: 42,
  entries: [{
    entry_id: '00000000-0000-4000-8000-000000000006',
    sequence_no: 1,
    entry_type: 'set',
    exercise_name: 'Bench press',
    set_number: 1,
    reps: 8,
    load_kg: 60,
  }],
};

describe('ApiClient workout history', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('requests a paginated history page with date filters', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue({
      ok: true,
      json: async () => ({ items: [workout], next_cursor: 'next/cursor' }),
    } as Response);
    vi.stubGlobal('fetch', fetchMock);
    const api = new ApiClient(async () => 'token', 'https://example.test');

    const page = await api.workouts(athleteID, '2026-01-01', '2026-08-31', 25, 'previous/cursor');

    expect(page).toEqual({ items: [workout], next_cursor: 'next/cursor' });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('https://example.test/api/v2/athletes/00000000-0000-4000-8000-000000000001/workouts?limit=25&from=2026-01-01&to=2026-08-31&cursor=previous%2Fcursor');
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ headers: expect.objectContaining({ Authorization: 'Bearer token' }) });
  });

  it('loads the full workout selected from history', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue({
      ok: true,
      json: async () => workout,
    } as Response);
    vi.stubGlobal('fetch', fetchMock);
    const api = new ApiClient(async () => null, 'https://example.test/');

    await expect(api.workout(athleteID, workoutID)).resolves.toEqual(workout);
    expect(fetchMock.mock.calls[0]?.[0]).toBe(`https://example.test/api/v2/athletes/${athleteID}/workouts/${workoutID}`);
  });

  it('deletes a workout without trying to parse the empty response body', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue({ ok: true, status: 204, json: async () => { throw new Error('no body'); } } as unknown as Response);
    vi.stubGlobal('fetch', fetchMock);
    const api = new ApiClient(async () => null, 'https://example.test');

    await expect(api.deleteWorkout(workout)).resolves.toBeUndefined();
    expect(fetchMock.mock.calls[0]?.[0]).toBe(`https://example.test/api/v2/athletes/${athleteID}/workouts/${workoutID}`);
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ method: 'DELETE' });
  });
});
