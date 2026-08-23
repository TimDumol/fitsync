import { openDB, type DBSchema, type IDBPDatabase } from 'idb';
import { seedRoutines } from './data';
import type { ActiveWorkout, Routine, SyncItemType, SyncMutation, WorkoutRecord } from './types';

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
  syncQueue: {
    key: string;
    value: SyncMutation;
    indexes: { 'by-updated': string };
  };
}

let databasePromise: Promise<IDBPDatabase<FitSyncDatabase>> | null = null;

function database() {
  if (!databasePromise) {
    databasePromise = openDB<FitSyncDatabase>('fitsync-local', 2, {
      upgrade(db, oldVersion) {
        if (oldVersion < 1) {
          const routines = db.createObjectStore('routines', { keyPath: 'id' });
          routines.createIndex('by-updated', 'updatedAt');
          const drafts = db.createObjectStore('drafts', { keyPath: 'id' });
          drafts.createIndex('by-updated', 'updatedAt');
          const workouts = db.createObjectStore('workouts', { keyPath: 'id' });
          workouts.createIndex('by-started', 'startedAt');
        }
        if (oldVersion < 2) {
          const syncQueue = db.createObjectStore('syncQueue', { keyPath: 'key' });
          syncQueue.createIndex('by-updated', 'updatedAt');
        }
      }
    });
  }
  return databasePromise;
}

function syncKey(itemType: SyncItemType, itemId: string) {
  return `${itemType}:${itemId}`;
}

async function queueMutation(mutation: Omit<SyncMutation, 'key'>) {
  const db = await database();
  await db.put('syncQueue', { ...mutation, key: syncKey(mutation.itemType, mutation.itemId) });
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
  await queueMutation({ itemType: 'routine', itemId: routine.id, payload: routine, updatedAt: routine.updatedAt, deletedAt: null });
}

export async function getLatestDraft() {
  const db = await database();
  const drafts = await db.getAllFromIndex('drafts', 'by-updated');
  return drafts.at(-1) ?? null;
}

export async function putDraft(draft: ActiveWorkout) {
  const db = await database();
  await db.put('drafts', draft);
  await queueMutation({ itemType: 'draft', itemId: draft.id, payload: draft, updatedAt: draft.updatedAt, deletedAt: null });
}

export async function deleteDraft(id: string) {
  const db = await database();
  await db.delete('drafts', id);
  const deletedAt = new Date().toISOString();
  await queueMutation({ itemType: 'draft', itemId: id, payload: null, updatedAt: deletedAt, deletedAt });
}

export async function listWorkouts() {
  const db = await database();
  const workouts = await db.getAllFromIndex('workouts', 'by-started');
  return workouts.reverse();
}

export async function putWorkout(workout: WorkoutRecord) {
  const db = await database();
  await db.put('workouts', workout);
  await queueMutation({ itemType: 'workout', itemId: workout.id, payload: workout, updatedAt: workout.updatedAt, deletedAt: null });
}

export async function listDrafts() {
  const db = await database();
  return db.getAllFromIndex('drafts', 'by-updated');
}

export async function listSyncMutations() {
  const db = await database();
  return db.getAllFromIndex('syncQueue', 'by-updated');
}

export async function clearSyncMutations(mutations: SyncMutation[]) {
  if (!mutations.length) return;
  const db = await database();
  const transaction = db.transaction('syncQueue', 'readwrite');
  for (const mutation of mutations) {
    const current = await transaction.store.get(mutation.key);
    if (current?.updatedAt === mutation.updatedAt && current.deletedAt === mutation.deletedAt) {
      await transaction.store.delete(mutation.key);
    }
  }
  await transaction.done;
}

export async function putRemoteItem(mutation: SyncMutation) {
  const db = await database();
  const storeName = mutation.itemType === 'routine'
    ? 'routines'
    : mutation.itemType === 'draft'
      ? 'drafts'
      : mutation.itemType === 'workout'
        ? 'workouts'
        : null;
  if (!storeName) return;
  if (mutation.deletedAt) {
    await db.delete(storeName, mutation.itemId);
  } else if (mutation.payload) {
    await db.put(storeName, mutation.payload as never);
  }
}
