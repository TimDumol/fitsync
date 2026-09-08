import type { GarminLogEvent, GarminProgressEvent, NativeSyncOptions, NativeSyncResult } from '../../modules/garmin-sync/src/GarminSync.types';

export type { ConnectionState, GarminLogEvent, GarminProgressEvent, NativeSyncOptions, NativeSyncResult } from '../../modules/garmin-sync/src/GarminSync.types';

export type GarminError = { code: string; message: string; retryable: boolean; details?: string };
export type GarminStatus = { last_ingested_at: string | null; latest_record_date: string | null; datasets: Record<string, string> };
export type ProgressEvent = GarminProgressEvent;
export type SyncOptions = NativeSyncOptions;
export type SyncResult = NativeSyncResult;
