import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';
import { Redirect } from 'expo-router';
import { useAuth } from '../src/auth/AuthProvider';
import { AppButton } from '../src/components/AppButton';
import { AppIcon } from '../src/components/AppIcon';
import { colors, radii, spacing } from '../src/ui/theme';

export default function SignIn(): JSX.Element {
  const { ready, signedIn, signIn, authError, clearAuthError } = useAuth();
  if (ready && signedIn) return <Redirect href="/(tabs)/today" />;
  return <View testID="sign-in-screen" style={styles.container}>
    <View style={styles.brandMark}><AppIcon name="dumbbell" size={38} color={colors.white} /></View>
    <Text testID="sign-in-title" style={styles.title}>FitSync</Text>
    <Text style={styles.kicker}>Training, in sync</Text>
    <Text style={styles.subtitle}>Log the work. Keep the plan in view.</Text>
    {authError ? <View accessible accessibilityRole="alert" style={styles.error}>
      <View style={styles.errorHeader}><AppIcon name="alert-circle-outline" size={20} color={colors.danger} /><Text style={styles.errorTitle}>Sign-in error</Text></View>
      <Text selectable style={styles.errorText}>{authError}</Text>
      <AppButton label="Dismiss" variant="danger" onPress={clearAuthError} />
    </View> : null}
    {!ready ? <ActivityIndicator color={colors.primary} /> : <AppButton testID="sign-in-button" label={authError ? 'Try sign-in again' : 'Sign in with Pocket ID'} icon="login" onPress={() => void signIn()} />}
  </View>;
}
const styles = StyleSheet.create({
  container: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.sm, padding: spacing.xl, backgroundColor: colors.background },
  brandMark: { width: 76, height: 76, alignItems: 'center', justifyContent: 'center', marginBottom: spacing.md, borderRadius: 26, backgroundColor: colors.primary, transform: [{ rotate: '-8deg' }] },
  title: { color: colors.ink, fontSize: 40, fontWeight: '800', letterSpacing: -1 },
  kicker: { color: colors.primary, fontSize: 13, fontWeight: '800', letterSpacing: 1.3, textTransform: 'uppercase' },
  subtitle: { color: colors.muted, marginTop: spacing.sm, marginBottom: spacing.xl, fontSize: 15 },
  error: { width: '100%', gap: spacing.md, padding: spacing.lg, marginBottom: spacing.sm, borderWidth: 1, borderColor: '#F5B8B3', borderRadius: radii.md, backgroundColor: colors.dangerSoft },
  errorHeader: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  errorTitle: { color: colors.danger, fontSize: 16, fontWeight: '800' },
  errorText: { color: '#8E2D27', lineHeight: 21 },
});
