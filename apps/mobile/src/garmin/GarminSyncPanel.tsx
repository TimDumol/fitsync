import { useEffect, useState } from 'react';
import { activateKeepAwakeAsync, deactivateKeepAwake } from 'expo-keep-awake';
import { useRouter } from 'expo-router';
import { Alert, Pressable, StyleSheet, Switch, Text, View } from 'react-native';
import type { ApiClient } from '../api/client';
import { AppButton } from '../components/AppButton';
import { AppIcon } from '../components/AppIcon';
import { StatusPill } from '../components/StatusPill';
import { colors, radii, spacing } from '../ui/theme';
import { useGarminSync } from './useGarminSync';
import type { AccessTokenProvider } from './service';
import { GarminConnectForm } from './GarminConnectForm';
import { GarminMfaForm } from './GarminMfaForm';

export function GarminSyncPanel({ athleteID, api, getAccessToken }: { athleteID: string | null; api: ApiClient; getAccessToken: AccessTokenProvider }): JSX.Element {
  const state = useGarminSync(athleteID, api, getAccessToken);
  const router = useRouter();
  const [days, setDays] = useState<1 | 3 | 7>(3);
  const [extended, setExtended] = useState(true);
  useEffect(() => {
    if (!state.busy) return;
    void activateKeepAwakeAsync('fitsync-garmin-sync');
    return () => { void deactivateKeepAwake('fitsync-garmin-sync'); };
  }, [state.busy]);
  if (!athleteID) return <View style={styles.empty}><Text style={styles.copy}>Select an athlete to configure Garmin import.</Text></View>;
  return <View testID="garmin-import" style={styles.section}>
    <View style={styles.header}><View style={styles.headingRow}><View style={styles.icon}><AppIcon name="watch-variant" size={21} color={colors.primary} /></View><View style={styles.headingCopy}><Text style={styles.heading}>Garmin import</Text><Text style={styles.copy}>Bring your health data into FitSync.</Text></View></View></View>
    {state.connection !== 'connected' && !state.mfaLoginID && <GarminConnectForm busy={state.busy} onSubmit={state.startLogin} />}
    {state.mfaLoginID && <GarminMfaForm busy={state.busy} onSubmit={state.resumeLogin} onCancel={state.cancelLogin} />}
    {state.connection === 'connected' && !state.mfaLoginID && <View style={styles.section}>
      <View style={styles.connectedRow}><StatusPill label="Connected" tone="success" /><Text testID="garmin-connected" style={styles.connectedCopy}>Garmin Connect</Text></View>
      <View style={styles.detailList}><Text style={styles.detail}>Last successful upload: <Text style={styles.detailStrong}>{state.status?.last_ingested_at ?? 'Never'}</Text></Text><Text style={styles.detail}>Latest record date: <Text style={styles.detailStrong}>{state.status?.latest_record_date ?? 'None'}</Text></Text></View>
      <Text style={styles.controlLabel}>Days to import</Text>
      <View style={styles.segmented}>{([1, 3, 7] as const).map((option) => <Pressable key={option} accessibilityRole="button" accessibilityState={{ selected: days === option }} onPress={() => setDays(option)} style={[styles.segment, days === option && styles.segmentSelected]}><Text style={[styles.segmentText, days === option && styles.segmentTextSelected]}>{option} day{option === 1 ? '' : 's'}</Text></Pressable>)}</View>
      <View style={styles.switchRow}><Switch testID="garmin-extended" value={extended} onValueChange={setExtended} trackColor={{ false: colors.line, true: colors.primary }} thumbColor={colors.white} /><Text style={styles.detailStrong}>Include extended data</Text></View>
      <AppButton testID="sync-garmin-button" label={state.busy ? 'Syncing Garmin…' : 'Sync Garmin now'} icon="sync" onPress={() => void state.syncNow(days, extended)} disabled={state.busy} loading={state.busy} />
      {state.progress && <Text testID="garmin-progress" style={styles.progress}>{state.progress.message}: {state.progress.stage}</Text>}
      {state.result && <View style={styles.result}><AppIcon name="check-circle" size={18} color={colors.primary} /><Text testID="garmin-result" style={styles.resultText}>Uploaded {state.result.recordsSent} records in {state.result.batchesSent} batch.</Text></View>}
      <AppButton testID="disconnect-garmin-button" label="Disconnect Garmin" icon="link-off" variant="danger" onPress={() => Alert.alert('Disconnect Garmin?', 'Remove the locally stored Garmin session and pending data?', [{ text: 'Cancel', style: 'cancel' }, { text: 'Disconnect', style: 'destructive', onPress: () => void state.disconnect() }])} disabled={state.busy} />
    </View>}
    {state.error && <View style={styles.error}><Text testID="garmin-error" selectable style={styles.errorTitle}>{state.error.message}</Text><Text selectable style={styles.errorCode}>Diagnostic code: {state.error.code}</Text><Text style={styles.copy}>{state.error.retryable ? 'You can try again.' : 'Please reconnect Garmin if needed.'}</Text><AppButton testID="view-garmin-logs-button" label="View detailed app logs" icon="file-document-outline" variant="secondary" onPress={() => router.push('/logs')} />{state.error.code.startsWith('GARMIN_AUTH') && <AppButton label="Reconnect Garmin" icon="refresh" variant="secondary" onPress={() => void state.disconnect()} />}</View>}
  </View>;
}

const styles = StyleSheet.create({
  section: { gap: spacing.md, padding: spacing.xl, borderRadius: radii.lg, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderCurve: 'continuous' },
  header: { gap: spacing.sm },
  headingRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minWidth: 0 },
  icon: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center', borderRadius: radii.sm, backgroundColor: colors.primarySoft },
  headingCopy: { flex: 1, minWidth: 0, gap: spacing.xs },
  heading: { color: colors.ink, fontSize: 19, fontWeight: '800' },
  copy: { color: colors.muted, lineHeight: 20 },
  empty: { padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.surfaceMuted },
  connectedRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  connectedCopy: { flexShrink: 1, color: colors.muted, fontSize: 13, fontWeight: '700' },
  detailList: { gap: spacing.xs, paddingVertical: spacing.sm },
  detail: { color: colors.muted, fontSize: 13 },
  detailStrong: { color: colors.ink, fontWeight: '700' },
  controlLabel: { color: colors.ink, fontSize: 13, fontWeight: '800' },
  segmented: { flexDirection: 'row', gap: spacing.xs, padding: spacing.xs, borderRadius: radii.sm, backgroundColor: colors.surfaceMuted },
  segment: { flex: 1, alignItems: 'center', paddingVertical: 9, borderRadius: 8 },
  segmentSelected: { backgroundColor: colors.surface },
  segmentText: { color: colors.muted, fontSize: 12, fontWeight: '700' },
  segmentTextSelected: { color: colors.primaryDark },
  switchRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  progress: { color: colors.muted, fontSize: 13 },
  result: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, padding: spacing.md, borderRadius: radii.sm, backgroundColor: colors.primarySoft },
  resultText: { flex: 1, color: colors.primaryDark, fontSize: 13, fontWeight: '700' },
  error: { gap: spacing.sm, padding: spacing.md, borderRadius: radii.md, backgroundColor: colors.dangerSoft },
  errorTitle: { color: colors.danger, fontWeight: '800' },
  errorCode: { color: colors.danger, fontFamily: 'monospace', fontSize: 12, fontWeight: '700' },
});
