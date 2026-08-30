import { Button, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSQLiteContext } from 'expo-sqlite';
import { useAuth } from '../../src/auth/AuthProvider';
import { setSelectedAthleteID as persistSelectedAthleteID } from '../../src/db/repository';
import { useSync } from '../../src/hooks/useSync';

export default function Settings(): JSX.Element {
  const db = useSQLiteContext();
  const { me, api, selectedAthleteID, setSelectedAthleteID, logout } = useAuth();
  useSync(api, selectedAthleteID);
  return <ScrollView contentContainerStyle={styles.content}><Text style={styles.title}>Settings</Text><Text>Timezone: {me?.athlete.timezone}</Text><Text>Units: metric</Text><Text style={styles.heading}>Athlete</Text>{me?.accessible_athletes.map((athlete) => <Button key={athlete.athlete_id} title={`${athlete.handle}${athlete.athlete_id === selectedAthleteID ? ' ✓' : ''}`} onPress={() => { setSelectedAthleteID(athlete.athlete_id); void persistSelectedAthleteID(db, athlete.athlete_id); }} />)}<Button title="Log out" color="#D92D20" onPress={() => void logout()} /></ScrollView>;
}
const styles = StyleSheet.create({ content: { padding: 20, gap: 14 }, title: { fontSize: 32, fontWeight: '700' }, heading: { fontWeight: '600', marginTop: 12 } });
