import { createClient } from '@neondatabase/neon-js';

export type Json = string | number | boolean | null | { [key: string]: Json | undefined } | Json[];

export interface Database {
  public: {
    Tables: {
      fitsync_items: {
        Row: {
          user_id: string;
          item_type: 'routine' | 'draft' | 'workout' | 'settings';
          item_id: string;
          payload: Json | null;
          updated_at: string;
          deleted_at: string | null;
        };
        Insert: {
          user_id?: string;
          item_type: 'routine' | 'draft' | 'workout' | 'settings';
          item_id: string;
          payload?: Json | null;
          updated_at: string;
          deleted_at?: string | null;
        };
        Update: {
          payload?: Json | null;
          updated_at?: string;
          deleted_at?: string | null;
        };
        Relationships: [];
      };
    };
    Views: Record<string, never>;
    Functions: Record<string, never>;
    Enums: Record<string, never>;
    CompositeTypes: Record<string, never>;
  };
}

export interface AuthUser {
  id: string;
  email: string;
  name: string;
}

export interface AuthSession {
  user: AuthUser;
  session: { id: string; expiresAt: Date | string };
}

const authUrl = import.meta.env.VITE_NEON_AUTH_URL?.trim();
const dataApiUrl = import.meta.env.VITE_NEON_DATA_API_URL?.trim();

export const isNeonConfigured = Boolean(authUrl && dataApiUrl);

export const neon = isNeonConfigured
  ? createClient<Database>({
      auth: { url: authUrl! },
      dataApi: { url: dataApiUrl! }
    })
  : null;

function errorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return 'Neon Auth could not complete the request.';
}

export async function getSession(): Promise<AuthSession | null> {
  if (!neon) return null;
  const result = await neon.auth.getSession();
  if (result.error) throw new Error(errorMessage(result.error));
  return (result.data as AuthSession | null) ?? null;
}

export async function signIn(email: string, password: string) {
  if (!neon) throw new Error('Neon is not configured for this build.');
  const result = await neon.auth.signIn.email({ email, password });
  if (result.error) throw new Error(errorMessage(result.error));
  return getSession();
}

export async function signUp(name: string, email: string, password: string) {
  if (!neon) throw new Error('Neon is not configured for this build.');
  const result = await neon.auth.signUp.email({ name, email, password });
  if (result.error) throw new Error(errorMessage(result.error));
  return getSession();
}

export async function signOut() {
  if (!neon) return;
  const result = await neon.auth.signOut();
  if (result.error) throw new Error(errorMessage(result.error));
}
