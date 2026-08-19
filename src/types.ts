export type EffortMode = 'RPE' | 'RIR' | 'Hidden';

export interface RoutineSetTemplate {
  id: string;
  category: 'warmup' | 'working';
  weightKg: number;
  targetReps: number;
}

export interface RoutineExercise {
  id: string;
  name: string;
  equipment: string;
  restSeconds: number;
  repRange: string;
  sets: RoutineSetTemplate[];
}

export interface Routine {
  id: string;
  name: string;
  notes: string;
  exercises: RoutineExercise[];
  updatedAt: string;
}

export interface PerformedSet {
  id: string;
  category: 'warmup' | 'working' | 'drop';
  weightKg: number;
  reps: number;
  effort: number | null;
  completed: boolean;
}

export interface PerformedExercise {
  id: string;
  name: string;
  equipment: string;
  restSeconds: number;
  notes: string;
  sets: PerformedSet[];
}

export interface ActiveWorkout {
  id: string;
  routineId: string | null;
  title: string;
  startedAt: string;
  updatedAt: string;
  restEndsAt: string | null;
  exercises: PerformedExercise[];
  notes: string;
}

export interface WorkoutRecord extends Omit<ActiveWorkout, 'restEndsAt'> {
  endedAt: string;
}

export interface AppSettings {
  weightUnit: 'kg' | 'lb';
  defaultRestSeconds: number;
  effortMode: EffortMode;
}

export type AppScreen =
  | 'train'
  | 'live'
  | 'history'
  | 'history-detail'
  | 'routine-editor'
  | 'settings'
  | 'summary';
