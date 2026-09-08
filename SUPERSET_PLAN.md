# Structured supersets and round groups implementation plan

## 1. Purpose

Add explicit, end-to-end support for supersets and other non-nested groups of
exercises that are performed in rounds. A planned workout must be able to say
that exercises A and B are performed as:

```text
A set 1 -> B set 1 -> recovery
A set 2 -> B set 2 -> recovery
A set 3 -> B set 3
```

The server, MCP planner contract, REST API, mobile preview, active workout,
offline draft, and completed workout comparison must agree on that meaning.
The planner must no longer encode execution-critical structure only in free-text
notes.

This document is normative. Implement the phases in order. The scope includes
LLM-mediated migration of existing eligible routines on the live server after
the new contract has been deployed. Do not write an automatic note parser or a
SQL data-rewrite script for that semantic migration.

## 2. Current behavior and the bug being fixed

The existing model has an ordered flat `planned_session_steps` list. A step has
an exercise and a prescription, but no relationship to adjacent exercises.
`session_type` is a free string and cannot describe multiple differently
structured sections within one session.

The current mobile conversion in `apps/mobile/src/workout/types.ts` walks each
step and emits all of its prescribed sets before moving to the next step. For
two steps A and B with three sets each, it emits:

```text
A1, A2, A3, B1, B2, B3
```

Putting `Superset A`, `A1`, or `A2` in session or step notes changes neither
the order nor timer behavior. The active workout also defaults a missing
`rest_seconds` to 120 seconds after every set, which is usually wrong between
members of a superset.

The existing E2E fixture in `apps/mobile/src/api/e2e.ts` deliberately exhibits
this problem: `session_type` is `superset`, while actual group membership is
only described by notes. Update that fixture as part of this work so it proves
the structured behavior instead.

## 3. Fixed design decisions

### 3.1 Use a compositional round-group primitive

Do not implement supersets by branching on `planned_session.session_type`.
One session may contain ungrouped straight sets, several supersets, and a
circuit. `session_type` remains descriptive metadata and does not control
execution.

Add zero or more `groups` to each planned session. Each group references two or
more existing steps by their session-local `sequence_no` values. The first
release has one execution mode, `rounds`:

```json
{
  "group_id": "0199...",
  "label": "Superset A",
  "mode": "rounds",
  "rounds": 3,
  "step_sequence_nos": [2, 3],
  "notes": "Keep transitions short"
}
```

This is explicit superset support without defining separate execution engines
for `superset`, `tri_set`, `giant_set`, and `circuit`:

- two members are displayed as a superset by default;
- three or more members are displayed as a circuit by default;
- a non-empty `label` overrides the generated display label;
- every group is executed member-by-member for each round;
- groups cannot nest or overlap in the first release.

`mode` is retained even though only `rounds` is initially allowed. It gives a
clean extension point for a later, separately designed timed mode. Do not add
AMRAP, EMOM, ladders, drop sets, interval trees, conditional branches, or
nested blocks in this implementation.

### 3.2 Keep steps as the exercise prescription source of truth

Group objects contain execution structure only. Exercise names, loads, reps,
RIR, tempo, and per-set rest remain on the referenced `PlannedStep` objects.
Do not copy exercise prescriptions into a group.

For a round group with `rounds = N`, every member step must:

- have `step_type = exercise`;
- have exactly N objects in `prescription.sets`;
- specify a non-null `rest_seconds` on every prescribed set.

For grouped steps, `rest_seconds` means recovery after that particular member
has been performed in that round. A normal superset therefore uses a short or
zero recovery on the first member and the full recovery on the last member:

```json
{
  "groups": [
    {
      "group_id": "0199...",
      "label": "Superset A",
      "mode": "rounds",
      "rounds": 3,
      "step_sequence_nos": [2, 3]
    }
  ],
  "steps": [
    {
      "sequence_no": 2,
      "step_type": "exercise",
      "exercise_name": "Bench press",
      "prescription": {
        "sets": [
          {"reps_min": 8, "reps_max": 10, "rir": 2, "rest_seconds": 0},
          {"reps_min": 8, "reps_max": 10, "rir": 2, "rest_seconds": 0},
          {"reps_min": 8, "reps_max": 10, "rir": 2, "rest_seconds": 0}
        ]
      }
    },
    {
      "sequence_no": 3,
      "step_type": "exercise",
      "exercise_name": "Barbell row",
      "prescription": {
        "sets": [
          {"reps_min": 8, "reps_max": 10, "rir": 2, "rest_seconds": 120},
          {"reps_min": 8, "reps_max": 10, "rir": 2, "rest_seconds": 120},
          {"reps_min": 8, "reps_max": 10, "rir": 2, "rest_seconds": 120}
        ]
      }
    }
  ]
}
```

Do not add another group-level rest field in this release. Per-member,
per-round `rest_seconds` supports both immediate transitions and circuits with
short rests between stations. It also avoids two competing rest values.

### 3.3 Preserve the actual workout storage model

Do not add a group ID or round number to `workout_entries`. Existing actual
entries already have:

- a global `sequence_no`, which records execution order;
- `planned_step_id`, which identifies the prescribed exercise;
- `set_number`, which is the round number for a grouped step.

The planned session embedded in a mobile workout supplies group metadata while
the workout is active. The server's existing plan-versus-actual comparison
groups entries by `planned_step_id`, so it must continue to work after entries
are interleaved. Add tests proving this.

Completed workout history must remain readable even if it was recorded before
groups existed. Do not rewrite completed workout entries.

### 3.4 Compatibility

- A session with absent or empty `groups` behaves exactly as it does today.
- Existing ungrouped plan rows require no structural data backfill.
- `groups` is optional on input and is serialized as an empty array on output.
- Do not infer groups at API read time from `session_type` or notes.
- Do not reinterpret the existing top-level `Prescription.rounds`; leave its
  current interval-oriented compatibility behavior unchanged. New exercise
  round structure uses session `groups` only.
- Old mobile builds will ignore neither unknown fields nor malformed shapes:
  their Zod objects are strict. Therefore deploy a group-aware mobile build
  before semantically migrating/publishing the live active plan, or coordinate
  the rollout so the old build is no longer used. The server schema deployment
  alone is safe while all groups remain empty.

## 4. Canonical contract

### 4.1 `PlannedStepGroup`

Add the following Go/domain and wire concept:

```go
type PlannedStepGroup struct {
    ID                  string `json:"group_id"`
    IdempotencyKey      string `json:"idempotency_key,omitempty"` // MCP only
    PlannedSessionID    string `json:"planned_session_id,omitempty"`
    AthleteID           string `json:"athlete_id,omitempty"`
    Label               string `json:"label,omitempty"`
    Mode                string `json:"mode"`
    Rounds              int    `json:"rounds"`
    StepSequenceNumbers []int  `json:"step_sequence_nos"`
    Notes               *string `json:"notes,omitempty"`
}
```

Use the idiomatic field name `StepSequenceNos` instead of
`StepSequenceNumbers` if that better matches the surrounding Go code, but the
JSON property must be exactly `step_sequence_nos`.

Bounds and semantic validation:

- `group_id`: UUIDv7 when supplied to an MCP write; the server may generate it;
- `idempotency_key`: follow the same rules as plan, session, and step keys;
- `label`: 0-128 UTF-8 bytes;
- `mode`: exactly `rounds`;
- `rounds`: integer 1-20;
- `step_sequence_nos`: 2-10 unique integers, each in 1-50;
- `notes`: nullable, at most 4000 UTF-8 bytes;
- every referenced sequence number must exist in the same session;
- every referenced step must be an exercise step with exactly `rounds` sets;
- every set of a referenced step must have explicit `rest_seconds`;
- a step may belong to at most one group;
- members must be listed in execution order;
- grouped steps must occupy one contiguous range in the session's sorted step
  list. For example `[2, 3]` is valid and `[2, 4]` is invalid if step 3 is not
  in the same group.

Contiguity keeps legacy flat rendering sensible and makes ordering groups among
ungrouped steps deterministic. A group's position in the session is the
position of its first member step. `step_sequence_nos` need not be numerically
consecutive if the session sequence numbers have gaps, but no other live step
may sort between two group members.

Validation failures use the existing `invalid_input:` prefix and a concise
reason, for example:

```text
invalid_input: group references an unknown step sequence
invalid_input: a planned step belongs to more than one group
invalid_input: grouped exercise set count must equal group rounds
invalid_input: grouped exercise sets require explicit rest_seconds
invalid_input: group members must be contiguous
```

### 4.2 Session shape

Add `Groups []PlannedStepGroup json:"groups,omitempty"` to
`fitnessrepo.PlannedSession` and `fitnessrepo.PlannedSessionWrite`. Ensure the
draft input, plan output, REST output, mobile generated types, and runtime Zod
schemas all expose the same property.

MCP draft input example:

```json
{
  "title": "September strength plan",
  "sessions": [
    {
      "date": "2026-09-07",
      "title": "Full body",
      "sport": "strength",
      "session_type": "full_body",
      "priority": "normal",
      "groups": [
        {
          "idempotency_key": "sep-strength-2026-09-07-superset-a",
          "label": "Superset A",
          "mode": "rounds",
          "rounds": 3,
          "step_sequence_nos": [2, 3]
        }
      ],
      "steps": []
    }
  ]
}
```

Update MCP schema descriptions, not only property types. Tell the planner that:

- notes must not be used as the sole representation of a superset or circuit;
- all round groups go in `groups`;
- a two-member round group is a superset;
- `rest_seconds` is recovery after the individual group member;
- set counts must equal `rounds`;
- transitions normally use `0` on non-final members and the intended recovery
  on the final member.

Keep notes available for coaching cues that do not control execution.

## 5. Server database migration

Create the next Goose migration after `00004_mcp_idempotency.sql`. Do not edit
an already-applied migration.

Add a normalized top-level group table whose member list is stored as validated
JSON:

```sql
CREATE TABLE planned_step_groups (
    id TEXT PRIMARY KEY,
    planned_session_id TEXT NOT NULL REFERENCES planned_sessions(id),
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    label TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL CHECK (mode IN ('rounds')),
    rounds INTEGER NOT NULL CHECK (rounds BETWEEN 1 AND 20),
    step_sequence_nos_raw TEXT NOT NULL CHECK (json_valid(step_sequence_nos_raw)),
    notes TEXT,
    created_at INTEGER NOT NULL,
    deleted_at INTEGER,
    idempotency_key TEXT
);

CREATE INDEX planned_step_groups_session_idx
    ON planned_step_groups (planned_session_id, id);

CREATE UNIQUE INDEX planned_step_groups_athlete_idempotency_idx
    ON planned_step_groups (athlete_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;
```

The Go repository, not a SQLite JSON expression, enforces array length,
integer values, membership, overlap, contiguity, and referenced-step rules.
The table deliberately mirrors the aggregate-child/tombstone approach used by
`planned_session_steps`.

The migration's `Down` section drops the two indexes and then the table.
Running the schema migration must not derive groups from existing notes and
must not alter any existing plan, session, step, or workout row.

## 6. Server repository implementation

### 6.1 Types and equality

In `internal/fitnessrepo/types.go`:

- add `PlannedStepGroup`;
- add `Groups` to planned-session read and write types;
- keep group IDs/internal ownership fields out of REST/MCP inputs where the
  surrounding input/output split already does so.

Update draft equality/idempotency comparison so groups participate. Compare
groups in stable order and compare `step_sequence_nos` exactly in declared
member order. A retry of an identical draft must not allocate a new sync
sequence.

### 6.2 Validation

Extend `validatePlanWrite` after all session steps have been validated. Build a
map from step sequence number to step, then validate groups against that map.
Do not scatter cross-reference validation inside `validateStep` because a
single step does not have enough context.

Also validate:

- unique group IDs within the plan request;
- valid UUIDv7 group IDs;
- valid/unique idempotency keys through the existing helper;
- group IDs owned by another athlete cannot be reused;
- no group can refer to a step in another session.

### 6.3 Transactional write behavior

Extend `UpsertPlanDraft` in `internal/fitnessrepo/workout_app.go`:

1. Preserve the existing lock rule for started/completed sessions.
2. Tombstone live `planned_step_groups` belonging to sessions of the draft
   before tombstoning/replacing those sessions.
3. Upsert sessions and steps as today.
4. Upsert groups in the same transaction after their referenced steps.
5. Serialize `step_sequence_nos_raw` as a JSON integer array in declared order.
6. Use the same transaction timestamp and plan aggregate `sync_seq` behavior as
   the other children.
7. Normalize ownership/uniqueness failures to the existing stable error
   prefixes.

Add `PlannedStepGroupIDForIdempotencyKey` alongside the existing lookup
functions in `internal/fitnessrepo/idempotency.go`. In the MCP handler, resolve
each omitted `group_id` before calling the repository, just as plan, session,
and step IDs are resolved now.

### 6.4 Reads

Add `loadGroups` beside `loadSteps` and load groups whenever `includeSteps` is
true. For the purposes of the public contract, `include_steps` includes the
execution grouping of those steps. When steps are omitted, groups must also be
omitted.

Return groups in deterministic session order:

1. sequence number of the first member step ascending;
2. group ID ascending as a defensive tie-breaker.

Malformed stored member JSON is an `internal_error:`; do not silently return an
empty group or leak raw JSON.

Plan pull/synchronization remains a single training-plan aggregate. Group
changes bump the plan's `sync_seq`; do not create a new sync change kind.

## 7. MCP changes

Update `internal/mcpserver/server.go` and `schema_common.go`:

- add group input/output structs where necessary;
- add bounded `plannedStepGroupSchema()` with
  `additionalProperties: false`;
- add `groups` to every full plan-draft session schema and plan output;
- resolve generated group IDs and idempotency keys in `upsertPlanDraft`;
- count groups in the write result by adding `group_count` to
  `PlanDraftOutput`;
- convert groups in `toPlannedSessionOutput` without exposing athlete IDs;
- improve `upsert_training_plan_draft`'s tool/property descriptions so an LLM
  selects groups rather than notes for execution structure.

Do not silently parse existing notes in an MCP handler. Semantic migration is
a separate, explicit LLM operation described in section 12.

The older `upsert_planned_session` action currently does not provide the main
atomic detailed-plan workflow. Do not expand its responsibility as part of
this work unless tests show it already round-trips steps. Structured groups
must be fully supported by `upsert_training_plan_draft`, which is the planner's
canonical writer.

## 8. REST/OpenAPI changes

In `api/openapi.yaml` add `PlannedStepGroup` and an optional `groups` array on
`PlannedSession`:

```yaml
PlannedStepGroup:
  type: object
  additionalProperties: false
  required: [group_id, mode, rounds, step_sequence_nos]
  properties:
    group_id: { type: string, format: uuid }
    label: { type: string, maxLength: 128 }
    mode: { type: string, enum: [rounds] }
    rounds: { type: integer, minimum: 1, maximum: 20 }
    step_sequence_nos:
      type: array
      minItems: 2
      maxItems: 10
      uniqueItems: true
      items: { type: integer, minimum: 1, maximum: 50 }
    notes: { type: string, nullable: true, maxLength: 4000 }
```

Regenerate `internal/httpapi/generated.go` through the repository's normal
generation path; do not hand-maintain generated output if generation is
available. Update `apiPlannedSession` to convert groups and always emit a
non-null empty slice for a full session without groups.

No workout write payload change is required. A workout's embedded
`planned_session` in responses naturally carries groups through the updated
session conversion.

## 9. Mobile contracts and offline storage

Update:

- `apps/mobile/src/api/generated.ts`;
- `apps/mobile/src/api/schemas.ts`;
- API fixtures and client/schema tests.

Add:

```ts
export interface PlannedStepGroup {
  group_id: UUID;
  label?: string;
  mode: 'rounds';
  rounds: number;
  step_sequence_nos: number[];
  notes?: string | null;
}
```

Add `groups?: PlannedStepGroup[]` to `PlannedSession`. Normalize absent groups
to `[]` at the API boundary or consistently use `session.groups ?? []`; do not
allow scattered unsafe assumptions.

No mobile SQLite schema migration is necessary because cached plans and active
drafts are JSON blobs. Existing cached plans without groups remain valid.
Nevertheless, incrementing the mobile SQLite schema version is not a substitute
for runtime backward compatibility and should not be done merely for this
field addition.

## 10. Mobile execution semantics

### 10.1 Build a tested execution-order helper

Do not embed group expansion logic directly in JSX. Add pure helpers in
`apps/mobile/src/workout/types.ts` or a focused `execution.ts` module.

Given sorted session steps and groups:

1. Build a map from member step sequence number to its group.
2. Walk session steps in ascending `sequence_no`.
3. For an ungrouped step, expand it exactly as today.
4. At the first member of a group, emit all group rounds and skip later members
   during the outer walk.
5. Within each round, emit members in `step_sequence_nos` order.
6. For member step S in zero-based round R, use
   `S.prescription.sets[R]` and set the actual entry's `set_number` to `R + 1`.
7. Preserve a single globally increasing actual-entry `sequence_no`.
8. Set `planned_step_id`, exercise identity, reps, load, RIR, duration, and
   `is_warmup` as the current conversion does.
9. Carry `step.notes` into each generated entry's `notes`, or display the note
   by resolving `planned_step_id` from the embedded session. Prefer resolution
   for display if duplicating the note into every completed entry would make
   history noisy.

Example expected order for group members 2 and 3 with three rounds:

```text
step 2 set 1
step 3 set 1
step 2 set 2
step 3 set 2
step 2 set 3
step 3 set 3
```

`prescribedRestSeconds` continues to resolve rest from the actual entry's
planned step and `set_number`. Because grouped steps require explicit rests,
it must not use the 120-second fallback for them.

### 10.2 Planned-session preview

Update `apps/mobile/app/planned-session/[id].tsx` to render a group as one
visual section rather than unrelated cards:

- header: explicit label or generated `Superset`/`Circuit` label;
- subtitle: `N rounds`;
- member labels such as `A1`, `A2` or `1`, `2`;
- each member's per-round prescription;
- transition/recovery text derived from each set's `rest_seconds`;
- group notes and exercise coaching notes in distinguishable locations.

Ungrouped steps keep the existing card layout. The screen's exercise count is
the number of exercise steps, not the number of groups. Its step count may
continue to count every planned step.

### 10.3 Active workout

The first release may retain the editable list rather than introducing a
single-card wizard, but it must make execution order and group boundaries
obvious:

- render generated entries in their interleaved execution order;
- show group label and `Round X of N` for grouped entries;
- show exercise coaching notes while recording the set;
- visually separate the end of one group round from the next;
- for `rest_seconds > 0`, show/start the existing recovery timer;
- for `rest_seconds = 0`, do not start a zero-length timer and do not show
  `Start 0:00 rest`; show `Next exercise` or no timer action;
- after completing a member, make the next generated entry apparent;
- preserve editing, removal, draft persistence, offline outbox behavior,
  finish, discard, and resume.

Do not automatically mark a set complete merely because the user starts a
timer. The current data model has no explicit completion boolean; keep the
existing edit-and-finish semantics unless a separate design adds one.

`Add set` should continue to add a set for the last actual exercise. An added
set is an unplanned deviation and need not modify group rounds or the embedded
plan. Ensure its `sequence_no` remains unique. `Add unplanned exercise` also
remains outside all planned groups.

### 10.4 Summary and comparison

Keep the completed summary ordered by actual `sequence_no`. Optionally show
group/round badges when the embedded planned session is available, but do not
make summary correctness depend on those badges.

Server comparison remains per planned step. It must count three actual sets for
each member of a three-round group and preserve actual reps/load in execution
order for that step.

## 11. Tests and acceptance criteria

### 11.1 Migration and repository tests

Add tests that prove:

- the new Goose migration applies from a fresh database;
- upgrade from migration 4 preserves existing plan/session/step rows and
  returns `groups: []`;
- Goose down/up remains valid in the existing migration test;
- a group round-trips with label, mode, rounds, ordered members, notes, and
  idempotency key;
- identical draft retry does not bump `sync_seq`;
- changed group structure does bump `sync_seq`;
- draft replacement tombstones removed groups;
- cross-athlete group ID/idempotency reuse is rejected without leaking owner
  information;
- malformed member JSON produces an internal error;
- each semantic validation rule in section 4 has a focused rejection test;
- a completed workout linked to grouped steps still compares planned and actual
  sets correctly.

### 11.2 MCP and HTTP tests

Test that:

- tool schemas include bounded strict group objects and useful descriptions;
- omitted group IDs are generated as UUIDv7;
- group idempotency keys resolve retries to the same IDs;
- plan reads return deterministic group ordering;
- REST plan/session JSON round-trips groups;
- a legacy session returns an empty group collection;
- malformed or overlapping groups return HTTP 400 / `invalid_input:` through
  the appropriate boundary.

### 11.3 Mobile unit tests

At minimum cover:

- no groups preserves the old straight-set order;
- a two-member, three-round group emits A1, B1, A2, B2, A3, B3;
- a three-member circuit uses declared member order;
- grouped entries retain correct `planned_step_id`, `set_number`, reps, load,
  RIR, and global sequence number;
- ungrouped steps before and after a group remain in the right position;
- two separate groups do not interleave with each other;
- `rest_seconds` resolves per member and per round;
- zero rest does not activate the timer;
- legacy JSON without `groups` parses successfully;
- invalid server group shapes fail Zod parsing rather than being guessed at.

Update the E2E fixture to use two real structured supersets and remove grouping
meaning from its notes. Notes may retain ordinary cues. E2E must prove:

- preview displays both group labels and round counts;
- starting the fixture creates interleaved entries;
- the transition from the first to second member does not offer a 120-second
  recovery when its prescribed rest is zero;
- the final member uses the prescribed recovery;
- completing and syncing the workout produces correct per-exercise comparison.

### 11.4 Repository entrypoints

Use the repository entrypoints for verification and run them outside the
sandbox as required by `AGENTS.md`:

```sh
./scripts/test.sh --go
./scripts/test.sh --vet
./scripts/test.sh --python
./scripts/test.sh
./scripts/build.sh
./scripts/build-mobile.sh
./scripts/test-mobile-e2e.sh
```

Focused mobile unit tests may be run during iteration through the existing npm
test command, but they do not replace the repository entrypoints.

The implementation is accepted only when:

1. An MCP-created two-exercise group is stored and read as structure, not only
   notes.
2. The plan preview visibly groups its exercises.
3. Starting it produces round-interleaved actual entries.
4. Rest behavior follows the just-completed member's prescribed rest.
5. Offline save/resume and outbox sync retain group context.
6. A completed workout compares correctly against each grouped planned step.
7. Legacy ungrouped sessions behave unchanged.
8. The existing eligible live routines have completed the reviewed migration
   procedure in section 12.

## 12. LLM-mediated migration of existing server routines

### 12.1 Scope and safety boundary

Semantic migration is required, but it must be performed by an LLM through the
normal MCP plan read/write actions, with human review of ambiguous intent. Do
not implement a production script that searches notes for `superset`, `A1`,
`A2`, `circuit`, `rounds`, `paired with`, or similar phrases and rewrites rows.
Those phrases are not a reliable grammar.

Migrate:

- the current active plan's future `planned` sessions whose notes or step notes
  express supersets/circuits;
- any unstarted draft plan intended for later publication;
- other future routines explicitly selected by the operator.

Do not migrate or mutate:

- completed workouts or their actual entries;
- completed, skipped, cancelled, or already-started planned sessions;
- archived plans merely to make old history look structured;
- an ambiguous note without confirming the intended members, order, round
  count, and rest transitions.

### 12.2 Deployment prerequisite

Perform semantic migration only after:

1. the database migration and group-aware server are deployed;
2. server verification passes;
3. a group-aware mobile build is installed on every device used to start the
   migrated plan;
4. that mobile build has successfully fetched a harmless plan response with
   `groups: []`;
5. a disposable draft containing a test superset has been created, read back,
   previewed on mobile, started, and discarded successfully.

### 12.3 Inventory pass

Use `get_training_plan` with `include_steps: true` to read the active plan and
any selected draft by ID. The migrating LLM must produce a review table before
writing anything:

| Session date/title | Evidence in notes | Proposed members | Order | Rounds | Rest after each member | Confidence |
|---|---|---|---|---:|---|---|

Confidence is one of:

- `high`: explicit labels identify every member/order and set counts agree;
- `medium`: grouping is explicit but round count or transitions require a
  reasonable interpretation;
- `low`: members or execution meaning are ambiguous.

Only high-confidence rows may be translated without a clarification. Medium
and low-confidence rows require the user/operator to approve the proposed
interpretation. The LLM must not treat `session_type = superset` by itself as
enough evidence to partition multiple exercises.

For the currently upcoming routine, specifically inspect both session-level
and step-level notes. Preserve ordinary coaching cues after removing only the
text whose sole purpose was to encode group structure. Never discard unrelated
notes.

### 12.4 Translation rules

For each approved group:

1. Resolve member steps to their existing sequence numbers in declared order.
2. Determine rounds from explicit text and prescribed set counts.
3. If all member set counts agree, use that count unless explicit approved text
   says otherwise.
4. If member set counts differ, stop and request a decision; the first release
   does not support unequal rounds.
5. Set `rest_seconds` explicitly on every grouped prescribed set.
6. Preserve an explicitly prescribed transition rest.
7. If the note only says to perform exercises back-to-back, use zero after
   non-final members.
8. Preserve an explicit between-round recovery on the final member.
9. If final-member recovery is absent, do not silently assume the mobile
   default. Ask the operator for the intended recovery.
10. Create a concise group label such as `Superset A`; keep useful coaching
    text in group or step notes.
11. Leave all ungrouped steps and all non-group prescription fields unchanged.

### 12.5 Writing an active-plan replacement

An active plan cannot be replaced in place by the draft writer. To migrate an
active plan safely:

1. Read and retain the complete active plan with steps and groups.
2. Create a new draft plan through `upsert_training_plan_draft`.
3. Use a new plan ID, or omit it and provide a new stable migration
   idempotency key.
4. Use new session, step, and group IDs, or omit them and provide new stable
   idempotency keys. Do not reuse child IDs from the old plan because the
   repository prevents moving children between plans.
5. Copy plan metadata and every still-relevant future session, changing only
   the approved group structure, required grouped rests, and obsolete
   structure-only note fragments.
6. Normally omit past/completed/skipped/cancelled sessions from the replacement
   active plan; they remain available on the archived original plan and in
   workout history.
7. Do not change session dates, exercise identity, loads, reps, RIR, tempo,
   priority, or unrelated targets during migration.
8. Read the new draft back by its returned plan ID with `include_steps: true`.
9. Complete the verification checklist below.
10. Publish the replacement only after review. Publishing archives the old
    active plan through the existing atomic plan lifecycle.

For an eligible unstarted draft, the LLM may update the same plan ID using its
existing child IDs/idempotency keys, provided the repository's normal lock and
ownership checks allow it. Read it back and review it before publication.

### 12.6 Pre-publication verification

The migrating LLM must compare old and new plans and report:

- old and new plan IDs;
- session count and the exact dates/titles retained or intentionally omitted;
- step count per session;
- exercise key/name and sequence number equality;
- every reps/load/RIR/tempo value before and after;
- every changed `rest_seconds`, with the reason;
- group labels, member order, and rounds;
- note text removed, moved, or preserved;
- any ambiguity or unsupported routine left as notes;
- confirmation that no completed/started entity was mutated.

Do not publish if an unapproved difference exists. If publication succeeds,
read the active plan again and confirm its ID equals the replacement plan ID
and its groups match the reviewed draft.

### 12.7 Migration idempotency and recovery

Use deterministic migration idempotency keys scoped to the source plan and
entity, for example:

```text
superset-migration:<old-plan-id>:plan:v1
superset-migration:<old-session-id>:session:v1
superset-migration:<old-step-id>:step:v1
superset-migration:<old-session-id>:group:a:v1
```

Never include health data, exercise notes, or other sensitive content in an
idempotency key. Retrying after a network failure must resolve to the same
draft and children.

If validation or review fails, leave the new plan in draft and keep the old
plan active. If publication has already succeeded, do not directly flip SQL
status fields. Correct the replacement through the supported plan lifecycle or
create and publish another reviewed replacement. Record the old and new plan
IDs in the deployment/migration notes.

### 12.8 Suggested migration instruction for the LLM operator

Use an instruction equivalent to:

```text
Read the complete active training plan, including steps. Identify future
planned sessions whose notes describe supersets or circuits. First show a
table of proposed structured round groups with members, order, rounds, and
rest after every member. Do not write until ambiguous rows are confirmed.
Then create a new idempotent draft that preserves all approved future plan
content, replacing only note-encoded execution structure with groups. Use new
child identities, read the draft back, and provide a field-by-field migration
diff. Publish only after explicit approval. Do not mutate completed, started,
skipped, cancelled, or archived history.
```

This instruction is operational guidance, not application code. The migration
remains subject to MCP authorization, repository validation, draft review, and
explicit publication.

## 13. Implementation order

Implement in this order to keep each stage testable:

1. Add the Goose schema migration and migration tests.
2. Add repository group types, validation, persistence, loading, equality, and
   idempotency lookup with focused Go tests.
3. Add MCP schemas/conversion/ID resolution and tests.
4. Add OpenAPI types, regenerate Go HTTP types, convert REST output, and test.
5. Add mobile TypeScript/Zod contracts and legacy parsing tests.
6. Add pure execution expansion/rest helpers and exhaustive unit tests.
7. Update planned-session preview and active-workout rendering/timer behavior.
8. Update fixtures and mobile E2E tests.
9. Run the full repository verification entrypoints outside the sandbox.
10. Deploy schema/server, then the compatible mobile build.
11. Perform the LLM inventory and reviewed semantic migration of eligible live
    routines.
12. Verify the newly active plan from both MCP and the mobile app.

Do not combine steps 10 and 11 into a startup migration. The schema upgrade is
deterministic infrastructure work; interpreting human-authored routine notes is
a reviewed semantic operation.

## 14. Explicitly out of scope

- automatic NLP parsing or SQL rewriting of existing notes;
- rewriting completed workout history;
- nested groups;
- overlapping groups;
- unequal member set counts inside one group;
- timed AMRAP/EMOM execution;
- ladders, pyramids, drop sets, rest-pause sets, and conditional branches;
- wearable control or automatic rep detection;
- changing plan ownership, synchronization aggregate boundaries, or the
  historical session lock rules;
- a general-purpose workout programming language.
