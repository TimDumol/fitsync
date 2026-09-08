import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import MaterialCommunityIcons from '@expo/vector-icons/MaterialCommunityIcons';
import { useFonts } from 'expo-font';
import { SQLiteProvider } from 'expo-sqlite';
import { Stack } from 'expo-router';
import { StatusBar } from 'react-native';
import { AuthProvider } from '../src/auth/AuthProvider';
import { migrate } from '../src/db/migrations';
import { appLog } from '../src/logging/app-logger';
import { appBuildInfo } from '../src/app-info';

appLog('info', 'app', 'FitSync mobile app initialized', appBuildInfo);

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 30_000 } } });

export default function RootLayout(): JSX.Element {
  const [iconsLoaded] = useFonts(MaterialCommunityIcons.font);
  if (!iconsLoaded) return <></>;
  return <QueryClientProvider client={queryClient}><StatusBar barStyle="dark-content" backgroundColor="#F7F8F4" /><SQLiteProvider databaseName="fitsync.db" onInit={migrate}><AuthProvider><Stack screenOptions={{ headerBackTitle: 'Back', contentStyle: { backgroundColor: '#F7F8F4' } }}><Stack.Screen name="(tabs)" options={{ headerShown: false }} /><Stack.Screen name="logs" options={{ presentation: 'modal', title: 'App logs' }} /></Stack></AuthProvider></SQLiteProvider></QueryClientProvider>;
}
