import type { Routine } from './types';

const stamp = new Date().toISOString();

export const seedRoutines: Routine[] = [
  {
    id: 'upper-a',
    name: 'Upper A',
    notes: 'Alternate horizontal push and pull movements. Keep 1–2 reps in reserve.',
    updatedAt: stamp,
    exercises: [
      {
        id: 'bench-press-barbell',
        name: 'Bench Press',
        equipment: 'Barbell',
        restSeconds: 180,
        repRange: '6–8',
        sets: [
          { id: 'bench-warmup', category: 'warmup', weightKg: 40, targetReps: 10 },
          { id: 'bench-1', category: 'working', weightKg: 75, targetReps: 8 },
          { id: 'bench-2', category: 'working', weightKg: 75, targetReps: 8 }
        ]
      },
      {
        id: 'seated-row-cable',
        name: 'Seated Row',
        equipment: 'Cable',
        restSeconds: 120,
        repRange: '8–10',
        sets: [
          { id: 'row-1', category: 'working', weightKg: 62.5, targetReps: 10 },
          { id: 'row-2', category: 'working', weightKg: 62.5, targetReps: 10 },
          { id: 'row-3', category: 'working', weightKg: 62.5, targetReps: 10 }
        ]
      },
      {
        id: 'overhead-press-dumbbell',
        name: 'Overhead Press',
        equipment: 'Dumbbell',
        restSeconds: 120,
        repRange: '8–10',
        sets: [
          { id: 'ohp-1', category: 'working', weightKg: 24, targetReps: 10 },
          { id: 'ohp-2', category: 'working', weightKg: 24, targetReps: 10 },
          { id: 'ohp-3', category: 'working', weightKg: 24, targetReps: 10 }
        ]
      },
      {
        id: 'lat-pulldown-cable',
        name: 'Lat Pulldown',
        equipment: 'Cable',
        restSeconds: 120,
        repRange: '8–12',
        sets: [
          { id: 'lat-1', category: 'working', weightKg: 60, targetReps: 10 },
          { id: 'lat-2', category: 'working', weightKg: 60, targetReps: 10 },
          { id: 'lat-3', category: 'working', weightKg: 60, targetReps: 10 }
        ]
      }
    ]
  },
  {
    id: 'lower-a',
    name: 'Lower A',
    notes: 'Squat emphasis with posterior-chain assistance.',
    updatedAt: stamp,
    exercises: [
      {
        id: 'back-squat-barbell',
        name: 'Back Squat',
        equipment: 'Barbell',
        restSeconds: 180,
        repRange: '6–8',
        sets: [
          { id: 'squat-1', category: 'working', weightKg: 100, targetReps: 8 },
          { id: 'squat-2', category: 'working', weightKg: 100, targetReps: 8 },
          { id: 'squat-3', category: 'working', weightKg: 100, targetReps: 8 }
        ]
      },
      {
        id: 'romanian-deadlift-barbell',
        name: 'Romanian Deadlift',
        equipment: 'Barbell',
        restSeconds: 180,
        repRange: '8–10',
        sets: [
          { id: 'rdl-1', category: 'working', weightKg: 80, targetReps: 10 },
          { id: 'rdl-2', category: 'working', weightKg: 80, targetReps: 10 },
          { id: 'rdl-3', category: 'working', weightKg: 80, targetReps: 10 }
        ]
      }
    ]
  },
  {
    id: 'upper-b',
    name: 'Upper B',
    notes: 'Vertical push and pull emphasis.',
    updatedAt: stamp,
    exercises: [
      {
        id: 'incline-bench-dumbbell',
        name: 'Incline Bench Press',
        equipment: 'Dumbbell',
        restSeconds: 150,
        repRange: '8–10',
        sets: [
          { id: 'incline-1', category: 'working', weightKg: 30, targetReps: 10 },
          { id: 'incline-2', category: 'working', weightKg: 30, targetReps: 10 },
          { id: 'incline-3', category: 'working', weightKg: 30, targetReps: 10 }
        ]
      }
    ]
  }
];
