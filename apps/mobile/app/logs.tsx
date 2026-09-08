import { useEffect, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Stack } from 'expo-router';
import { AppButton } from '../src/components/AppButton';
import { colors, radii, spacing } from '../src/ui/theme';
import { appLog, clearAppLogs, formatAppLog, getAppLogs, subscribeToAppLogs, type AppLogEntry } from '../src/logging/app-logger';
import { appBuildInfo } from '../src/app-info';

export default function Logs(): JSX.Element {
  const insets = useSafeAreaInsets();
  const [entries, setEntries] = useState<AppLogEntry[]>(getAppLogs);

  useEffect(() => {
    const subscription = subscribeToAppLogs((entry) => setEntries((current) => [...current, entry].slice(-500)));
    return () => subscription.remove();
  }, []);

  const clear = () => {
    clearAppLogs();
    setEntries([]);
    appLog('info', 'logs', 'App log buffer cleared');
  };

  return <>
    <Stack.Screen options={{ title: 'App logs' }} />
    <View style={[styles.screen, { paddingTop: process.env.EXPO_OS === 'android' ? insets.top : 0 }]}><ScrollView style={styles.scroll} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
      <View style={styles.header}>
        <View style={styles.headingCopy}>
          <Text style={styles.title}>Diagnostic report</Text>
          <Text selectable style={styles.subtitle}>Detailed app, API, Garmin, and native bridge events from this app session. Sensitive credentials and tokens are redacted.</Text>
          <Text selectable style={styles.buildInfo}>Version {appBuildInfo.version} · Built {appBuildInfo.buildTimestamp}</Text>
        </View>
        <Text selectable style={styles.count}>{entries.length} entries</Text>
      </View>
      <View style={styles.actions}>
        <AppButton label="Refresh" icon="refresh" variant="secondary" fullWidth={false} onPress={() => setEntries(getAppLogs())} />
        <AppButton label="Clear" icon="delete-outline" variant="danger" fullWidth={false} onPress={clear} />
      </View>
      {entries.length === 0
        ? <View style={styles.empty}><Text style={styles.emptyTitle}>No app logs yet</Text><Text style={styles.emptyCopy}>Try the Garmin action again and return here for the detailed report.</Text></View>
        : <View style={styles.logList}>{entries.map((entry) => <View key={entry.id} style={styles.logEntry}><View style={styles.logMeta}><Text selectable style={[styles.level, styles[entry.level]]}>{entry.level.toUpperCase()}</Text><Text selectable style={styles.scope}>{entry.scope}</Text><Text selectable style={styles.timestamp}>{entry.timestamp}</Text></View><Text selectable style={styles.logText}>{formatAppLog(entry)}</Text></View>)}</View>}
    </ScrollView></View>
  </>;
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  scroll: { flex: 1 },
  content: { padding: spacing.xl, paddingBottom: spacing.xxxl, gap: spacing.lg, backgroundColor: colors.background },
  header: { gap: spacing.sm },
  headingCopy: { gap: spacing.xs },
  title: { color: colors.ink, fontSize: 28, fontWeight: '800' },
  subtitle: { color: colors.muted, lineHeight: 20 },
  buildInfo: { color: colors.primaryDark, fontFamily: 'monospace', fontSize: 12, fontWeight: '700' },
  count: { color: colors.primaryDark, fontSize: 13, fontWeight: '800' },
  actions: { flexDirection: 'row', gap: spacing.sm },
  empty: { gap: spacing.sm, padding: spacing.xl, borderRadius: radii.md, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line },
  emptyTitle: { color: colors.ink, fontSize: 16, fontWeight: '800' },
  emptyCopy: { color: colors.muted, lineHeight: 20 },
  logList: { gap: spacing.sm },
  logEntry: { gap: spacing.sm, padding: spacing.md, borderRadius: radii.sm, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line },
  logMeta: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minWidth: 0 },
  level: { paddingHorizontal: spacing.sm, paddingVertical: 3, borderRadius: radii.pill, overflow: 'hidden', fontSize: 10, fontWeight: '900' },
  debug: { color: colors.muted, backgroundColor: colors.surfaceMuted },
  info: { color: colors.primaryDark, backgroundColor: colors.primarySoft },
  warn: { color: colors.warmInk, backgroundColor: colors.warm },
  error: { color: colors.danger, backgroundColor: colors.dangerSoft },
  scope: { flexShrink: 1, color: colors.ink, fontSize: 12, fontWeight: '800' },
  timestamp: { flex: 1, minWidth: 0, color: colors.subtle, fontSize: 10, textAlign: 'right' },
  logText: { color: colors.ink, fontFamily: 'monospace', fontSize: 11, lineHeight: 17 },
});
