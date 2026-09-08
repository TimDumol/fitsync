import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { Link } from 'expo-router';
import { ActivityIndicator, Alert, Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAuth } from '../../src/auth/AuthProvider';
import { AppButton } from '../../src/components/AppButton';
import { AppIcon } from '../../src/components/AppIcon';
import { dateRangeForDays, muscleSummaryText } from '../../src/analysis/muscles';
import { MuscleOverview } from '../../src/components/MuscleOverview';
import { ScreenHeader } from '../../src/components/ScreenHeader';
import { StatusPill } from '../../src/components/StatusPill';
import { SyncStatus } from '../../src/components/SyncStatus';
import { localWorkouts, useSync } from '../../src/hooks/useSync';
import { useSQLiteContext } from 'expo-sqlite';
import { removePastWorkout } from '../../src/workout/history';
import { colors, radii, spacing } from '../../src/ui/theme';

export default function History(): JSX.Element {
  const { api, me, selectedAthleteID } = useAuth();
  const db = useSQLiteContext();
  const insets = useSafeAreaInsets();
  const queryClient = useQueryClient();
  const athleteID = selectedAthleteID ?? me?.athlete.athlete_id ?? null;
  const [days, setDays] = useState(7);
  const range = useMemo(() => dateRangeForDays(days), [days]);
  const { syncing, pending, error } = useSync(api, athleteID);
  const [deletingWorkoutID, setDeletingWorkoutID] = useState<string | null>(null);
  const query = useInfiniteQuery({
    queryKey: ['workouts', athleteID, range.from, range.to],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      try {
        return await api.workouts(athleteID!, range.from, range.to, 50, pageParam);
      } catch {
        // The sync cache is the source of truth when the API is unavailable.
        const cached = await localWorkouts(db, athleteID!);
        return { items: cached.filter((workout) => workout.date >= range.from && workout.date <= range.to) };
      }
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: Boolean(athleteID),
  });
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const confirmDelete = (workout: (typeof items)[number]) => Alert.alert('Delete workout?', `Remove “${workout.title}” from your history?`, [
    { text: 'Cancel', style: 'cancel' },
    { text: 'Delete', style: 'destructive', onPress: () => void (async () => {
      setDeletingWorkoutID(workout.workout_id);
      try {
        await removePastWorkout(db, api, workout);
        await queryClient.invalidateQueries({ queryKey: ['workout', athleteID, workout.workout_id] });
        await query.refetch();
      } catch (caught) {
        Alert.alert('Could not delete workout', caught instanceof Error ? caught.message : String(caught));
      } finally {
        setDeletingWorkoutID(null);
      }
    })() },
  ]);

  return <View style={[styles.screen, { paddingTop: process.env.EXPO_OS === 'android' ? insets.top : 0 }]}><ScrollView
    testID="history-screen"
    style={styles.scroll}
    contentInsetAdjustmentBehavior="automatic"
    contentContainerStyle={styles.content}
    refreshControl={<RefreshControl refreshing={syncing || query.isRefetching} onRefresh={() => void query.refetch()} />}
  >
    <ScreenHeader eyebrow="Your training log" title="History" subtitle="Spot patterns, celebrate consistency." />
    <SyncStatus syncing={syncing} pending={pending} error={error}/>
    <View style={styles.overviewSection}>
      <View style={styles.sectionHeader}>
        <View style={styles.headerCopy}><Text style={styles.sectionTitle}>Muscles worked</Text><Text style={styles.muted}>Across the past {days} days</Text></View>
        <View style={styles.rangePicker}>{[7, 14, 30].map((option) => <Pressable key={option} testID={`history-range-${option}`} accessibilityRole="button" accessibilityState={{ selected: days === option }} onPress={() => setDays(option)} style={[styles.rangeButton, days === option && styles.rangeButtonSelected]}><Text style={[styles.rangeText, days === option && styles.rangeTextSelected]}>{option}d</Text></Pressable>)}</View>
      </View>
      {!query.isLoading && !query.isError ? <MuscleOverview testID="history-muscle-overview" workouts={items} title={`Past ${days} days`} emptyText="Complete a workout with tagged exercises to see your muscle balance." /> : null}
    </View>
    {query.isLoading ? <ActivityIndicator testID="history-loading" color={colors.primary} /> : query.isError ? <View testID="history-error-state" style={styles.state}><AppIcon name="alert-circle-outline" size={28} color={colors.danger} /><Text style={styles.stateTitle}>Workout history is unavailable.</Text><AppButton label="Try again" icon="refresh" variant="secondary" onPress={() => void query.refetch()} /></View> : items.length === 0 ? <View testID="history-empty-state" style={styles.state}><View style={styles.emptyIcon}><AppIcon name="history" size={26} color={colors.primary} /></View><Text style={styles.stateTitle}>No workouts recorded yet.</Text><Text style={styles.muted}>Completed workouts will appear here.</Text></View> : items.map((workout) => <View key={workout.workout_id} style={styles.row}><Link href={{ pathname: '/workout/[id]/summary', params: { id: workout.workout_id } }} asChild><Pressable testID={`history-workout-row-${workout.workout_id}`} accessibilityRole="button" style={styles.rowLink}><View style={styles.rowIcon}><AppIcon name="dumbbell" size={21} color={colors.primary} /></View><View style={styles.rowCopy}><Text style={styles.rowTitle} numberOfLines={2} ellipsizeMode="tail">{workout.title}</Text><Text style={styles.muted}>{workout.date} · {workout.entries.length} entries · {workout.planned_session_id ? 'Planned' : 'Unplanned'}</Text><Text style={styles.muscleLine}>Muscles: {muscleSummaryText(workout)}</Text></View><View style={styles.statusWrap}><StatusPill label={workout.status} tone={workout.status === 'completed' ? 'success' : 'neutral'} /></View></Pressable></Link><Pressable testID={`history-delete-workout-${workout.workout_id}`} accessibilityRole="button" accessibilityLabel={`Delete ${workout.title}`} disabled={deletingWorkoutID === workout.workout_id} onPress={() => confirmDelete(workout)} style={({ pressed }) => [styles.deleteButton, pressed && styles.pressed]}>{deletingWorkoutID === workout.workout_id ? <ActivityIndicator color={colors.danger} /> : <AppIcon name="trash-can-outline" size={20} color={colors.danger} />}</Pressable></View>)}
    {query.isFetchingNextPage ? <ActivityIndicator testID="history-loading-more" color={colors.primary} /> : query.hasNextPage ? <AppButton testID="history-load-more" label="Load more" icon="arrow-down" variant="secondary" onPress={() => void query.fetchNextPage()} /> : null}
  </ScrollView></View>;
}
const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  scroll: { flex: 1 },
  content: { padding: spacing.xl, paddingBottom: spacing.xxxl, gap: spacing.lg, backgroundColor: colors.background },
  overviewSection: { gap: spacing.md },
  sectionHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', gap: 12, minWidth: 0 },
  headerCopy: { flex: 1, minWidth: 0, gap: spacing.xs },
  sectionTitle: { color: colors.ink, fontSize: 20, fontWeight: '800' },
  rangePicker: { flexDirection: 'row', gap: 4, padding: 3, backgroundColor: colors.surfaceMuted, borderRadius: radii.sm },
  rangeButton: { minWidth: 38, paddingVertical: 7, paddingHorizontal: 8, borderRadius: 7, alignItems: 'center' },
  rangeButtonSelected: { backgroundColor: colors.ink },
  rangeText: { color: colors.muted, fontSize: 12, fontWeight: '700' },
  rangeTextSelected: { color: colors.white },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minWidth: 0, padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, borderCurve: 'continuous' },
  rowLink: { flex: 1, flexShrink: 1, flexDirection: 'row', alignItems: 'center', gap: spacing.md, minWidth: 0 },
  deleteButton: { width: 42, height: 42, flexShrink: 0, alignItems: 'center', justifyContent: 'center', borderRadius: radii.sm, backgroundColor: colors.dangerSoft },
  pressed: { opacity: 0.72 },
  rowIcon: { width: 42, height: 42, alignItems: 'center', justifyContent: 'center', borderRadius: radii.sm, backgroundColor: colors.primarySoft },
  rowCopy: { flex: 1, minWidth: 0, gap: spacing.xs },
  rowTitle: { flexShrink: 1, minWidth: 0, color: colors.ink, fontSize: 15, fontWeight: '800' },
  statusWrap: { flexShrink: 0 },
  muscleLine: { flexShrink: 1, color: colors.muted, fontSize: 12 },
  state: { alignItems: 'center', gap: spacing.md, padding: spacing.xxl, borderRadius: radii.lg, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line },
  stateTitle: { color: colors.ink, fontSize: 16, fontWeight: '800', textAlign: 'center' },
  emptyIcon: { width: 56, height: 56, alignItems: 'center', justifyContent: 'center', borderRadius: 18, backgroundColor: colors.primarySoft },
  muted: { color: colors.muted },
});
