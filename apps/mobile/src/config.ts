const defaultAPIBaseURL = 'https://api.aries.timdumol.com';
const defaultPocketIDIssuer = 'https://auth.aries.timdumol.com';

function publicHTTPSURL(value: string | undefined, fallback: string, name: string): string {
  const configured = (value?.trim() || fallback).replace(/\/$/, '');
  let parsed: URL;
  try {
    parsed = new URL(configured);
  } catch {
    throw new Error(`${name} must be a valid absolute URL`);
  }
  if (parsed.protocol !== 'https:' || parsed.search || parsed.hash) {
    throw new Error(`${name} must be an HTTPS URL without a query or fragment`);
  }
  return configured;
}

export const apiBaseURL = publicHTTPSURL(
  process.env.EXPO_PUBLIC_API_BASE_URL,
  defaultAPIBaseURL,
  'EXPO_PUBLIC_API_BASE_URL',
);

export const pocketIDIssuer = publicHTTPSURL(
  process.env.EXPO_PUBLIC_POCKET_ID_ISSUER,
  defaultPocketIDIssuer,
  'EXPO_PUBLIC_POCKET_ID_ISSUER',
);

export const oauthResource = publicHTTPSURL(
  process.env.EXPO_PUBLIC_OAUTH_RESOURCE,
  `${apiBaseURL}/mcp`,
  'EXPO_PUBLIC_OAUTH_RESOURCE',
);

export const oauthClientID = process.env.EXPO_PUBLIC_OAUTH_CLIENT_ID?.trim() || 'fitsync-mobile';
