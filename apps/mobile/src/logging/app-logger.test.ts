import { beforeEach, describe, expect, it, vi } from 'vitest';
import { appLog, clearAppLogs, formatAppLog, getAppLogs } from './app-logger';

describe('app logger', () => {
  beforeEach(() => {
    clearAppLogs();
    vi.spyOn(console, 'log').mockImplementation(() => undefined);
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
  });

  it('keeps detailed diagnostics while redacting sensitive values', () => {
    const entry = appLog('error', 'garmin', 'Login failed', { code: 'GARMIN_AUTH_FAILED', password: 'secret', nested: { accessToken: 'bearer-token' }, message: 'Bearer abc123' });

    expect(getAppLogs()).toHaveLength(1);
    expect(formatAppLog(entry)).toContain('GARMIN_AUTH_FAILED');
    expect(formatAppLog(entry)).not.toContain('secret');
    expect(formatAppLog(entry)).not.toContain('bearer-token');
    expect(formatAppLog(entry)).not.toContain('abc123');
  });

  it('retains only the most recent entries', () => {
    for (let index = 0; index < 505; index += 1) appLog('debug', 'test', `event-${index}`);

    const logs = getAppLogs();
    expect(logs).toHaveLength(500);
    expect(logs[0]?.message).toBe('event-5');
    expect(logs.at(-1)?.message).toBe('event-504');
  });
});
