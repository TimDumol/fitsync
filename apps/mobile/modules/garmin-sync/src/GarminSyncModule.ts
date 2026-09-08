import { requireNativeModule } from 'expo-modules-core';
import type { ConnectionState, GarminLogEvent, GarminProgressEvent, NativeGarminError, NativeSyncOptions, NativeSyncResult } from './GarminSync.types';

export type GarminSyncNativeModule = {
  getConnectionState(athleteID: string): Promise<ConnectionState>;
  startLogin(athleteID: string, email: string, password: string): Promise<{ status: 'connected' } | { status: 'needs_mfa'; loginID: string }>;
  resumeLogin(athleteID: string, loginID: string, code: string): Promise<{ status: 'connected' }>;
  cancelLogin(loginID: string): Promise<void>;
  syncNow(options: NativeSyncOptions): Promise<NativeSyncResult>;
  disconnect(athleteID: string): Promise<void>;
  addListener(event: 'garminSyncProgress', listener: (event: GarminProgressEvent) => void): { remove(): void };
  addListener(event: 'garminSyncLog', listener: (event: GarminLogEvent) => void): { remove(): void };
};

export class GarminNativeError extends Error {
  readonly code: string;
  readonly retryable: boolean;
  readonly details?: string;

  constructor(error: NativeGarminError) {
    super(error.message);
    this.name = 'GarminNativeError';
    this.code = error.code;
    this.retryable = error.retryable;
    this.details = error.details;
  }
}

export function normalizeGarminNativeError(error: unknown): GarminNativeError | null {
  if (error instanceof GarminNativeError) return error;
  if (!(error instanceof Error)) return null;
  const match = /^([A-Z][A-Z0-9_]*)\|(true|false)\|(.*?)(?:\n---GARMIN_DETAILS---\n([\s\S]*))?$/.exec(error.message);
  if (!match) return null;
  const [, code, retryable, message, details] = match;
  if (!code || !retryable || message === undefined) return null;
  return new GarminNativeError({ code, retryable: retryable === 'true', message, ...(details ? { details } : {}) });
}

let nativeModule: GarminSyncNativeModule | null = null;

export function getGarminSyncNativeModule(): GarminSyncNativeModule {
  if (nativeModule) return nativeModule;
  nativeModule = requireNativeModule<GarminSyncNativeModule>('GarminSync');
  return nativeModule;
}
