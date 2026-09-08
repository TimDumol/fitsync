import { StyleSheet, Text, View } from 'react-native';
import { colors, spacing } from '../ui/theme';

export function ScreenHeader({ eyebrow, title, subtitle }: { eyebrow?: string; title: string; subtitle?: string }): JSX.Element {
  return <View style={styles.container}>
    {eyebrow ? <Text style={styles.eyebrow}>{eyebrow}</Text> : null}
    <Text style={styles.title}>{title}</Text>
    {subtitle ? <Text style={styles.subtitle}>{subtitle}</Text> : null}
  </View>;
}

const styles = StyleSheet.create({
  container: { gap: spacing.xs, minWidth: 0 },
  eyebrow: { color: colors.primary, fontSize: 12, fontWeight: '800', letterSpacing: 1.1, textTransform: 'uppercase' },
  title: { flexShrink: 1, color: colors.ink, fontSize: 34, fontWeight: '800', letterSpacing: -0.7 },
  subtitle: { flexShrink: 1, color: colors.muted, fontSize: 15, lineHeight: 22 },
});
