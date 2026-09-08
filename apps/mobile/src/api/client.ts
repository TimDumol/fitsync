import type { GarminSyncStatus, MeResponse, PlanStatus, SyncPage, TrainingPlan, Workout } from './generated';
import { garminSyncStatusSchema, meResponseSchema, planResponseSchema, planSchema, plannedSessionsResponseSchema, plannedSessionSchema, publishPlanResponseSchema, syncPageSchema, workoutPageSchema, workoutSchema } from './runtime';
import { apiBaseURL } from '../config';
import { appLog, errorForLog } from '../logging/app-logger';

export class ApiError extends Error {
  constructor(public readonly status: number, message: string, public readonly requestID?: string) {
    super(message);
    this.name = 'ApiError';
  }
}

export type TokenProvider = () => Promise<string | null>;

export class ApiClient {
  private readonly root: string;

  constructor(private readonly tokenProvider: TokenProvider, root = apiBaseURL) {
    this.root = root.replace(/\/+$/, '');
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const method = init.method ?? 'GET';
    const startedAt = Date.now();
    appLog('debug', 'api', 'Request started', { method, path });
    try {
      const token = await this.tokenProvider();
      const response = await fetch(`${this.root}${path}`, {
        ...init,
        headers: { Accept: 'application/json', 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}), ...init.headers },
      });
      const requestID = response.headers?.get('x-request-id') ?? undefined;
      if (!response.ok) {
        let message = `Request failed (${response.status})`;
        try {
          const body = await response.json() as { error?: string; error_description?: string; message?: string };
          message = body.error_description ?? body.error ?? body.message ?? message;
        } catch { /* bounded error body is optional */ }
        appLog('warn', 'api', 'Request returned an error', { method, path, status: response.status, requestID, durationMs: Date.now() - startedAt, message });
        throw new ApiError(response.status, message, requestID);
      }
      if (response.status === 204) {
        appLog('debug', 'api', 'Request completed', { method, path, status: response.status, requestID, durationMs: Date.now() - startedAt });
        return undefined as T;
      }
      const value = await response.json() as T;
      appLog('debug', 'api', 'Request completed', { method, path, status: response.status, requestID, durationMs: Date.now() - startedAt });
      return value;
    } catch (caught) {
      if (!(caught instanceof ApiError)) appLog('error', 'api', 'Request failed before a successful response', { method, path, durationMs: Date.now() - startedAt, error: errorForLog(caught) });
      throw caught;
    }
  }

  async me(): Promise<MeResponse> { return meResponseSchema.parse(await this.request('/api/v2/me')); }
  async garminSyncStatus(athleteID: string): Promise<GarminSyncStatus> {
    return garminSyncStatusSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(athleteID)}/garmin/sync-status`));
  }
  async activePlan(athleteID: string): Promise<TrainingPlan | null> {
    const result = planResponseSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(athleteID)}/training-plans/active`));
    return result.plan === null ? null : planSchema.parse(result.plan) as TrainingPlan;
  }
  async plan(athleteID: string, planID: string): Promise<TrainingPlan> {
    const result = planResponseSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(athleteID)}/training-plans/${encodeURIComponent(planID)}`));
    return planSchema.parse(result.plan) as TrainingPlan;
  }
  async plannedSessions(athleteID: string, from: string, to: string): Promise<TrainingPlan['sessions']> {
    const result = plannedSessionsResponseSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(athleteID)}/planned-sessions?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`));
    return result.sessions as TrainingPlan['sessions'];
  }
  async publishPlan(athleteID: string, planID: string): Promise<TrainingPlan> {
    const result = publishPlanResponseSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(athleteID)}/training-plans/${encodeURIComponent(planID)}/publish`, { method: 'POST', body: '{}' }));
    return planSchema.parse(result.plan) as TrainingPlan;
  }
  async skipSession(athleteID: string, sessionID: string): Promise<TrainingPlan['sessions'][number]> {
    return plannedSessionSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(athleteID)}/planned-sessions/${encodeURIComponent(sessionID)}/skip`, { method: 'POST', body: '{}' })) as TrainingPlan['sessions'][number];
  }
  async workouts(athleteID: string, from?: string, to?: string, limit = 50, cursor?: string): Promise<{ items: Workout[]; next_cursor?: string | null }> {
    const query = new URLSearchParams({ limit: String(limit), ...(from ? { from } : {}), ...(to ? { to } : {}), ...(cursor ? { cursor } : {}) });
    const result = workoutPageSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(athleteID)}/workouts?${query}`));
    return { items: result.items as unknown as Workout[], next_cursor: result.next_cursor };
  }
  async workout(athleteID: string, workoutID: string): Promise<Workout> {
    return workoutSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(athleteID)}/workouts/${encodeURIComponent(workoutID)}`)) as unknown as Workout;
  }
  async saveWorkout(workout: Workout): Promise<Workout> {
    return workoutSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(workout.athlete_id)}/workouts/${encodeURIComponent(workout.workout_id)}`, { method: 'PUT', body: JSON.stringify(workout) })) as unknown as Workout;
  }
  async completeWorkout(workout: Workout): Promise<Workout> {
    return workoutSchema.parse(await this.request(`/api/v2/athletes/${encodeURIComponent(workout.athlete_id)}/workouts/${encodeURIComponent(workout.workout_id)}/complete`, { method: 'POST', body: JSON.stringify(workout) })) as unknown as Workout;
  }
  async deleteWorkout(workout: Workout): Promise<void> {
    await this.request<void>(`/api/v2/athletes/${encodeURIComponent(workout.athlete_id)}/workouts/${encodeURIComponent(workout.workout_id)}`, { method: 'DELETE' });
  }
  async pull(athleteID: string, after: number, limit = 50): Promise<SyncPage> {
    const result = await this.request<unknown>(`/api/v2/sync/pull?athlete_id=${encodeURIComponent(athleteID)}&after=${after}&limit=${limit}`);
    const page = syncPageSchema.parse(result);
    return { ...page, changes: page.changes.map((change) => ({ ...change, entity: change.operation === 'delete' ? undefined : change.kind === 'training_plan' ? planSchema.parse(change.entity) as unknown as TrainingPlan : workoutSchema.parse(change.entity) as unknown as Workout })) } as SyncPage;
  }
}

export const apiPlanStatus: PlanStatus[] = ['draft', 'active', 'completed', 'archived'];
