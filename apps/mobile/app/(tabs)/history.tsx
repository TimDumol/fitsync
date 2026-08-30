import { useQuery } from '@tanstack/react-query';
import { Link } from 'expo-router';
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../../src/auth/AuthProvider';
import { localWorkouts, useSync } from '../../src/hooks/useSync';
import { useSQLiteContext } from 'expo-sqlite';

export default function History(): JSX.Element {
  const { api, me, selectedAthleteID } = useAuth();
  const db = useSQLiteContext();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? null;
  useSync(api, athleteID);
  const query = useQuery({ queryKey: ['workouts', athleteID], queryFn: async () => { try { return await api.workouts(athleteID!); } catch { return { items: await localWorkouts(db, athleteID!) }; } }, enabled: Boolean(athleteID) });
  return <ScrollView contentContainerStyle={styles.content}><Text style={styles.title}>History</Text>{query.isLoading ? <ActivityIndicator /> : query.data?.items.map((workout) => <Link key={workout.workout_id} href={{ pathname: '/workout/[id]/summary', params: { id: workout.workout_id } }} asChild><View style={styles.row}><Text style={styles.rowTitle}>{workout.date} · {workout.title}</Text><Text>{workout.planned_session_id ? 'Planned' : 'Unplanned'} · {workout.entries.length} entries · {workout.status}</Text></View></Link>)}</ScrollView>;
}
const styles = StyleSheet.create({ content: { padding: 20, gap: 12 }, title: { fontSize: 32, fontWeight: '700' }, row: { padding: 16, backgroundColor: '#F2F4F7', gap: 5 }, rowTitle: { fontWeight: '600' } });
