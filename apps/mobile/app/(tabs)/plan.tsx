import { useQuery } from '@tanstack/react-query';
import { Link } from 'expo-router';
import { useEffect, useState } from 'react';
import { Button, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../../src/auth/AuthProvider';
import { SyncStatus } from '../../src/components/SyncStatus';
import { localPlan, useSync } from '../../src/hooks/useSync';
import { useSQLiteContext } from 'expo-sqlite';
import { plans, savePlan } from '../../src/db/repository';

export default function Plan(): JSX.Element {
  const { api, me, selectedAthleteID } = useAuth();
  const db = useSQLiteContext();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? null;
  const { syncing, pending } = useSync(api, athleteID);
  const [cachedPlans, setCachedPlans] = useState<Awaited<ReturnType<typeof plans>>>([]);
  const query = useQuery({ queryKey: ['active-plan', athleteID], queryFn: async () => { try { return await api.activePlan(athleteID!); } catch { return localPlan(db, athleteID!); } }, enabled: Boolean(athleteID) });
  useEffect(() => { if (athleteID) void plans(db, athleteID).then(setCachedPlans); }, [athleteID, db, syncing, query.data]);
  const plan = query.data;
  const drafts = cachedPlans.filter((item) => item.status === 'draft');
  return <ScrollView contentContainerStyle={styles.content}><Text style={styles.title}>Plan</Text><SyncStatus syncing={syncing} pending={pending}/>{plan ? <><Text style={styles.planTitle}>{plan.title}</Text><Text>{plan.start_date ?? 'No start'} – {plan.end_date ?? 'No end'}</Text><Text>{String(plan.goal.summary ?? 'Active training plan')}</Text>{plan.sessions.map((session) => <Link key={session.session_id} href={{ pathname: '/planned-session/[id]', params: { id: session.session_id } }} asChild><View style={styles.session}><Text style={styles.sessionTitle}>{session.date} · {session.title}</Text><Text>{session.status} · {session.steps.length} steps</Text></View></Link>)}</> : <View style={styles.card}><Text>No active plan yet.</Text><Button title="Refresh" onPress={() => void query.refetch()} /></View>}{drafts.map((draft) => <View key={draft.plan_id} style={styles.draft}><Text style={styles.sessionTitle}>Draft · {draft.title}</Text><Text>{draft.sessions.length} sessions</Text><Button title="Publish draft" onPress={() => void api.publishPlan(athleteID!, draft.plan_id).then(async (published) => { await savePlan(db, published); setCachedPlans((current) => current.map((item) => item.plan_id === published.plan_id ? published : item)); await query.refetch(); })} /></View>)}</ScrollView>;
}
const styles = StyleSheet.create({ content: { padding: 20, gap: 12 }, title: { fontSize: 32, fontWeight: '700' }, planTitle: { fontSize: 22, fontWeight: '600' }, session: { padding: 16, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: '#D0D5DD', gap: 4 }, sessionTitle: { fontWeight: '600' }, card: { padding: 18, backgroundColor: '#F2F4F7', gap: 12 }, draft: { padding: 16, backgroundColor: '#FFF4E5', gap: 6 } });
