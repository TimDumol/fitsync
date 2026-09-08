import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import type { AppIconName } from './AppIcon';
import { AppIcon } from './AppIcon';
import { colors, radii, spacing } from '../ui/theme';

type ButtonVariant = 'primary' | 'secondary' | 'quiet' | 'danger';

export function AppButton({
  label,
  onPress,
  variant = 'primary',
  icon,
  testID,
  disabled = false,
  loading = false,
  fullWidth = true,
}: {
  label: string;
  onPress?: () => void;
  variant?: ButtonVariant;
  icon?: AppIconName;
  testID?: string;
  disabled?: boolean;
  loading?: boolean;
  fullWidth?: boolean;
}): JSX.Element {
  const foreground = variant === 'primary' ? colors.white : variant === 'danger' ? colors.danger : colors.ink;
  return <Pressable
    testID={testID}
    accessibilityRole="button"
    accessibilityState={{ disabled }}
    disabled={disabled || loading}
    onPress={onPress}
    style={({ pressed }) => [
      styles.base,
      fullWidth && styles.fullWidth,
      styles[variant],
      (disabled || loading) && styles.disabled,
      pressed && !(disabled || loading) && styles.pressed,
    ]}
  >
    {loading ? <ActivityIndicator color={foreground} /> : <View style={styles.content}>
      {icon ? <AppIcon name={icon} size={18} color={foreground} /> : null}
      <Text style={[styles.label, { color: foreground }]}>{label}</Text>
    </View>}
  </Pressable>;
}

const styles = StyleSheet.create({
  base: { minHeight: 48, flexShrink: 1, paddingHorizontal: spacing.lg, borderRadius: radii.md, alignItems: 'center', justifyContent: 'center', borderWidth: 1, borderCurve: 'continuous' },
  fullWidth: { width: '100%' },
  content: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: spacing.sm, minWidth: 0, flexShrink: 1 },
  label: { flexShrink: 1, textAlign: 'center', fontSize: 15, fontWeight: '700' },
  primary: { backgroundColor: colors.primary, borderColor: colors.primary },
  secondary: { backgroundColor: colors.surface, borderColor: colors.line },
  quiet: { backgroundColor: colors.surfaceMuted, borderColor: colors.surfaceMuted },
  danger: { backgroundColor: colors.dangerSoft, borderColor: colors.dangerSoft },
  disabled: { opacity: 0.5 },
  pressed: { opacity: 0.82, transform: [{ scale: 0.985 }] },
});
