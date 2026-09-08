import { StyleSheet, Text, View } from 'react-native';
import { colors, radii, spacing } from '../ui/theme';

export function StatusPill({ label, tone = 'neutral' }: { label: string; tone?: 'neutral' | 'success' | 'warm' | 'danger' }): JSX.Element {
  const toneBackground = tone === 'success' ? styles.success : tone === 'warm' ? styles.warm : tone === 'danger' ? styles.danger : styles.neutral;
  const toneDot = tone === 'success' ? styles.successDot : tone === 'warm' ? styles.warmDot : tone === 'danger' ? styles.dangerDot : styles.neutralDot;
  const toneText = tone === 'success' ? styles.successText : tone === 'warm' ? styles.warmText : tone === 'danger' ? styles.dangerText : undefined;
  return <View style={[styles.pill, toneBackground]}>
    <View style={[styles.dot, toneDot]} />
    <Text style={[styles.label, toneText]}>{label}</Text>
  </View>;
}

const styles = StyleSheet.create({
  pill: { flexDirection: 'row', alignItems: 'center', alignSelf: 'flex-start', gap: spacing.xs, paddingVertical: 6, paddingHorizontal: 10, borderRadius: radii.pill },
  neutral: { backgroundColor: colors.surfaceMuted },
  success: { backgroundColor: colors.primarySoft },
  warm: { backgroundColor: colors.warm },
  danger: { backgroundColor: colors.dangerSoft },
  dot: { width: 6, height: 6, borderRadius: 3, backgroundColor: colors.subtle },
  neutralDot: { backgroundColor: colors.muted },
  successDot: { backgroundColor: colors.primary },
  warmDot: { backgroundColor: colors.warmInk },
  dangerDot: { backgroundColor: colors.danger },
  label: { color: colors.muted, fontSize: 12, fontWeight: '700' },
  successText: { color: colors.primaryDark },
  warmText: { color: colors.warmInk },
  dangerText: { color: colors.danger },
});
