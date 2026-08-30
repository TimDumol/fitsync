import { useQuery } from '@tanstack/react-query';
import { useLocalSearchParams } from 'expo-router';
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../../../src/auth/AuthProvider';

export default function WorkoutSummary(): JSX.Element {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { api, selectedAthleteID, me } = useAuth();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? '';
  const query = useQuery({ queryKey: ['workout', athleteID, id], queryFn: () => api.workout(athleteID, id), enabled: Boolean(athleteID && id) });
  if (query.isLoading) return <ActivityIndicator />;
  if (!query.data) return <View><Text>Workout is pending sync.</Text></View>;
  const workout = query.data;
  return <ScrollView contentContainerStyle={styles.content}><Text style={styles.title}>{workout.title}</Text><Text>{workout.date} · {workout.status}</Text><Text>{workout.entries.length} entries{workout.duration_minutes ? ` · ${workout.duration_minutes} minutes` : ''}</Text>{workout.comparison?.steps.map((step) => <View key={step.planned_step_id} style={styles.row}><Text style={styles.rowTitle}>{step.exercise_name ?? step.exercise_key}</Text><Text>Prescribed {step.planned_sets ?? 0} sets · Actual {step.actual_sets ?? 0} sets</Text></View>)}</ScrollView>;
}
const styles = StyleSheet.create({ content: { padding: 20, gap: 12 }, title: { fontSize: 28, fontWeight: '700' }, row: { padding: 14, backgroundColor: '#F2F4F7', gap: 4 }, rowTitle: { fontWeight: '600' } });
