import { StyleSheet, Text, View } from 'react-native';

export function SyncStatus({ syncing, pending, error }: { syncing: boolean; pending: number; error?: string | null }): JSX.Element {
  if (error) return <View style={styles.error}><Text style={styles.errorTitle}>Sync error</Text><Text selectable style={styles.errorText}>{error}</Text></View>;
  return <View style={styles.row}><Text style={styles.text}>{syncing ? 'Syncing…' : pending > 0 ? `${pending} pending` : 'Synced'}</Text></View>;
}
const styles = StyleSheet.create({
  row: { paddingVertical: 4 },
  text: { color: '#667085', fontSize: 12 },
  error: { gap: 4, padding: 12, borderWidth: 2, borderColor: '#F04438', borderRadius: 10, backgroundColor: '#FEF3F2' },
  errorTitle: { color: '#B42318', fontWeight: '700' },
  errorText: { color: '#7A271A' },
});
