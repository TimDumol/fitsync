import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { useSQLiteContext } from 'expo-sqlite';
import { useAuth } from '../../src/auth/AuthProvider';
import { AppButton } from '../../src/components/AppButton';
import { AppIcon } from '../../src/components/AppIcon';
import { ScreenHeader } from '../../src/components/ScreenHeader';
import { SyncStatus } from '../../src/components/SyncStatus';
import { setSelectedAthleteID as persistSelectedAthleteID } from '../../src/db/repository';
import { useSync } from '../../src/hooks/useSync';
import { GarminSyncPanel } from '../../src/garmin/GarminSyncPanel';
import { colors, radii, spacing } from '../../src/ui/theme';
import { appBuildInfo } from '../../src/app-info';

export default function Settings(): JSX.Element {
  const db = useSQLiteContext();
  const insets = useSafeAreaInsets();
  const router = useRouter();
  const { me, api, selectedAthleteID, setSelectedAthleteID, getAccessToken, logout } = useAuth();
  const { syncing, pending, error } = useSync(api, selectedAthleteID);
  return <View style={[styles.screen, { paddingTop: process.env.EXPO_OS === 'android' ? insets.top : 0 }]}><ScrollView testID="settings-screen" style={styles.scroll} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
    <ScreenHeader eyebrow="Your account" title="Settings" subtitle="Tune your profile and connected devices." />
    <SyncStatus syncing={syncing} pending={pending} error={error}/>
    <View style={styles.infoCard}>
      <View style={styles.infoRow}><View style={styles.infoIcon}><AppIcon name="map-clock-outline" size={20} color={colors.primary} /></View><View style={styles.infoCopy}><Text style={styles.infoLabel}>Timezone</Text><Text style={styles.infoValue}>{me?.athlete.timezone ?? 'Not set'}</Text></View></View>
      <View style={styles.divider} />
      <View style={styles.infoRow}><View style={styles.infoIcon}><AppIcon name="scale-balance" size={20} color={colors.primary} /></View><View style={styles.infoCopy}><Text style={styles.infoLabel}>Units</Text><Text style={styles.infoValue}>Metric</Text></View></View>
    </View>
    <View style={styles.buildCard}><Text style={styles.buildTitle}>App diagnostics</Text><Text selectable style={styles.buildValue}>Version {appBuildInfo.version}</Text><Text selectable style={styles.buildValue}>Built {appBuildInfo.buildTimestamp}</Text></View>
    <View style={styles.sectionHeader}><Text style={styles.heading}>Athlete</Text><Text style={styles.muted}>Choose the profile to sync</Text></View>
    <View style={styles.athletes}>{me?.accessible_athletes.map((athlete) => <AppButton key={athlete.athlete_id} label={athlete.handle} icon={athlete.athlete_id === selectedAthleteID ? 'check-circle' : 'account-circle-outline'} variant={athlete.athlete_id === selectedAthleteID ? 'primary' : 'secondary'} onPress={() => { setSelectedAthleteID(athlete.athlete_id); void persistSelectedAthleteID(db, athlete.athlete_id); }} />)}</View>
    <GarminSyncPanel athleteID={selectedAthleteID} api={api} getAccessToken={getAccessToken} />
    <AppButton testID="view-app-logs-button" label="View app logs" icon="file-document-outline" variant="secondary" onPress={() => router.push('/logs')} />
    <AppButton testID="logout-button" label="Log out" icon="logout" variant="danger" onPress={() => void logout()} />
  </ScrollView></View>;
}
const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  scroll: { flex: 1 },
  content: { padding: spacing.xl, paddingBottom: spacing.xxxl, gap: spacing.lg, backgroundColor: colors.background },
  infoCard: { padding: spacing.lg, gap: spacing.md, borderRadius: radii.lg, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderCurve: 'continuous' },
  infoRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  infoIcon: { width: 40, height: 40, alignItems: 'center', justifyContent: 'center', borderRadius: radii.sm, backgroundColor: colors.primarySoft },
  infoCopy: { flex: 1, minWidth: 0 },
  infoLabel: { color: colors.muted, fontSize: 12, fontWeight: '700' },
  infoValue: { color: colors.ink, marginTop: 2, fontSize: 15, fontWeight: '700' },
  divider: { height: StyleSheet.hairlineWidth, backgroundColor: colors.line },
  buildCard: { gap: spacing.xs, padding: spacing.lg, borderRadius: radii.lg, backgroundColor: colors.surfaceMuted, borderWidth: 1, borderColor: colors.line },
  buildTitle: { color: colors.ink, fontSize: 14, fontWeight: '800' },
  buildValue: { color: colors.muted, fontFamily: 'monospace', fontSize: 12 },
  sectionHeader: { gap: spacing.xs, marginTop: spacing.sm },
  heading: { color: colors.ink, fontSize: 20, fontWeight: '800' },
  muted: { color: colors.muted, fontSize: 14 },
  athletes: { gap: spacing.sm },
});
