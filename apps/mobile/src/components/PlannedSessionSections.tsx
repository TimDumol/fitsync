import { StyleSheet, Text, View } from 'react-native';
import type { PlannedStep, PlannedStepGroup, PrescriptionSet } from '../api/generated';
import { DEFAULT_REST_SECONDS, formatDuration } from '../workout/types';
import { AppIcon } from './AppIcon';
import { colors, radii, spacing } from '../ui/theme';

export function PlannedGroupCard({ group, steps }: { group: PlannedStepGroup; steps: PlannedStep[] }): JSX.Element {
  const members = group.step_sequence_nos.map((sequenceNo) => steps.find((step) => step.sequence_no === sequenceNo)).filter((step): step is PlannedStep => Boolean(step));
  const label = group.label || (members.length === 2 ? 'Superset' : 'Circuit');
  return <View testID={`planned-group-${group.group_id}`} style={styles.groupCard}>
    <View style={styles.groupHeader}><View style={styles.groupTitleCopy}><Text testID={`planned-group-${group.group_id}-label`} style={styles.groupTitle}>{label}</Text><Text style={styles.groupSubtitle}>{group.rounds} rounds · members performed in order</Text></View><View style={styles.roundPill}><Text style={styles.roundPillText}>{group.rounds}×</Text></View></View>
    {members.map((step, memberIndex) => <View key={step.step_id} style={styles.member}><View style={styles.memberHeader}><Text style={styles.memberTag}>{String.fromCharCode(65 + memberIndex)}</Text><Text style={styles.memberTitle}>{step.exercise_name ?? step.step_type}</Text></View>{Array.from({ length: group.rounds }, (_, round) => <PrescriptionRow key={`${step.step_id}-${round}`} label={`${String.fromCharCode(65 + memberIndex)}${round + 1}`} set={step.prescription.sets?.[round]} />)}{step.notes ? <Text style={styles.coachingNote}>{step.notes}</Text> : null}</View>)}
    {group.notes ? <View style={styles.groupNotes}><AppIcon name="note-text-outline" size={17} color={colors.warmInk} /><Text style={styles.groupNotesText}>{group.notes}</Text></View> : null}
  </View>;
}

export function PlannedStepCard({ step }: { step: PlannedStep }): JSX.Element {
  return <View style={styles.step}><View style={styles.stepHeader}><View style={styles.stepNumber}><Text style={styles.stepNumberText}>{step.sequence_no}</Text></View><Text style={styles.stepTitle}>{step.exercise_name ?? step.step_type}</Text></View>{step.prescription.sets?.map((set, index) => <PrescriptionRow key={`${step.step_id}-${index}`} label={`Set ${index + 1}`} set={set} />)}{step.notes ? <Text style={styles.muted}>{step.notes}</Text> : null}</View>;
}

function PrescriptionRow({ label, set }: { label: string; set?: PrescriptionSet }): JSX.Element {
  const restSeconds = set?.rest_seconds;
  return <View style={styles.setRow}><Text style={styles.setLabel}>{label}</Text><Text style={styles.setDetails}>{set?.reps_min ?? '—'} reps · {set?.load_kg ?? '—'} kg · RIR {set?.rir ?? '—'}</Text><Text style={styles.rest}>{restSeconds == null ? `${formatDuration(DEFAULT_REST_SECONDS)} rest` : restSeconds > 0 ? `${formatDuration(restSeconds)} recovery` : 'Next exercise'}</Text></View>;
}

const styles = StyleSheet.create({
  muted: { color: colors.muted, fontSize: 13, lineHeight: 20 },
  step: { gap: spacing.md, padding: spacing.lg, borderRadius: radii.md, backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.line },
  stepHeader: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minWidth: 0 },
  stepNumber: { width: 30, height: 30, alignItems: 'center', justifyContent: 'center', borderRadius: 10, backgroundColor: colors.primarySoft },
  stepNumberText: { color: colors.primaryDark, fontWeight: '800' },
  stepTitle: { flex: 1, color: colors.ink, fontSize: 16, fontWeight: '800' },
  groupCard: { gap: spacing.md, padding: spacing.lg, borderRadius: radii.lg, backgroundColor: colors.primarySoft, borderWidth: 1, borderColor: '#BFE9D7' },
  groupHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: spacing.md, minWidth: 0 },
  groupTitleCopy: { flex: 1, minWidth: 0 },
  groupTitle: { color: colors.primaryDark, fontSize: 20, fontWeight: '800' },
  groupSubtitle: { color: colors.muted, fontSize: 12, marginTop: 2 },
  roundPill: { minWidth: 40, paddingVertical: 7, paddingHorizontal: 8, alignItems: 'center', borderRadius: radii.pill, backgroundColor: colors.white },
  roundPillText: { color: colors.primaryDark, fontWeight: '800' },
  member: { gap: spacing.sm, padding: spacing.md, borderRadius: radii.md, backgroundColor: colors.surface },
  memberHeader: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minWidth: 0 },
  memberTag: { width: 26, height: 26, textAlign: 'center', textAlignVertical: 'center', borderRadius: 8, backgroundColor: colors.primary, color: colors.white, fontWeight: '800', overflow: 'hidden' },
  memberTitle: { flex: 1, minWidth: 0, color: colors.ink, fontWeight: '800' },
  groupNotes: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.sm, padding: spacing.sm, borderRadius: radii.sm, backgroundColor: colors.warm },
  groupNotesText: { flex: 1, color: colors.warmInk, lineHeight: 19 },
  coachingNote: { color: colors.muted, fontSize: 13, lineHeight: 19 },
  setRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingTop: spacing.sm, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: colors.line },
  setLabel: { width: 42, color: colors.muted, fontSize: 12, fontWeight: '700' },
  setDetails: { flex: 1, minWidth: 0, color: colors.ink, fontSize: 13, fontWeight: '700' },
  rest: { flexShrink: 0, color: colors.muted, fontSize: 11 },
});
