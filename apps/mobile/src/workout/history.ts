import type { SQLiteDatabase } from 'expo-sqlite';
import type { ApiClient } from '../api/client';
import type { Workout } from '../api/generated';
import { deleteWorkout } from '../db/repository';
import { flushOutbox, enqueueWorkout } from '../sync/outbox';

/** Remove the local copy immediately and let the outbox finish the server tombstone online or offline. */
export async function removePastWorkout(db: SQLiteDatabase, api: ApiClient, workout: Workout): Promise<void> {
  await deleteWorkout(db, workout.workout_id);
  await enqueueWorkout(db, 'delete', workout);
  await flushOutbox(db, api);
}
