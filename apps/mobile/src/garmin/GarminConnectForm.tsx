import { useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';
import { AppButton } from '../components/AppButton';
import { colors, radii, spacing } from '../ui/theme';

export function GarminConnectForm({ busy, onSubmit }: { busy: boolean; onSubmit: (email: string, password: string) => Promise<void> }): JSX.Element {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const submit = async () => {
    try { await onSubmit(email.trim(), password); } finally { setPassword(''); }
  };
  const disabled = busy || !email.trim() || !password;
  return <View style={styles.box}>
    <Text style={styles.copy}>Garmin credentials are used on this phone and are not sent to FitSync.</Text>
    <TextInput testID="garmin-email" autoCapitalize="none" keyboardType="email-address" placeholder="Garmin email" value={email} onChangeText={setEmail} editable={!busy} style={styles.input} />
    <TextInput testID="garmin-password" placeholder="Garmin password" secureTextEntry value={password} onChangeText={setPassword} editable={!busy} style={styles.input} />
    <AppButton testID="connect-garmin-button" label="Connect Garmin" icon="watch-variant" onPress={() => void submit()} disabled={disabled} />
    <Text style={styles.note}>Garmin Connect uses an unofficial interface.</Text>
  </View>;
}

export function GarminMfaForm({ busy, onSubmit, onCancel }: { busy: boolean; onSubmit: (code: string) => Promise<void>; onCancel: () => Promise<void> }): JSX.Element {
  const [code, setCode] = useState('');
  return <View style={styles.box}>
    <Text style={styles.heading}>Verify Garmin</Text>
    <Text style={styles.copy}>Enter the code Garmin sent you. This verification expires in ten minutes.</Text>
    <TextInput testID="garmin-mfa-code" keyboardType="number-pad" placeholder="MFA code" value={code} onChangeText={setCode} editable={!busy} style={styles.input} />
    <AppButton testID="verify-garmin-button" label="Verify" icon="check" onPress={() => void onSubmit(code)} disabled={busy || !code.trim()} />
    <AppButton testID="cancel-garmin-button" label="Cancel" variant="quiet" onPress={() => void onCancel()} disabled={busy} />
  </View>;
}

const styles = StyleSheet.create({
  box: { gap: spacing.md },
  copy: { color: colors.muted, lineHeight: 20 },
  input: { minHeight: 48, borderWidth: 1, borderColor: colors.line, borderRadius: radii.sm, paddingHorizontal: spacing.md, backgroundColor: colors.background, color: colors.ink },
  note: { color: colors.subtle, fontSize: 12 },
  heading: { color: colors.ink, fontSize: 17, fontWeight: '800' },
});
