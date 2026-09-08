# Exercise normalization implementation plan

## 1. Goal

Make exercise identity reliable when an LLM writes plans and workouts through
MCP, without placing the full exercise catalog in every write-tool schema and
without rejecting useful free-form exercise descriptions.

The governing rule is:

> An MCP client may freely author `exercise_name`, but it may only reuse or
> select an `exercise_key` returned by FitSync. It must not invent keys.

FitSync remains authoritative for canonical keys. Unknown or ambiguous names
remain valid and are stored with a null key.

## 2. Current behavior and motivating evidence

The current representation already has the right two fields:

- `exercise_key`: nullable machine identity;
- `exercise_name`: user-facing free-form label.

However, normalization is currently performed mainly by
`get_exercise_progress`. Plan and workout writes accept any key of the allowed
length and persist the supplied key and name unchanged.

A read-only production audit on 2026-09-05 found:

- 118 non-deleted planned exercise/warm-up steps, of which 27 had keys;
- all 27 steps in the active MCP plan had keys;
- all 91 archived plan steps lacked keys;
- 77 non-deleted workout sets, of which 53 had keys;
- all 24 mobile-created sets lacked keys because their linked planned steps
  lacked keys;
- the database contains plausible but inconsistent keys such as
  `dumbbell_lateral_raise` and `lateral_raise`, and
  `dumbbell_row` and `one_arm_dumbbell_row`;
- the code-owned catalog contains only 12 definitions, so many stored keys are
  not actually recognized by `get_exercise_progress`.

No production data should be changed as part of the initial implementation.
Historical backfill is a separate, reviewed rollout step.

## 3. Required outcome

After this work:

1. MCP write schemas remain compact and do not enumerate the catalog.
2. A single catalog resolver owns all key validation and name normalization.
3. Repository write paths normalize MCP, REST, and mobile data consistently.
4. Known explicit keys are accepted; deprecated keys are rewritten.
5. Unknown explicit keys are rejected.
6. Missing keys are filled only by exact reviewed alias matches.
7. Unknown or ambiguous names are preserved unchanged with a null key.
8. MCP exposes one bounded batch resolver for catalog discovery.
9. Bulk MCP writes return a concise normalization report.
10. Existing read behavior and existing free-form names remain compatible.

## 4. Non-goals

Do not implement any of the following in this iteration:

- fuzzy automatic assignment of canonical keys;
- LLM-generated or automatically persisted catalog entries;
- athlete-specific alias tables;
- embedding/vector search;
- rewriting `exercise_name` into a canonical display name;
- parsing alternatives such as `Pull-up or chin-up` into structured choices;
- changing muscle attribution or load semantics based on free text;
- a database migration solely for normalization metadata;
- production backfill or deployment.

## 5. Data model and compatibility

Keep the existing storage fields. No migration is required:

```json
{
  "exercise_key": "one_arm_dumbbell_row",
  "exercise_name": "1-arm DB row — upper-back biased"
}
```

`exercise_key` identifies the comparable exercise variation.
`exercise_name` preserves the LLM-authored instruction exactly, apart from the
existing empty-string/null handling. Do not replace it with the catalog's
canonical name.

An unresolved entry is valid:

```json
{
  "exercise_key": null,
  "exercise_name": "Pull-up or chin-up"
}
```

Do not add normalization status to persisted workout JSON or planned-step
columns. Status is computed during resolution and returned only where useful.

## 6. Catalog redesign

### 6.1 Definition type

Extend `internal/exercisecatalog/catalog.go`:

```go
type ExerciseDefinition struct {
    Key              string
    CanonicalName    string
    Description      string
    Aliases          []string
    DeprecatedKeys   []string
    DirectMuscles    []string
    IndirectMuscles  []string
    SupportsE1RM     bool
}
```

Do not add `FamilyKey` in this iteration. It is useful for future movement-
family analysis but is not needed to normalize writes.

Requirements:

- `Key` is lower snake case, 1-128 characters.
- `CanonicalName` is non-empty and at most 256 characters.
- `Description` is a concise distinction useful to an LLM. Keep it under 300
  characters.
- aliases and deprecated keys map to exactly one canonical key;
- catalog construction must panic in production and fail tests if a normalized
  alias, canonical key, or deprecated key is claimed by two definitions;
- returned definitions and slices must be copies, preserving the package's
  current immutability behavior;
- `NormalizeAlias` remains the only textual normalization primitive;
- do not infer muscle groups from newly observed labels. For a new definition,
  leave muscle fields empty unless a reviewed mapping already exists.

### 6.2 Catalog coverage

Retain all existing definitions. Add definitions for all legitimate keys
currently observed in production so strict write validation does not reject the
current vocabulary:

```text
b_stance_romanian_deadlift
bicycle_crunch
bulgarian_split_squat
calf_raise
deficit_push_up
dip
dumbbell_lateral_raise
hammer_curl
hanging_knee_raise
lateral_raise
lying_leg_raise
one_arm_dumbbell_press
one_arm_floor_press
one_arm_overhead_press
push_up
reverse_crunch
reverse_wrist_curl
side_plank
single_arm_dumbbell_curl
single_leg_calf_raise
sliding_hamstring_curl
superman_dynamic
tibialis_raise
tibialis_raise_slant_board
```

Treat these stored keys as deprecated aliases:

```text
dumbbell_row                         -> one_arm_dumbbell_row
standing_overhead_triceps_extension -> triceps_extension
```

Do not merge these pairs in this iteration because the generic and specific
forms may not be comparable:

```text
lateral_raise              != dumbbell_lateral_raise
calf_raise                 != single_leg_calf_raise
one_arm_dumbbell_press     != one_arm_overhead_press
tibialis_raise             != tibialis_raise_slant_board
```

Add only unambiguous reviewed aliases. Include the production labels below:

```text
B-stance DB RDL                         -> b_stance_romanian_deadlift
Bicycle crunch                          -> bicycle_crunch
Bulgarian split squat                   -> bulgarian_split_squat
Calf raise                              -> calf_raise
Deficit push-up on parallettes          -> deficit_push_up
Deficit push-up on 17 cm parallettes    -> deficit_push_up
Dip                                     -> dip
DB lateral raise                        -> dumbbell_lateral_raise
Dumbbell lateral raise                  -> dumbbell_lateral_raise
Hammer curl                             -> hammer_curl
Lateral raise                           -> lateral_raise
Leg raise                               -> lying_leg_raise
One-arm dumbbell press                  -> one_arm_dumbbell_press
One-arm floor press                     -> one_arm_floor_press
1-arm DB overhead press                 -> one_arm_overhead_press
Push-up                                 -> push_up
Reverse crunch                          -> reverse_crunch
Reverse wrist curl                      -> reverse_wrist_curl
Single-arm dumbbell curl                -> single_arm_dumbbell_curl
1-arm DB curl                           -> single_arm_dumbbell_curl
Single-leg calf raise                   -> single_leg_calf_raise
Single-leg loaded calf raise            -> single_leg_calf_raise
Single-leg loaded calf raise on slant board -> single_leg_calf_raise
Superman                                -> superman_dynamic
Tibialis raise                          -> tibialis_raise
Tibialis raise (slant board)            -> tibialis_raise_slant_board
```

The following labels express alternatives or underspecified variants. Do not
add them as exact aliases even if an existing production plan supplied a key:

```text
Pull-up or chin-up
Hanging knee raise or leg raise
Side plank or long-lever plank
Sliding hamstring curl or hamstring walkout
```

`1-arm DB row — upper-back biased` may be an alias for
`one_arm_dumbbell_row`: the preserved name retains the technique modifier, and
the underlying unilateral dumbbell-row identity remains explicit.

## 7. Resolver API

Implement the resolver in `internal/exercisecatalog`. It must have no database,
HTTP, MCP, or repository dependency.

Suggested public types:

```go
type ResolutionStatus string

const (
    StatusExplicit             ResolutionStatus = "explicit_key"
    StatusExactAlias           ResolutionStatus = "exact_alias"
    StatusDeprecatedKey        ResolutionStatus = "deprecated_key"
    StatusUnknown              ResolutionStatus = "unknown"
)

type Resolution struct {
    Key           *string
    CanonicalName string
    Status        ResolutionStatus
    MatchedAlias  string
    OriginalKey   string
}

func Resolve(exerciseKey, exerciseName *string) (Resolution, error)
```

Use stable error prefixes consistent with the repository:

```text
invalid_input: unknown exercise_key
invalid_input: exercise_key conflicts with exercise_name
```

Resolution rules, in order:

1. Treat a nil or whitespace-only key as omitted.
2. If a key is supplied and is canonical, select it with `explicit_key`.
3. If a key is supplied and is deprecated, select its replacement with
   `deprecated_key` and retain the submitted value in `OriginalKey`.
4. If a supplied key is unknown, return `invalid_input: unknown exercise_key`.
5. When a key and name are both present, exact-normalize the name. If it maps
   to a different canonical key, return
   `invalid_input: exercise_key conflicts with exercise_name`. An unknown name
   does not conflict with a known explicit key.
6. If no key is supplied and the name exactly matches a reviewed alias, return
   that key with `exact_alias`.
7. Otherwise return a nil key with `unknown`.

Never return an error merely because a name is unknown. Never change the
submitted name.

Add helpers if they reduce duplication:

```go
func IsCanonicalKey(string) bool
func DefinitionForKeyOrDeprecated(string) (ExerciseDefinition, string, bool)
```

`NormalizeExercise` must continue to accept canonical keys and reviewed name
aliases. It should also accept deprecated keys and return their replacement.

## 8. Catalog search

Add deterministic candidate search to `internal/exercisecatalog`. Search is for
LLM assistance only; it must never assign a key during a write.

Suggested API:

```go
type SearchResult struct {
    Key           string
    CanonicalName string
    Description   string
    MatchedAlias  string
    Score         int
}

func Search(query string, limit int) []SearchResult
```

Use this bounded, deterministic scoring algorithm:

1. Normalize the query and each alias with `NormalizeAlias`.
2. Exact equality scores 100.
3. An alias occurring as a complete contiguous token sequence within the query
   scores 90. This should find `one arm db row` inside a longer coaching label.
4. The query occurring as a complete token sequence within an alias scores 80.
5. Otherwise calculate token Jaccard similarity. Include results only when the
   score is at least 50, using `round(100 * intersection / union)`.
6. Keep the best alias score per canonical key.
7. Sort by descending score, then canonical key ascending.
8. Clamp the result limit to 1-10. Empty queries return no results.

Token comparisons must use whole normalized tokens, not raw substrings. This
prevents matches such as `dip` inside an unrelated word.

## 9. Repository write normalization

Normalization belongs at the repository boundary so MCP, REST, and mobile
writes behave identically.

Add unexported helpers in `internal/fitnessrepo`, for example in a new
`exercise_normalization.go`:

```go
func normalizePlannedStep(step PlannedStep) (PlannedStep, exercisecatalog.Resolution, error)
func normalizeStrengthEntry(entry StrengthEntry) (StrengthEntry, exercisecatalog.Resolution, error)
func normalizeWorkoutWrite(in WorkoutWrite) (WorkoutWrite, []EntryNormalization, error)
```

The helpers must copy and replace only `ExerciseKey`. They must not mutate
`ExerciseName`, muscle groups, notes, or caller-owned slices.

Apply them before validation, equality/idempotency comparisons, and SQL writes
in all of these paths:

- `Repository.UpsertPlanDraft`;
- `Repository.UpsertWorkout`;
- `Repository.UpsertWorkoutAggregate` / `CompleteWorkout`;
- the `set_entry_exercise_key` operation in `Repository.PatchWorkout`.

`Repository.UpsertPlannedSession` currently writes only session metadata and no
steps, so it needs no normalization change.

Only normalize exercise-bearing objects:

- planned steps with a non-empty key or name;
- workout entries with `entry_type == "set"` and a non-empty key or name.

Rest and note entries without exercise identity remain untouched.

An exact name match fills a missing key before persistence. A deprecated key is
persisted using its replacement. Unknown explicit keys abort the entire atomic
write. Unknown names remain null-keyed.

Update any same-write/idempotency comparisons so a retry using an alias and a
retry using its canonical key compare as the same normalized value.

The deprecated low-level MCP `update_workout` tool and REST sync-mutation entry
path can still write raw entry payload JSON. They must not bypass the policy:

- expose a small repository/catalog helper that normalizes a key/name pair, or
  decode the two fields and call `exercisecatalog.Resolve` before `applyEntry`;
- replace only `exercise_key` in the payload and preserve all other JSON;
- reject an unknown explicit key or an exact key/name conflict atomically;
- exact-normalize a missing key when `exercise_name` is present;
- leave payloads with no exercise identity unchanged.

Do not attempt to normalize arbitrary nested JSON. Only the top-level entry
payload fields `exercise_key` and `exercise_name` are in scope. Add regression
tests in `internal/httpapi/api_test.go` for this legacy path.

## 10. Compact MCP write schemas

Do not put catalog keys in an `enum` or `oneOf` in a write schema.

Add a reusable `exerciseKeyProp()` in
`internal/mcpserver/schema_common.go`:

```json
{
  "type": "string",
  "maxLength": 128,
  "pattern": "^[a-z0-9]+(?:_[a-z0-9]+)*$",
  "description": "Canonical key previously returned by FitSync. Omit when uncertain; never invent one."
}
```

Use `nullable(exerciseKeyProp())` for planned steps and workout entries. Use
the non-null form for `set_entry_exercise_key` and read filters that require a
key. Keep `exercise_name` free-form and describe it as:

```text
Free-form exercise label or coaching description; preserved as supplied.
```

Do not make `exercise_key` required. Preserve current compatibility around
nullable fields.

Update the descriptions of `upsert_training_plan_draft`, `upsert_workout`, and
`patch_workout` to state concisely that clients must omit uncertain keys and
must not invent them. Do not embed the catalog or a long example list in tool
descriptions.

## 11. `resolve_exercises` MCP tool

Register one new read-only tool in `internal/mcpserver/server.go`:

```text
resolve_exercises
```

Description:

```text
Resolve a batch of free-form exercise names to FitSync canonical keys before a plan or workout write. Exact aliases resolve automatically; non-exact matches are suggestions only.
```

Input schema:

```json
{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "target_athlete_id": {"type": "string", "format": "uuid"},
    "exercises": {
      "type": "array",
      "minItems": 1,
      "maxItems": 100,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "ref": {"type": "string", "maxLength": 128},
          "name": {"type": "string", "maxLength": 256}
        },
        "required": ["ref", "name"]
      }
    },
    "max_candidates": {"type": "integer", "minimum": 1, "maximum": 10}
  },
  "required": ["exercises"]
}
```

`ref` values must be non-empty and unique within the request. They are opaque
correlation values chosen by the LLM.

Authorize the tool with `fitness:read` using the existing `principal` path,
even though the first catalog is global. This keeps household targeting and
future athlete-specific behavior consistent.

Output contract:

```json
{
  "catalog_revision": 1,
  "results": [
    {
      "ref": "day1-step1",
      "name": "Bulgarian split squat",
      "status": "exact_alias",
      "exercise_key": "bulgarian_split_squat",
      "canonical_name": "Bulgarian split squat",
      "matched_alias": "Bulgarian split squat",
      "candidates": []
    },
    {
      "ref": "day1-step2",
      "name": "Pull-up or chin-up",
      "status": "suggestions",
      "exercise_key": null,
      "candidates": [
        {
          "exercise_key": "pull_up",
          "canonical_name": "Pull-up",
          "description": "Vertical bodyweight pull-up.",
          "matched_alias": "pull up",
          "score": 50
        }
      ]
    }
  ]
}
```

Allowed output statuses:

- `exact_alias`: an exact reviewed alias resolved; `exercise_key` is non-null;
- `suggestions`: no exact match, but bounded candidates exist;
- `unknown`: no exact match and no candidates.

Suggestions must never populate `exercise_key`. Always return `candidates` as
an array, including when empty. Preserve result order from the request.

Define a package constant for `catalog_revision`, initially `1`. Increment it
whenever canonical keys, aliases, deprecated keys, or descriptions change.

## 12. MCP write normalization reports

Return an additive report from `upsert_training_plan_draft` and
`upsert_workout`. Do not change existing result fields.

Suggested shared output types:

```go
type normalizationItem struct {
    Ref             string   `json:"ref"`
    ExerciseName    string   `json:"exercise_name,omitempty"`
    OriginalKey     string   `json:"original_key,omitempty"`
    ExerciseKey     *string  `json:"exercise_key,omitempty"`
    Status          string   `json:"status"`
    Candidates      []candidateOutput `json:"candidates"`
}

type normalizationReport struct {
    CatalogRevision int                 `json:"catalog_revision"`
    Explicit        int                 `json:"explicit"`
    ResolvedFromName int                `json:"resolved_from_name"`
    DeprecatedKeysRewritten int         `json:"deprecated_keys_rewritten"`
    Unknown         int                 `json:"unknown"`
    Items           []normalizationItem `json:"items"`
}
```

Only include entries in `items` when their status is `deprecated_key`,
`unknown`, or when suggestions exist. This keeps successful bulk-write output
small. Return `items` as an empty array rather than null.

Stable refs:

- plan step: `session_id + ":" + step_id`;
- workout entry: `entry_id`.

For an unknown name, use catalog `Search` to include at most three suggestions,
but do not change the persisted null key.

The repository is authoritative for normalization. The MCP layer must build
the report from the original request and the normalized stored result, or from
repository-returned non-persisted resolution metadata. Do not implement a
second resolver in `internal/mcpserver`.

For `upsert_workout`, preserve the existing top-level `WorkoutWinner` JSON
shape and add `normalization` as an additive field. If adding transport-only
metadata to `fitnessrepo.WorkoutWinner` would leak into REST responses or break
the generated OpenAPI contract, introduce an MCP-specific wrapper that embeds
or reproduces the existing top-level fields. Add a regression test for the
chosen shape.

`patch_workout` should return the existing result. It may rely on validation
errors and does not need a normalization report in this iteration.

## 13. Read-path behavior

Update read-time normalization so deprecated keys remain discoverable:

- `get_exercise_progress` must accept a canonical key, deprecated key, or exact
  reviewed alias and report the canonical key;
- `entryMatchesExercise` must compare canonicalized stored keys, because old
  rows may still contain deprecated keys;
- strength filters should canonicalize requested keys and stored entry keys
  before comparison;
- unknown read filter keys should return `invalid_input: unknown exercise_key`
  rather than an empty, misleading result.

Do not rewrite old rows merely by reading them.

## 14. Tests

### 14.1 Catalog unit tests

Expand `internal/exercisecatalog/catalog_test.go` with table-driven tests for:

- every canonical key resolves to itself;
- every reviewed alias resolves to the expected key;
- deprecated keys resolve to replacements;
- alias punctuation, case, Unicode dash, and repeated whitespace behavior;
- an invented variation remains unknown;
- alternative labels listed in section 6.2 remain unknown;
- duplicate canonical keys panic during catalog construction;
- duplicate normalized aliases panic;
- deprecated-key collisions panic;
- `Definitions` returns defensive copies;
- deterministic search scoring and tie ordering;
- token-boundary matching;
- search result limit and empty query behavior.

Make catalog construction testable with a helper that accepts a definition
slice instead of testing package initialization indirectly.

### 14.2 Repository tests

Add cases in `internal/fitnessrepo/workout_app_test.go` and
`internal/fitnessrepo/repository_test.go` for:

- an omitted key plus exact alias is persisted with its canonical key;
- unknown free text is persisted unchanged with a null key;
- a known explicit key and descriptive unknown name are accepted;
- a supplied unknown key aborts the atomic write;
- a conflicting exact name and key abort the atomic write;
- a deprecated key is rewritten before persistence;
- plan draft, MCP workout, mobile aggregate workout, completion, patch, and
  legacy raw-entry sync paths all apply the same rules;
- retry/equality behavior compares normalized keys;
- linked old unkeyed planned steps produce keyed mobile workout entries when
  their names are exact aliases;
- unknown alternatives remain valid and unkeyed.

### 14.3 MCP protocol tests

Expand `internal/mcpserver/server_test.go` to assert:

- `resolve_exercises` is registered and read-only;
- its schema is strict and bounded;
- write schemas use a compact pattern and do not contain a catalog enum;
- duplicate or empty refs are rejected;
- exact, suggested, and unknown results have the required output shape;
- result order follows request order;
- cross-athlete authorization is unchanged;
- plan and workout writes return normalization reports;
- unknown explicit keys surface as `invalid_input`;
- unresolved free-form names still write successfully;
- the full tool list/schema serialization remains comfortably bounded. Record
  a generous maximum serialized byte size rather than an exact snapshot so
  harmless description edits do not make the test brittle.

### 14.4 Existing behavior

Keep existing tests proving that muscle attribution never guesses from free
text and that unknown exercise progress does not make unsupported claims.

## 15. Documentation

Update `README.md` MCP action documentation with:

- the rule that clients must not invent `exercise_key` values;
- the name-first write behavior;
- one batched `resolve_exercises` example;
- the fact that exact aliases normalize during writes;
- the fact that unknown names remain accepted and unkeyed;
- the normalization report returned from bulk writes.

Keep the documentation examples short. Do not print the entire catalog in the
README or tool descriptions.

## 16. Historical backfill plan

Do not run this during implementation. After deployment and observation:

1. Back up the SQLite database using the existing deployment procedure.
2. Run a read-only dry-run that evaluates only rows with a null key.
3. Report counts grouped by exact resolved key, unknown name, and ambiguous
   suggestion.
4. Review the proposed exact mappings manually.
5. In one transaction, populate only exact reviewed aliases.
6. Preserve every existing `exercise_name` byte-for-byte.
7. Allocate/bump the appropriate sync sequence for changed active plans and
   workouts so mobile clients receive the updates.
8. Do not backfill suggestions or alternatives.

Do not directly update JSON with ad hoc production shell commands. Implement a
small repository-aware maintenance command or migration utility with dry-run
and explicit apply modes when this phase is authorized.

## 17. Observability

Add aggregate structured counters or debug-level summary fields for:

- explicit canonical keys;
- exact alias resolutions;
- deprecated-key rewrites;
- unknown names;
- rejected unknown keys;
- key/name conflicts.

Do not log exercise names, workout notes, or full MCP arguments. Normal request
logs should contain counts and statuses only.

Observability must not be required for correctness. If the project has no
metrics backend, a single structured summary log per bulk write is sufficient.

## 18. Implementation sequence

Execute in this order:

1. Refactor catalog construction and add metadata fields.
2. Reconcile the known production vocabulary and deprecated keys.
3. Implement and fully test `Resolve` and `Search`.
4. Add repository normalization helpers.
5. Apply them to every plan, workout, completion, and patch write path.
6. Canonicalize exercise progress and strength-filter reads.
7. Replace generic MCP key schemas with `exerciseKeyProp()` and improve the
   concise descriptions.
8. Add `resolve_exercises` input/output types, schema, handler, registration,
   authorization, and protocol tests.
9. Add normalization reports to plan and workout MCP writes.
10. Update README documentation.
11. Run focused tests during iteration.
12. Run the repository entrypoints below before declaring completion.

Do not combine the historical backfill or production deployment with these
steps.

## 19. Verification commands

Repository instructions require these scripts to run outside the sandbox.
Use the focused commands during iteration and the complete commands at the end:

```sh
./scripts/test.sh --go
./scripts/test.sh --vet
./scripts/test.sh
./scripts/build.sh
```

No mobile UI change is expected, but repository normalization affects the
mobile sync path. If any TypeScript, OpenAPI, generated mobile type, or mobile
sync file changes, also run:

```sh
./scripts/build-mobile.sh
./scripts/test-mobile-e2e.sh
```

Do not substitute raw `go test`, `go vet`, or ad hoc Python commands for final
verification.

## 20. Acceptance checklist

The implementation is complete only when all statements are true:

- [ ] MCP write schemas contain no exercise catalog enum.
- [ ] Every supplied exercise key is validated by the catalog.
- [ ] Unknown supplied keys fail atomically.
- [ ] Exact reviewed names receive canonical keys on every write path.
- [ ] Unknown names remain writable and retain their original labels.
- [ ] Deprecated keys are rewritten and reported.
- [ ] Exact key/name conflicts fail atomically.
- [ ] The LLM can resolve up to 100 names in one MCP call.
- [ ] Non-exact candidates are suggestions and are never auto-assigned.
- [ ] Plan and workout MCP writes return bounded normalization summaries.
- [ ] Exercise progress works through canonical, deprecated, and reviewed alias
      inputs.
- [ ] No read operation mutates historical rows.
- [ ] No exercise names or notes are added to ordinary logs.
- [ ] Existing REST/mobile compatibility tests pass.
- [ ] `./scripts/test.sh` passes outside the sandbox.
- [ ] `./scripts/build.sh` succeeds outside the sandbox.
