# FitSync Workout Logging App Implementation Plan

## 1. Purpose and status

This document is the implementation specification for a React Native + Expo
workout logging application backed by the existing Fitnessd service.

The required end-to-end workflow is:

1. ChatGPT reads an athlete profile, recent health data, activities, and workout
   history through MCP.
2. ChatGPT designs a detailed workout routine and stores it as a draft training
   plan through MCP.
3. The athlete reviews and publishes the plan through ChatGPT or the app.
4. The app downloads the active plan and displays the prescribed sessions.
5. The athlete starts a prescribed session and records the actual workout,
   including sets, reps, load, duration, RIR, and notes.
6. The app uploads the completed workout and links it to the prescribed session.
7. ChatGPT retrieves the workout log and compares prescribed work with actual
   work through MCP.

This plan is normative. Implement the phases in order. When a detail here
conflicts with an older planning document, this document controls the workout
app work.

## 2. Fixed design decisions

### 2.1 Writer ownership

Do not build a general multi-writer conflict system. The two clients own
different aggregate types:

| Aggregate | Writer | Readers |
|---|---|---|
| Training plan, planned sessions, prescriptions | MCP; app may publish or skip | MCP and app |
| Workout session and actual entries | App | MCP and app |
| Garmin data | Existing ingestion process | MCP and app |
| Athlete profile and check-ins | Existing MCP actions | MCP; app support is later |

The app must not edit plan prescriptions. MCP must not edit workout logs. A
completed workout is immutable through MCP in the first release.

The only shared lifecycle operation is a planned session status. Implement it
through narrow commands that update status fields only. Never replace an entire
planned-session row merely to mark it skipped, cancelled, or completed.

### 2.2 Aggregate boundaries

A training plan is one synchronization aggregate containing:

- plan metadata;
- its planned sessions;
- each session's ordered prescription steps.

A workout is one synchronization aggregate containing:

- workout-session metadata;
- its ordered actual entries.

Children are saved in the same transaction as their parent. Mobile sync does
not synchronize individual steps or entries independently.

### 2.3 Synchronization cursor

Do not use client timestamps as a pull cursor. Do not add per-record revisions,
base revisions, a generic conflict UI, or an idempotency-result table.

Use one server-owned, monotonically increasing integer sequence. Allocate one
sequence number whenever a top-level synchronization aggregate changes:

- a plan and all of its children share the plan's new `sync_seq`;
- a workout and all of its children share the workout's new `sync_seq`.

UUIDv7 entity IDs plus transactional upsert semantics make retries safe. A
repeated identical request with the same IDs must converge on the same stored
aggregate.

### 2.4 Historical integrity

- A planned session becomes frozen when a workout referencing it is started.
- Plan revisions may replace only future, unstarted sessions.
- Publishing a new plan archives the previously active plan.
- A completed or started session cannot be cancelled by MCP.
- The prescription and actual log remain separate so analysis can compare them.
- Normal application behavior uses tombstones for top-level deletes; it does
  not hard-delete workout history.

### 2.5 Scope of the first release

The first release supports strength workouts well and permits generic timed or
distance-based interval steps. It does not need:

- social features;
- coach accounts;
- payments;
- media uploads;
- Apple Health or Health Connect integration;
- live wearable control;
- collaborative real-time editing;
- a user-editable global exercise catalog;
- exact background synchronization guarantees on iOS.

## 3. Current repository baseline

The existing server already provides:

- OAuth/OIDC authorization through Pocket ID;
- household-scoped athlete access;
- `workout_sessions` and ordered `workout_entries`;
- `training_plans` and `planned_sessions`;
- structured strength-workout repository types;
- MCP tools for health, activities, workouts, profiles, plans, and analysis;
- `/api/v1/sync/pull` and `/api/v1/sync/push` for sessions, entries, and
  documents.

Important gaps:

1. `planned_sessions.targets_raw` contains only aggregate targets. It cannot
   express an ordered routine such as warm-up, three exercises with different
   set prescriptions, rests, and a cooldown.
2. MCP can upsert one planned session, but cannot atomically create a named plan
   with its goal, sessions, and detailed prescriptions.
3. REST sync does not include plans or planned sessions.
4. The current millisecond timestamp cursor can skip a write near a pull
   boundary even when clients modify different entities.
5. A workout entry cannot identify the prescription step it fulfills.

Preserve existing v1 behavior while the app is developed. Add v2 endpoints or
additive repository services rather than breaking existing clients.

## 4. Canonical data contracts

### 4.1 Planned prescription step

Every planned step has these common fields:

```json
{
  "step_id": "UUIDv7",
  "sequence_no": 1,
  "step_type": "exercise",
  "exercise_key": "back_squat",
  "exercise_name": "Back Squat",
  "prescription": {},
  "notes": "Controlled descent"
}
```

Allowed `step_type` values:

- `warmup`
- `exercise`
- `interval`
- `rest`
- `cooldown`
- `note`

`exercise_key` is a stable machine key when one is known. `exercise_name` is
the display fallback and is required for exercise steps. Do not infer muscle
groups from an arbitrary display name.

The `prescription` object may contain:

```json
{
  "sets": [
    {
      "reps_min": 5,
      "reps_max": 5,
      "load_kg": null,
      "rir": 2,
      "rpe": null,
      "duration_seconds": null,
      "rest_seconds": 150,
      "tempo": "3-1-1"
    }
  ],
  "rounds": null,
  "distance_meters": null,
  "duration_seconds": null,
  "pace_min_per_km_min": null,
  "pace_min_per_km_max": null,
  "heart_rate_zone": null
}
```

Unknown values are `null` or omitted, never zero. A zero is valid only when it
has domain meaning. Bounds must match existing MCP conventions:

- at most 20 prescribed sets per step;
- reps: 0-1000;
- load: 0-2000 kg;
- RIR: 0-10;
- RPE: 1-10;
- duration and rest: 0-86400 seconds;
- at most 50 steps per session;
- at most 120 sessions per plan.

### 4.2 Training plan aggregate

```json
{
  "plan_id": "UUIDv7",
  "athlete_id": "UUID",
  "title": "Four-week strength foundation",
  "description": "Three strength sessions each week",
  "goal": {
    "summary": "Build consistent full-body strength",
    "target_date": "2026-10-01"
  },
  "status": "draft",
  "start_date": "2026-09-01",
  "end_date": "2026-09-28",
  "source": "mcp",
  "sessions": [
    {
      "session_id": "UUIDv7",
      "date": "2026-09-01",
      "title": "Strength A",
      "sport": "strength",
      "session_type": "full_body",
      "priority": "high",
      "targets": {"duration_minutes": 50},
      "notes": null,
      "status": "planned",
      "steps": []
    }
  ],
  "sync_seq": 42
}
```

Allowed plan statuses are `draft`, `active`, `completed`, and `archived`.
Continue storing exactly one active plan per athlete.

### 4.3 Workout aggregate

The existing structured workout fields remain canonical. Add links to the
planned aggregate:

```json
{
  "workout_id": "UUIDv7",
  "athlete_id": "UUID",
  "planned_session_id": "UUIDv7",
  "date": "2026-09-01",
  "title": "Strength A",
  "workout_type": "strength",
  "source": "mobile",
  "status": "completed",
  "started_at": "2026-09-01T18:02:00+02:00",
  "completed_at": "2026-09-01T18:51:00+02:00",
  "duration_minutes": 49,
  "perceived_effort": 7,
  "notes": "Last squat set was difficult",
  "entries": [
    {
      "entry_id": "UUIDv7",
      "planned_step_id": "UUIDv7",
      "sequence_no": 1,
      "entry_type": "set",
      "exercise_key": "back_squat",
      "exercise_name": "Back Squat",
      "set_number": 1,
      "reps": 5,
      "load_kg": 80,
      "rir": 2,
      "is_warmup": false
    }
  ],
  "sync_seq": 77
}
```

Allowed workout statuses are `in_progress`, `completed`, and `discarded`.
Actual entries may deviate from the prescription. Adding or removing an actual
set never modifies `planned_session_steps`.

## 5. Database migration

Create `migrations/00003_workout_app.sql`. Do not rewrite migrations 00001 or
00002.

### 5.1 Server sequence

Create a singleton counter:

```sql
CREATE TABLE sync_clock (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    value INTEGER NOT NULL CHECK (value >= 0)
);
INSERT INTO sync_clock (id, value) VALUES (1, 0);
```

Add `sync_seq INTEGER NOT NULL DEFAULT 0` to these top-level aggregates:

- `training_plans`
- `workout_sessions`
- `app_documents`, only if v2 continues synchronizing documents

Add indexes on `(athlete_id, sync_seq, id)` for each synchronized aggregate.

Backfill existing top-level rows with distinct positive sequence values. SQL
may use each table's SQLite `rowid` plus the maximum value assigned to the
previous table. Set `sync_clock.value` to the maximum assigned sequence. Add a
migration test proving that an initial pull after sequence zero returns legacy
workouts and plans.

Implement a shared repository helper that runs inside the caller's transaction:

```sql
UPDATE sync_clock SET value = value + 1 WHERE id = 1 RETURNING value;
```

Allocate one value per changed aggregate, not one value per child row.

### 5.2 Plan metadata

Add to `training_plans`:

- `description TEXT`
- `status TEXT NOT NULL DEFAULT 'draft'`
- `start_date TEXT`
- `end_date TEXT`
- `source TEXT NOT NULL DEFAULT 'manual'`
- `source_ref TEXT`
- `created_at INTEGER NOT NULL DEFAULT 0`
- `sync_seq INTEGER NOT NULL DEFAULT 0`

Retain the existing `active` column temporarily for compatibility. Repository
writes must keep it consistent with `status`: active plans have `active=1` and
all other statuses have `active=0`. A later migration may remove `active` after
all old code is retired.

Backfill existing plans as `active` when `active=1`, otherwise `archived`.

### 5.3 Planned sessions and steps

Add to `planned_sessions`:

- `title TEXT NOT NULL DEFAULT 'Workout'`
- `description TEXT`
- `created_at INTEGER NOT NULL DEFAULT 0`

Create:

```sql
CREATE TABLE planned_session_steps (
    id TEXT PRIMARY KEY,
    planned_session_id TEXT NOT NULL REFERENCES planned_sessions(id),
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    sequence_no INTEGER NOT NULL CHECK (sequence_no > 0),
    step_type TEXT NOT NULL CHECK (
        step_type IN ('warmup','exercise','interval','rest','cooldown','note')
    ),
    exercise_key TEXT,
    exercise_name TEXT,
    prescription_raw TEXT NOT NULL CHECK (json_valid(prescription_raw)),
    notes TEXT,
    created_at INTEGER NOT NULL,
    deleted_at INTEGER,
    UNIQUE (planned_session_id, sequence_no)
);

CREATE INDEX planned_steps_session_idx
    ON planned_session_steps (planned_session_id, sequence_no, id);
```

Validate in the repository that `planned_session_id` and `athlete_id` belong to
the same athlete. Keep the redundant athlete ID because all sensitive queries
must remain explicitly athlete-scoped.

### 5.4 Workout links

Add to `workout_sessions`:

- `planned_session_id TEXT REFERENCES planned_sessions(id)`
- `status TEXT NOT NULL DEFAULT 'completed'`
- `started_at_utc_ms INTEGER`
- `completed_at_utc_ms INTEGER`
- `sync_seq INTEGER NOT NULL DEFAULT 0`

Add an index on `(athlete_id, planned_session_id)`.

Add to `workout_entries`:

- `planned_step_id TEXT REFERENCES planned_session_steps(id)`

Do not require a planned-session link: unplanned workouts remain valid. Validate
that every linked planned session and planned step belongs to the workout's
athlete and that the step belongs to the linked session.

### 5.5 Derived planned-session state

When a completed workout references a planned session, return that planned
session as `completed` and include `completed_workout_id`, even if compatibility
columns have not yet been updated. The completion command should also update
the existing status/link columns transactionally for old readers.

## 6. Go service architecture

Do not duplicate business logic between REST and MCP.

Add repository/service methods approximately shaped as follows:

```go
type PlanWrite struct {
    Plan     TrainingPlan
    Sessions []PlannedSessionWrite
}

func (r *Repository) UpsertPlanDraft(ctx context.Context, athleteID string, in PlanWrite) (PlanResult, error)
func (r *Repository) PublishPlan(ctx context.Context, athleteID, planID string) (PlanResult, error)
func (r *Repository) SetPlannedSessionStatus(ctx context.Context, athleteID, sessionID, status string) (PlannedSession, error)
func (r *Repository) PlanByID(ctx context.Context, athleteID, planID string, includeSteps bool) (PlanResult, error)
func (r *Repository) UpsertWorkoutAggregate(ctx context.Context, athleteID string, in WorkoutWrite) (WorkoutWinner, error)
func (r *Repository) Workout(ctx context.Context, athleteID, workoutID string) (StrengthWorkout, error)
func (r *Repository) PullChanges(ctx context.Context, athleteID string, after int64, limit int) (SyncPage, error)
```

Put transaction orchestration in the repository or a new small application
service package. Transport handlers should authorize, validate transport-level
bounds, call one service method, and serialize its result.

### 6.1 Saving a plan draft

`UpsertPlanDraft` must:

1. Validate the plan ID, session IDs, and step IDs as UUIDv7.
2. Validate ownership and all bounds before starting writes.
3. Reject modification if any replaced session has a linked in-progress or
   completed workout.
4. Begin one transaction.
5. Insert or update the draft plan.
6. Replace only the draft plan's unstarted sessions and their steps.
7. Allocate one `sync_seq` and store it on the plan.
8. Commit and return the complete stored aggregate.

An existing active plan cannot be replaced through this method. It must first
be revised into a new draft plan with new IDs.

### 6.2 Publishing a plan

`PublishPlan` must be idempotent:

1. Return the plan unchanged if it is already active.
2. Require the target plan to be a non-deleted draft owned by the athlete.
3. Archive the currently active plan, if any.
4. Mark the draft active and keep the compatibility `active` values correct.
5. Allocate new sync sequences for both changed plan aggregates.
6. Commit in one transaction.

### 6.3 Saving and completing a workout

An in-progress workout is a complete replaceable aggregate owned by the app.
Saving it must not change a linked prescription.

Completing a workout must, in one transaction:

1. Validate that it is currently in progress or already completed.
2. Store the final metadata and actual entries.
3. Mark it completed and set `completed_at_utc_ms`.
4. If linked, mark the planned session completed and set its completion link.
5. Bump the containing plan's sequence so app plan views refresh.
6. Allocate and store the workout sequence.
7. Commit.

A repeated completion of the same already-completed workout is a successful
no-op when the submitted aggregate is identical.

### 6.4 Plan lifecycle rules

- `draft -> active` is allowed.
- `active -> completed` and `active -> archived` are allowed.
- `planned -> skipped` is allowed from the app.
- `planned -> cancelled` is allowed from MCP only when no workout has started.
- `planned -> completed` occurs only through a linked completed workout or an
  owned Garmin activity link.
- `completed` cannot move back to another status in the first release.
- Starting a workout freezes that planned session's prescription.

Return stable errors prefixed with `invalid_input:`, `not_found:`, `conflict:`,
or `internal_error:` as used by the existing repository.

## 7. REST API v2

Update `api/openapi.yaml` before writing handlers and regenerate
`internal/httpapi/generated.go`. Preserve v1 endpoints.

### 7.1 Identity and bootstrap

```text
GET /api/v2/me
```

Return the authenticated athlete, accessible household athletes, timezone, and
API capabilities. Do not make the app infer its athlete ID from token display
claims.

### 7.2 Plan reads

```text
GET /api/v2/athletes/{id}/training-plans/active
GET /api/v2/athletes/{id}/training-plans/{planId}
GET /api/v2/athletes/{id}/planned-sessions?from=YYYY-MM-DD&to=YYYY-MM-DD
```

Plan responses include ordered steps. Bound date ranges to 366 days. An athlete
with no active plan receives `200` with `plan: null`, not `404`.

### 7.3 Plan commands

```text
POST /api/v2/athletes/{id}/training-plans/{planId}/publish
POST /api/v2/athletes/{id}/planned-sessions/{sessionId}/skip
```

The app may publish an existing draft but cannot edit prescriptions. The skip
command is idempotent.

### 7.4 Workout commands

```text
GET /api/v2/athletes/{id}/workouts?from=&to=&limit=&cursor=
GET /api/v2/athletes/{id}/workouts/{workoutId}
PUT /api/v2/athletes/{id}/workouts/{workoutId}
POST /api/v2/athletes/{id}/workouts/{workoutId}/complete
```

`PUT` saves the full in-progress workout aggregate. Require the path ID to equal
the body ID. Accept at most 200 entries and a 1 MiB request body. The complete
endpoint accepts the final aggregate so the final save and status transition
are atomic.

### 7.5 Pull synchronization

```text
GET /api/v2/sync/pull?athlete_id={id}&after={sequence}&limit={1..50}
```

Return:

```json
{
  "changes": [
    {
      "sync_seq": 42,
      "kind": "training_plan",
      "operation": "upsert",
      "entity": {}
    }
  ],
  "next_after": 42,
  "has_more": false
}
```

Supported v2 kinds are initially `training_plan` and `workout`. Include
`document` only if the app has an immediate use for app documents.

Implementation requirements:

- query top-level aggregates with `sync_seq > after`;
- merge them in ascending `(sync_seq, kind, id)` order;
- hydrate each selected aggregate and its children in the same read transaction;
- return at most the requested number of aggregates;
- set `next_after` to the last returned sequence;
- if no changes exist, return the input `after` unchanged;
- never set the cursor from wall-clock time;
- include top-level tombstones as `operation: delete` with no sensitive payload.

An initial pull uses `after=0`. The migration backfill guarantees that existing
plans and workouts are returned.

## 8. MCP changes

Follow existing authentication, athlete targeting, annotations, error prefixes,
schema bounds, and `additionalProperties: false` conventions.

### 8.1 Add `upsert_training_plan_draft`

Purpose: atomically create or replace one complete draft plan.

Input:

```json
{
  "target_athlete_id": null,
  "plan_id": "UUIDv7",
  "title": "Four-week strength foundation",
  "description": "Three sessions per week",
  "goal": {},
  "start_date": "2026-09-01",
  "end_date": "2026-09-28",
  "source_ref": "optional conversation reference",
  "sessions": []
}
```

Each session contains its ordered steps using the canonical contracts above.
The server sets `source=mcp`. This is an idempotent, non-destructive write tool.

Return the stored plan aggregate, counts of sessions and steps, and `sync_seq`.
Do not return only an acknowledgement; ChatGPT must be able to verify what was
stored.

### 8.2 Add `publish_training_plan`

Input contains `target_athlete_id` and `plan_id`. It activates the draft and
archives the previous active plan. Mark it as a write action and destructive
because it replaces the active routine, even though history is retained.

Return the published plan and the ID of the archived plan when applicable.

### 8.3 Enhance `get_training_plan`

Add:

- optional `plan_id`, with active plan as the default;
- `include_steps`, default true;
- plan metadata and status;
- session titles and descriptions;
- ordered prescription steps;
- linked completion summaries.

Keep existing date bounds and athlete authorization.

### 8.4 Add `get_workout`

Input contains `workout_id` and optional target athlete. Return one authorized
workout with its actual entries and, when linked, the planned-session
prescription. Include a deterministic prescribed-versus-actual comparison, but
do not generate coaching prose in the server.

Keep `get_strength_workouts` for list and aggregate queries. Enhance its output
with `planned_session_id` but do not break existing fields.

### 8.5 ChatGPT interaction sequence

The intended tool sequence is:

1. `get_athlete_profile`
2. `get_training_context` and relevant history actions
3. ChatGPT proposes the routine in conversation
4. after user agreement, `upsert_training_plan_draft`
5. ChatGPT summarizes the exact stored draft
6. after publication approval, `publish_training_plan`
7. later, `get_workout` or `get_strength_workouts` to inspect results

Do not make ChatGPT perform one MCP call per session or step. The complete plan
must be accepted as one bounded atomic graph.

## 9. Expo application

Create the app under `apps/mobile/` so the Go repository root remains clean.
Use an Expo-managed TypeScript application.

Recommended dependencies:

- Expo Router for navigation;
- `expo-auth-session` for OAuth authorization code + PKCE;
- `expo-secure-store` for refresh and access tokens;
- `expo-sqlite` for durable local state;
- TanStack Query for HTTP reads and cache invalidation;
- `zod` for runtime validation of persisted and API data;
- `date-fns` or an equally small date library;
- `expo-notifications` only after core logging works.

Do not store OAuth tokens in AsyncStorage or SQLite. Do not log token values,
health payloads, workout notes, or injury information.

### 9.1 Suggested layout

```text
apps/mobile/
  app/
    _layout.tsx
    sign-in.tsx
    (tabs)/
      today.tsx
      plan.tsx
      history.tsx
      settings.tsx
    planned-session/[id].tsx
    workout/[id].tsx
    workout/[id]/summary.tsx
  src/
    api/
      client.ts
      generated.ts
    auth/
      AuthProvider.tsx
      tokenStore.ts
    db/
      migrations.ts
      repository.ts
    sync/
      pull.ts
      outbox.ts
    workout/
      reducer.ts
      types.ts
    components/
    hooks/
  app.json
  package.json
  tsconfig.json
```

Generate or derive the TypeScript API client from the OpenAPI contract. Do not
hand-maintain a second, incompatible set of transport interfaces.

### 9.2 Local SQLite data

Store:

- selected athlete ID;
- last successful `sync_seq` cursor per athlete;
- cached plan aggregates;
- cached workout aggregates;
- active workout drafts;
- a small outbox of pending workout saves/completions.

An outbox item contains operation type, aggregate ID, serialized request, retry
count, and next-attempt time. IDs are generated before the first local save.
Successful server responses replace local aggregates and remove the outbox
item.

### 9.3 Authentication

Use the existing Pocket ID authorization and token endpoints with PKCE. The app
must:

1. launch the system browser;
2. handle an Expo deep-link callback;
3. exchange the authorization code;
4. store tokens in SecureStore;
5. refresh before expiry;
6. clear credentials and local sensitive caches on logout;
7. call `/api/v2/me` to resolve accessible athletes.

Use `fitness:read fitness:write openid offline_access` unless server scope policy
is intentionally narrowed later.

### 9.4 Screens

#### Today

- selected athlete and local date;
- today's planned session or a rest-day state;
- prescribed duration and summary;
- start/resume button;
- visible offline or pending-sync status.

#### Plan

- calendar or grouped chronological list;
- active plan title, date range, and goal;
- badges for planned, completed, skipped, and cancelled sessions;
- a separate section for MCP-created drafts with a publish action.

#### Planned session detail

- ordered prescription steps;
- all target sets or intervals;
- notes and coaching cues;
- completed-workout summary when available;
- start, resume, or view-result action depending on state.

#### Active workout

- prescribed exercises and sets prepopulated as actual-entry drafts;
- one-tap set completion;
- editable reps, load, RIR/RPE, duration, and notes;
- add/remove set and add unplanned exercise;
- rest timer;
- elapsed workout timer;
- local autosave after every meaningful edit;
- finish and discard actions with confirmation.

Do not send one network request per set. Persist locally immediately and upload
the complete aggregate opportunistically.

#### History

- paginated workouts ordered newest first;
- planned/unplanned indicator;
- summary of duration, effort, exercises, working sets, and volume;
- detail view with prescribed-versus-actual values.

#### Settings

- athlete selector;
- units and timezone display;
- last sync cursor/time;
- pending outbox count and manual retry;
- logout.

### 9.5 Active workout state machine

Use an explicit reducer with these states:

```text
idle -> starting -> active -> finishing -> completed
                         \-> discarded
```

The reducer owns entries, timers, validation, dirty state, and completion. The
screen must not scatter this logic across component-local state.

Starting a prescribed workout:

1. generate a workout UUIDv7 and entry UUIDv7 values;
2. copy prescribed set defaults into actual-entry drafts;
3. retain `planned_session_id` and `planned_step_id` links;
4. save locally before navigating to the active screen;
5. enqueue an in-progress upload when online.

On application launch, detect and offer to resume any active local workout.

### 9.6 Sync behavior

Run pull sync:

- after sign-in;
- when the selected athlete changes;
- when the app returns to the foreground;
- after a successful workout upload;
- through pull-to-refresh.

Process changes in ascending sequence order in one local SQLite transaction.
Advance the local cursor only after all changes in the response are committed.
Continue while `has_more=true`.

Run the outbox before or after pull consistently. Recommended order is:

1. upload pending workout mutations;
2. pull server changes;
3. refresh visible queries.

Exact background execution is not required. Correctness must depend on durable
local state and foreground retry.

## 10. Validation and security

- Authorize every repository query by athlete ID after household authorization.
- Never trust athlete ID, planned-session ownership, or planned-step ownership
  solely because it appears in a request body.
- Require UUIDv7 for all newly created plan, session, step, workout, and entry
  IDs.
- Validate dates as real calendar dates and timestamps as RFC 3339 with offsets.
- Enforce JSON array, string, request-body, date-range, and pagination bounds.
- Return normalized data through REST and MCP; never return raw Garmin payloads,
  FIT archives, or GPS coordinates from the new actions.
- Do not log request bodies for plan or workout mutations.
- Keep medical/injury profile fields out of mobile caches until a screen needs
  them.
- Use HTTPS outside local development.

## 11. Testing requirements

### 11.1 Database and repository tests

Add tests for:

- migration up/down/up and foreign-key integrity;
- sync-sequence backfill of existing plans and workouts;
- globally increasing sequence allocation;
- atomic plan graph insertion and rollback;
- duplicate session/step sequences;
- cross-athlete plan, session, step, and workout links;
- publishing archives the old active plan;
- repeated publish is idempotent;
- started/completed prescriptions cannot be replaced;
- workout completion links the planned session atomically;
- unplanned workouts remain valid;
- repeated identical workout save/completion converges;
- initial pull after zero and paginated incremental pulls;
- a write committed after a pull snapshot appears on the next pull.

### 11.2 REST tests

Add OpenAPI/handler tests for:

- authentication and household isolation;
- no-active-plan response;
- complete plan graph serialization;
- request and graph bounds;
- invalid UUID/date/timestamp/status transitions;
- in-progress save and atomic completion;
- pull ordering, cursor advancement, `has_more`, and tombstones;
- request-body limits;
- stable error responses.

### 11.3 MCP tests

Add tests for:

- all new tools are registered with correct annotations;
- schemas reject unknown fields and oversized graphs;
- one plan call persists its complete graph atomically;
- invalid step 100 rolls back steps 1-99;
- draft and publish workflow;
- active/archived plan behavior;
- enhanced plan reads include ordered steps;
- `get_workout` returns linked prescribed and actual data;
- unauthorized athlete IDs do not reveal existence;
- tool results remain bounded and omit raw health data.

### 11.4 Mobile tests

Use unit tests for:

- workout reducer transitions;
- prescription-to-actual draft conversion;
- local autosave and restore;
- outbox retry and removal;
- sync cursor advancement only after transaction commit;
- ordered change application and pagination;
- token-store behavior without printing secrets;
- date display in the athlete timezone.

Add a small device E2E suite, using Maestro or an equivalent Expo-compatible
runner, covering sign-in with a test issuer, start workout, record sets, restart,
resume, complete, and observe synced history.

## 12. Implementation phases and checks

### Phase 1: Schema and domain types

1. Add migration 00003.
2. Add planned-step and aggregate Go types.
3. Add sequence allocation helper.
4. Add migration and validation tests.

Check:

```bash
./scripts/test.sh --go
./scripts/test.sh --vet
```

### Phase 2: Repository services

1. Implement plan draft upsert and publish.
2. Implement plan reads with steps.
3. Extend workout writes with planned links and status.
4. Implement atomic completion and lifecycle rules.
5. Implement sequence-based aggregate pull.

Check:

```bash
./scripts/test.sh --go
```

### Phase 3: MCP

1. Add plan draft and publish schemas/tools.
2. Enhance `get_training_plan`.
3. Add `get_workout` and planned links to workout lists.
4. Add action tests and realistic bounded fixtures.

Check:

```bash
./scripts/test.sh --go
```

### Phase 4: REST v2

1. Update OpenAPI first.
2. Regenerate Go transport code.
3. Add `/me`, plan, workout, completion, and pull handlers.
4. Add contract and authorization tests.

Check:

```bash
go generate ./...
./scripts/test.sh --go
./scripts/test.sh --vet
```

Generation must be deterministic: a second `go generate ./...` must leave the
worktree unchanged.

### Phase 5: Expo shell and authentication

1. Scaffold `apps/mobile`.
2. Add navigation, configuration, auth, SecureStore, and `/me`.
3. Add local SQLite migrations and generated API types.
4. Implement athlete selection.

Check with mobile lint, typecheck, and tests defined as package scripts. Do not
place build output or credentials in Git.

### Phase 6: Plan and synchronization UI

1. Implement sequence pull and local plan persistence.
2. Build Today, Plan, and planned-session detail screens.
3. Display draft/active status and publish drafts.
4. Add offline and pending-sync indicators.

### Phase 7: Workout logging

1. Implement the workout reducer.
2. Implement local start, autosave, restore, and discard.
3. Implement actual-set editing and rest timer.
4. Add outbox upload and atomic completion.
5. Build history and prescribed-versus-actual detail.

### Phase 8: End-to-end hardening

1. Exercise the complete ChatGPT -> database -> app -> workout -> MCP loop.
2. Test offline restart and delayed upload.
3. Test token expiry and refresh.
4. Review logs for sensitive content.
5. Run all repository entrypoints.

Final check:

```bash
./scripts/test.sh
./scripts/build.sh
```

## 13. Definition of done

The work is complete only when this scenario passes against a disposable test
database and test identity provider:

1. An athlete has a profile and recent synthetic training data.
2. ChatGPT retrieves the context through MCP.
3. One MCP call stores a four-week draft with multiple sessions and detailed
   strength prescriptions.
4. Publishing makes the plan active and archives the previous plan.
5. A fresh app installation pulls after sequence zero and displays the plan.
6. The app starts a prescribed workout while online, then loses connectivity.
7. The athlete records sets and terminates the app process.
8. Relaunch restores the exact active workout from SQLite.
9. Completion while offline creates a durable outbox item.
10. Reconnection uploads the workout exactly once, links it to the planned
    session, and advances local sync state.
11. The app shows the session as completed with actual results.
12. MCP retrieves the workout and its prescribed-versus-actual details.
13. No plan step, workout entry, token, raw health payload, or private data from
    another athlete is leaked through errors or logs.

## 14. Working rules for implementation agents

- Read only targeted files and ranges. Do not inspect private data or dumps.
- Preserve unrelated user changes in the existing dirty worktree.
- Update OpenAPI before generated handlers or mobile transport types.
- Add tests in the same phase as behavior; do not postpone all tests to the end.
- Use synthetic fixtures only.
- Prefer focused verification during iteration, then run the complete repository
  checks before handoff.
- Do not begin the Expo UI until the plan aggregate and API contracts are tested.
- Do not silently broaden scope into nutrition, medical advice, social features,
  or wearable integration.
- If an implementation choice is not specified, choose the smallest design that
  preserves the writer-ownership model and the end-to-end acceptance scenario.
