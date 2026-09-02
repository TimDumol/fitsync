import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';
import { Redirect } from 'expo-router';
import { useAuth } from '../src/auth/AuthProvider';

export default function OAuthRedirect(): JSX.Element {
  const { signedIn, authError } = useAuth();
  if (signedIn) return <Redirect href="/(tabs)/today" />;
  if (authError) return <Redirect href="/sign-in" />;
  return <View style={styles.container}><ActivityIndicator /><Text>Completing Pocket ID sign-in…</Text></View>;
}

const styles = StyleSheet.create({
  container: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12 },
});
