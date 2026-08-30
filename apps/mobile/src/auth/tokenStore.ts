import * as SecureStore from 'expo-secure-store';

const ACCESS = 'fitsync.access_token';
const REFRESH = 'fitsync.refresh_token';
const EXPIRY = 'fitsync.access_expires_at';

export interface StoredTokens { accessToken: string; refreshToken?: string; expiresAt: number; }

export async function readTokens(): Promise<StoredTokens | null> {
  const [accessToken, refreshToken, expiry] = await Promise.all([SecureStore.getItemAsync(ACCESS), SecureStore.getItemAsync(REFRESH), SecureStore.getItemAsync(EXPIRY)]);
  if (!accessToken || !expiry) return null;
  const expiresAt = Number(expiry);
  return Number.isFinite(expiresAt) ? { accessToken, refreshToken: refreshToken ?? undefined, expiresAt } : null;
}

export async function saveTokens(tokens: StoredTokens): Promise<void> {
  await Promise.all([
    SecureStore.setItemAsync(ACCESS, tokens.accessToken),
    tokens.refreshToken ? SecureStore.setItemAsync(REFRESH, tokens.refreshToken) : SecureStore.deleteItemAsync(REFRESH),
    SecureStore.setItemAsync(EXPIRY, String(tokens.expiresAt)),
  ]);
}

export async function clearTokens(): Promise<void> { await Promise.all([SecureStore.deleteItemAsync(ACCESS), SecureStore.deleteItemAsync(REFRESH), SecureStore.deleteItemAsync(EXPIRY)]); }
