export function requireNativeModule<T>(_name: string): T {
  throw new Error('Native Garmin module is unavailable in fixture tests');
}
