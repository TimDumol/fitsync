import { useQuery } from '@tanstack/react-query';
import { Link } from 'expo-router';
import { RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useEffect, useState } from 'react';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAuth } from '../../src/auth/AuthProvider';
import { AppButton } from '../../src/components/AppButton';
import { AppIcon } from '../../src/components/AppIcon';
import { ScreenHeader } from '../../src/components/ScreenHeader';
import { StatusPill } from '../../src/components/StatusPill';
import { SyncStatus } from '../../src/components/SyncStatus';
import { useSync, localPlan } from '../../src/hooks/useSync';
import { useSQLiteContext } from 'expo-sqlite';
import { activeDrafts } from '../../src/db/repository';
import type { Workout } from '../../src/api/generated';
import { colors, radii, spacing } from '../../src/ui/theme';

function localDate(timezone: string): string { return new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()); }

export default function Today(): JSX.Element {
  const { signedIn, selectedAthleteID, me, api } = useAuth();
  const db = useSQLiteContext();
  const insets = useSafeAreaInsets();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? null;
  const { syncing, pending, error } = useSync(api, athleteID);
  const [draft, setDraft] = useState<Workout | null>(null);
  useEffect(() => { if (athleteID) void activeDrafts(db, athleteID).then((items) => setDraft(items[0] ?? null)); }, [athleteID, db, syncing]);
  const today = localDate(me?.athlete.timezone ?? 'UTC');
  const plan = useQuery({ queryKey: ['active-plan', athleteID], queryFn: async () => { try { return await api.activePlan(athleteID!); } catch { return localPlan(db, athleteID!); } }, enabled: signedIn && Boolean(athleteID) });
  const session = plan.data?.sessions.find((item) => item.date === today);
  if (!signedIn) return <View />;
  return <View style={[styles.screen, { paddingTop: process.env.EXPO_OS === 'android' ? insets.top : 0 }]}><ScrollView
    testID="today-screen"
    style={styles.scroll}
    contentInsetAdjustmentBehavior="automatic"
    contentContainerStyle={styles.content}
    refreshControl={<RefreshControl refreshing={syncing || plan.isRefetching} onRefresh={() => void plan.refetch()} tintColor={colors.primary} />}
  >
    <ScreenHeader eyebrow={today} title="Today" subtitle="One focused session at a time." />
    <SyncStatus syncing={syncing} pending={pending} error={error} />
    {draft ? <View testID="resume-workout-card" style={styles.resume}>
      <View style={styles.cardHeader}><StatusPill label="In progress" tone="warm" /><AppIcon name="play-circle-outline" size={24} color={colors.warmInk} /></View>
      <View style={styles.cardCopy}><Text style={styles.cardTitle}>Workout in progress</Text><Text style={styles.muted}>{draft.title} · saved locally</Text></View>
      <Link href={{ pathname: '/workout/[id]', params: { id: draft.workout_id } }} asChild><AppButton testID="resume-workout-button" label="Resume workout" icon="play" /></Link>
    </View> : null}
    {session ? <View testID="today-session-card" style={styles.card}>
      <View style={styles.cardHeader}><StatusPill label={session.status === 'completed' ? 'Completed' : 'Up next'} tone={session.status === 'completed' ? 'success' : 'neutral'} /><AppIcon name="dumbbell" size={24} color={colors.primary} /></View>
      <View style={styles.cardCopy}><Text style={styles.cardTitle}>{session.title}</Text><Text style={styles.muted}>{session.steps.length} prescribed steps · {session.targets.duration_minutes ? `${String(session.targets.duration_minutes)} min` : 'strength session'}</Text></View>
      <Link href={{ pathname: '/planned-session/[id]', params: { id: session.session_id } }} asChild><AppButton testID="start-workout-button" label={session.status === 'completed' ? 'View result' : 'Start workout'} icon={session.status === 'completed' ? 'arrow-right' : 'play'} variant={session.status === 'completed' ? 'secondary' : 'primary'} /></Link>
    </View> : <View testID="today-rest-card" style={styles.restCard}>
      <View style={styles.restIcon}><AppIcon name="weather-night" size={26} color={colors.primary} /></View>
      <View style={styles.cardCopy}><Text style={styles.cardTitle}>Rest day</Text><Text style={styles.muted}>No planned session today. Recovery is part of the plan.</Text></View>
    </View>}
  </ScrollView></View>;
}
const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  scroll: { flex: 1 },
  content: { padding: spacing.xl, paddingBottom: spacing.xxxl, gap: spacing.lg, backgroundColor: colors.background },
  card: { gap: spacing.lg, padding: spacing.xl, borderRadius: radii.lg, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderCurve: 'continuous' },
  resume: { gap: spacing.lg, padding: spacing.xl, borderRadius: radii.lg, backgroundColor: colors.warm, borderWidth: 1, borderColor: '#F8D6AE', borderCurve: 'continuous' },
  restCard: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, padding: spacing.xl, borderRadius: radii.lg, backgroundColor: colors.surfaceMuted, borderWidth: 1, borderColor: colors.line, borderCurve: 'continuous' },
  cardHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  cardCopy: { flex: 1, minWidth: 0, gap: spacing.xs },
  cardTitle: { color: colors.ink, fontSize: 20, fontWeight: '800', letterSpacing: -0.2 },
  muted: { color: colors.muted, fontSize: 14, lineHeight: 21 },
  restIcon: { width: 48, height: 48, alignItems: 'center', justifyContent: 'center', borderRadius: radii.md, backgroundColor: colors.primarySoft },
});
