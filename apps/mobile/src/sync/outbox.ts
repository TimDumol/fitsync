import { ApiClient } from '../api/client';
import { SyncPage, Workout } from '../api/generated';
import { applyChanges, deleteDraft, saveDraft, saveWorkout, syncCursor } from '../db/repository';
import type { SQLiteDatabase } from 'expo-sqlite';

export type OutboxOperation = 'save' | 'complete';

export async function enqueueWorkout(db: SQLiteDatabase, operation: OutboxOperation, workout: Workout): Promise<void> {
  if (operation === 'complete') await db.runAsync('DELETE FROM outbox WHERE aggregate_id=? AND operation=?', workout.workout_id, 'save');
  const update = await db.runAsync('UPDATE outbox SET request_json=?,retry_count=0,next_attempt_at=? WHERE aggregate_id=? AND operation=?', JSON.stringify(workout), Date.now(), workout.workout_id, operation);
  if (update.changes === 0) await db.runAsync('INSERT INTO outbox(athlete_id,operation,aggregate_id,request_json,retry_count,next_attempt_at) VALUES(?,?,?,?,0,?)', workout.athlete_id, operation, workout.workout_id, JSON.stringify(workout), Date.now());
}

export async function pendingOutboxCount(db: SQLiteDatabase): Promise<number> { const row = await db.getFirstAsync<{ count: number }>('SELECT COUNT(*) AS count FROM outbox'); return row?.count ?? 0; }

export async function flushOutbox(db: SQLiteDatabase, api: ApiClient, now = Date.now()): Promise<void> {
  const items = await db.getAllAsync<{ id: number; operation: OutboxOperation; request_json: string; retry_count: number }>('SELECT id,operation,request_json,retry_count FROM outbox WHERE next_attempt_at<=? ORDER BY id LIMIT 20', now);
  for (const item of items) {
    const workout = JSON.parse(item.request_json) as Workout;
    try {
      const saved = item.operation === 'complete' ? await api.completeWorkout(workout) : await api.saveWorkout(workout);
      await db.withTransactionAsync(async () => {
        await saveWorkout(db, saved);
        await deleteDraft(db, saved.workout_id);
        await db.runAsync('DELETE FROM outbox WHERE id=?', item.id);
      });
    } catch {
      const retryCount = item.retry_count + 1;
      const delay = Math.min(60 * 60 * 1000, 1_000 * 2 ** Math.min(retryCount, 10));
      await db.runAsync('UPDATE outbox SET retry_count=?,next_attempt_at=? WHERE id=?', retryCount, now + delay, item.id);
    }
  }
}

export async function pullChanges(db: SQLiteDatabase, api: ApiClient, athleteID: string): Promise<SyncPage> {
  let cursor = await syncCursor(db, athleteID);
  let last: SyncPage = { changes: [], next_after: cursor, has_more: false };
  do {
    last = await api.pull(athleteID, cursor, 50);
    if (last.changes.length === 0) { cursor = last.next_after; break; }
    // applyChanges commits the aggregates and cursor together. A failed
    // transaction leaves the old cursor so the page can be retried safely.
    await applyChanges(db, athleteID, last.changes, last.next_after);
    cursor = last.next_after;
  } while (last.has_more);
  return last;
}

export async function syncNow(db: SQLiteDatabase, api: ApiClient, athleteID: string): Promise<void> { await flushOutbox(db, api); await pullChanges(db, api, athleteID); }

export async function persistLocalDraft(db: SQLiteDatabase, workout: Workout): Promise<void> { await db.withTransactionAsync(async () => { await saveDraft(db, workout); await enqueueWorkout(db, 'save', workout); }); }
