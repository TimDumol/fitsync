import type { Workout, WorkoutEntry } from '../api/generated';

// Keep this fallback aligned with the server's explicit exercise catalog. It
// makes a just-finished offline workout useful before its first sync.
const EXERCISE_MUSCLE_GROUPS: Record<string, { direct: string[]; indirect: string[] }> = {
  back_squat: { direct: ['quads', 'glutes'], indirect: ['hamstrings'] },
  front_squat: { direct: ['quads', 'glutes'], indirect: ['hamstrings'] },
  goblet_squat: { direct: ['quads', 'glutes'], indirect: [] },
  deadlift: { direct: ['hamstrings', 'glutes', 'back'], indirect: [] },
  bench_press: { direct: ['chest'], indirect: ['triceps', 'shoulders'] },
  overhead_press: { direct: ['shoulders'], indirect: ['triceps'] },
  pull_up: { direct: ['back'], indirect: ['biceps'] },
  barbell_row: { direct: ['back'], indirect: ['biceps'] },
  one_arm_dumbbell_row: { direct: ['back'], indirect: ['biceps'] },
  romanian_deadlift: { direct: ['hamstrings', 'glutes'], indirect: [] },
};

const GROUP_LABELS: Record<string, string> = {
  back: 'Back', biceps: 'Biceps', calves: 'Calves', chest: 'Chest', core: 'Core',
  glutes: 'Glutes', hamstrings: 'Hamstrings', quads: 'Quads', shoulders: 'Shoulders', triceps: 'Triceps',
};

const GROUP_COLORS: Record<string, string> = {
  back: '#5B8DEF', biceps: '#8B72E8', calves: '#36A269', chest: '#E06C54', core: '#D99A3D',
  glutes: '#D35D8A', hamstrings: '#3BA7A0', quads: '#D76B3F', shoulders: '#6675D9', triceps: '#9A65B8',
};

export interface MuscleSummary {
  group: string;
  label: string;
  sets: number;
  workouts: number;
  percentage: number;
  color: string;
}

export function muscleGroupsForEntry(entry: WorkoutEntry): string[] {
  const groups = entry.direct_muscle_groups?.length || entry.indirect_muscle_groups?.length
    ? [...(entry.direct_muscle_groups ?? []), ...(entry.indirect_muscle_groups ?? [])]
    : entry.muscle_groups?.length
      ? entry.muscle_groups
      : entry.exercise_key
        ? [...(EXERCISE_MUSCLE_GROUPS[entry.exercise_key]?.direct ?? []), ...(EXERCISE_MUSCLE_GROUPS[entry.exercise_key]?.indirect ?? [])]
        : [];
  return [...new Set(groups.map((group) => group.trim().toLowerCase()).filter(Boolean))];
}

export function summarizeMuscles(workouts: Workout | Workout[]): MuscleSummary[] {
  const values = Array.isArray(workouts) ? workouts : [workouts];
  const totals = new Map<string, { sets: number; workouts: Set<string> }>();
  let totalTouches = 0;

  for (const workout of values) {
    if (workout.status === 'discarded') continue;
    for (const entry of workout.entries) {
      if (entry.entry_type !== 'set' || entry.is_warmup) continue;
      for (const group of muscleGroupsForEntry(entry)) {
        const current = totals.get(group) ?? { sets: 0, workouts: new Set<string>() };
        current.sets += 1;
        current.workouts.add(workout.workout_id);
        totals.set(group, current);
        totalTouches += 1;
      }
    }
  }

  return [...totals.entries()]
    .sort(([leftGroup, left], [rightGroup, right]) => right.sets - left.sets || leftGroup.localeCompare(rightGroup))
    .map(([group, value]) => ({
      group,
      label: muscleGroupLabel(group),
      sets: value.sets,
      workouts: value.workouts.size,
      percentage: totalTouches > 0 ? (value.sets / totalTouches) * 100 : 0,
      color: GROUP_COLORS[group] ?? '#7A869A',
    }));
}

export function muscleGroupLabel(group: string): string {
  const normalized = group.trim().toLowerCase();
  return GROUP_LABELS[normalized] ?? titleCase(normalized);
}

export function muscleSummaryText(workout: Workout): string {
  const labels = summarizeMuscles(workout).map((item) => item.label);
  return labels.length > 0 ? labels.join(' · ') : 'Muscle details unavailable';
}

export function dateRangeForDays(days: number, now = new Date()): { from: string; to: string } {
  const end = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const start = new Date(end);
  start.setDate(start.getDate() - Math.max(0, days - 1));
  return { from: formatDate(start), to: formatDate(end) };
}

function formatDate(value: Date): string {
  return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, '0')}-${String(value.getDate()).padStart(2, '0')}`;
}

function titleCase(value: string): string {
  return value.replace(/[_-]+/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase());
}
