import { ActivityIndicator, Button, StyleSheet, Text, View } from 'react-native';
import { Redirect } from 'expo-router';
import { useAuth } from '../src/auth/AuthProvider';

export default function SignIn(): JSX.Element {
  const { ready, signedIn, signIn } = useAuth();
  if (ready && signedIn) return <Redirect href="/(tabs)/today" />;
  return <View style={styles.container}><Text style={styles.title}>FitSync</Text><Text style={styles.subtitle}>Log the work. Keep the plan in view.</Text>{!ready ? <ActivityIndicator /> : <Button title="Sign in with Pocket ID" onPress={() => void signIn()} />}</View>;
}
const styles = StyleSheet.create({ container: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 16, padding: 24 }, title: { fontSize: 36, fontWeight: '700' }, subtitle: { color: '#667085', marginBottom: 16 } });
