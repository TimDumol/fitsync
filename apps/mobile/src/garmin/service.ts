import { e2eMode } from '../config';
import type { ApiClient } from '../api/client';
import type { ConnectionState, GarminLogEvent, GarminProgressEvent, NativeSyncOptions, NativeSyncResult } from '../../modules/garmin-sync/src/GarminSync.types';
import { GarminNativeError, getGarminSyncNativeModule, normalizeGarminNativeError } from '../../modules/garmin-sync/src/GarminSyncModule';

export type AccessTokenProvider = (forceRefresh?: boolean) => Promise<string>;
export type GarminNative = ReturnType<typeof getGarminSyncNativeModule>;

class FixtureGarminNative implements GarminNative {
  private readonly connected = new Set<string>();
  private readonly pendingLogins = new Map<string, string>();
  private readonly listeners = new Set<(event: GarminProgressEvent) => void>();
  private readonly logListeners = new Set<(event: GarminLogEvent) => void>();

  async getConnectionState(athleteID: string): Promise<ConnectionState> { return this.connected.has(athleteID) ? 'connected' : 'disconnected'; }
  async startLogin(athleteID: string, email: string, _password: string): Promise<{ status: 'connected' } | { status: 'needs_mfa'; loginID: string }> {
    if (email.toLowerCase().includes('fail')) throw new GarminNativeError({ code: 'GARMIN_AUTH_FAILED', message: 'Garmin authentication failed', retryable: false });
    if (email.toLowerCase().includes('mfa')) {
      const loginID = `fixture-login-${athleteID}`;
      this.pendingLogins.set(loginID, athleteID);
      return { status: 'needs_mfa', loginID };
    }
    this.connected.add(athleteID);
    return { status: 'connected' };
  }
  async resumeLogin(athleteID: string, loginID: string, _code: string): Promise<{ status: 'connected' }> {
    if (_code !== '123456') throw new GarminNativeError({ code: 'GARMIN_AUTH_FAILED', message: 'The Garmin verification code was not accepted', retryable: false });
    if (this.pendingLogins.get(loginID) !== athleteID) throw new GarminNativeError({ code: 'GARMIN_MFA_EXPIRED', message: 'Garmin verification expired', retryable: false });
    this.pendingLogins.delete(loginID);
    this.connected.add(athleteID);
    return { status: 'connected' };
  }
  async cancelLogin(loginID: string): Promise<void> { this.pendingLogins.delete(loginID); }
  async syncNow(options: NativeSyncOptions): Promise<NativeSyncResult> {
    if (!this.connected.has(options.athleteID)) throw new GarminNativeError({ code: 'GARMIN_AUTH_FAILED', message: 'Reconnect Garmin before syncing', retryable: true });
    const stages: GarminProgressEvent['stage'][] = ['authenticating', 'fetching_activities', 'fetching_sleep', 'fetching_recovery', 'fetching_activity_details', 'fetching_training', 'uploading', 'cleaning_up', 'complete'];
    stages.forEach((stage, index) => this.listeners.forEach((listener) => listener({ stage, completed: index, total: stages.length - 1, message: 'Syncing Garmin data' })));
    const now = new Date().toISOString();
    return { status: 'success', recordsSent: 3, archivesSent: 0, batchesSent: 1, startedAt: now, completedAt: now, latestRecordDate: now.slice(0, 10) };
  }
  async disconnect(athleteID: string): Promise<void> { this.connected.delete(athleteID); }
  addListener(event: 'garminSyncProgress' | 'garminSyncLog', listener: ((event: GarminProgressEvent) => void) | ((event: GarminLogEvent) => void)): { remove(): void } {
    if (event === 'garminSyncProgress') {
      const progressListener = listener as (event: GarminProgressEvent) => void;
      this.listeners.add(progressListener);
      return { remove: () => this.listeners.delete(progressListener) };
    }
    const logListener = listener as (event: GarminLogEvent) => void;
    this.logListeners.add(logListener);
    return { remove: () => this.logListeners.delete(logListener) };
  }
}

export class GarminSyncService {
  constructor(private readonly native: GarminNative = e2eMode ? new FixtureGarminNative() : getGarminSyncNativeModule()) {}

  getConnectionState(athleteID: string): Promise<ConnectionState> { return this.native.getConnectionState(athleteID); }
  startLogin(athleteID: string, email: string, password: string) { return this.native.startLogin(athleteID, email, password); }
  resumeLogin(athleteID: string, loginID: string, code: string) { return this.native.resumeLogin(athleteID, loginID, code); }
  cancelLogin(loginID: string): Promise<void> { return this.native.cancelLogin(loginID); }
  disconnect(athleteID: string): Promise<void> { return this.native.disconnect(athleteID); }
  subscribe(listener: (event: GarminProgressEvent) => void): { remove(): void } { return this.native.addListener('garminSyncProgress', listener); }
  subscribeLogs(listener: (event: GarminLogEvent) => void): { remove(): void } { return this.native.addListener('garminSyncLog', listener); }

  async syncNow(options: Omit<NativeSyncOptions, 'accessToken'>, accessToken: AccessTokenProvider): Promise<NativeSyncResult> {
    const firstToken = await accessToken(false);
    try {
      return await this.native.syncNow({ ...options, accessToken: firstToken });
    } catch (error) {
      const nativeError = normalizeGarminNativeError(error);
      if (!nativeError || nativeError.code !== 'FITNESSD_UNAUTHORIZED') throw nativeError ?? error;
      const refreshed = await accessToken(true);
      try {
        return await this.native.syncNow({ ...options, accessToken: refreshed });
      } catch (retryError) {
        throw normalizeGarminNativeError(retryError) ?? retryError;
      }
    }
  }
}

export function isGarminNativeError(error: unknown): error is GarminNativeError {
  return normalizeGarminNativeError(error) !== null;
}

export function toGarminNativeError(error: unknown): GarminNativeError | null { return normalizeGarminNativeError(error); }

export function serviceForTests(native: GarminNative): GarminSyncService { return new GarminSyncService(native); }
