import {
  ArrowLeft,
  BatteryMedium,
  CalendarDays,
  Check,
  CheckCircle2,
  ChevronRight,
  Circle,
  Dumbbell,
  Ellipsis,
  HardDrive,
  History,
  MessageSquareText,
  Pencil,
  Plus,
  Save,
  Settings,
  SkipForward,
  Smartphone,
  Trash2
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import {
  deleteDraft,
  getLatestDraft,
  initializeLocalData,
  listRoutines,
  listWorkouts,
  putDraft,
  putRoutine,
  putWorkout
} from './storage';
import type {
  ActiveWorkout,
  AppScreen,
  AppSettings,
  PerformedExercise,
  Routine,
  WorkoutRecord
} from './types';

const defaultSettings: AppSettings = {
  weightUnit: 'kg',
  defaultRestSeconds: 120,
  effortMode: 'RPE'
};

const poundsPerKilogram = 2.2046226218;

function id(prefix: string) {
  return `${prefix}-${crypto.randomUUID()}`;
}

function formatClock(seconds: number) {
  const safe = Math.max(0, Math.round(seconds));
  return `${String(Math.floor(safe / 60)).padStart(2, '0')}:${String(safe % 60).padStart(2, '0')}`;
}

function workoutDuration(startedAt: string, endedAt = new Date().toISOString()) {
  return Math.max(0, Math.floor((Date.parse(endedAt) - Date.parse(startedAt)) / 1000));
}

function formatDuration(seconds: number) {
  if (seconds < 60) return `${seconds}s`;
  return `${Math.round(seconds / 60)}m`;
}

function formatDate(iso: string) {
  return new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' }).format(new Date(iso));
}

function displayWeight(weightKg: number, unit: AppSettings['weightUnit']) {
  const value = unit === 'lb' ? weightKg * poundsPerKilogram : weightKg;
  return Number(value.toFixed(2));
}

function storedWeight(value: number, unit: AppSettings['weightUnit']) {
  const weightKg = unit === 'lb' ? value / poundsPerKilogram : value;
  return Number(weightKg.toFixed(4));
}

function formatWeight(weightKg: number, unit: AppSettings['weightUnit']) {
  return `${displayWeight(weightKg, unit).toLocaleString()} ${unit}`;
}

function formatVolume(weightKg: number, unit: AppSettings['weightUnit']) {
  return unit === 'lb'
    ? `${Math.round(weightKg * poundsPerKilogram).toLocaleString()} lb`
    : `${(weightKg / 1000).toFixed(1)} t`;
}

function targetRepsFromRange(repRange: string, fallback: number) {
  const values = repRange.match(/\d+/g);
  return values?.length ? Number(values.at(-1)) : fallback;
}

function volume(workout: Pick<ActiveWorkout, 'exercises'>) {
  return workout.exercises.reduce(
    (total, exercise) =>
      total + exercise.sets.filter((set) => set.completed).reduce((sum, set) => sum + set.weightKg * set.reps, 0),
    0
  );
}

function buildWorkout(routine?: Routine): ActiveWorkout {
  const now = new Date().toISOString();
  const fallbackExercise: PerformedExercise = {
    id: id('exercise'),
    name: 'New exercise',
    equipment: 'Other',
    restSeconds: 120,
    notes: '',
    sets: [
      {
        id: id('set'),
        category: 'working',
        weightKg: 0,
        reps: 0,
        effort: null,
        completed: false
      }
    ]
  };

  return {
    id: id('workout'),
    routineId: routine?.id ?? null,
    title: routine?.name ?? 'Empty workout',
    startedAt: now,
    updatedAt: now,
    restEndsAt: null,
    notes: '',
    exercises:
      routine?.exercises.map((exercise) => ({
        id: exercise.id,
        name: exercise.name,
        equipment: exercise.equipment,
        restSeconds: exercise.restSeconds,
        notes: '',
        sets: exercise.sets.map((set) => ({
          id: id(set.id),
          category: set.category,
          weightKg: set.weightKg,
          reps: set.targetReps,
          effort: null,
          completed: false
        }))
      })) ?? [fallbackExercise]
  };
}

function App() {
  const [screen, setScreen] = useState<AppScreen>('train');
  const [routines, setRoutines] = useState<Routine[]>([]);
  const [workouts, setWorkouts] = useState<WorkoutRecord[]>([]);
  const [draft, setDraft] = useState<ActiveWorkout | null>(null);
  const [editingRoutine, setEditingRoutine] = useState<Routine | null>(null);
  const [selectedWorkout, setSelectedWorkout] = useState<WorkoutRecord | null>(null);
  const [completedWorkout, setCompletedWorkout] = useState<WorkoutRecord | null>(null);
  const [now, setNow] = useState(Date.now());
  const [ready, setReady] = useState(false);
  const [notice, setNotice] = useState('');
  const [settings, setSettings] = useState<AppSettings>(() => {
    const saved = localStorage.getItem('fitsync-settings');
    return saved ? (JSON.parse(saved) as AppSettings) : defaultSettings;
  });

  useEffect(() => {
    let active = true;
    void (async () => {
      await initializeLocalData();
      const [savedRoutines, savedWorkouts, savedDraft] = await Promise.all([
        listRoutines(),
        listWorkouts(),
        getLatestDraft()
      ]);
      if (!active) return;
      setRoutines(savedRoutines);
      setWorkouts(savedWorkouts);
      setDraft(savedDraft);
      setReady(true);
    })();
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    localStorage.setItem('fitsync-settings', JSON.stringify(settings));
  }, [settings]);

  const completedSets = useMemo(
    () => draft?.exercises.flatMap((exercise) => exercise.sets).filter((set) => set.completed).length ?? 0,
    [draft]
  );
  const totalSets = useMemo(
    () => draft?.exercises.reduce((total, exercise) => total + exercise.sets.length, 0) ?? 0,
    [draft]
  );
  const restSeconds = draft?.restEndsAt ? Math.max(0, Math.ceil((Date.parse(draft.restEndsAt) - now) / 1000)) : 0;

  function updateDraft(transform: (current: ActiveWorkout) => ActiveWorkout) {
    setDraft((current) => {
      if (!current) return current;
      const next = { ...transform(current), updatedAt: new Date().toISOString() };
      void putDraft(next);
      return next;
    });
  }

  async function startRoutine(routine?: Routine) {
    const next = buildWorkout(routine);
    await putDraft(next);
    setDraft(next);
    setScreen('live');
  }

  function resumeWorkout() {
    if (draft) setScreen('live');
  }

  function updateSet(exerciseId: string, setId: string, key: 'weightKg' | 'reps' | 'effort', value: number | null) {
    updateDraft((current) => ({
      ...current,
      exercises: current.exercises.map((exercise) =>
        exercise.id !== exerciseId
          ? exercise
          : {
              ...exercise,
              sets: exercise.sets.map((set) => (set.id === setId ? { ...set, [key]: value } : set))
            }
      )
    }));
  }

  function toggleSet(exerciseId: string, setId: string) {
    updateDraft((current) => {
      const exercise = current.exercises.find((item) => item.id === exerciseId);
      const target = exercise?.sets.find((set) => set.id === setId);
      const completing = !target?.completed;
      return {
        ...current,
        restEndsAt: completing
          ? new Date(Date.now() + (exercise?.restSeconds ?? settings.defaultRestSeconds) * 1000).toISOString()
          : null,
        exercises: current.exercises.map((item) =>
          item.id !== exerciseId
            ? item
            : {
                ...item,
                sets: item.sets.map((set) => (set.id === setId ? { ...set, completed: !set.completed } : set))
              }
        )
      };
    });
  }

  function addSet(exerciseId: string) {
    updateDraft((current) => ({
      ...current,
      exercises: current.exercises.map((exercise) => {
        if (exercise.id !== exerciseId) return exercise;
        const previous = exercise.sets.at(-1);
        return {
          ...exercise,
          sets: [
            ...exercise.sets,
            {
              id: id('set'),
              category: 'working',
              weightKg: previous?.weightKg ?? 0,
              reps: previous?.reps ?? 0,
              effort: null,
              completed: false
            }
          ]
        };
      })
    }));
  }

  function addExercise() {
    updateDraft((current) => ({
      ...current,
      exercises: [
        ...current.exercises,
        {
          id: id('exercise'),
          name: `Custom exercise ${current.exercises.length + 1}`,
          equipment: 'Other',
          restSeconds: settings.defaultRestSeconds,
          notes: '',
          sets: [
            {
              id: id('set'),
              category: 'working',
              weightKg: 0,
              reps: 0,
              effort: null,
              completed: false
            }
          ]
        }
      ]
    }));
  }

  async function finishWorkout() {
    if (!draft) return;
    const endedAt = new Date().toISOString();
    const { restEndsAt: _restEndsAt, ...workoutData } = draft;
    const record: WorkoutRecord = {
      ...workoutData,
      endedAt,
      updatedAt: endedAt
    };
    await putWorkout(record);
    await deleteDraft(draft.id);
    setDraft(null);
    setWorkouts(await listWorkouts());
    setCompletedWorkout(record);
    setScreen('summary');
  }

  async function saveRoutine() {
    if (!editingRoutine) return;
    const next = { ...editingRoutine, updatedAt: new Date().toISOString() };
    await putRoutine(next);
    setRoutines(await listRoutines());
    setEditingRoutine(null);
    setNotice(`${next.name} saved on this device.`);
    setScreen('train');
  }

  function beginRoutineEdit(routine?: Routine) {
    const next = routine
      ? structuredClone(routine)
      : {
          id: id('routine'),
          name: 'New routine',
          notes: '',
          updatedAt: new Date().toISOString(),
          exercises: []
        };
    setEditingRoutine(next);
    setScreen('routine-editor');
  }

  function viewWorkout(workout: WorkoutRecord) {
    setSelectedWorkout(workout);
    setScreen('history-detail');
  }

  function renderTrain() {
    return (
      <section className="app-screen">
        <div className="eyebrow">{new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long' }).format(new Date())}</div>
        <h1>Ready to train?</h1>
        <p className="subtitle">Choose a routine or begin with an empty workout.</p>

        {draft && (
          <button className="resume-banner" type="button" onClick={resumeWorkout}>
            <span><History size={18} /> Resume {draft.title}</span>
            <small>Started {new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(new Date(draft.startedAt))}</small>
          </button>
        )}
        {notice && <div className="notice"><CheckCircle2 size={18} /> {notice}</div>}

        <div className="section-heading">
          <h2>Your routines</h2>
          <button className="text-action" type="button" onClick={() => beginRoutineEdit()}>New routine</button>
        </div>
        <div className="routine-list">
          {routines.map((routine) => {
            const setCount = routine.exercises.reduce((total, exercise) => total + exercise.sets.length, 0);
            return (
              <article className="routine-row" key={routine.id}>
                <div>
                  <h3>{routine.name}</h3>
                  <p>{routine.exercises.length} exercises · {setCount} sets</p>
                </div>
                <div className="row-actions">
                  <button className="icon-action" type="button" aria-label={`Edit ${routine.name}`} onClick={() => beginRoutineEdit(routine)}><Pencil size={17} /></button>
                  <button className="start-action" type="button" onClick={() => void startRoutine(routine)}>Start</button>
                </div>
              </article>
            );
          })}
        </div>
        <div className="section-heading"><h2>Flexible session</h2></div>
        <button className="secondary-action full-action" type="button" onClick={() => void startRoutine()}><Plus size={18} /> Start empty workout</button>
      </section>
    );
  }

  function renderLive() {
    if (!draft) return renderTrain();
    const progress = totalSets ? Math.round((completedSets / totalSets) * 100) : 0;
    return (
      <section className="app-screen live-screen">
        <div className="live-heading">
          <div>
            <div className="eyebrow">Active workout</div>
            <h1>{draft.title}</h1>
            <div className="live-clock">{formatClock(workoutDuration(draft.startedAt))}</div>
          </div>
          <button className="finish-action" type="button" onClick={() => void finishWorkout()}>Finish</button>
        </div>
        <div className="progress-track" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={progress}><span style={{ width: `${progress}%` }} /></div>

        {restSeconds > 0 && (
          <div className="rest-banner">
            <div><span>Rest timer</span><strong>{formatClock(restSeconds)}</strong></div>
            <button type="button" onClick={() => updateDraft((current) => ({ ...current, restEndsAt: null }))}>Skip <SkipForward size={16} /></button>
          </div>
        )}

        <div className="exercise-list">
          {draft.exercises.map((exercise) => (
            <article className="exercise-card" key={exercise.id}>
              <header>
                <div><h3>{exercise.name} · {exercise.equipment}</h3><p>{exercise.restSeconds / 60} min rest</p></div>
                <button className="icon-action" type="button" aria-label={`${exercise.name} options`}><Ellipsis size={18} /></button>
              </header>
              <div className={`set-grid set-grid-${settings.effortMode === 'Hidden' ? 'compact' : 'effort'} set-head`}>
                <span>Set</span><span>{settings.weightUnit}</span><span>Reps</span>{settings.effortMode !== 'Hidden' && <span>{settings.effortMode}</span>}<span />
              </div>
              {exercise.sets.map((set, index) => (
                <div className={`set-grid set-grid-${settings.effortMode === 'Hidden' ? 'compact' : 'effort'} set-row ${set.completed ? 'completed' : ''}`} key={set.id}>
                  <span className="set-number">{set.category === 'warmup' ? 'W' : index + 1}</span>
                  <input aria-label={`Set ${index + 1} weight in ${settings.weightUnit}`} type="number" inputMode="decimal" step="0.5" value={displayWeight(set.weightKg, settings.weightUnit)} onChange={(event) => updateSet(exercise.id, set.id, 'weightKg', storedWeight(Number(event.target.value), settings.weightUnit))} />
                  <input aria-label={`Set ${index + 1} repetitions`} type="number" inputMode="numeric" value={set.reps} onChange={(event) => updateSet(exercise.id, set.id, 'reps', Number(event.target.value))} />
                  {settings.effortMode !== 'Hidden' && <input aria-label={`Set ${index + 1} ${settings.effortMode}`} type="number" inputMode="decimal" step="0.5" value={set.effort ?? ''} placeholder="–" onChange={(event) => updateSet(exercise.id, set.id, 'effort', event.target.value === '' ? null : Number(event.target.value))} />}
                  <button className="complete-set" type="button" aria-label={set.completed ? 'Mark set incomplete' : 'Complete set'} onClick={() => toggleSet(exercise.id, set.id)}>{set.completed ? <Check size={17} /> : <Circle size={17} />}</button>
                </div>
              ))}
              <button className="add-set" type="button" onClick={() => addSet(exercise.id)}>+ Add set</button>
            </article>
          ))}
        </div>
        <div className="split-actions">
          <button className="secondary-action" type="button" onClick={() => updateDraft((current) => ({ ...current, notes: `${current.notes}${current.notes ? '\n' : ''}Session note` }))}><MessageSquareText size={17} /> Note</button>
          <button className="secondary-action" type="button" onClick={addExercise}><Plus size={17} /> Exercise</button>
        </div>
      </section>
    );
  }

  function renderRoutineEditor() {
    if (!editingRoutine) return renderTrain();
    const patchExercise = (exerciseId: string, patch: Partial<Routine['exercises'][number]>) => {
      setEditingRoutine((current) => current ? { ...current, exercises: current.exercises.map((exercise) => exercise.id === exerciseId ? { ...exercise, ...patch } : exercise) } : current);
    };
    return (
      <section className="app-screen">
        <div className="back-heading"><button type="button" aria-label="Back" onClick={() => setScreen('train')}><ArrowLeft size={18} /></button><span>Edit routine</span></div>
        <div className="form-stack">
          <label>Routine name<input value={editingRoutine.name} onChange={(event) => setEditingRoutine({ ...editingRoutine, name: event.target.value })} /></label>
          <label>Routine notes<textarea value={editingRoutine.notes} onChange={(event) => setEditingRoutine({ ...editingRoutine, notes: event.target.value })} /></label>
        </div>
        <div className="section-heading"><h2>Exercises</h2></div>
        <div className="editor-exercises">
          {editingRoutine.exercises.map((exercise) => (
            <article className="editor-exercise" key={exercise.id}>
              <div className="editor-title"><input aria-label="Exercise name" value={exercise.name} onChange={(event) => patchExercise(exercise.id, { name: event.target.value })} /><button type="button" aria-label="Remove exercise" onClick={() => setEditingRoutine({ ...editingRoutine, exercises: editingRoutine.exercises.filter((item) => item.id !== exercise.id) })}><Trash2 size={17} /></button></div>
              <div className="editor-grid">
                <label>Sets<input type="number" min="1" value={exercise.sets.length} onChange={(event) => {
                  const count = Math.max(1, Number(event.target.value));
                  const template = exercise.sets.at(-1) ?? { id: id('template'), category: 'working' as const, weightKg: 0, targetReps: 8 };
                  const sets = Array.from({ length: count }, (_, index) => exercise.sets[index] ?? { ...template, id: id('template') });
                  patchExercise(exercise.id, { sets });
                }} /></label>
                <label>Rep range<input value={exercise.repRange} onChange={(event) => {
                  const repRange = event.target.value;
                  const targetReps = targetRepsFromRange(repRange, exercise.sets.at(-1)?.targetReps ?? 8);
                  patchExercise(exercise.id, { repRange, sets: exercise.sets.map((set) => ({ ...set, targetReps })) });
                }} /></label>
                <label>Rest<select value={exercise.restSeconds} onChange={(event) => patchExercise(exercise.id, { restSeconds: Number(event.target.value) })}><option value={60}>1 min</option><option value={90}>90 sec</option><option value={120}>2 min</option><option value={180}>3 min</option></select></label>
              </div>
            </article>
          ))}
        </div>
        <button className="secondary-action full-action" type="button" onClick={() => setEditingRoutine({ ...editingRoutine, exercises: [...editingRoutine.exercises, { id: id('exercise'), name: 'New exercise', equipment: 'Other', restSeconds: settings.defaultRestSeconds, repRange: '8–10', sets: [{ id: id('template'), category: 'working', weightKg: 0, targetReps: 8 }] }] })}><Plus size={18} /> Add exercise</button>
        <div className="split-actions"><button className="secondary-action" type="button" onClick={() => setScreen('train')}>Cancel</button><button className="primary-action" type="button" onClick={() => void saveRoutine()}><Save size={17} /> Save routine</button></div>
      </section>
    );
  }

  function renderHistory() {
    return (
      <section className="app-screen">
        <div className="eyebrow">Training log</div><h1>History</h1><p className="subtitle">Completed workouts stored on this device.</p>
        <div className="section-heading"><h2>Recent workouts</h2></div>
        <div className="history-list">
          {workouts.length === 0 && <div className="empty-state"><HardDrive size={22} /><p>Your completed workouts will appear here.</p></div>}
          {workouts.map((workout) => (
            <button type="button" className="history-row" key={workout.id} onClick={() => viewWorkout(workout)}>
              <div><h3>{workout.title}</h3><p>{formatDuration(workoutDuration(workout.startedAt, workout.endedAt))} · {workout.exercises.flatMap((exercise) => exercise.sets).filter((set) => set.completed).length} sets · {formatWeight(volume(workout), settings.weightUnit)}</p></div>
              <div className="history-date">{formatDate(workout.startedAt)}<ChevronRight size={17} /></div>
            </button>
          ))}
        </div>
      </section>
    );
  }

  function renderHistoryDetail() {
    if (!selectedWorkout) return renderHistory();
    return (
      <section className="app-screen">
        <div className="back-heading"><button type="button" aria-label="Back" onClick={() => setScreen('history')}><ArrowLeft size={18} /></button><span>Workout details</span></div>
        <div className="eyebrow">{new Intl.DateTimeFormat(undefined, { dateStyle: 'long' }).format(new Date(selectedWorkout.startedAt))}</div><h1>{selectedWorkout.title}</h1>
        <SummaryCard workout={selectedWorkout} weightUnit={settings.weightUnit} />
        <div className="section-heading"><h2>Exercises</h2></div>
        <div className="history-list">
          {selectedWorkout.exercises.map((exercise) => <article className="history-exercise" key={exercise.id}><h3>{exercise.name}</h3><p>{exercise.sets.filter((set) => set.completed).map((set) => `${formatWeight(set.weightKg, settings.weightUnit)} × ${set.reps}`).join(' · ') || 'No completed sets'}</p></article>)}
        </div>
      </section>
    );
  }

  function renderSummary() {
    if (!completedWorkout) return renderHistory();
    return (
      <section className="app-screen completion-screen">
        <div className="completion-icon"><Check size={28} /></div><h1>Workout complete</h1><p className="subtitle">{completedWorkout.title} has been saved on this device.</p>
        <SummaryCard workout={completedWorkout} weightUnit={settings.weightUnit} />
        <label className="summary-note">Quick note<textarea placeholder="How did the session feel?" value={completedWorkout.notes} onChange={(event) => setCompletedWorkout({ ...completedWorkout, notes: event.target.value })} /></label>
        <button className="primary-action full-action" type="button" onClick={async () => { await putWorkout(completedWorkout); setWorkouts(await listWorkouts()); setScreen('history'); }}>Done</button>
      </section>
    );
  }

  function renderSettings() {
    return (
      <section className="app-screen">
        <div className="eyebrow">Local preferences</div><h1>Settings</h1><p className="subtitle">No account or cloud connection is configured.</p>
        <div className="section-heading"><h2>Workout defaults</h2></div>
        <div className="form-stack">
          <label>Weight unit<select value={settings.weightUnit} onChange={(event) => setSettings({ ...settings, weightUnit: event.target.value as AppSettings['weightUnit'] })}><option value="kg">Kilograms</option><option value="lb">Pounds</option></select></label>
          <label>Default rest timer<select value={settings.defaultRestSeconds} onChange={(event) => setSettings({ ...settings, defaultRestSeconds: Number(event.target.value) })}><option value={90}>90 seconds</option><option value={120}>2 minutes</option><option value={180}>3 minutes</option></select></label>
          <label>Effort scale<select value={settings.effortMode} onChange={(event) => setSettings({ ...settings, effortMode: event.target.value as AppSettings['effortMode'] })}><option>RPE</option><option>RIR</option><option>Hidden</option></select></label>
        </div>
        <div className="notice"><HardDrive size={18} /> Workouts and routines remain on this device.</div>
      </section>
    );
  }

  if (!ready) return <div className="loading-screen"><Dumbbell size={28} /><span>Loading FitSync…</span></div>;

  const appScreens: Record<AppScreen, () => React.ReactNode> = {
    train: renderTrain,
    live: renderLive,
    history: renderHistory,
    'history-detail': renderHistoryDetail,
    'routine-editor': renderRoutineEditor,
    settings: renderSettings,
    summary: renderSummary
  };
  const showNavigation = !['live', 'routine-editor', 'summary'].includes(screen);

  return (
    <div className="app-shell">
      <header className="app-bar"><strong>FitSync</strong><span><Smartphone size={15} /> Saved on device</span></header>
      <main>{appScreens[screen]()}</main>
      {showNavigation && (
        <nav className="bottom-nav" aria-label="Primary navigation">
          <button type="button" className={screen === 'train' ? 'active' : ''} onClick={() => setScreen('train')}><Dumbbell size={19} /><span>Train</span></button>
          <button type="button" className={screen === 'history' || screen === 'history-detail' ? 'active' : ''} onClick={() => setScreen('history')}><CalendarDays size={19} /><span>History</span></button>
          <button type="button" className={screen === 'settings' ? 'active' : ''} onClick={() => setScreen('settings')}><Settings size={19} /><span>Settings</span></button>
        </nav>
      )}
    </div>
  );
}

function SummaryCard({ workout, weightUnit }: { workout: WorkoutRecord; weightUnit: AppSettings['weightUnit'] }) {
  const sets = workout.exercises.flatMap((exercise) => exercise.sets).filter((set) => set.completed).length;
  return (
    <article className="summary-card"><h2>Session summary</h2><div><span><small>Time</small><strong>{formatDuration(workoutDuration(workout.startedAt, workout.endedAt))}</strong></span><span><small>Sets</small><strong>{sets}</strong></span><span><small>Volume</small><strong>{formatVolume(volume(workout), weightUnit)}</strong></span></div></article>
  );
}

export default App;
