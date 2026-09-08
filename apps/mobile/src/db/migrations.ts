import type { SQLiteDatabase } from 'expo-sqlite';

export const schemaVersion = 3;

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
      operation TEXT NOT NULL CHECK(operation IN ('save', 'complete', 'delete')),
      aggregate_id TEXT NOT NULL,
      request_json TEXT NOT NULL,
      retry_count INTEGER NOT NULL DEFAULT 0,
      next_attempt_at INTEGER NOT NULL
    );
    CREATE INDEX IF NOT EXISTS outbox_retry_idx ON outbox(next_attempt_at, id);
  `);
  const current = await database.getFirstAsync<{ value: string }>('SELECT value FROM local_meta WHERE key=?', 'schema_version');
  // A local-data reset may remove the version marker while leaving the
  // tables in place. Treat that database as needing the latest table shape
  // so an older outbox CHECK constraint cannot block offline deletes.
  if (!current || Number(current.value) < schemaVersion) {
    await database.withTransactionAsync(async () => {
      // SQLite cannot alter a CHECK constraint in place. Recreate only this
      // small queue table while preserving any pending offline work.
      await database.execAsync(`
        CREATE TABLE outbox_v2 (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          athlete_id TEXT NOT NULL,
          operation TEXT NOT NULL CHECK(operation IN ('save', 'complete', 'delete')),
          aggregate_id TEXT NOT NULL,
          request_json TEXT NOT NULL,
          retry_count INTEGER NOT NULL DEFAULT 0,
          next_attempt_at INTEGER NOT NULL
        );
        INSERT INTO outbox_v2(id,athlete_id,operation,aggregate_id,request_json,retry_count,next_attempt_at)
          SELECT id,athlete_id,operation,aggregate_id,request_json,retry_count,next_attempt_at FROM outbox;
        DROP TABLE outbox;
        ALTER TABLE outbox_v2 RENAME TO outbox;
        CREATE INDEX IF NOT EXISTS outbox_retry_idx ON outbox(next_attempt_at, id);
      `);
      if (current) await database.runAsync('UPDATE local_meta SET value=? WHERE key=?', String(schemaVersion), 'schema_version');
      else await database.runAsync('INSERT INTO local_meta(key, value) VALUES(?, ?)', 'schema_version', String(schemaVersion));
    });
  }
}
