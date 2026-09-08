import { createContext, PropsWithChildren, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { makeRedirectUri, useAuthRequest, useAutoDiscovery } from 'expo-auth-session';
import type { AuthSessionResult } from 'expo-auth-session';
import * as Linking from 'expo-linking';
import * as WebBrowser from 'expo-web-browser';
import { useSQLiteContext } from 'expo-sqlite';
import { ApiClient, ApiError } from '../api/client';
import { MeResponse } from '../api/generated';
import { e2eMode, oauthClientID, oauthResource, pocketIDIssuer } from '../config';
import { E2EApiClient } from '../api/e2e';
import { clearLocalData, selectedAthleteID as readSelectedAthleteID, setSelectedAthleteID as persistSelectedAthleteID } from '../db/repository';
import { clearTokens, readTokens, saveTokens, StoredTokens } from './tokenStore';
import { appLog, errorForLog } from '../logging/app-logger';

WebBrowser.maybeCompleteAuthSession();

interface AuthValue {
  ready: boolean;
  signedIn: boolean;
  me: MeResponse | null;
  selectedAthleteID: string | null;
  setSelectedAthleteID: (id: string) => void;
  signIn: () => Promise<void>;
  getAccessToken: (forceRefresh?: boolean) => Promise<string>;
  logout: () => Promise<void>;
  authError: string | null;
  clearAuthError: () => void;
  api: ApiClient;
}
const AuthContext = createContext<AuthValue | null>(null);
const mobileRedirectURI = 'fitsync:///oauthredirect';

type TokenResponse = {
  access_token?: string;
  refresh_token?: string;
  expires_in?: number;
  error?: string;
  error_description?: string;
};

type DiscoveryResponse = { token_endpoint?: string };

function describeError(stage: string, error: unknown): string {
  const detail = error instanceof Error && error.message ? error.message : String(error);
  if (error instanceof ApiError) {
    const guidance = detail === 'identity_pending'
      ? ' Pocket ID accepted the login, but this user is not linked to a FitSync athlete yet.'
      : '';
    const requestID = error.requestID ? ` Request ID: ${error.requestID}.` : '';
    return `${stage}: ${detail} (HTTP ${error.status}).${guidance}${requestID}`;
  }
  return `${stage}: ${detail}`;
}

function isCredentialFailure(error: unknown): boolean {
  return (error instanceof ApiError && error.status === 401) ||
    (error instanceof Error && error.message === 'Session expired');
}

async function readTokenResponse(response: Response, stage: string): Promise<TokenResponse> {
  let value: TokenResponse;
  try {
    value = await response.json() as TokenResponse;
  } catch {
    throw new Error(`${stage} returned invalid JSON (HTTP ${response.status})`);
  }
  if (!response.ok) {
    const detail = value.error_description ?? value.error ?? `HTTP ${response.status}`;
    throw new Error(`${stage} failed: ${detail} (HTTP ${response.status})`);
  }
  return value;
}

async function tokenEndpoint(): Promise<string> {
  const response = await fetch(`${pocketIDIssuer}/.well-known/openid-configuration`);
  let discovery: DiscoveryResponse;
  try {
    discovery = await response.json() as DiscoveryResponse;
  } catch {
    throw new Error(`Pocket ID discovery returned invalid JSON (HTTP ${response.status})`);
  }
  if (!response.ok) throw new Error(`Pocket ID discovery failed (HTTP ${response.status})`);
  if (!discovery.token_endpoint) throw new Error('Pocket ID discovery has no token endpoint');
  return discovery.token_endpoint;
}

async function exchangeCode(code: string, redirectURI: string, verifier: string): Promise<StoredTokens> {
  const endpoint = await tokenEndpoint();
  const body = new URLSearchParams({
    grant_type: 'authorization_code',
    code,
    redirect_uri: redirectURI,
    client_id: oauthClientID,
    code_verifier: verifier,
    resource: oauthResource,
  }).toString();
  const response = await fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body });
  const value = await readTokenResponse(response, 'Pocket ID token exchange');
  if (!value.access_token) throw new Error('Pocket ID token exchange returned no access token');
  return { accessToken: value.access_token, refreshToken: value.refresh_token, expiresAt: Date.now() + (value.expires_in ?? 3600) * 1000 };
}

async function refreshAccessToken(current: StoredTokens): Promise<StoredTokens> {
  if (!current.refreshToken) return current;
  const endpoint = await tokenEndpoint();
  const body = new URLSearchParams({
    grant_type: 'refresh_token',
    refresh_token: current.refreshToken,
    client_id: oauthClientID,
    resource: oauthResource,
  }).toString();
  const response = await fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body });
  const value = await readTokenResponse(response, 'Pocket ID token refresh');
  if (!value.access_token) throw new Error('Pocket ID token refresh returned no access token');
  return { accessToken: value.access_token, refreshToken: value.refresh_token ?? current.refreshToken, expiresAt: Date.now() + (value.expires_in ?? 3600) * 1000 };
}

export function AuthProvider({ children }: PropsWithChildren): JSX.Element {
  const discovery = useAutoDiscovery(pocketIDIssuer);
  const redirectURI = makeRedirectUri({ native: mobileRedirectURI, scheme: 'fitsync', path: 'oauthredirect', isTripleSlashed: true });
  const [request, response, promptAsync] = useAuthRequest({ clientId: oauthClientID, redirectUri: redirectURI, responseType: 'code', scopes: ['fitness:read', 'fitness:write', 'openid', 'offline_access'], extraParams: { resource: oauthResource }, usePKCE: true }, discovery);
  const [tokens, setTokens] = useState<StoredTokens | null>(null);
  const [me, setMe] = useState<MeResponse | null>(null);
  const [selectedAthleteID, setSelected] = useState<string | null>(null);
  const [ready, setReady] = useState(false);
  const [tokensLoaded, setTokensLoaded] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const exchangedCode = useRef<string | null>(null);
  const authInProgress = useRef(false);
  const callbackReceived = useRef(false);
  const db = useSQLiteContext();

  const getAccessToken = useCallback(async (forceRefresh = false) => {
    const current = tokens ?? await readTokens();
    if (!current) throw new Error('Session expired');
    if (!forceRefresh && (current.expiresAt > Date.now() + 60_000 || !current.refreshToken)) return current.accessToken;
    if (!current.refreshToken) return current.accessToken;
    appLog('debug', 'auth', 'Refreshing FitSync access token', { forced: forceRefresh });
    const refreshed = await refreshAccessToken(current);
    await saveTokens(refreshed);
    setTokens(refreshed);
    return refreshed.accessToken;
  }, [tokens]);
  const tokenProvider = useCallback(async () => getAccessToken(), [getAccessToken]);
  const e2eApi = useMemo(() => e2eMode ? new E2EApiClient() : null, []);
  const api = useMemo(() => e2eApi ?? new ApiClient(tokenProvider), [e2eApi, tokenProvider]);

  useEffect(() => {
    if (e2eMode) {
      let cancelled = false;
      void clearLocalData(db).then(() => {
        if (cancelled) return;
        setTokens({ accessToken: 'e2e-fixture-token', expiresAt: Date.now() + 60 * 60 * 1000 });
        setTokensLoaded(true);
      });
      return () => { cancelled = true; };
    }
    let cancelled = false;
    void readTokens().then((value) => {
      if (cancelled) return;
      setTokens(value);
      setTokensLoaded(true);
      if (!value) setReady(true);
    }).catch((error) => {
      if (cancelled) return;
      setTokens(null);
      setTokensLoaded(true);
      setAuthError(describeError('Reading the saved sign-in', error));
      appLog('error', 'auth', 'Reading the saved sign-in failed', { error: errorForLog(error) });
      setReady(true);
    });
    return () => { cancelled = true; };
  }, [db]);

  useEffect(() => {
    let cancelled = false;
    if (!tokens) {
      setMe(null);
      if (tokensLoaded) setReady(true);
      return () => { cancelled = true; };
    }
    setReady(false);
    void api.me().then(async (value) => {
      if (cancelled) return;
      setMe(value);
      const stored = await readSelectedAthleteID(db);
      if (cancelled) return;
      setSelected(stored && value.accessible_athletes.some((athlete) => athlete.athlete_id === stored) ? stored : value.athlete.athlete_id);
      setAuthError(null);
    }).catch(async (error) => {
      if (cancelled) return;
      setMe(null);
      setAuthError(describeError('Loading your FitSync account', error));
      appLog('error', 'auth', 'Loading the FitSync account failed', { error: errorForLog(error) });
      // Keep tokens for server-side authorization and network errors so the
      // user can see the actual failure and retry. Only a rejected credential
      // should force a fresh OAuth sign-in.
      if (isCredentialFailure(error)) {
        await clearTokens();
        if (!cancelled) setTokens(null);
      }
    }).finally(() => {
      if (!cancelled) setReady(true);
    });
    return () => { cancelled = true; };
  }, [api, db, tokens, tokensLoaded]);

  const completeAuthorization = useCallback((result: AuthSessionResult) => {
    if (result.type === 'error') {
      const detail = result.error?.message ?? result.params.error_description ?? result.params.error ?? 'OAuth authorization failed';
      setAuthError(`Pocket ID authorization failed: ${detail}`);
      return;
    }
    if (result.type !== 'success') return;
    const code = result.params.code;
    if (!code) {
      setAuthError('Pocket ID authorization returned no authorization code');
      return;
    }
    if (!request?.codeVerifier) {
      setAuthError('Pocket ID authorization returned, but the PKCE verifier is missing. Please try again.');
      return;
    }
    if (exchangedCode.current === code) return;
    exchangedCode.current = code;
    void exchangeCode(code, redirectURI, request.codeVerifier).then(async (value) => {
      await saveTokens(value);
      setAuthError(null);
      setTokens(value);
    }).catch((error) => {
      setTokens(null);
      setAuthError(describeError('Completing Pocket ID sign-in', error));
    });
  }, [request?.codeVerifier, redirectURI]);

  useEffect(() => {
    if (!request) return;
    const subscription = Linking.addEventListener('url', ({ url }) => {
      // expo-web-browser's Android polyfill can resolve with `dismiss` when
      // the app resumes just before it delivers the deep-link event. Keep a
      // second listener so the callback is not mistaken for cancellation.
      if (!authInProgress.current || !url.startsWith(redirectURI)) return;
      callbackReceived.current = true;
      completeAuthorization(request.parseReturnUrl(url));
    });
    return () => subscription.remove();
  }, [completeAuthorization, request, redirectURI]);

  useEffect(() => {
    if (response?.type === 'error' || response?.type === 'success') {
      callbackReceived.current = true;
      completeAuthorization(response);
    }
  }, [completeAuthorization, response]);

  const signIn = useCallback(async () => {
    setAuthError(null);
    appLog('info', 'auth', 'Pocket ID sign-in started');
    if (!request) {
      setAuthError('Sign-in is still initializing. Please wait a moment and try again.');
      return;
    }
    authInProgress.current = true;
    callbackReceived.current = false;
    try {
      const result = await promptAsync();
      if (result.type === 'error' || result.type === 'success') {
        callbackReceived.current = true;
        completeAuthorization(result);
        authInProgress.current = false;
      } else if (result.type === 'cancel' || result.type === 'dismiss') {
        // On Android, allow the deep-link event to arrive after the browser
        // promise resolves. A real browser close still reports cancellation.
        await new Promise((resolve) => setTimeout(resolve, 1000));
        authInProgress.current = false;
        if (!callbackReceived.current) setAuthError('Pocket ID sign-in was closed before the app received a callback.');
      } else {
        authInProgress.current = false;
        setAuthError('Pocket ID sign-in could not be started. Please try again.');
      }
    } catch (error) {
      authInProgress.current = false;
      setAuthError(describeError('Opening Pocket ID sign-in', error));
      appLog('error', 'auth', 'Opening Pocket ID sign-in failed', { error: errorForLog(error) });
    }
  }, [completeAuthorization, promptAsync, request]);
  const logout = useCallback(async () => { await clearTokens(); await clearLocalData(); setTokens(null); setMe(null); setSelected(null); setAuthError(null); setReady(true); }, []);
  const setSelectedAthleteID = useCallback((id: string) => { setSelected(id); void persistSelectedAthleteID(db, id); }, [db]);
  const clearAuthError = useCallback(() => setAuthError(null), []);
  return <AuthContext.Provider value={{ ready, signedIn: Boolean(tokens && me), me, selectedAthleteID, setSelectedAthleteID, signIn, getAccessToken, logout, authError, clearAuthError, api }}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthValue { const value = useContext(AuthContext); if (!value) throw new Error('useAuth must be used inside AuthProvider'); return value; }
