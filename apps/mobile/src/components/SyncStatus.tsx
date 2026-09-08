import { StyleSheet, Text, View } from 'react-native';
import { AppIcon } from './AppIcon';
import { colors, radii, spacing } from '../ui/theme';

export function SyncStatus({ syncing, pending, error }: { syncing: boolean; pending: number; error?: string | null }): JSX.Element {
  if (error) return <View style={styles.error}><AppIcon name="alert-circle-outline" size={18} color={colors.danger} /><View style={styles.errorCopy}><Text style={styles.errorTitle}>Sync error</Text><Text selectable style={styles.errorText}>{error}</Text></View></View>;
  const label = syncing ? 'Syncing…' : pending > 0 ? `${pending} pending` : 'Synced';
  return <View style={styles.row}><View style={[styles.dot, syncing && styles.dotActive]} /><Text style={styles.text}>{label}</Text></View>;
}
const styles = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', alignSelf: 'flex-start', gap: spacing.sm, paddingVertical: spacing.xs },
  dot: { width: 7, height: 7, borderRadius: 4, backgroundColor: colors.primary },
  dotActive: { backgroundColor: colors.warmInk },
  text: { color: colors.muted, fontSize: 12, fontWeight: '700' },
  error: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.sm, padding: spacing.md, borderWidth: 1, borderColor: '#F5B8B3', borderRadius: radii.md, backgroundColor: colors.dangerSoft },
  errorCopy: { flex: 1, gap: spacing.xs },
  errorTitle: { color: colors.danger, fontWeight: '800' },
  errorText: { color: '#8E2D27' },
});
