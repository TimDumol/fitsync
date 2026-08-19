import { openDB, type DBSchema, type IDBPDatabase } from 'idb';
import { seedRoutines } from './data';
import type { ActiveWorkout, Routine, WorkoutRecord } from './types';

interface FitSyncDatabase extends DBSchema {
  routines: {
    key: string;
    value: Routine;
    indexes: { 'by-updated': string };
  };
  drafts: {
    key: string;
    value: ActiveWorkout;
    indexes: { 'by-updated': string };
  };
  workouts: {
    key: string;
    value: WorkoutRecord;
    indexes: { 'by-started': string };
  };
}

let databasePromise: Promise<IDBPDatabase<FitSyncDatabase>> | null = null;

function database() {
  if (!databasePromise) {
    databasePromise = openDB<FitSyncDatabase>('fitsync-local', 1, {
      upgrade(db) {
        const routines = db.createObjectStore('routines', { keyPath: 'id' });
        routines.createIndex('by-updated', 'updatedAt');
        const drafts = db.createObjectStore('drafts', { keyPath: 'id' });
        drafts.createIndex('by-updated', 'updatedAt');
        const workouts = db.createObjectStore('workouts', { keyPath: 'id' });
        workouts.createIndex('by-started', 'startedAt');
      }
    });
  }
  return databasePromise;
}

export async function initializeLocalData() {
  const db = await database();
  if ((await db.count('routines')) === 0) {
    const transaction = db.transaction('routines', 'readwrite');
    await Promise.all([
      ...seedRoutines.map((routine) => transaction.store.put(routine)),
      transaction.done
    ]);
  }
}

export async function listRoutines() {
  const db = await database();
  return db.getAll('routines');
}

export async function putRoutine(routine: Routine) {
  const db = await database();
  await db.put('routines', routine);
}

export async function getLatestDraft() {
  const db = await database();
  const drafts = await db.getAllFromIndex('drafts', 'by-updated');
  return drafts.at(-1) ?? null;
}

export async function putDraft(draft: ActiveWorkout) {
  const db = await database();
  await db.put('drafts', draft);
}

export async function deleteDraft(id: string) {
  const db = await database();
  await db.delete('drafts', id);
}

export async function listWorkouts() {
  const db = await database();
  const workouts = await db.getAllFromIndex('workouts', 'by-started');
  return workouts.reverse();
}

export async function putWorkout(workout: WorkoutRecord) {
  const db = await database();
  await db.put('workouts', workout);
}
