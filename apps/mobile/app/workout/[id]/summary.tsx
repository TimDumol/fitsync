import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { ActivityIndicator, Alert, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAuth } from '../../../src/auth/AuthProvider';
import { useSQLiteContext } from 'expo-sqlite';
import { AppIcon } from '../../../src/components/AppIcon';
import { localWorkout } from '../../../src/hooks/useSync';
import type { WorkoutEntry } from '../../../src/api/generated';
import { muscleGroupLabel, muscleGroupsForEntry } from '../../../src/analysis/muscles';
import { MuscleOverview } from '../../../src/components/MuscleOverview';
import { ScreenHeader } from '../../../src/components/ScreenHeader';
import { StatusPill } from '../../../src/components/StatusPill';
import { AppButton } from '../../../src/components/AppButton';
import { removePastWorkout } from '../../../src/workout/history';
import { colors, radii, spacing } from '../../../src/ui/theme';

export default function WorkoutSummary(): JSX.Element {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { api, selectedAthleteID, me } = useAuth();
  const db = useSQLiteContext();
  const insets = useSafeAreaInsets();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? '';
  const query = useQuery({ queryKey: ['workout', athleteID, id], queryFn: async () => { try { return await api.workout(athleteID, id); } catch { return localWorkout(db, athleteID, id); } }, enabled: Boolean(athleteID && id) });
  if (query.isLoading) return <View style={styles.center}><ActivityIndicator testID="workout-summary-loading" color={colors.primary} /></View>;
  if (!query.data) return <View testID="workout-summary-screen" style={styles.center}><AppIcon name="cloud-upload-outline" size={30} color={colors.primary} /><Text style={styles.emptyTitle}>Workout is pending sync.</Text><Text style={styles.muted}>We’ll show the full summary when it reaches your account.</Text></View>;
  const workout = query.data;
  const deletePastWorkout = () => Alert.alert('Delete workout?', `Remove “${workout.title}” from your history?`, [
    { text: 'Cancel', style: 'cancel' },
    { text: 'Delete', style: 'destructive', onPress: () => void (async () => {
      try {
        await removePastWorkout(db, api, workout);
        await queryClient.invalidateQueries({ queryKey: ['workouts', athleteID] });
        router.back();
      } catch (caught) {
        Alert.alert('Could not delete workout', caught instanceof Error ? caught.message : String(caught));
      }
    })() },
  ]);
  const entries = [...workout.entries].sort((a, b) => a.sequence_no - b.sequence_no);
  return <View style={[styles.screen, { paddingTop: process.env.EXPO_OS === 'android' ? insets.top : 0 }]}><ScrollView testID="workout-summary-screen" style={styles.scroll} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
    <ScreenHeader eyebrow={workout.date} title={workout.title} subtitle={workout.duration_minutes != null ? `${workout.duration_minutes} minutes · ${workout.entries.length} entries` : `${workout.entries.length} entries recorded`} />
    <StatusPill label={workout.status} tone={workout.status === 'completed' ? 'success' : 'neutral'} />
    {workout.status === 'completed' ? <AppButton testID="delete-workout-button" label="Delete workout" icon="trash-can-outline" variant="danger" onPress={deletePastWorkout} /> : null}
    <Text testID="workout-summary-meta" style={styles.accessibleMeta}>{workout.date} · {workout.status}{workout.duration_minutes != null ? ` · ${workout.duration_minutes} minutes` : ''}</Text>
    <MuscleOverview testID="workout-muscle-overview" workouts={workout} title="Muscles worked" subtitle="Working-set focus for this routine" />
    {workout.notes ? <View style={styles.notes}><Text style={styles.sectionTitle}>Notes</Text><Text>{workout.notes}</Text></View> : null}
    <Text style={styles.sectionTitle}>Recorded sets</Text>
    {entries.length === 0 ? <Text style={styles.muted}>No entries recorded.</Text> : entries.map((entry) => <View key={entry.entry_id} testID={`workout-summary-entry-${entry.sequence_no}`} style={styles.row}><View style={styles.rowHeader}><View style={styles.rowIcon}><AppIcon name={entry.entry_type === 'set' ? 'dumbbell' : 'note-text-outline'} size={18} color={colors.primary} /></View><Text style={styles.rowTitle}>{entry.exercise_name ?? entry.user_label ?? entry.entry_type}{entry.set_number ? ` · Set ${entry.set_number}` : ''}</Text></View><Text style={styles.detail}>{entryDetails(entry)}</Text>{muscleGroupsForEntry(entry).length > 0 ? <View style={styles.chips}>{muscleGroupsForEntry(entry).map((group) => <Text key={group} style={styles.chip}>{muscleGroupLabel(group)}</Text>)}</View> : null}</View>)}
    {workout.comparison?.steps.length ? <><Text style={styles.sectionTitle}>Against plan</Text>{workout.comparison.steps.map((step) => <View key={step.planned_step_id} style={styles.row}><Text style={styles.rowTitle}>{step.exercise_name ?? step.exercise_key}</Text><Text style={styles.detail}>Prescribed {step.planned_sets ?? 0} sets · Actual {step.actual_sets ?? 0} sets</Text></View>)}</> : null}
  </ScrollView></View>;
}

function entryDetails(entry: WorkoutEntry): string {
  if (entry.entry_type === 'rest') return entry.duration_seconds != null ? `${entry.duration_seconds} seconds` : 'Rest';
  if (entry.entry_type === 'note') return entry.notes ?? 'Note';
  const details = [
    entry.reps != null ? `${entry.reps} reps` : null,
    entry.load_kg != null ? `${entry.load_kg} kg` : entry.load_description,
    entry.is_bodyweight ? 'Bodyweight' : null,
    entry.rir != null ? `RIR ${entry.rir}` : null,
    entry.duration_seconds != null ? `${entry.duration_seconds} seconds` : null,
  ].filter((item): item is string => Boolean(item));
  return details.length > 0 ? details.join(' · ') : 'No measurements recorded';
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  scroll: { flex: 1 },
  content: { padding: spacing.xl, paddingBottom: spacing.xxxl, gap: spacing.lg, backgroundColor: colors.background },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.md, padding: spacing.xl, backgroundColor: colors.background },
  emptyTitle: { color: colors.ink, fontSize: 17, fontWeight: '800' },
  accessibleMeta: { position: 'absolute', width: 1, height: 1, opacity: 0 },
  sectionTitle: { color: colors.ink, fontSize: 20, fontWeight: '800', marginTop: spacing.sm },
  notes: { gap: spacing.sm, padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.warm, borderWidth: 1, borderColor: '#F8D6AE' },
  row: { gap: spacing.sm, padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line },
  rowHeader: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  rowIcon: { width: 32, height: 32, alignItems: 'center', justifyContent: 'center', borderRadius: 9, backgroundColor: colors.primarySoft },
  rowTitle: { flex: 1, color: colors.ink, fontWeight: '800' },
  detail: { color: colors.muted, lineHeight: 20 },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 5, marginTop: 2 },
  chip: { paddingVertical: 4, paddingHorizontal: 8, borderRadius: radii.pill, backgroundColor: colors.primarySoft, color: colors.primaryDark, fontSize: 11, overflow: 'hidden' },
  muted: { color: colors.muted },
});
