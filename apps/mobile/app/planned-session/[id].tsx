import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAuth } from '../../src/auth/AuthProvider';
import { useSQLiteContext } from 'expo-sqlite';
import { AppButton } from '../../src/components/AppButton';
import { AppIcon } from '../../src/components/AppIcon';
import { PlannedGroupCard, PlannedStepCard } from '../../src/components/PlannedSessionSections';
import { ScreenHeader } from '../../src/components/ScreenHeader';
import { StatusPill } from '../../src/components/StatusPill';
import { deleteDraft, saveActiveTimer, saveDraft, workouts } from '../../src/db/repository';
import { enqueueWorkout } from '../../src/sync/outbox';
import { prescriptionToActual } from '../../src/workout/types';
import type { PlannedStepGroup } from '../../src/api/generated';
import { colors, radii, spacing } from '../../src/ui/theme';

export default function PlannedSessionDetail(): JSX.Element {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const db = useSQLiteContext();
  const insets = useSafeAreaInsets();
  const queryClient = useQueryClient();
  const { api, selectedAthleteID, me } = useAuth();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? '';
  const plan = useQuery({ queryKey: ['active-plan', athleteID], queryFn: () => api.activePlan(athleteID), enabled: Boolean(athleteID) });
  const session = plan.data?.sessions.find((item) => item.session_id === id);
  if (!session) return <View style={styles.center}><Text>Planned session not found.</Text></View>;
  const start = async () => {
		let previousWorkouts;
		try {
			previousWorkouts = (await api.workouts(athleteID, undefined, session.date, 50)).items;
		} catch {
			previousWorkouts = (await workouts(db, athleteID)).filter((workout) => workout.date <= session.date);
		}
		const workout = prescriptionToActual(session, athleteID, session.date, previousWorkouts);
		await deleteDraft(db, workout.workout_id);
		await saveActiveTimer(db, workout.workout_id, 0);
		await saveDraft(db, workout);
		await enqueueWorkout(db, 'save', workout);
    router.push({ pathname: '/workout/[id]', params: { id: workout.workout_id } });
  };
  const skip = async () => { await api.skipSession(athleteID, session.session_id); await queryClient.invalidateQueries({ queryKey: ['active-plan', athleteID] }); };
  const groupBySequence = new Map<number, PlannedStepGroup>();
  for (const group of session.groups ?? []) for (const sequenceNo of group.step_sequence_nos) groupBySequence.set(sequenceNo, group);
  const renderedSteps: JSX.Element[] = [];
  const emittedGroups = new Set<string>();
  for (const step of [...session.steps].sort((a, b) => a.sequence_no - b.sequence_no)) {
    const group = groupBySequence.get(step.sequence_no);
    if (group) {
      if (emittedGroups.has(group.group_id) || group.step_sequence_nos[0] !== step.sequence_no) continue;
      emittedGroups.add(group.group_id);
      renderedSteps.push(<PlannedGroupCard key={group.group_id} group={group} steps={session.steps} />);
    } else {
      renderedSteps.push(<PlannedStepCard key={step.step_id} step={step} />);
    }
  }
  return <View style={[styles.screen, { paddingTop: process.env.EXPO_OS === 'android' ? insets.top : 0 }]}><ScrollView testID="planned-session-screen" style={styles.scroll} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
    <ScreenHeader eyebrow={session.date} title={session.title} subtitle={`${session.steps.length} exercises · ${session.status}`} />
    <StatusPill label={session.status} tone={session.status === 'completed' ? 'success' : session.status === 'skipped' ? 'warm' : 'neutral'} />
    {session.notes ? <View style={styles.notes}><AppIcon name="note-text-outline" size={20} color={colors.warmInk} /><Text style={styles.notesText}>{session.notes}</Text></View> : null}
    <View style={styles.sectionHeader}><Text style={styles.sectionTitle}>Session details</Text><Text style={styles.muted}>{session.steps.length} steps</Text></View>
    <View style={styles.steps}>{renderedSteps}</View>
    {session.status === 'planned' ? <View style={styles.actions}><AppButton testID="session-start-workout-button" label="Start workout" icon="play" onPress={() => void start()} /><AppButton testID="session-skip-button" label="Skip session" icon="skip-next" variant="quiet" onPress={() => void skip()} /></View> : session.completed_workout_id ? <AppButton testID="session-view-result-button" label="View result" icon="arrow-right" variant="secondary" onPress={() => router.push({ pathname: '/workout/[id]/summary', params: { id: session.completed_workout_id! } })} /> : null}
  </ScrollView></View>;
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  scroll: { flex: 1 },
  content: { padding: spacing.xl, paddingBottom: spacing.xxxl, gap: spacing.lg, backgroundColor: colors.background },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', backgroundColor: colors.background },
  sectionHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: spacing.sm },
  sectionTitle: { color: colors.ink, fontSize: 20, fontWeight: '800' },
  muted: { color: colors.muted, fontSize: 13, lineHeight: 20 },
  notes: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.sm, padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.warm, borderWidth: 1, borderColor: '#F8D6AE' },
  notesText: { flex: 1, color: colors.warmInk, lineHeight: 20 },
  steps: { gap: spacing.sm },
  step: { gap: spacing.md, padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line },
  stepHeader: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  stepNumber: { width: 30, height: 30, alignItems: 'center', justifyContent: 'center', borderRadius: 10, backgroundColor: colors.primarySoft },
  stepNumberText: { color: colors.primaryDark, fontWeight: '800' },
  stepTitle: { flex: 1, color: colors.ink, fontSize: 16, fontWeight: '800' },
  actions: { gap: spacing.sm },
});
