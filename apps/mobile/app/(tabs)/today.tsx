import { useQuery } from '@tanstack/react-query';
import { Link } from 'expo-router';
import { Button, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useEffect, useState } from 'react';
import { useAuth } from '../../src/auth/AuthProvider';
import { SyncStatus } from '../../src/components/SyncStatus';
import { useSync, localPlan } from '../../src/hooks/useSync';
import { useSQLiteContext } from 'expo-sqlite';
import { activeDrafts } from '../../src/db/repository';
import type { Workout } from '../../src/api/generated';

function localDate(timezone: string): string { return new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()); }

export default function Today(): JSX.Element {
  const { signedIn, selectedAthleteID, me, api } = useAuth();
  const db = useSQLiteContext();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? null;
  const { syncing, pending } = useSync(api, athleteID);
  const [draft, setDraft] = useState<Workout | null>(null);
  useEffect(() => { if (athleteID) void activeDrafts(db, athleteID).then((items) => setDraft(items[0] ?? null)); }, [athleteID, db, syncing]);
  const today = localDate(me?.athlete.timezone ?? 'UTC');
  const plan = useQuery({ queryKey: ['active-plan', athleteID], queryFn: async () => { try { return await api.activePlan(athleteID!); } catch { return localPlan(db, athleteID!); } }, enabled: signedIn && Boolean(athleteID) });
  const session = plan.data?.sessions.find((item) => item.date === today);
  if (!signedIn) return <View />;
  return <ScrollView contentContainerStyle={styles.content} refreshControl={<RefreshControl refreshing={syncing || plan.isRefetching} onRefresh={() => void plan.refetch()} />}><Text style={styles.eyebrow}>{today}</Text><Text style={styles.title}>Today</Text><SyncStatus syncing={syncing} pending={pending}/>{draft ? <View style={styles.resume}><Text style={styles.cardTitle}>Workout in progress</Text><Text>{draft.title} · saved locally</Text><Link href={{ pathname: '/workout/[id]', params: { id: draft.workout_id } }} asChild><Button title="Resume workout" /></Link></View> : null}{session ? <View style={styles.card}><Text style={styles.cardTitle}>{session.title}</Text><Text>{session.steps.length} prescribed steps · {session.targets.duration_minutes ? `${String(session.targets.duration_minutes)} min` : 'strength session'}</Text><Link href={{ pathname: '/planned-session/[id]', params: { id: session.session_id } }} asChild><Button title={session.status === 'completed' ? 'View result' : 'Start workout'} /></Link></View> : <View style={styles.card}><Text style={styles.cardTitle}>Rest day</Text><Text>No planned session today.</Text></View>}</ScrollView>;
}
const styles = StyleSheet.create({ content: { padding: 20, gap: 12 }, eyebrow: { color: '#667085' }, title: { fontSize: 32, fontWeight: '700' }, card: { gap: 12, padding: 18, borderRadius: 14, backgroundColor: '#F2F4F7' }, resume: { gap: 12, padding: 18, borderRadius: 14, backgroundColor: '#FFF4E5' }, cardTitle: { fontSize: 20, fontWeight: '600' } });
