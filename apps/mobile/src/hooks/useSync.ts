import { useCallback, useEffect, useState } from 'react';
import { AppState } from 'react-native';
import { useSQLiteContext } from 'expo-sqlite';
import type { SQLiteDatabase } from 'expo-sqlite';
import { ApiClient, ApiError } from '../api/client';
import { TrainingPlan, Workout } from '../api/generated';
import { activeDrafts, plans, workouts } from '../db/repository';
import { syncNow } from '../sync/outbox';

function describeSyncError(error: unknown): string {
  if (error instanceof ApiError) {
    const requestID = error.requestID ? ` Request ID: ${error.requestID}.` : '';
    return `${error.message} (HTTP ${error.status}).${requestID}`;
  }
  return error instanceof Error && error.message ? error.message : String(error);
}

export function useSync(api: ApiClient, athleteID: string | null): { syncing: boolean; pending: number; error: string | null } {
  const db = useSQLiteContext();
  const [syncing, setSyncing] = useState(false);
  const [pending, setPending] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const run = useCallback(async () => {
    if (!athleteID) return;
    setSyncing(true);
    setError(null);
    try { await syncNow(db, api, athleteID); } catch (syncError) {
      setError(describeSyncError(syncError));
    } finally { setSyncing(false); }
  }, [api, athleteID, db]);
  useEffect(() => { void run(); const subscription = AppState.addEventListener('change', (state) => { if (state === 'active') void run(); }); return () => subscription.remove(); }, [run]);
  useEffect(() => { let cancelled = false; const refresh = async () => { const row = await db.getFirstAsync<{ count: number }>('SELECT COUNT(*) AS count FROM outbox'); if (!cancelled) setPending(row?.count ?? 0); }; void refresh(); return () => { cancelled = true; }; }, [db, syncing]);
  return { syncing, pending, error };
}

export async function localPlan(db: SQLiteDatabase, athleteID: string): Promise<TrainingPlan | null> { const cached = await plans(db, athleteID); return cached.find((item) => item.status === 'active') ?? cached[0] ?? null; }
export async function localWorkouts(db: SQLiteDatabase, athleteID: string): Promise<Workout[]> { return workouts(db, athleteID); }
export async function localWorkout(db: SQLiteDatabase, athleteID: string, workoutID: string): Promise<Workout | null> {
  return (await localWorkouts(db, athleteID)).find((item) => item.workout_id === workoutID) ?? null;
}
export async function localActiveDraft(db: SQLiteDatabase, athleteID: string): Promise<Workout | null> { return (await activeDrafts(db, athleteID))[0] ?? null; }
