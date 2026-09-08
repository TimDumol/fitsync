import { useQuery } from '@tanstack/react-query';
import { Link } from 'expo-router';
import { useEffect, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAuth } from '../../src/auth/AuthProvider';
import { AppButton } from '../../src/components/AppButton';
import { AppIcon } from '../../src/components/AppIcon';
import { ScreenHeader } from '../../src/components/ScreenHeader';
import { StatusPill } from '../../src/components/StatusPill';
import { SyncStatus } from '../../src/components/SyncStatus';
import { localPlan, useSync } from '../../src/hooks/useSync';
import { useSQLiteContext } from 'expo-sqlite';
import { plans, savePlan } from '../../src/db/repository';
import { colors, radii, spacing } from '../../src/ui/theme';

export default function Plan(): JSX.Element {
  const { api, me, selectedAthleteID } = useAuth();
  const db = useSQLiteContext();
  const insets = useSafeAreaInsets();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? null;
  const { syncing, pending, error } = useSync(api, athleteID);
  const [cachedPlans, setCachedPlans] = useState<Awaited<ReturnType<typeof plans>>>([]);
  const [publishingPlanID, setPublishingPlanID] = useState<string | null>(null);
  const [publishError, setPublishError] = useState<string | null>(null);
  const query = useQuery({ queryKey: ['active-plan', athleteID], queryFn: async () => { try { return await api.activePlan(athleteID!); } catch { return localPlan(db, athleteID!); } }, enabled: Boolean(athleteID) });
  useEffect(() => { if (athleteID) void plans(db, athleteID).then(setCachedPlans); }, [athleteID, db, syncing, query.data]);
  const plan = query.data;
  const drafts = cachedPlans.filter((item) => item.status === 'draft');
  const publish = async (planID: string) => {
    if (!athleteID || publishingPlanID) return;
    setPublishingPlanID(planID);
    setPublishError(null);
    try {
      // The draft list is intentionally backed by the offline cache. Confirm
      // its lifecycle on the server immediately before publishing so a stale
      // cache cannot republish an archived plan after another plan won.
      const canonical = await api.plan(athleteID, planID);
      if (canonical.status !== 'draft') {
        await savePlan(db, canonical);
        setCachedPlans((current) => current.map((item) => item.plan_id === canonical.plan_id ? canonical : item));
        await query.refetch();
        setPublishError('That draft is no longer available. The plan list was refreshed.');
        return;
      }
      const published = await api.publishPlan(athleteID, planID);
      await savePlan(db, published);
      setCachedPlans((current) => current.map((item) => item.plan_id === published.plan_id ? published : item));
      await query.refetch();
    } catch (caught) {
      setPublishError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setPublishingPlanID(null);
    }
  };
  return <View style={[styles.screen, { paddingTop: process.env.EXPO_OS === 'android' ? insets.top : 0 }]}><ScrollView testID="plan-screen" style={styles.scroll} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
    <ScreenHeader eyebrow="Training calendar" title="Plan" subtitle="Keep the next session within reach." />
    <SyncStatus syncing={syncing} pending={pending} error={error} />
    {publishError ? <Text testID="publish-error" selectable style={styles.publishError}>{publishError}</Text> : null}
    {plan ? <>
      <View style={styles.planHero}>
        <View style={styles.planHeroIcon}><AppIcon name="target" size={25} color={colors.primary} /></View>
        <View style={styles.planCopy}><Text style={styles.planTitle}>{plan.title}</Text><Text style={styles.muted}>{plan.start_date ?? 'No start'} – {plan.end_date ?? 'No end'}</Text></View>
        <StatusPill label="Active" tone="success" />
        <Text style={styles.goal}>{String(plan.goal.summary ?? 'Active training plan')}</Text>
      </View>
      <View style={styles.sectionHeader}><Text style={styles.sectionTitle}>Sessions</Text><Text style={styles.muted}>{plan.sessions.length} planned</Text></View>
      <View style={styles.sessionList}>{plan.sessions.map((session) => <Link key={session.session_id} href={{ pathname: '/planned-session/[id]', params: { id: session.session_id } }} asChild><Pressable testID="planned-session-row" style={styles.session}>
        <View style={styles.sessionDate}><Text style={styles.dateDay}>{session.date.slice(8)}</Text><Text style={styles.dateMonth}>{session.date.slice(5, 7)}</Text></View>
        <View style={styles.sessionCopy}><Text style={styles.sessionTitle}>{session.title}</Text><Text style={styles.muted}>{session.steps.length} steps · {session.status}</Text></View>
        <AppIcon name={session.status === 'completed' ? 'check-circle' : 'chevron-right'} size={22} color={session.status === 'completed' ? colors.primary : colors.subtle} />
      </Pressable></Link>)}</View>
    </> : <View testID="plan-empty-state" style={styles.emptyCard}><View style={styles.emptyIcon}><AppIcon name="calendar-plus" size={26} color={colors.primary} /></View><Text style={styles.emptyTitle}>No active plan yet</Text><Text style={styles.muted}>Once a plan is published, your upcoming sessions will show up here.</Text><AppButton testID="refresh-plan-button" label="Refresh plan" icon="refresh" variant="secondary" onPress={() => void query.refetch()} /></View>}
    {drafts.map((draft) => <View key={draft.plan_id} style={styles.draft}><View style={styles.cardHeader}><StatusPill label="Draft" tone="warm" /><AppIcon name="file-edit-outline" size={22} color={colors.warmInk} /></View><Text style={styles.sessionTitle}>{draft.title}</Text><Text style={styles.muted}>{draft.sessions.length} sessions ready to publish</Text><AppButton testID={`publish-draft-${draft.plan_id}`} label="Publish draft" icon="publish" loading={publishingPlanID === draft.plan_id} disabled={publishingPlanID !== null} onPress={() => void publish(draft.plan_id)} /></View>)}
  </ScrollView></View>;
}
const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  scroll: { flex: 1 },
  content: { padding: spacing.xl, paddingBottom: spacing.xxxl, gap: spacing.lg, backgroundColor: colors.background },
  planHero: { gap: spacing.md, padding: spacing.xl, borderRadius: radii.lg, backgroundColor: colors.primarySoft, borderWidth: 1, borderColor: '#BFE9D7', borderCurve: 'continuous' },
  planHeroIcon: { width: 48, height: 48, alignItems: 'center', justifyContent: 'center', borderRadius: radii.md, backgroundColor: colors.white },
  planCopy: { minWidth: 0, gap: spacing.xs },
  planTitle: { flexShrink: 1, color: colors.ink, fontSize: 22, fontWeight: '800' },
  goal: { color: colors.primaryDark, fontSize: 14, lineHeight: 21 },
  sectionHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: spacing.sm },
  sectionTitle: { color: colors.ink, fontSize: 20, fontWeight: '800' },
  sessionList: { gap: spacing.sm },
  session: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minWidth: 0, padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderCurve: 'continuous' },
  pressed: { opacity: 0.72 },
  sessionDate: { width: 42, height: 46, alignItems: 'center', justifyContent: 'center', borderRadius: radii.sm, backgroundColor: colors.surfaceMuted },
  dateDay: { color: colors.ink, fontSize: 18, fontWeight: '800' },
  dateMonth: { color: colors.muted, fontSize: 10, fontWeight: '800' },
  sessionCopy: { flex: 1, gap: spacing.xs },
  sessionTitle: { flexShrink: 1, color: colors.ink, fontSize: 15, fontWeight: '700' },
  muted: { color: colors.muted, fontSize: 14, lineHeight: 20 },
  publishError: { color: colors.danger, lineHeight: 20 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  emptyCard: { alignItems: 'center', gap: spacing.md, padding: spacing.xxl, borderRadius: radii.lg, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderCurve: 'continuous' },
  emptyIcon: { width: 56, height: 56, alignItems: 'center', justifyContent: 'center', borderRadius: 18, backgroundColor: colors.primarySoft },
  emptyTitle: { color: colors.ink, fontSize: 19, fontWeight: '800' },
  draft: { gap: spacing.md, padding: spacing.xl, borderRadius: radii.lg, backgroundColor: colors.warm, borderWidth: 1, borderColor: '#F8D6AE', borderCurve: 'continuous' },
});
