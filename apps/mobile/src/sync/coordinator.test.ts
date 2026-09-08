import { describe, expect, it, vi } from 'vitest';
import { singleFlight } from './coordinator';

describe('singleFlight', () => {
  it('shares a concurrent operation for the same owner and key', async () => {
    const owner = {};
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    const task = vi.fn(async () => { await gate; return 'synced'; });

    const first = singleFlight(owner, 'athlete-1', task);
    const second = singleFlight(owner, 'athlete-1', task);

    expect(second).toBe(first);
    expect(task).toHaveBeenCalledTimes(1);
    release();
    await expect(first).resolves.toBe('synced');

    await expect(singleFlight(owner, 'athlete-1', task)).resolves.toBe('synced');
    expect(task).toHaveBeenCalledTimes(2);
  });

  it('does not share different keys or owners', async () => {
    const firstOwner = {};
    const secondOwner = {};
    const task = vi.fn(async () => 'ok');

    await expect(Promise.all([
      singleFlight(firstOwner, 'athlete-1', task),
      singleFlight(firstOwner, 'athlete-2', task),
      singleFlight(secondOwner, 'athlete-1', task),
    ])).resolves.toEqual(['ok', 'ok', 'ok']);
    expect(task).toHaveBeenCalledTimes(3);
  });
});
