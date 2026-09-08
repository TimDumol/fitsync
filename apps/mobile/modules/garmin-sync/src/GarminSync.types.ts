export type ConnectionState = 'disconnected' | 'connected' | 'needs_reconnect';

export type GarminProgressEvent = {
  stage: 'authenticating' | 'fetching_activities' | 'fetching_sleep' | 'fetching_recovery' | 'fetching_activity_details' | 'fetching_training' | 'uploading' | 'cleaning_up' | 'complete';
  completed: number;
  total: number;
  message: string;
};

export type GarminLogEvent = {
  level: 'debug' | 'info' | 'warn' | 'error';
  message: string;
};

export type NativeSyncOptions = {
  athleteID: string;
  apiBaseURL: string;
  days: 1 | 3 | 7;
  extended: boolean;
  accessToken: string;
};

export type NativeSyncResult = {
  status: 'success';
  recordsSent: number;
  archivesSent: number;
  batchesSent: number;
  startedAt: string;
  completedAt: string;
  latestRecordDate: string | null;
};

export type NativeGarminError = {
  code: string;
  message: string;
  retryable: boolean;
  details?: string;
};
