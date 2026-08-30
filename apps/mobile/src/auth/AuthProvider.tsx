import { createContext, PropsWithChildren, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { makeRedirectUri, useAuthRequest, useAutoDiscovery } from 'expo-auth-session';
import * as WebBrowser from 'expo-web-browser';
import { useSQLiteContext } from 'expo-sqlite';
import { ApiClient } from '../api/client';
import { MeResponse } from '../api/generated';
import { oauthClientID, oauthResource, pocketIDIssuer } from '../config';
import { clearLocalData, selectedAthleteID as readSelectedAthleteID, setSelectedAthleteID as persistSelectedAthleteID } from '../db/repository';
import { clearTokens, readTokens, saveTokens, StoredTokens } from './tokenStore';

WebBrowser.maybeCompleteAuthSession();

interface AuthValue {
  ready: boolean;
  signedIn: boolean;
  me: MeResponse | null;
  selectedAthleteID: string | null;
  setSelectedAthleteID: (id: string) => void;
  signIn: () => Promise<void>;
  logout: () => Promise<void>;
  api: ApiClient;
}
const AuthContext = createContext<AuthValue | null>(null);

async function exchangeCode(code: string, redirectURI: string, verifier: string): Promise<StoredTokens> {
  const discovery = await fetch(`${pocketIDIssuer}/.well-known/openid-configuration`).then((response) => response.json() as Promise<{ token_endpoint: string }>);
  const body = new URLSearchParams({ grant_type: 'authorization_code', code, redirect_uri: redirectURI, client_id: oauthClientID, code_verifier: verifier });
  const response = await fetch(discovery.token_endpoint, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body });
  if (!response.ok) throw new Error('Unable to sign in');
  const value = await response.json() as { access_token: string; refresh_token?: string; expires_in?: number };
  return { accessToken: value.access_token, refreshToken: value.refresh_token, expiresAt: Date.now() + (value.expires_in ?? 3600) * 1000 };
}

async function refreshAccessToken(current: StoredTokens): Promise<StoredTokens> {
  if (!current.refreshToken) return current;
  const discovery = await fetch(`${pocketIDIssuer}/.well-known/openid-configuration`).then((response) => response.json() as Promise<{ token_endpoint: string }>);
  const body = new URLSearchParams({ grant_type: 'refresh_token', refresh_token: current.refreshToken, client_id: oauthClientID });
  const response = await fetch(discovery.token_endpoint, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body });
  if (!response.ok) throw new Error('Session expired');
  const value = await response.json() as { access_token: string; refresh_token?: string; expires_in?: number };
  return { accessToken: value.access_token, refreshToken: value.refresh_token ?? current.refreshToken, expiresAt: Date.now() + (value.expires_in ?? 3600) * 1000 };
}

export function AuthProvider({ children }: PropsWithChildren): JSX.Element {
  const discovery = useAutoDiscovery(pocketIDIssuer);
  const redirectURI = makeRedirectUri({ scheme: 'fitsync' });
  const [request, response, promptAsync] = useAuthRequest({ clientId: oauthClientID, redirectUri: redirectURI, responseType: 'code', scopes: ['fitness:read', 'fitness:write', 'openid', 'offline_access'], extraParams: { resource: oauthResource }, usePKCE: true }, discovery);
  const [tokens, setTokens] = useState<StoredTokens | null>(null);
  const [me, setMe] = useState<MeResponse | null>(null);
  const [selectedAthleteID, setSelected] = useState<string | null>(null);
  const [ready, setReady] = useState(false);
  const db = useSQLiteContext();

  const tokenProvider = useCallback(async () => {
    const current = tokens ?? await readTokens();
    if (!current) return null;
    if (current.expiresAt > Date.now() + 60_000 || !current.refreshToken) return current.accessToken;
    const refreshed = await refreshAccessToken(current);
    await saveTokens(refreshed);
    setTokens(refreshed);
    return refreshed.accessToken;
  }, [tokens]);
  const api = useMemo(() => new ApiClient(tokenProvider), [tokenProvider]);

  useEffect(() => { void readTokens().then((value) => { setTokens(value); setReady(true); }); }, []);
  useEffect(() => { if (!tokens) { setMe(null); return; } void api.me().then(async (value) => { setMe(value); const stored = await readSelectedAthleteID(db); setSelected(stored && value.accessible_athletes.some((athlete) => athlete.athlete_id === stored) ? stored : value.athlete.athlete_id); }).catch(() => { void logout(); }); }, [api, db, tokens]);
  useEffect(() => {
    if (response?.type !== 'success' || !request?.codeVerifier || !response.params.code) return;
    void exchangeCode(response.params.code, redirectURI, request.codeVerifier).then(async (value) => { await saveTokens(value); setTokens(value); }).catch(() => setTokens(null));
  }, [request?.codeVerifier, response, redirectURI]);

  const signIn = useCallback(async () => { if (request) await promptAsync(); }, [promptAsync, request]);
  const logout = useCallback(async () => { await clearTokens(); await clearLocalData(); setTokens(null); setMe(null); setSelected(null); }, []);
  const setSelectedAthleteID = useCallback((id: string) => { setSelected(id); void persistSelectedAthleteID(db, id); }, [db]);
  return <AuthContext.Provider value={{ ready, signedIn: Boolean(tokens && me), me, selectedAthleteID, setSelectedAthleteID, signIn, logout, api }}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthValue { const value = useContext(AuthContext); if (!value) throw new Error('useAuth must be used inside AuthProvider'); return value; }
