import { StyleSheet, Text, View } from 'react-native';

export function SyncStatus({ syncing, pending }: { syncing: boolean; pending: number }): JSX.Element {
  return <View style={styles.row}><Text style={styles.text}>{syncing ? 'Syncing…' : pending > 0 ? `${pending} pending` : 'Synced'}</Text></View>;
}
const styles = StyleSheet.create({ row: { paddingVertical: 4 }, text: { color: '#667085', fontSize: 12 } });
