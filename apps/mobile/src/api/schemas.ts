import { z } from 'zod';

const stepType = z.enum(['warmup', 'exercise', 'interval', 'rest', 'cooldown', 'note']);
const prescriptionSet = z.object({
  reps_min: z.number().nullable().optional(), reps_max: z.number().nullable().optional(),
  load_kg: z.number().nullable().optional(), rir: z.number().nullable().optional(), rpe: z.number().nullable().optional(),
  duration_seconds: z.number().nullable().optional(), rest_seconds: z.number().nullable().optional(), tempo: z.string().nullable().optional(),
}).strict();
const prescription = z.object({
  sets: z.array(prescriptionSet).max(20).optional(), rounds: z.number().nullable().optional(),
  distance_meters: z.number().nullable().optional(), duration_seconds: z.number().nullable().optional(),
  pace_min_per_km_min: z.number().nullable().optional(), pace_min_per_km_max: z.number().nullable().optional(), heart_rate_zone: z.string().nullable().optional(),
}).strict();
export const plannedStepSchema = z.object({
  step_id: z.string().uuid(), sequence_no: z.number().int().positive(), step_type: stepType,
  exercise_key: z.string().nullable().optional(), exercise_name: z.string().nullable().optional(), prescription, notes: z.string().nullable().optional(),
}).strict();
export const plannedSessionSchema = z.object({
  session_id: z.string().uuid(), plan_id: z.string().uuid(), athlete_id: z.string().uuid(), date: z.string(), title: z.string(),
  description: z.string().nullable().optional(), sport: z.string(), session_type: z.string(), priority: z.string(), targets: z.record(z.unknown()),
  notes: z.string().nullable().optional(), status: z.enum(['planned', 'completed', 'skipped', 'cancelled']),
  completed_activity_id: z.string().nullable().optional(), completed_workout_id: z.string().uuid().nullable().optional(), steps: z.array(plannedStepSchema),
}).strict();
export const planSchema = z.object({
  plan_id: z.string().uuid(), athlete_id: z.string().uuid(), title: z.string(), description: z.string().nullable().optional(), goal: z.record(z.unknown()),
  status: z.enum(['draft', 'active', 'completed', 'archived']), start_date: z.string().nullable().optional(), end_date: z.string().nullable().optional(),
  source: z.string(), source_ref: z.string().nullable().optional(), created_at: z.number().optional(), sync_seq: z.number(), sessions: z.array(plannedSessionSchema),
}).strict();
const workoutEntrySchema = z.object({
  entry_id: z.string().uuid(), planned_step_id: z.string().uuid().nullable().optional(), sequence_no: z.number().int().positive(),
  entry_type: z.enum(['set', 'rest', 'note']), exercise_key: z.string().nullable().optional(), exercise_name: z.string().nullable().optional(),
  user_label: z.string().nullable().optional(), set_number: z.number().int().nullable().optional(), reps: z.number().nullable().optional(),
  duration_seconds: z.number().nullable().optional(), load_kg: z.number().nullable().optional(), load_description: z.string().nullable().optional(),
  is_bodyweight: z.boolean().optional(), to_failure: z.boolean().optional(), rir: z.number().nullable().optional(), rir_min: z.number().nullable().optional(),
  rir_max: z.number().nullable().optional(), laterality: z.string().nullable().optional(), muscle_groups: z.array(z.string()).optional(),
  notes: z.string().nullable().optional(), is_warmup: z.boolean().optional(),
}).strict();
const comparisonStepSchema = z.object({
  planned_step_id: z.string().uuid(), exercise_key: z.string().nullable().optional(), exercise_name: z.string().nullable().optional(),
  planned_sets: z.number().int().optional(), actual_sets: z.number().int().optional(), planned_reps: z.array(z.number()).optional(), actual_reps: z.array(z.number()).optional(),
  planned_load_kg: z.array(z.number()).optional(), actual_load_kg: z.array(z.number()).optional(),
}).strict();
export const workoutSchema = z.object({
  workout_id: z.string().uuid(), athlete_id: z.string().uuid(), planned_session_id: z.string().uuid().nullable().optional(), date: z.string(),
  title: z.string(), source: z.string(), workout_type: z.string().optional(), status: z.enum(['in_progress', 'completed', 'discarded']),
  started_at: z.string().nullable().optional(), completed_at: z.string().nullable().optional(), duration_minutes: z.number().nullable().optional(),
  perceived_effort: z.number().nullable().optional(), notes: z.string().nullable().optional(), linked_activity_id: z.string().nullable().optional(),
  sync_seq: z.number().optional(), entries: z.array(workoutEntrySchema), planned_session: plannedSessionSchema.optional(), comparison: z.object({ steps: z.array(comparisonStepSchema) }).strict().optional(),
}).strict();
