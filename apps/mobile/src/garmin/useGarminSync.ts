import { useCallback, useEffect, useMemo, useState } from 'react';
import type { ApiClient } from '../api/client';
import type { GarminError, GarminStatus, ProgressEvent } from './types';
import { GarminSyncService, isGarminNativeError, toGarminNativeError } from './service';
import type { AccessTokenProvider } from './service';
import { apiBaseURL } from '../config';
import { appLog, errorForLog } from '../logging/app-logger';

export function useGarminSync(athleteID: string | null, api: ApiClient, getAccessToken: AccessTokenProvider) {
  const service = useMemo(() => new GarminSyncService(), []);
  const [connection, setConnection] = useState<'disconnected' | 'connected' | 'needs_reconnect'>('disconnected');
  const [status, setStatus] = useState<GarminStatus | null>(null);
  const [progress, setProgress] = useState<ProgressEvent | null>(null);
  const [result, setResult] = useState<{ recordsSent: number; archivesSent: number; batchesSent: number } | null>(null);
  const [error, setError] = useState<GarminError | null>(null);
  const [busy, setBusy] = useState(false);
  const [mfaLoginID, setMfaLoginID] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!athleteID) return;
    appLog('debug', 'garmin', 'Refreshing Garmin connection status', { athleteID });
    const [state, serverStatus] = await Promise.all([service.getConnectionState(athleteID), api.garminSyncStatus(athleteID)]);
    setConnection(state);
    setStatus(serverStatus);
    appLog('debug', 'garmin', 'Garmin connection status refreshed', { athleteID, connection: state, latestRecordDate: serverStatus.latest_record_date });
  }, [api, athleteID, service]);

  useEffect(() => {
    setConnection('disconnected'); setStatus(null); setProgress(null); setResult(null); setError(null); setMfaLoginID(null);
    if (!athleteID) return;
    void refresh().catch((caught) => {
      appLog('error', 'garmin', 'Garmin status refresh failed', { athleteID, error: errorForLog(caught) });
      setError({ code: 'FITNESSD_REQUEST_FAILED', message: 'Garmin status is unavailable', retryable: true });
    });
  }, [athleteID, refresh]);

  useEffect(() => {
    const subscription = service.subscribe(setProgress);
    return () => subscription.remove();
  }, [service]);

  useEffect(() => {
    const subscription = service.subscribeLogs((event) => appLog(event.level, 'garmin-native', event.message));
    return () => subscription.remove();
  }, [service]);

  const run = useCallback(async (operation: string, action: () => Promise<void>) => {
    setBusy(true); setError(null);
    appLog('info', 'garmin', `${operation} started`);
    try {
      await action();
      appLog('info', 'garmin', `${operation} completed`);
    } catch (caught) {
      const nativeError = toGarminNativeError(caught);
      if (nativeError && isGarminNativeError(nativeError)) {
        if (nativeError.code === 'GARMIN_MFA_EXPIRED') setMfaLoginID(null);
        appLog('error', 'garmin', `${operation} failed`, { code: nativeError.code, retryable: nativeError.retryable, details: nativeError.details });
        setError({ code: nativeError.code, message: nativeError.message, retryable: nativeError.retryable, details: nativeError.details });
      } else {
        appLog('error', 'garmin', `${operation} failed`, { error: errorForLog(caught) });
        setError({ code: 'INTERNAL', message: 'Garmin operation could not be completed', retryable: true });
      }
    } finally { setBusy(false); }
  }, []);

  const startLogin = useCallback((email: string, password: string) => run('Garmin login', async () => {
    if (!athleteID) return;
    appLog('debug', 'garmin', 'Submitting Garmin credentials', { athleteID, hasEmail: Boolean(email), hasPassword: Boolean(password) });
    const response = await service.startLogin(athleteID, email, password);
    if (response.status === 'needs_mfa') {
      appLog('info', 'garmin', 'Garmin requested MFA verification', { athleteID });
      setMfaLoginID(response.loginID);
    } else { setConnection('connected'); setMfaLoginID(null); await refresh(); }
  }), [athleteID, refresh, run, service]);
  const resumeLogin = useCallback((code: string) => run('Garmin MFA verification', async () => {
    if (!athleteID || !mfaLoginID) return;
    await service.resumeLogin(athleteID, mfaLoginID, code);
    setMfaLoginID(null); setConnection('connected'); await refresh();
  }), [athleteID, mfaLoginID, refresh, run, service]);
  const cancelLogin = useCallback(() => run('Cancel Garmin MFA', async () => { if (mfaLoginID) await service.cancelLogin(mfaLoginID); setMfaLoginID(null); }), [mfaLoginID, run, service]);
  const syncNow = useCallback((days: 1 | 3 | 7, extended: boolean) => run('Garmin sync', async () => {
    if (!athleteID) return;
    const value = await service.syncNow({ athleteID, apiBaseURL, days, extended }, getAccessToken);
    setResult({ recordsSent: value.recordsSent, archivesSent: value.archivesSent, batchesSent: value.batchesSent });
    await refresh();
  }), [athleteID, getAccessToken, refresh, run, service]);
  const disconnect = useCallback(() => run('Disconnect Garmin', async () => { if (!athleteID) return; await service.disconnect(athleteID); setConnection('disconnected'); setStatus(null); setResult(null); }), [athleteID, run, service]);
  return { connection, status, progress, result, error, busy, mfaLoginID, startLogin, resumeLogin, cancelLogin, syncNow, disconnect, refresh };
}
