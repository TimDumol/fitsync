import { neon, type Database, type Json } from './neon';
import {
  clearSyncMutations,
  listDrafts,
  listRoutines,
  listSyncMutations,
  listWorkouts,
  putRemoteItem
} from './storage';
import type { AppSettings, SyncItemType, SyncMutation } from './types';

type CloudRow = Database['public']['Tables']['fitsync_items']['Row'];

export interface SyncResult {
  routines: Awaited<ReturnType<typeof listRoutines>>;
  drafts: Awaited<ReturnType<typeof listDrafts>>;
  workouts: Awaited<ReturnType<typeof listWorkouts>>;
  settings: AppSettings;
  settingsUpdatedAt: string;
  syncedAt: string;
  itemCount: number;
  pendingCount: number;
}

function key(itemType: SyncItemType, itemId: string) {
  return `${itemType}:${itemId}`;
}

function newest(first: SyncMutation | undefined, second: SyncMutation) {
  if (!first) return second;
  return Date.parse(second.updatedAt) > Date.parse(first.updatedAt) ? second : first;
}

function cloudMutation(row: CloudRow): SyncMutation {
  return {
    key: key(row.item_type, row.item_id),
    itemType: row.item_type,
    itemId: row.item_id,
    payload: row.payload as SyncMutation['payload'],
    updatedAt: row.updated_at,
    deletedAt: row.deleted_at
  };
}

export async function syncWithNeon(
  userId: string,
  settings: AppSettings,
  settingsUpdatedAt: string
): Promise<SyncResult> {
  if (!neon) throw new Error('Neon is not configured for this build.');

  const [routines, drafts, workouts, pending, remoteResult] = await Promise.all([
    listRoutines(),
    listDrafts(),
    listWorkouts(),
    listSyncMutations(),
    neon.from('fitsync_items').select('*')
  ]);
  if (remoteResult.error) throw new Error(remoteResult.error.message);

  const merged = new Map<string, SyncMutation>();
  for (const routine of routines) {
    const mutation: SyncMutation = { key: key('routine', routine.id), itemType: 'routine', itemId: routine.id, payload: routine, updatedAt: routine.updatedAt, deletedAt: null };
    merged.set(mutation.key, mutation);
  }
  for (const draft of drafts) {
    const mutation: SyncMutation = { key: key('draft', draft.id), itemType: 'draft', itemId: draft.id, payload: draft, updatedAt: draft.updatedAt, deletedAt: null };
    merged.set(mutation.key, mutation);
  }
  for (const workout of workouts) {
    const mutation: SyncMutation = { key: key('workout', workout.id), itemType: 'workout', itemId: workout.id, payload: workout, updatedAt: workout.updatedAt, deletedAt: null };
    merged.set(mutation.key, mutation);
  }
  const settingsMutation: SyncMutation = { key: key('settings', 'preferences'), itemType: 'settings', itemId: 'preferences', payload: settings, updatedAt: settingsUpdatedAt, deletedAt: null };
  merged.set(settingsMutation.key, settingsMutation);

  for (const mutation of pending) merged.set(mutation.key, newest(merged.get(mutation.key), mutation));
  for (const row of (remoteResult.data ?? []) as CloudRow[]) {
    const mutation = cloudMutation(row);
    merged.set(mutation.key, newest(merged.get(mutation.key), mutation));
  }

  let mergedSettings = settings;
  let mergedSettingsUpdatedAt = settingsUpdatedAt;
  for (const mutation of merged.values()) {
    if (mutation.itemType === 'settings') {
      if (!mutation.deletedAt && mutation.payload) {
        mergedSettings = mutation.payload as AppSettings;
        mergedSettingsUpdatedAt = mutation.updatedAt;
      }
      continue;
    }
    await putRemoteItem(mutation);
  }

  const rows = [...merged.values()].map((mutation) => ({
    user_id: userId,
    item_type: mutation.itemType,
    item_id: mutation.itemId,
    payload: mutation.payload as Json | null,
    updated_at: mutation.updatedAt,
    deleted_at: mutation.deletedAt
  }));
  if (rows.length) {
    const pushResult = await neon
      .from('fitsync_items')
      .upsert(rows, { onConflict: 'user_id,item_type,item_id' });
    if (pushResult.error) throw new Error(pushResult.error.message);
  }
  await clearSyncMutations(pending);

  const [nextRoutines, nextDrafts, nextWorkouts, remainingPending] = await Promise.all([
    listRoutines(),
    listDrafts(),
    listWorkouts(),
    listSyncMutations()
  ]);
  return {
    routines: nextRoutines,
    drafts: nextDrafts,
    workouts: nextWorkouts,
    settings: mergedSettings,
    settingsUpdatedAt: mergedSettingsUpdatedAt,
    syncedAt: new Date().toISOString(),
    itemCount: rows.filter((row) => !row.deleted_at).length,
    pendingCount: remainingPending.length
  };
}
