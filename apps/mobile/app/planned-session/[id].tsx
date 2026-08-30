import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { Button, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../../src/auth/AuthProvider';
import { useSQLiteContext } from 'expo-sqlite';
import { deleteDraft, saveDraft } from '../../src/db/repository';
import { enqueueWorkout } from '../../src/sync/outbox';
import { prescriptionToActual } from '../../src/workout/types';

export default function PlannedSessionDetail(): JSX.Element {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const db = useSQLiteContext();
  const queryClient = useQueryClient();
  const { api, selectedAthleteID, me } = useAuth();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? '';
  const plan = useQuery({ queryKey: ['active-plan', athleteID], queryFn: () => api.activePlan(athleteID), enabled: Boolean(athleteID) });
  const session = plan.data?.sessions.find((item) => item.session_id === id);
  if (!session) return <View style={styles.center}><Text>Planned session not found.</Text></View>;
  const start = async () => {
		const workout = prescriptionToActual(session, athleteID);
		await deleteDraft(db, workout.workout_id);
		await saveDraft(db, workout);
		await enqueueWorkout(db, 'save', workout);
    router.push({ pathname: '/workout/[id]', params: { id: workout.workout_id } });
  };
  const skip = async () => { await api.skipSession(athleteID, session.session_id); await queryClient.invalidateQueries({ queryKey: ['active-plan', athleteID] }); };
  return <ScrollView contentContainerStyle={styles.content}><Text style={styles.title}>{session.title}</Text><Text>{session.date} · {session.status}</Text>{session.notes ? <Text>{session.notes}</Text> : null}{session.steps.map((step) => <View key={step.step_id} style={styles.step}><Text style={styles.stepTitle}>{step.sequence_no}. {step.exercise_name ?? step.step_type}</Text>{step.prescription.sets?.map((set, index) => <Text key={`${step.step_id}-${index}`}>Set {index + 1}: {set.reps_min ?? '—'} reps · {set.load_kg ?? '—'} kg · RIR {set.rir ?? '—'}</Text>)}{step.notes ? <Text>{step.notes}</Text> : null}</View>)}{session.status === 'planned' ? <><Button title="Start workout" onPress={() => void start()} /><Button title="Skip session" color="#667085" onPress={() => void skip()} /></> : session.completed_workout_id ? <Button title="View result" onPress={() => router.push({ pathname: '/workout/[id]/summary', params: { id: session.completed_workout_id! } })} /> : null}</ScrollView>;
}
const styles = StyleSheet.create({ content: { padding: 20, gap: 14 }, center: { flex: 1, alignItems: 'center', justifyContent: 'center' }, title: { fontSize: 28, fontWeight: '700' }, step: { padding: 14, backgroundColor: '#F2F4F7', gap: 5 }, stepTitle: { fontWeight: '600' } });
