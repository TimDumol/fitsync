import { Tabs, Redirect } from 'expo-router';
import { useAuth } from '../../src/auth/AuthProvider';

export default function TabsLayout(): JSX.Element {
  const { ready, signedIn } = useAuth();
  if (ready && !signedIn) return <Redirect href="/sign-in" />;
  return <Tabs screenOptions={{ tabBarActiveTintColor: '#155EEF' }}><Tabs.Screen name="today" options={{ title: 'Today' }} /><Tabs.Screen name="plan" options={{ title: 'Plan' }} /><Tabs.Screen name="history" options={{ title: 'History' }} /><Tabs.Screen name="settings" options={{ title: 'Settings' }} /></Tabs>;
}
