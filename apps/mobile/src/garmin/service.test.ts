import { describe, expect, it, vi } from 'vitest';
import type { ConnectionState, GarminLogEvent, GarminProgressEvent, NativeSyncOptions, NativeSyncResult } from '../../modules/garmin-sync/src/GarminSync.types';
import { GarminNativeError } from '../../modules/garmin-sync/src/GarminSyncModule';
import { serviceForTests } from './service';

const athleteID = '00000000-0000-4000-8000-000000000001';
const options: Omit<NativeSyncOptions, 'accessToken'> = { athleteID, apiBaseURL: 'https://api.example.test', days: 3, extended: true };
const result: NativeSyncResult = { status: 'success', recordsSent: 2, archivesSent: 0, batchesSent: 1, startedAt: '2026-09-03T08:00:00Z', completedAt: '2026-09-03T08:00:01Z', latestRecordDate: '2026-09-03' };

function fakeNative(overrides: Partial<{
  getConnectionState: (id: string) => Promise<ConnectionState>;
  syncNow: (value: NativeSyncOptions) => Promise<NativeSyncResult>;
}> = {}) {
  const listeners = new Set<(event: GarminProgressEvent) => void>();
  const logListeners = new Set<(event: GarminLogEvent) => void>();
  return {
    getConnectionState: overrides.getConnectionState ?? (async () => 'disconnected' as ConnectionState),
    startLogin: async () => ({ status: 'connected' as const }),
    resumeLogin: async () => ({ status: 'connected' as const }),
    cancelLogin: async () => undefined,
    syncNow: overrides.syncNow ?? (async () => result),
    disconnect: async () => undefined,
    addListener: (event: 'garminSyncProgress' | 'garminSyncLog', listener: ((event: GarminProgressEvent) => void) | ((event: GarminLogEvent) => void)) => {
      if (event === 'garminSyncProgress') {
        const progressListener = listener as (event: GarminProgressEvent) => void;
        listeners.add(progressListener);
        return { remove: () => listeners.delete(progressListener) };
      }
      const logListener = listener as (event: GarminLogEvent) => void;
      logListeners.add(logListener);
      return { remove: () => logListeners.delete(logListener) };
    },
  };
}

describe('GarminSyncService', () => {
  it('gets a fresh token and retries one unconfirmed upload after 401', async () => {
    let calls = 0;
    const native = fakeNative({ syncNow: async () => {
      calls += 1;
      if (calls === 1) throw new GarminNativeError({ code: 'FITNESSD_UNAUTHORIZED', message: 'FitSync sign-in expired', retryable: true });
      return result;
    } });
    const tokens = vi.fn(async (forceRefresh = false) => forceRefresh ? 'refreshed-token' : 'fresh-token');

    await expect(serviceForTests(native).syncNow(options, tokens)).resolves.toEqual(result);
    expect(tokens.mock.calls).toEqual([[false], [true]]);
    expect(calls).toBe(2);
  });

  it.each(['FITNESSD_FORBIDDEN', 'FITNESSD_RATE_LIMITED'] as const)('does not retry %s', async (code) => {
    const native = fakeNative({ syncNow: async () => { throw new GarminNativeError({ code, message: 'safe error', retryable: code.endsWith('LIMITED') }); } });
    const tokens = vi.fn(async () => 'fresh-token');

    await expect(serviceForTests(native).syncNow(options, tokens)).rejects.toMatchObject({ code });
    expect(tokens).toHaveBeenCalledOnce();
  });

  it('keeps connection state isolated by athlete', async () => {
    const otherAthleteID = '00000000-0000-4000-8000-000000000002';
    const native = fakeNative({ getConnectionState: async (id) => id === athleteID ? 'connected' : 'disconnected' });
    const service = serviceForTests(native);

    await expect(service.getConnectionState(athleteID)).resolves.toBe('connected');
    await expect(service.getConnectionState(otherAthleteID)).resolves.toBe('disconnected');
  });
});
