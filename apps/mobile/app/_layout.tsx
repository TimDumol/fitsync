import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SQLiteProvider } from 'expo-sqlite';
import { Stack } from 'expo-router';
import { AuthProvider } from '../src/auth/AuthProvider';
import { migrate } from '../src/db/migrations';

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 30_000 } } });

export default function RootLayout(): JSX.Element {
  return <QueryClientProvider client={queryClient}><SQLiteProvider databaseName="fitsync.db" onInit={migrate}><AuthProvider><Stack screenOptions={{ headerBackTitle: 'Back' }} /></AuthProvider></SQLiteProvider></QueryClientProvider>;
}
