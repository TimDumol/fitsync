// Derived from api/openapi.yaml v2. Keep transport shapes here aligned with
// the generated Go models; domain state lives in src/workout instead.
export type UUID = string;

export type PlanStatus = 'draft' | 'active' | 'completed' | 'archived';
export type PlannedSessionStatus = 'planned' | 'completed' | 'skipped' | 'cancelled';
export type StepType = 'warmup' | 'exercise' | 'interval' | 'rest' | 'cooldown' | 'note';
export type WorkoutStatus = 'in_progress' | 'completed' | 'discarded';

export interface PrescriptionSet {
  reps_min?: number | null;
  reps_max?: number | null;
  load_kg?: number | null;
  rir?: number | null;
  rpe?: number | null;
  duration_seconds?: number | null;
  rest_seconds?: number | null;
  tempo?: string | null;
}

export interface Prescription {
  sets?: PrescriptionSet[];
  rounds?: number | null;
  distance_meters?: number | null;
  duration_seconds?: number | null;
  pace_min_per_km_min?: number | null;
  pace_min_per_km_max?: number | null;
  heart_rate_zone?: string | null;
}

export interface PlannedStep {
  step_id: UUID;
  sequence_no: number;
  step_type: StepType;
  exercise_key?: string | null;
  exercise_name?: string | null;
  prescription: Prescription;
  notes?: string | null;
}

export interface PlannedStepGroup {
  group_id: UUID;
  label?: string;
  mode: 'rounds';
  rounds: number;
  step_sequence_nos: number[];
  notes?: string | null;
}

export interface PlannedSession {
  session_id: UUID;
  plan_id: UUID;
  athlete_id: UUID;
  date: string;
  title: string;
  description?: string | null;
  sport: string;
  session_type: string;
  priority: string;
  targets: Record<string, unknown>;
  notes?: string | null;
  status: PlannedSessionStatus;
  completed_activity_id?: string | null;
  completed_workout_id?: UUID | null;
  steps: PlannedStep[];
  groups?: PlannedStepGroup[];
}

export interface TrainingPlan {
  plan_id: UUID;
  athlete_id: UUID;
  title: string;
  description?: string | null;
  goal: Record<string, unknown>;
  status: PlanStatus;
  start_date?: string | null;
  end_date?: string | null;
  source: string;
  source_ref?: string | null;
  created_at?: number;
  sync_seq: number;
  sessions: PlannedSession[];
}

export interface WorkoutEntry {
  entry_id: UUID;
  planned_step_id?: UUID | null;
  sequence_no: number;
  entry_type: 'set' | 'rest' | 'note';
  exercise_key?: string | null;
  exercise_name?: string | null;
  user_label?: string | null;
  set_number?: number | null;
  reps?: number | null;
  duration_seconds?: number | null;
  load_kg?: number | null;
  load_description?: string | null;
  is_bodyweight?: boolean;
  to_failure?: boolean;
  rir?: number | null;
  rir_min?: number | null;
  rir_max?: number | null;
  laterality?: 'bilateral' | 'unilateral' | 'left' | 'right' | 'alternating' | 'unknown' | null;
  reps_per_side?: boolean;
  load_basis?: 'total_external' | 'per_limb' | 'bodyweight_added' | 'assistance' | 'unknown' | null;
  load_per_limb_kg?: number | null;
  total_external_load_kg?: number | null;
  added_load_kg?: number | null;
  muscle_groups?: string[];
  direct_muscle_groups?: string[];
  indirect_muscle_groups?: string[];
  notes?: string | null;
  is_warmup?: boolean;
}

export interface StepComparison {
  planned_step_id: UUID;
  exercise_key?: string | null;
  exercise_name?: string | null;
  planned_sets?: number;
  actual_sets?: number;
  planned_reps?: number[];
  actual_reps?: number[];
  planned_load_kg?: number[];
  actual_load_kg?: number[];
}

export interface Workout {
  workout_id: UUID;
  athlete_id: UUID;
  planned_session_id?: UUID | null;
  date: string;
  title: string;
  source: string;
  workout_type?: string;
  status: WorkoutStatus;
  started_at?: string | null;
  completed_at?: string | null;
  duration_minutes?: number | null;
  perceived_effort?: number | null;
  notes?: string | null;
  linked_activity_id?: string | null;
  sync_seq?: number;
  entries: WorkoutEntry[];
  planned_session?: PlannedSession;
  comparison?: { steps: StepComparison[] };
}

export interface MeResponse {
  athlete: { athlete_id: UUID; handle: string; timezone: string; units?: 'metric' };
  accessible_athletes: Array<{ athlete_id: UUID; handle: string; timezone: string; units?: 'metric' }>;
  capabilities: { plans?: boolean; workouts?: boolean; sync?: boolean };
}

export interface GarminSyncStatus {
  last_ingested_at: string | null;
  latest_record_date: string | null;
  datasets: Record<string, string>;
}

export interface SyncChange {
  sync_seq: number;
  kind: 'training_plan' | 'workout';
  operation: 'upsert' | 'delete';
  entity_id: UUID;
  entity?: TrainingPlan | Workout;
}

export interface SyncPage {
  changes: SyncChange[];
  next_after: number;
  has_more: boolean;
}
