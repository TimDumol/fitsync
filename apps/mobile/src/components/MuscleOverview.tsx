import { StyleSheet, Text, View } from 'react-native';
import type { Workout } from '../api/generated';
import { summarizeMuscles } from '../analysis/muscles';
import { colors, radii, spacing } from '../ui/theme';

interface MuscleOverviewProps {
  workouts: Workout | Workout[];
  title?: string;
  subtitle?: string;
  emptyText?: string;
  testID?: string;
}

export function MuscleOverview({ workouts, title = 'Muscles worked', subtitle, emptyText = 'No tagged muscle groups in this workout.', testID }: MuscleOverviewProps): JSX.Element {
  const values = Array.isArray(workouts) ? workouts : [workouts];
  const summary = summarizeMuscles(workouts);
  const trainingDays = new Set(values.filter((workout) => workout.status !== 'discarded').map((workout) => workout.date)).size;
  const workingSets = values.filter((workout) => workout.status !== 'discarded').reduce((total, workout) => total + workout.entries.filter((entry) => entry.entry_type === 'set' && !entry.is_warmup).length, 0);

  return <View testID={testID} style={styles.card}>
    <View style={styles.header}>
      <View style={styles.headerCopy}>
        <Text style={styles.title}>{title}</Text>
        {subtitle ? <Text style={styles.subtitle}>{subtitle}</Text> : null}
      </View>
      <View style={styles.stats}>
        <Text style={styles.statValue}>{workingSets}</Text>
        <Text style={styles.statLabel}>working sets</Text>
      </View>
    </View>
    {summary.length === 0 ? <Text style={styles.empty}>{emptyText}</Text> : <View style={styles.bars}>
      {summary.map((item) => <View key={item.group} testID={`${testID ?? 'muscle-overview'}-${item.group}`} style={styles.barRow}>
        <View style={styles.barLabelRow}>
          <View style={styles.labelWrap}><View style={[styles.dot, { backgroundColor: item.color }]} /><Text style={styles.label}>{item.label}</Text></View>
          <Text style={styles.count}>{item.sets} {item.sets === 1 ? 'set' : 'sets'}</Text>
        </View>
        <View style={styles.track}><View style={[styles.fill, { width: `${Math.max(8, item.percentage)}%`, backgroundColor: item.color }]} /></View>
      </View>)}
    </View>}
    {values.length > 1 ? <Text style={styles.footer}>{trainingDays} {trainingDays === 1 ? 'training day' : 'training days'} · {summary.length} muscle groups</Text> : null}
  </View>;
}

const styles = StyleSheet.create({
  card: { padding: spacing.lg, borderRadius: radii.lg, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line, gap: spacing.lg, borderCurve: 'continuous' },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-start', gap: spacing.md },
  headerCopy: { flex: 1, gap: spacing.xs },
  title: { fontSize: 20, fontWeight: '800', color: colors.ink },
  subtitle: { color: colors.muted, fontSize: 13 },
  stats: { alignItems: 'flex-end' },
  statValue: { fontSize: 20, fontWeight: '800', color: colors.ink },
  statLabel: { color: colors.muted, fontSize: 11 },
  bars: { gap: spacing.md },
  barRow: { gap: 5 },
  barLabelRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  labelWrap: { flex: 1, minWidth: 0, flexDirection: 'row', alignItems: 'center', gap: 7 },
  dot: { width: 9, height: 9, borderRadius: 5 },
  label: { flexShrink: 1, color: colors.ink, fontWeight: '700' },
  count: { flexShrink: 0, color: colors.muted, fontSize: 12 },
  track: { height: 8, borderRadius: 4, backgroundColor: colors.surfaceMuted, overflow: 'hidden' },
  fill: { height: '100%', borderRadius: 4 },
  empty: { color: colors.muted, lineHeight: 20 },
  footer: { color: colors.muted, fontSize: 12 },
});
