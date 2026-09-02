import { ActivityIndicator, Button, StyleSheet, Text, View } from 'react-native';
import { Redirect } from 'expo-router';
import { useAuth } from '../src/auth/AuthProvider';

export default function SignIn(): JSX.Element {
  const { ready, signedIn, signIn, authError, clearAuthError } = useAuth();
  if (ready && signedIn) return <Redirect href="/(tabs)/today" />;
  return <View style={styles.container}>
    <Text style={styles.title}>FitSync</Text>
    <Text style={styles.subtitle}>Log the work. Keep the plan in view.</Text>
    {authError ? <View accessible accessibilityRole="alert" style={styles.error}>
      <Text style={styles.errorTitle}>Sign-in error</Text>
      <Text selectable style={styles.errorText}>{authError}</Text>
      <Button title="Dismiss" color="#B42318" onPress={clearAuthError} />
    </View> : null}
    {!ready ? <ActivityIndicator /> : <Button title={authError ? 'Try sign-in again' : 'Sign in with Pocket ID'} onPress={() => void signIn()} />}
  </View>;
}
const styles = StyleSheet.create({
  container: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 16, padding: 24 },
  title: { fontSize: 36, fontWeight: '700' },
  subtitle: { color: '#667085', marginBottom: 16 },
  error: { width: '100%', gap: 8, padding: 16, borderWidth: 2, borderColor: '#F04438', borderRadius: 12, backgroundColor: '#FEF3F2' },
  errorTitle: { color: '#B42318', fontSize: 18, fontWeight: '700' },
  errorText: { color: '#7A271A', lineHeight: 21 },
});
