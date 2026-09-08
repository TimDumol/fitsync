export type AppLogLevel = 'debug' | 'info' | 'warn' | 'error';

export type AppLogEntry = {
  id: number;
  timestamp: string;
  level: AppLogLevel;
  scope: string;
  message: string;
  details?: unknown;
};

const MAX_ENTRIES = 500;
const SENSITIVE_KEY = /password|passphrase|token|authorization|cookie|secret|credential|mfa/i;
const BEARER_VALUE = /(Bearer\s+)[^\s,;]+/gi;
const SENSITIVE_VALUE = /((?:password|passphrase|token|authorization|cookie|secret|credential|mfa)[\w-]*\s*[:=]\s*)([^\s,;}]+)/gi;

let nextID = 1;
let entries: AppLogEntry[] = [];
const listeners = new Set<(entry: AppLogEntry) => void>();

function redactString(value: string): string {
  return value.replace(BEARER_VALUE, '$1[REDACTED]').replace(SENSITIVE_VALUE, '$1[REDACTED]');
}

function redact(value: unknown, key = ''): unknown {
  if (SENSITIVE_KEY.test(key)) return '[REDACTED]';
  if (value instanceof Error) return { name: value.name, message: redactString(value.message), stack: value.stack ? redactString(value.stack) : undefined };
  if (typeof value === 'string') return redactString(value);
  if (Array.isArray(value)) return value.map((item) => redact(item));
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([childKey, childValue]) => [childKey, redact(childValue, childKey)]));
  }
  return value;
}

export function errorForLog(error: unknown): { name: string; message: string; stack?: string } {
  if (error instanceof Error) {
    return { name: error.name, message: redactString(error.message), ...(error.stack ? { stack: redactString(error.stack) } : {}) };
  }
  return { name: typeof error, message: redactString(String(error)) };
}

function detailsForConsole(details: unknown): string {
  if (details === undefined) return '';
  try {
    return ` ${JSON.stringify(details)}`;
  } catch {
    return ' [unserializable details]';
  }
}

export function appLog(level: AppLogLevel, scope: string, message: string, details?: unknown): AppLogEntry {
  const entry: AppLogEntry = {
    id: nextID++,
    timestamp: new Date().toISOString(),
    level,
    scope,
    message: redactString(message),
    ...(details === undefined ? {} : { details: redact(details) }),
  };
  entries = [...entries, entry].slice(-MAX_ENTRIES);
  const consoleMessage = `[${scope}] ${entry.message}${detailsForConsole(entry.details)}`;
  if (level === 'error') console.error(consoleMessage);
  else if (level === 'warn') console.warn(consoleMessage);
  else console.log(consoleMessage);
  listeners.forEach((listener) => listener(entry));
  return entry;
}

export function getAppLogs(): AppLogEntry[] {
  return entries.slice();
}

export function subscribeToAppLogs(listener: (entry: AppLogEntry) => void): { remove: () => void } {
  listeners.add(listener);
  return { remove: () => listeners.delete(listener) };
}

export function clearAppLogs(): void {
  entries = [];
}

export function formatAppLog(entry: AppLogEntry): string {
  const details = entry.details === undefined ? '' : `\n${JSON.stringify(entry.details, null, 2)}`;
  return `${entry.timestamp} ${entry.level.toUpperCase()} [${entry.scope}] ${entry.message}${details}`;
}
