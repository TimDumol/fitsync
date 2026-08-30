import { openDatabaseAsync, SQLiteDatabase } from 'expo-sqlite';
import { SyncChange, TrainingPlan, Workout } from '../api/generated';
import { migrate } from './migrations';

export async function database(): Promise<SQLiteDatabase> { const db = await openDatabaseAsync('fitsync.db'); await migrate(db); return db; }

export async function selectedAthleteID(db: SQLiteDatabase): Promise<string | null> {
  const row = await db.getFirstAsync<{ value: string }>('SELECT value FROM local_meta WHERE key=?', 'selected_athlete_id');
  return row?.value ?? null;
}
export async function setSelectedAthleteID(db: SQLiteDatabase, id: string): Promise<void> { await db.runAsync('INSERT INTO local_meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value', 'selected_athlete_id', id); }
export async function syncCursor(db: SQLiteDatabase, athleteID: string): Promise<number> {
  const row = await db.getFirstAsync<{ value: string }>('SELECT value FROM local_meta WHERE key=?', `sync_cursor:${athleteID}`);
  return row ? Number(row.value) || 0 : 0;
}
export async function setSyncCursor(db: SQLiteDatabase, athleteID: string, cursor: number): Promise<void> { await db.runAsync('INSERT INTO local_meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value', `sync_cursor:${athleteID}`, String(cursor)); }

export async function savePlan(db: SQLiteDatabase, plan: TrainingPlan): Promise<void> { await db.runAsync('INSERT INTO cached_plans(plan_id,athlete_id,sync_seq,json) VALUES(?,?,?,?) ON CONFLICT(plan_id) DO UPDATE SET athlete_id=excluded.athlete_id,sync_seq=excluded.sync_seq,json=excluded.json', plan.plan_id, plan.athlete_id, plan.sync_seq, JSON.stringify(plan)); }
export async function deletePlan(db: SQLiteDatabase, planID: string): Promise<void> { await db.runAsync('DELETE FROM cached_plans WHERE plan_id=?', planID); }
export async function plans(db: SQLiteDatabase, athleteID: string): Promise<TrainingPlan[]> { const rows = await db.getAllAsync<{ json: string }>('SELECT json FROM cached_plans WHERE athlete_id=? ORDER BY sync_seq DESC', athleteID); return rows.map((row) => JSON.parse(row.json) as TrainingPlan); }
export async function saveWorkout(db: SQLiteDatabase, workout: Workout): Promise<void> { await db.runAsync('INSERT INTO cached_workouts(workout_id,athlete_id,sync_seq,json) VALUES(?,?,?,?) ON CONFLICT(workout_id) DO UPDATE SET athlete_id=excluded.athlete_id,sync_seq=excluded.sync_seq,json=excluded.json', workout.workout_id, workout.athlete_id, workout.sync_seq ?? 0, JSON.stringify(workout)); }
export async function deleteWorkout(db: SQLiteDatabase, workoutID: string): Promise<void> { await db.runAsync('DELETE FROM cached_workouts WHERE workout_id=?', workoutID); }
export async function workouts(db: SQLiteDatabase, athleteID: string): Promise<Workout[]> { const rows = await db.getAllAsync<{ json: string }>('SELECT json FROM cached_workouts WHERE athlete_id=? ORDER BY sync_seq DESC, workout_id DESC', athleteID); return rows.map((row) => JSON.parse(row.json) as Workout); }
export async function saveDraft(db: SQLiteDatabase, workout: Workout): Promise<void> { await db.runAsync('INSERT INTO active_workout_drafts(workout_id,athlete_id,json,updated_at) VALUES(?,?,?,?) ON CONFLICT(workout_id) DO UPDATE SET json=excluded.json,updated_at=excluded.updated_at', workout.workout_id, workout.athlete_id, JSON.stringify(workout), Date.now()); }
export async function activeDrafts(db: SQLiteDatabase, athleteID: string): Promise<Workout[]> { const rows = await db.getAllAsync<{ json: string }>("SELECT json FROM active_workout_drafts WHERE athlete_id=? AND json_extract(json,'$.status')='in_progress' ORDER BY updated_at DESC", athleteID); return rows.map((row) => JSON.parse(row.json) as Workout); }
export async function deleteDraft(db: SQLiteDatabase, workoutID: string): Promise<void> { await db.runAsync('DELETE FROM active_workout_drafts WHERE workout_id=?', workoutID); }

export async function applyChanges(db: SQLiteDatabase, athleteID: string, changes: SyncChange[], cursor: number): Promise<void> {
  await db.withTransactionAsync(async () => {
    for (const change of changes) {
      if (change.kind === 'training_plan') {
        if (change.operation === 'delete') await deletePlan(db, change.entity_id);
        else if (change.entity && 'plan_id' in change.entity) await savePlan(db, change.entity as TrainingPlan);
      } else if (change.operation === 'delete') {
        await deleteWorkout(db, change.entity_id);
      } else if (change.entity && 'workout_id' in change.entity) {
        await saveWorkout(db, change.entity as Workout);
      }
    }
    await setSyncCursor(db, athleteID, cursor);
  });
}

export async function clearLocalData(db?: SQLiteDatabase): Promise<void> {
  const owned = db ?? await database();
  await owned.withTransactionAsync(async () => { await owned.execAsync('DELETE FROM local_meta; DELETE FROM cached_plans; DELETE FROM cached_workouts; DELETE FROM active_workout_drafts; DELETE FROM outbox;'); });
  if (!db) await owned.closeAsync();
}
