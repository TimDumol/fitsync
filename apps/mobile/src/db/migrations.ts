import type { SQLiteDatabase } from 'expo-sqlite';

export const schemaVersion = 1;

export async function migrate(database: SQLiteDatabase): Promise<void> {
  await database.execAsync(`
    PRAGMA journal_mode = WAL;
    CREATE TABLE IF NOT EXISTS local_meta (key TEXT PRIMARY KEY NOT NULL, value TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS cached_plans (plan_id TEXT PRIMARY KEY NOT NULL, athlete_id TEXT NOT NULL, sync_seq INTEGER NOT NULL, json TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS cached_workouts (workout_id TEXT PRIMARY KEY NOT NULL, athlete_id TEXT NOT NULL, sync_seq INTEGER NOT NULL, json TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS active_workout_drafts (workout_id TEXT PRIMARY KEY NOT NULL, athlete_id TEXT NOT NULL, json TEXT NOT NULL, updated_at INTEGER NOT NULL);
    CREATE TABLE IF NOT EXISTS outbox (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      athlete_id TEXT NOT NULL,
      operation TEXT NOT NULL CHECK(operation IN ('save', 'complete')),
      aggregate_id TEXT NOT NULL,
      request_json TEXT NOT NULL,
      retry_count INTEGER NOT NULL DEFAULT 0,
      next_attempt_at INTEGER NOT NULL
    );
    CREATE INDEX IF NOT EXISTS outbox_retry_idx ON outbox(next_attempt_at, id);
  `);
  await database.runAsync('INSERT OR IGNORE INTO local_meta(key, value) VALUES(?, ?)', 'schema_version', String(schemaVersion));
}
