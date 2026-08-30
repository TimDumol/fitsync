import { z } from 'zod';
import { planSchema, plannedSessionSchema, workoutSchema } from './schemas';

const athlete = z.object({ athlete_id: z.string().uuid(), handle: z.string(), timezone: z.string(), units: z.enum(['metric']).optional() }).strict();
export const meResponseSchema = z.object({ athlete, accessible_athletes: z.array(athlete), capabilities: z.object({ plans: z.boolean().optional(), workouts: z.boolean().optional(), sync: z.boolean().optional() }).strict() }).strict();
export const syncPageSchema = z.object({ changes: z.array(z.object({ sync_seq: z.number(), kind: z.enum(['training_plan', 'workout']), operation: z.enum(['upsert', 'delete']), entity_id: z.string().uuid(), entity: z.unknown().optional() }).strict()), next_after: z.number(), has_more: z.boolean() }).strict();
export const planResponseSchema = z.object({ plan: planSchema.nullable() }).strict();
export const plannedSessionsResponseSchema = z.object({ sessions: z.array(plannedSessionSchema) }).strict();
export const publishPlanResponseSchema = z.object({ plan: planSchema, archived_plan_id: z.string().uuid().nullable().optional() }).strict();
export const workoutPageSchema = z.object({ items: z.array(workoutSchema), next_cursor: z.string().nullable().optional() }).strict();
export { planSchema, plannedSessionSchema, workoutSchema };
