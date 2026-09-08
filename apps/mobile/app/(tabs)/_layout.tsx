import MaterialCommunityIcons from '@expo/vector-icons/MaterialCommunityIcons';
import { Tabs, Redirect } from 'expo-router';
import { StyleSheet, View, type ColorValue } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAuth } from '../../src/auth/AuthProvider';
import { colors } from '../../src/ui/theme';
import type { ComponentProps } from 'react';

export default function TabsLayout(): JSX.Element {
  const { ready, signedIn } = useAuth();
  const insets = useSafeAreaInsets();
  if (ready && !signedIn) return <Redirect href="/sign-in" />;
  return <Tabs screenOptions={{
    tabBarActiveTintColor: colors.primary,
    tabBarInactiveTintColor: colors.muted,
    tabBarLabelStyle: styles.label,
    tabBarIconStyle: styles.icon,
    tabBarStyle: [styles.bar, { height: 64 + insets.bottom, paddingBottom: 8 + insets.bottom }],
    headerShown: false,
  }}>
    <Tabs.Screen name="today" options={{ title: 'Today', tabBarIcon: ({ color, focused }) => <TabIcon name={focused ? 'calendar-today' : 'calendar-blank-outline'} color={color} focused={focused} /> }} />
    <Tabs.Screen name="plan" options={{ title: 'Plan', tabBarIcon: ({ color, focused }) => <TabIcon name={focused ? 'dumbbell' : 'dumbbell'} color={color} focused={focused} /> }} />
    <Tabs.Screen name="history" options={{ title: 'History', tabBarIcon: ({ color, focused }) => <TabIcon name={focused ? 'chart-line' : 'chart-line-variant'} color={color} focused={focused} /> }} />
    <Tabs.Screen name="settings" options={{ title: 'Settings', tabBarIcon: ({ color, focused }) => <TabIcon name={focused ? 'cog' : 'cog-outline'} color={color} focused={focused} /> }} />
  </Tabs>;
}

function TabIcon({ name, color, focused }: { name: ComponentProps<typeof MaterialCommunityIcons>['name']; color: ColorValue; focused: boolean }): JSX.Element {
  return <View style={[styles.iconWrap, focused && styles.iconWrapActive]}><MaterialCommunityIcons name={name} color={color} size={22} /></View>;
}

const styles = StyleSheet.create({
  bar: { backgroundColor: colors.surface, borderTopColor: colors.line, borderTopWidth: StyleSheet.hairlineWidth, paddingTop: 7 },
  icon: { height: 26, marginBottom: 1 },
  iconWrap: { width: 42, height: 28, alignItems: 'center', justifyContent: 'center', borderRadius: 10 },
  iconWrapActive: { backgroundColor: colors.primarySoft },
  label: { fontSize: 11, fontWeight: '700' },
});
