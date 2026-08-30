import { Redirect } from 'expo-router';
import { useAuth } from '../src/auth/AuthProvider';

export default function Index(): JSX.Element { const { ready, signedIn } = useAuth(); if (!ready) return <></>; return <Redirect href={signedIn ? '/(tabs)/today' : '/sign-in'} />; }
