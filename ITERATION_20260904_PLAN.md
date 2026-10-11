# FitSync coaching-data iteration plan — 2026-09-04

## 1. Goal

Make Fitnessd's MCP responses reliable enough for repeated coaching decisions without
requiring the caller to rediscover units, missing data, hard-session classification,
planned/completed links, or strength conventions on every call.

The implementation must preserve the existing architecture:

- Garmin records remain raw, immutable source data in `garmin_records`.
- `internal/fitnessdata` owns Garmin decoding and unit normalization.
- `internal/fitnessrepo` owns athlete-scoped persistence and composition.
- deterministic analysis owns classifications and returns its evidence.
- `internal/mcpserver` owns strict schemas, authorization, and transport mapping.
- the MCP may give a bounded rule-based recommendation, but it must never hide the
  facts or rules that produced it and must not claim medical safety.

This document is implementation-ready and normative. Work through the phases in
order. Do not start a later phase while an earlier phase's checks fail.

## 2. Current-state findings

These findings are based on the repository as it exists on 2026-09-04 and should be
treated as the starting point, not reimplemented from scratch.

1. `fitnessdata.SleepSummary` already has `MainHours`, `NapHours`, and `TotalHours`,
   and `DecodeSleep` can add a top-level legacy `nap_seconds` value. The direct raw
   decoder does not read `sleep.dailySleepDTO.napTimeSeconds`, however. More
   importantly, a Garmin-synced nap may be present only in the daily Body Battery
   events, which are already ingested inside the `daily_recovery` raw envelope.
2. `fitsync_garmin/sleep.py` calls the main-sleep value `total_sleep_hours` and the
   main-plus-nap value `total_sleep_including_naps_hours`. This historical naming is
   the source of avoidable ambiguity.
3. `classifyRecovery` uses `SleepSummary.TotalHours`, but the response flattens that
   to the ambiguous `recovery.sleep_hours` and omits per-signal availability and
   freshness.
4. `TrainingContext` reads activities using `Detail: "compact"`.
   `projectActivitySummary(..., "compact")` clears aerobic effect, anaerobic effect,
   HR-zone minutes, and perceived effort. `calculateLoad` then calls `hardActivity`
   after those fields have been cleared. This is a known direct cause of
   `hard_sessions_last_7_days` incorrectly returning zero.
5. `hardActivity` does not currently implement the documented Z4+Z5 rule and returns
   only a boolean, so callers cannot see why a session was hard.
6. metric descriptor rows are unit-aware, but flat metric items trust names such as
   `distance_km`. There is no activity-distance sanity check. This permits a source
   or decoder mismatch to expose metres as kilometres.
7. planned sessions, Garmin activities, and structured workouts have explicit link
   fields, but there is no one-call chronological reconciliation view.
8. strength payloads permit `laterality`, `reps`, `load_kg`, and free-form muscle
   groups, but do not define per-side/per-limb semantics. The exercise catalog only
   maps a small number of exact keys to an undifferentiated muscle list.
9. `upsert_workout` is a full replacement. There is no safe field-level mutation.
10. repository source files are currently shared by MCP and the mobile REST API.
    Additive domain changes must not silently break mobile sync or generated OpenAPI
    types.

## 3. Scope and priorities

### 3.1 Deliver in this order

| Phase | Priority | Deliverable |
|---|---:|---|
| 0 | prerequisite | synthetic fixtures and characterization tests for observed payloads |
| 1 | 1 | correct main sleep, naps, and 24-hour sleep semantics |
| 2 | 2 | correct hard-session detection and expose evidence |
| 3 | 3 | correct activity metric units and add plausibility checks |
| 4 | 4 | first-class availability, freshness, stable shapes, units, and provenance |
| 5 | 5 | `get_training_timeline` |
| 6 | 6 | `get_exercise_progress` and exercise alias normalization |
| 7 | 7 | explicit unilateral, repetition, and load semantics |
| 8 | 8 | direct/indirect/weighted muscle-group volume |
| 9 | 9 | `get_activity_intervals` |
| 10 | 10 | `get_readiness_for_session` |
| 11 | follow-up | `record_quick_readiness`, `patch_workout`, and additional Garmin health signals |

Phases 1-4 are correctness work and form one release gate. Do not ship new coaching
actions from phases 5-10 on top of the currently ambiguous data.

### 3.2 Explicit non-goals

- no ML readiness model, opaque composite score, injury-risk prediction, or medical
  diagnosis;
- no raw Garmin JSON, GPS, routes, polylines, or FIT archives in MCP output;
- no fuzzy exercise-name guessing outside a reviewed alias table;
- no automatic persistence of inferred plan/activity/workout links;
- no unbounded metric, lap, timeline, or exercise-history output;
- no change to household authorization or `fitness:read`/`fitness:write` scopes.

## 4. Cross-cutting contracts

Implement these contracts before adding new public actions.

### 4.1 Units and display values

Canonical machine units remain:

- duration: seconds for interval/metric records and minutes or hours only where the
  suffix says so;
- distance: kilometres for fields ending `_km`, metres for fields ending `_m` or
  `_meters`;
- load: kilograms with an explicit load basis;
- pace: decimal minutes per kilometre;
- heart rate: beats per minute;
- power: watts.

Add display fields only alongside canonical numeric fields. Display fields are
derived and never parsed back into calculations. Examples:

```json
{
  "duration_seconds": 180,
  "duration_display": "3:00",
  "pace_min_per_km": 6.2833,
  "pace_display": "6:17/km"
}
```

Put formatting helpers in a small shared Go file under `internal/fitnessdata` or a
new `internal/fitnessanalysis` package. Do not duplicate formatting in MCP handlers.

### 4.2 Availability and freshness

Use the following shape for a signal used in a coaching classification:

```json
{
  "available": false,
  "value": null,
  "observed_date": null,
  "last_observed_date": "2026-08-27",
  "age_days": 8,
  "stale": true,
  "source_status": "unavailable",
  "provenance": {
    "source": "garmin",
    "dataset": "sleep",
    "method": "decode_sleep_hrv_v2",
    "updated_at": "2026-09-04T06:30:00Z"
  }
}
```

Rules:

- `available` means a usable value exists for the requested/as-of date. It does not
  mean that a historical value exists.
- `last_observed_date` is the latest date on or before `as_of` with a usable value.
- `age_days` is calendar-day difference in the athlete timezone. It is `null` when
  no historical value was found in the bounded search.
- search at most 90 days for context/readiness and at most the requested range for
  daily health. Do not issue an unbounded historical query.
- initial stale thresholds are two calendar days for main sleep, sleep score, HRV,
  resting HR, and Body Battery at wake. Put thresholds in named constants and test
  the boundary. `stale` is independent from Garmin's own HRV status string.
- an actual numeric zero remains available. Missing values remain JSON `null`.
- the source `updated_at` is `garmin_records.ingested_at` converted to RFC 3339, not
  the time the MCP response was generated.

For `get_daily_health`, always return the stable core nodes `sleep`, `hrv`,
`heart_rate`, `stress`, and `body_battery`. Each node has `available` and nullable
values rather than being `{}` or omitted. `spo2` and `daily_activity` may remain
optional projections. Retain the existing `metrics` input for compatibility, but it
must not remove the five core status nodes; it only controls optional detailed
fields.

### 4.3 Derived-metric provenance

Every derived aggregate or classification must expose:

```json
{
  "name": "hard_session_v2",
  "version": 2,
  "inputs_used": 3,
  "inputs_available": 4,
  "confidence": "medium"
}
```

Where useful, include per-field provenance rather than repeating it on every scalar.
Never emit a classification without the evaluated values, threshold/rule IDs, and
negative or positive evidence.

### 4.4 Compatibility policy

- changes to read responses are additive for one release;
- retain `sleep.main_hours`, `sleep.nap_hours`, and `sleep.total_hours` as deprecated
  aliases for one release, but make `total_hours` equal the corrected 24-hour total;
- add unambiguous canonical names now: `main_sleep_hours`, `nap_sleep_hours`, and
  `total_sleep_24h`;
- retain `recovery.sleep_hours` for one release as an alias of
  `recovery.sleep.total_24h`; add `recovery.sleep.main_hours` and
  `recovery.sleep.nap_hours` immediately;
- retain `muscle_group_sets` and legacy `volume_load_kg`, clearly mark their methods,
  while adding normalized summaries;
- do not reuse an existing field name with a different unit;
- update MCP protocol tests to ensure the deprecated fields stay correct during the
  compatibility window.

## 5. Phase 0 — characterize source payloads

Do this before decoder changes. The real-data inspection is local and must not place
health data in the repository or test output.

1. Inspect one date with a Garmin nap in both the raw sleep envelope and the raw
   `daily_recovery.events`/Body Battery event envelope. Record only field names,
   types, units, and timestamp behavior in a developer note or code comment. Never
   commit the payload or its values.
2. Inspect one affected running activity's `metricDescriptors` and first few metric
   rows. Confirm whether the distance descriptor unit is `meter`, whether descriptor
   indices remain aligned after coordinate removal, and which route (descriptor
   envelope or flat items) produced the bad `distance_km` values.
3. Inspect a known VO2/threshold activity summary and detail. Record which of these
   are actually present: source type, workout name, training-effect values, zone
   time, laps, and structured-workout identifiers.
4. Convert each discovered shape into the smallest possible synthetic fixture. Use
   invented timestamps, values, IDs, and names.
5. Add failing characterization tests before modifying behavior.

Files:

- `internal/fitnessdata/decoder_test.go`
- `test_garmin_sleep_probe.py`
- `test_fitsync_garmin.py`
- optionally new synthetic JSON files under `internal/fitnessdata/testdata/`

Gate:

- tests reproduce the missing nap, false non-hard session, and metre/kilometre
  anomaly using synthetic data;
- no real payload, athlete data, coordinate, or token is added to Git.

## 6. Phase 1 — sleep and nap correctness

### 6.1 Canonical data model

Replace the ambiguous internal sleep fields with this conceptual shape:

```go
type SleepSummary struct {
    MainSleepHours *float64
    NapSleepHours  *float64
    TotalSleep24H  *float64
    NapCount       int
    Naps           []NapSummary
    // Existing stage, score, qualifier, respiration, and stress fields remain.
}

type NapSummary struct {
    StartLocal      *string
    EndLocal        *string
    DurationSeconds float64
    DurationDisplay string
    Score           *float64
    Source          string
}
```

Return at most eight naps per day, ordered by local start time. This is a structural
bound, not post-marshal truncation.

### 6.2 Day semantics

For health date `D`:

- main sleep is the primary sleep record whose Garmin calendar date/end date is `D`;
- naps are events whose local start date is `D` in the athlete timezone;
- `total_sleep_24h = main_sleep_hours + nap_sleep_hours` only when main sleep is
  present;
- if main sleep is missing but naps are known, expose nap hours and count but leave
  `total_sleep_24h` null. Do not imply that naps are the whole day's sleep;
- `nap_sleep_hours` is zero only when the source explicitly establishes that there
  were no naps. If the nap source was not fetched, it is null;
- de-duplicate the sleep DTO aggregate and detailed Body Battery nap events. Detailed
  events are authoritative for count/times; the DTO aggregate is a fallback for
  duration. Never add both representations of the same nap.

### 6.3 Decoder and ingestion changes

1. Extend `DecodeSleep` to read direct
   `sleep.dailySleepDTO.napTimeSeconds` and the known legacy keys.
2. Add a bounded nap-event decoder for the already-ingested recovery event envelope.
   Keep source-specific keys inside `internal/fitnessdata`.
3. Merge sleep and nap events in `Repository.DailyHealth`, where athlete timezone
   and records from both datasets are available.
4. Make `fitsync_garmin/sleep.py` emit canonical names in its curated human output.
   Retain deprecated keys for one release.
5. Verify `_raw_ingest_records` continues uploading the raw envelope without
   changing or rounding source values.
6. Change recovery classification to evaluate `total_sleep_24h`. Return all three
   values and the exact evaluated field in `method.inputs`.

Files:

- `internal/fitnessdata/types.go`
- `internal/fitnessdata/sleep.go`
- add `internal/fitnessdata/nap.go` if event decoding is non-trivial
- `internal/fitnessrepo/repository.go`
- `internal/fitnessrepo/types.go`
- `internal/fitnessrepo/analysis.go`
- `fitsync_garmin/sleep.py`
- associated Go and Python tests

Acceptance cases:

- 6.28 hours main sleep plus a 30-minute nap returns 6.28, 0.50, and 6.78;
- two detailed naps are counted once each even when `napTimeSeconds` also exists;
- explicit no-nap data returns `nap_count: 0` and `nap_sleep_hours: 0`;
- unavailable nap source returns null nap duration, not zero;
- recovery's `sleep_duration_low` evaluates the corrected 24-hour total;
- pagination and day-boundary behavior work in `Europe/Madrid`, including DST.

## 7. Phase 2 — hard-session detection and training-load context

### 7.1 Fix the known projection bug first

Do not calculate load from MCP-projected activity values. In `TrainingContext`, fetch
or retain an internal analysis projection containing all classification inputs:

- aerobic and anaerobic training effect;
- Z4 and Z5 minutes;
- source/session type;
- linked manual perceived effort;
- planned session type when explicitly linked.

The simplest safe initial fix is to request `Detail: "summary"` for load analysis and
project only when assigning `RecentActivities`. A better final structure is a private
`AnalysisActivity` type so presentation detail can never erase analysis inputs.

### 7.2 Hard-session v2 contract

Return a classification record rather than only a boolean:

```json
{
  "activity_id": "123",
  "date": "2026-09-04",
  "hard": true,
  "reasons": [
    {"rule": "aerobic_effect_gte_3_5", "value": 4.1, "threshold": 3.5}
  ],
  "method": "hard_session_v2"
}
```

An activity/session is hard when any of the following is true:

1. normalized session type is `intervals`, `tempo`, `threshold`, `vo2`, or `race`;
2. aerobic or anaerobic training effect is at least 3.5;
3. Z4 plus Z5 time is at least 20 minutes;
4. linked structured/manual perceived effort is at least 8;
5. an explicitly linked planned session has one of the hard session types above.

Normalize type aliases in one reviewed map. Include at least `vo2`, `vo2max`,
`interval`, `intervals`, `threshold`, `tempo`, and `race`; preserve the raw value.
Do not infer hard status from activity name with substring matching.

De-duplicate a linked Garmin activity and structured workout into one session before
counting. Expose all matching reasons, not only the first.

### 7.3 Expand training-load output

Add:

- rolling running distance for 7, 14, and 28 inclusive days;
- rolling running duration for 7, 14, and 28 inclusive days;
- current 7-day versus previous 7-day running-distance change percentage;
- hard running session count and bounded hard-session evidence for the last 7 days;
- double-session day count for the last 7 and 28 days;
- days since the last complete rest day;
- total Garmin load and running-only Garmin load;
- structured strength session/working/high-effort-set counts as a separate fatigue
  context, not as invented Garmin load.

Definitions:

- windows include `as_of` and count backward by athlete-local calendar date;
- a double-session day has at least two de-duplicated sessions of at least 20
  minutes; return the session IDs/categories used;
- a complete rest day has no de-duplicated activity or workout of at least 20
  minutes. Search at most 90 days and return null if no rest day is found;
- weekly change is `(last_7 - previous_7) / previous_7 * 100`; return null when the
  previous window is null or zero;
- running-specific load sums Garmin activity load for category `running` only;
- do not invent strength load from tonnage. Return volume/high-effort evidence under
  a separate `strength_context` object.

Training monotony and strain are deferred until daily load coverage is trustworthy.
When implemented later: monotony is mean daily load divided by population standard
deviation over seven athlete-local days, strain is 7-day load times monotony, and
both are unavailable if any day lacks expected activity-sync coverage or the standard
deviation is zero.

Acceptance cases:

- the known VO2 workout is hard after compact recent activities are requested;
- each of the five rules independently labels a session hard at its boundary;
- 19.99 Z4+Z5 minutes is not hard and 20.00 is hard;
- one linked Garmin/manual pair counts once;
- every counted hard session includes a non-empty reason list;
- rolling windows and rest-day search respect athlete-local dates.

## 8. Phase 3 — activity metric units and sanity checks

### 8.1 Normalize by source key and descriptor unit

Use an explicit conversion table. Do not rely on `strings.Contains` alone for unit
meaning.

For descriptor-series metrics:

- descriptor key identifies the metric;
- descriptor `unit` identifies conversion;
- `meter`, `meters`, and `m` divide by 1000 before assignment to `DistanceKM`;
- `kilometer`, `kilometers`, and `km` do not convert;
- an absent or unknown distance unit makes the value unavailable and emits
  `UNKNOWN_METRIC_DISTANCE_UNIT`.

For flat legacy metric items:

- `distance_m`/`distance_meters` are metres;
- `distance_km`/`distanceKm` are kilometres;
- ambiguous `distance` is accepted only with an adjacent explicit unit;
- do not use magnitude guessing as the primary conversion method.

Descriptor index handling must use `metricsIndex`, not slice position. Add a test
where indices are sparse or reordered. Verify coordinate stripping updates both
descriptors and rows without shifting a non-coordinate value under the wrong key.

### 8.2 Validate cumulative distance

After decoding:

- cumulative distance must be non-negative;
- it should be non-decreasing, allowing a 0.01 km device/reset tolerance;
- no point may exceed the activity summary distance by more than the greater of
  0.10 km or 2%;
- invalid distance values become null for that point and add a bounded warning with
  code `IMPLAUSIBLE_METRIC_DISTANCE`; never silently relabel or clamp them;
- other valid metrics in the same point remain available.

Change the decoder result to carry normalization warnings so repository/MCP output
can expose them without logging health values.

Acceptance cases:

- raw `24.58` metres returns `0.02458` km;
- a 5.25 km summary never returns a metric point at 24.58 km;
- sparse/reordered descriptors decode correctly;
- unknown units produce null plus a stable warning;
- GPS fields remain recursively absent.

## 9. Phase 4 — transparent recovery and ergonomic output

### 9.1 Recovery response

Replace flattened recovery values with stable evidence objects while keeping aliases
for one release:

```json
{
  "classification": "strained",
  "confidence": "medium",
  "sleep": {
    "main_hours": 6.28,
    "nap_hours": 0.50,
    "total_24h": 6.78,
    "score": 59,
    "available": true,
    "stale": false
  },
  "hrv": {
    "value": null,
    "baseline": 46,
    "ratio": null,
    "available": false,
    "last_observed_date": "2026-08-27",
    "age_days": 8,
    "stale": true,
    "source_status": "unavailable"
  },
  "resting_heart_rate": {
    "value": null,
    "baseline": 52,
    "delta": null,
    "available": false,
    "last_observed_date": "2026-09-02",
    "age_days": 2,
    "stale": true
  },
  "method": {
    "name": "rule_based_recovery_v2",
    "valid_primary_signals": 2,
    "negative_signals": ["sleep_score_low"],
    "evaluations": [
      {"rule": "sleep_score_low", "value": 59, "operator": "<", "threshold": 60, "matched": true}
    ]
  }
}
```

Return an evaluation for every rule whose input is available. Return a separate
`unavailable_rules` list for rules that could not be evaluated. Keep baselines even
when the current value is missing, provided the baseline has enough samples.

### 9.2 Daily-health stable schema

Introduce typed status wrappers rather than `map[string]any`. Every returned day has
the same core object keys. Each metric object includes:

- its normalized values;
- `available` for that date;
- source record `updated_at`;
- source/method provenance;
- optional source status;
- no omitted numeric field merely because its value is zero.

Keep response-level `data_status`, but do not make callers use it to infer whether a
particular signal vanished. Add warnings such as `STALE_HRV`, `STALE_RESTING_HR`,
and `NAP_SOURCE_UNAVAILABLE` with stable severity.

### 9.3 Zone provenance

Wrap activity zones in:

```json
{
  "zone_model": "activity_specific",
  "zone_source": "garmin_activity_snapshot",
  "effective_date": "2026-09-04",
  "version": "sha256:<hash-of-normalized-boundaries>",
  "zones": []
}
```

Rules:

- use `activity_specific` for boundaries returned with an activity;
- use `athlete_profile` for configured profile zones;
- if Garmin does not reveal its underlying model, say `unknown`; do not infer
  percent-max-HR, LTHR, or HRR from boundaries;
- the version is a deterministic hash of metric type plus normalized ordered
  boundaries, not a database ID;
- longitudinal analysis groups zone minutes by version or warns
  `ZONE_DEFINITION_CHANGED`. It must not silently compare incompatible definitions.

### 9.4 Other output ergonomics

Add display forms for pace and duration to activity, split, interval, plan target,
and timeline values. Use typed `Provenance` and `Method` structs across all new
responses. Do not retrofit redundant provenance onto every stored ID or title.

Gate for phases 1-4:

- all correctness acceptance cases pass;
- old aliases and new canonical fields agree;
- missing, zero, and stale are distinguishable in JSON;
- `get_training_context` shows all values evaluated by its classification;
- no analysis result changes when the recent-activity presentation switches between
  compact and summary.

## 10. Phase 5 — `get_training_timeline`

### 10.1 Input

```json
{
  "target_athlete_id": null,
  "from": "2026-08-25",
  "to": "2026-09-07",
  "include_unplanned": true,
  "include_entries": false,
  "order": "asc",
  "limit": 100,
  "cursor": null
}
```

- required inclusive range, maximum 366 days;
- limit 1-100, default 50;
- `include_entries` defaults false and must still respect the existing 200-entry
  workout bound;
- paginate by `(date, stable timeline item ID)`.

### 10.2 Output model

Return day groups rather than pretending every day has a one-to-one plan/completion:

```json
{
  "date": "2026-09-03",
  "planned": [],
  "completed": [],
  "links": [],
  "status": "partial",
  "totals": {"duration_minutes": 52, "distance_km": 6.2, "garmin_load": 74}
}
```

Completed items have a stable kind: `garmin_activity`, `structured_workout`, or a
single `linked_pair`. A linked pair contains both source IDs but contributes only
once to duration/session counts.

### 10.3 Reconciliation rules

Apply in this order:

1. explicit `planned_sessions.completed_activity_id` or `completed_workout_id`;
2. explicit `workout_sessions.planned_session_id`;
3. explicit structured workout `linked_activity_id`;
4. deterministic read-only inferred match;
5. otherwise leave items unlinked.

For an inferred match, require same athlete/date and compatible normalized sport,
then score exact session type, start-time proximity, duration, and distance. Return
the score, method, and candidate IDs. Only mark `matched_inferred` when exactly one
candidate exceeds a documented threshold and beats the second candidate by a
documented margin. Ambiguous candidates remain unlinked with warning
`AMBIGUOUS_TIMELINE_MATCH`. Never persist inferred links from this read action.

Day/session statuses:

- `complete`: every non-cancelled planned item has a completion;
- `partial`: at least one but not all planned items completed, or only part of a
  multi-part planned session can be linked;
- `missed`: past date with planned items and no completion;
- `planned`: current/future date with incomplete planned items;
- `unplanned`: completions exist with no planned items;
- `rest`: neither planned nor completed training exists.

Files:

- new `internal/fitnessrepo/timeline.go`
- `internal/fitnessrepo/types.go`
- `internal/mcpserver/schema_common.go` or a new `schema_timeline.go`
- `internal/mcpserver/server.go`
- repository and MCP tests

## 11. Phase 6 — `get_exercise_progress` and aliases

### 11.1 Exercise catalog redesign

Change the catalog entry from a plain muscle slice to typed metadata:

```go
type ExerciseDefinition struct {
    Key              string
    Aliases          []string
    DirectMuscles    []string
    IndirectMuscles  []string
    SupportsE1RM     bool
}
```

Normalize alias input by Unicode case-folding, trimming, collapsing whitespace, and
removing only reviewed punctuation. An alias maps to exactly one canonical key.
Reject startup/tests if two exercises claim the same normalized alias. Include
reviewed aliases for the examples in the feedback, including one-arm dumbbell row,
DB row, OADBR, and single-arm row. Preserve the original user label in output.

Do not normalize unknown free text to a guessed exercise. Return
`exercise_key: null`, `normalization_status: "unknown"` and retain `exercise_name`.

### 11.2 Input and output

```json
{
  "exercise_key": "one_arm_dumbbell_row",
  "from": "2026-06-01",
  "to": "2026-09-04",
  "include_warmups": false,
  "limit": 100,
  "cursor": null
}
```

- exactly one canonical `exercise_key`, 1-128 characters;
- maximum range 730 days;
- at most 100 sessions per page and 200 sets per response;
- sessions ascending in the analysis section even if page order is descending.

Return:

- normalized exercise definition and alias provenance;
- sessions with set/reps/load/load basis/RIR and display values;
- best set per session;
- session volume when semantics are unambiguous;
- estimated 1RM for 1-12 reps, positive external load, supported exercises only;
- estimated rep PRs grouped by rep count;
- comparison of latest versus previous comparable sets at the same reps and RIR
  within one point;
- trend label `progressing`, `stable`, `regressing`, or `insufficient_data`, plus
  numeric evidence and sample counts.

Use Epley only where already documented:
`estimated_1rm_kg = normalized_external_load_kg * (1 + reps / 30)`.
Do not calculate e1RM for bodyweight movements, isolation movements where the
catalog says unsupported, ambiguous legacy loads, or sets above 12 reps. A rep PR is
descriptive: the greatest normalized load recorded for a rep count at a reported
RIR band. It is not an e1RM.

## 12. Phase 7 — strength repetition and load semantics

### 12.1 Additive entry fields

Add these typed fields to workout write/read contracts and mobile sync:

```json
{
  "laterality": "unilateral",
  "reps": 12,
  "reps_per_side": true,
  "load_basis": "per_limb",
  "load_per_limb_kg": 15,
  "total_external_load_kg": 15,
  "added_load_kg": null
}
```

Enums:

- `laterality`: `bilateral`, `unilateral`, `left`, `right`, `alternating`, `unknown`;
- `load_basis`: `total_external`, `per_limb`, `bodyweight_added`, `assistance`,
  `unknown`.

Validation and calculation:

- `reps_per_side=true` is allowed for `unilateral` or `alternating`, not `bilateral`;
- for `left` or `right`, reps are for the named side and are not doubled;
- for `unilateral` with `reps_per_side=true`, normalized total reps are `reps * 2`;
- `load_per_limb_kg` is the mass of one implement/limb;
- `total_external_load_kg` is the instantaneous external mass moved in one rep, not
  multiplied by repetitions or sides;
- `bodyweight_added` stores added external load separately; do not add athlete body
  mass unless a future model explicitly supplies it;
- assisted load remains signed/semantically separate and is excluded from external
  volume and e1RM until a dedicated formula exists;
- normalized volume is the sum of per-side reps times the external load used on that
  side. For 12 reps/side with one 15 kg dumbbell it is `12 * 2 * 15 = 360 kg`;
- for two 15 kg dumbbells in a bilateral press, total external load is 30 kg and 12
  reps produces 360 kg.

Legacy `load_kg` remains readable. If the new fields are absent, set
`load_semantics: "ambiguous_legacy"`. Continue returning legacy volume for
compatibility but exclude ambiguous sets from normalized volume, e1RM, and progress
claims. Do not silently assume that `load_kg` means pair total.

Persistence can remain additive JSON in `workout_entries.payload_raw`, but update all
of these surfaces together:

- `entryPayload`, `StrengthEntry`, validation, and summaries;
- MCP `entrySchema` and input structs;
- `api/openapi.yaml`, regenerated `internal/httpapi/generated.go`, and REST mappings;
- mobile Kotlin API/domain contracts, SQLite JSON payload handling, and workout UI;
- sync round-trip tests proving the fields survive server/mobile/server conversion.

## 13. Phase 8 — direct and indirect muscle volume

Return:

```json
{
  "biceps": {"direct_sets": 5, "indirect_sets": 6, "weighted_sets": 8.0},
  "triceps": {"direct_sets": 3, "indirect_sets": 6, "weighted_sets": 6.0}
}
```

Rules:

- the reviewed exercise catalog supplies direct and indirect roles;
- explicit entry fields `direct_muscle_groups` and `indirect_muscle_groups` override
  catalog roles;
- old `muscle_groups` is treated as direct only when explicitly supplied by the
  user; catalog-derived old groups are replaced by catalog roles;
- a working set contributes 1.0 direct set and 1.0 indirect set to each applicable
  group;
- `weighted_sets = direct_sets + 0.5 * indirect_sets`;
- expose the weighting method and never call weighted sets a physiological truth;
- warmups contribute zero;
- retain the old flat `muscle_group_sets` for one release.

Update workout summaries, context strength aggregates, training analysis, and
exercise progress from the same shared summarizer. Add table tests for rows, presses,
isolation exercises, explicit overrides, and warmups.

## 14. Phase 9 — `get_activity_intervals`

### 14.1 Input

```json
{
  "activity_id": "12345",
  "include_laps": true,
  "include_recoveries": true,
  "max_segments": 100
}
```

- activity ID required, 1-128 characters;
- maximum 100 segments, default 50;
- read-only, athlete-scoped, and GPS-free.

### 14.2 Source precedence

1. explicit Garmin structured-workout steps linked to the activity, if available;
2. Garmin typed splits/laps carrying an explicit step type;
3. ordinary laps/splits returned as `classification: "unclassified"`;
4. do not infer work/recovery solely from pace oscillation in v1.

During source characterization, determine whether current split/detail endpoints
contain structured step identity. If they do not, extend `sync_activity_details` to
fetch the smallest Garmin endpoint needed and store it inside the existing
activity-detail raw envelope. Do not fetch or return GPS.

### 14.3 Output

Return warm-up, work, recovery, cooldown, and unclassified segments with:

- sequence and source lap/step ID;
- start elapsed seconds;
- duration seconds and display;
- distance km;
- pace and display;
- average/max HR;
- average/max power;
- cadence where present;
- recovery HR drop only when both the preceding work-end HR and recovery-end HR are
  observed within documented time windows;
- classification source and confidence.

Never fabricate four 3-minute repetitions from noisy samples. If only laps exist,
return useful lap summaries and make the missing classification explicit.

## 15. Phase 10 — `get_readiness_for_session`

This action composes the corrected primitives. It must not duplicate Garmin parsing
or SQL.

### 15.1 Input

```json
{
  "target_athlete_id": null,
  "as_of": "2026-09-04T20:00:00+02:00",
  "session_date": "2026-09-05",
  "proposed_session_type": "running",
  "proposed_duration_minutes": 30,
  "proposed_intensity": "easy"
}
```

- `as_of` is an optional RFC 3339 timestamp with an explicit offset and defaults to
  the injected current time. Derive its local date in the athlete timezone. Reject a
  timestamp more than five minutes in the future;
- session date must be between the athlete-local date of `as_of` and that date plus
  14 days;
- type uses normalized sport/session enums;
- duration 1-1440 minutes;
- intensity: `easy`, `moderate`, `hard`, or `maximal`.

### 15.2 Output

```json
{
  "recommendation": "rest",
  "confidence": "medium",
  "reasons": [],
  "counterpoints": [],
  "rules_evaluated": [],
  "rules_unavailable": [],
  "relevant_recent_sessions": [],
  "recovery_signals": {},
  "load_context": {},
  "nearby_plan": [],
  "method": {"name": "session_readiness_rules_v1", "version": 1}
}
```

Allowed recommendation precedence is:
`rest > replace_with_easy > reduce > proceed`.

Initial deterministic rules:

- `rest` for illness reported, meaningful pain, or recovery classification `low`;
- for an easy/moderate proposal, `rest` when all three are true: recovery is
  `strained`, at least six consecutive training days, and a hard session occurred
  within the previous 24 hours relative to the exact `as_of` timestamp;
- `replace_with_easy` for a hard/maximal proposal when recovery is `strained`, a hard
  session occurred in the previous 48 hours relative to `as_of`, or a key hard/long
  planned session is within the following 48 hours;
- `reduce` when no higher rule matched and any of these is true: recovery is
  `strained`, at least six consecutive training days, running load is elevated versus
  its recent range, or a key hard/long planned session is the next day;
- `proceed` only when no reducing rule matched. It means no configured rule objected,
  not that the session is medically safe.

Define a `long` planned session as either explicitly typed `long` or at least 90
minutes/15 km for running. Put these thresholds in named constants.

Every reason/counterpoint contains a stable rule ID, actual value, threshold, source
entity IDs, and date. Counterpoints are factual context only, such as load remaining
within recent range or corrected nap duration; they never cancel a higher-precedence
rule silently.

Confidence is based on input availability:

- high: sleep, HRV, RHR, recent activities, and nearby plan are current;
- medium: at least three of those five are current;
- low: two are current;
- unavailable: fewer than two are current. In that case recommendation must be
  `reduce` unless an explicit rest rule fired, and the response must say the evidence
  is incomplete.

Add an end-to-end MCP test for the motivating case: six consecutive training days,
a same-day VO2 activity and strength workout, low main-sleep score, a 30-minute nap,
and a long run on the following day. The result must show corrected 24-hour sleep,
both recent sessions, the future plan, fired rule IDs, and a deterministic
recommendation.

## 16. Phase 11 — lower-priority write and health improvements

These are part of the feedback but should follow the ten ordered priorities above.

### 16.1 `record_quick_readiness`

Input:

```json
{
  "recorded_at": "2026-09-04T08:00:00+02:00",
  "energy": 3,
  "leg_freshness": 2,
  "sleep_quality": 3,
  "pain_present": false
}
```

- all ratings are integers 1-5;
- `pain_present` is required; if true, optional area/severity may be supplied;
- if pain is true without detail, store `pain_present` without inventing severity and
  return `needs_detail: ["pain"]`;
- write to the existing `checkins.content_raw` model with `schema_version: 2` and use
  the same LWW/idempotency rules as `record_checkin`;
- do not map leg freshness into fatigue by arithmetic. Add it as its own signal;
- full and quick check-ins must round-trip through `get_checkins`.

### 16.2 `patch_workout`

Use an explicit operation list, not arbitrary JSON Patch paths:

```json
{
  "workout_id": "...",
  "expected_sync_seq": 42,
  "client_updated_at": 1788500000000,
  "operations": [
    {"op": "set_linked_activity", "activity_id": "12345"},
    {"op": "set_entry_rir", "entry_id": "...", "rir": 2},
    {"op": "set_entry_exercise_key", "entry_id": "...", "exercise_key": "one_arm_dumbbell_row"},
    {"op": "append_note", "text": "Left side felt harder"}
  ]
}
```

Allow only reviewed operations for linked activity, notes, entry RIR, exercise key,
muscle roles, and the new repetition/load semantics. Maximum 50 operations. Apply all
operations in one transaction; if any entry is absent or not owned by the workout,
apply none. Require `expected_sync_seq` and return `conflict` with the current winner
when stale. Allocate one new session sync sequence. Do not tombstone or replace
untouched entries. Add concurrent mobile/full-upsert conflict tests.

### 16.3 Additional Garmin health fields

Add only fields proven present in the already-ingested raw datasets, in this order:

1. overnight respiration;
2. HRV baseline and deviation from baseline;
3. Body Battery morning/high/low/change;
4. overnight stress;
5. Garmin training readiness and recovery time;
6. sleep need/debt;
7. illness flags;
8. weight/body-composition history.

For each signal: add a synthetic raw and legacy fixture, explicit units, stable
availability/freshness, source status, and provenance before exposing it. Do not make
the readiness action depend on a newly exposed Garmin score until its semantics and
coverage are tested.

## 17. Code organization

Before phases 5-10, move pure calculations out of `internal/fitnessrepo/analysis.go`
into `internal/fitnessanalysis` as anticipated by `ACTION_SET_PLAN.md`:

```text
internal/fitnessanalysis/
  recovery.go
  hard_session.go
  load.go
  timeline.go
  strength.go
  readiness.go
  display.go
```

The analysis package consumes normalized typed values and has no SQL, raw JSON, MCP,
or wall-clock calls. Pass `as_of`, timezone, and all source values explicitly. Keep
repository functions responsible for bounded reads and link composition.

Add dedicated MCP schema files as the surface grows:

```text
internal/mcpserver/
  schema_timeline.go
  schema_strength_analysis.go
  schema_readiness.go
  schema_patch.go
```

All input objects use `additionalProperties: false`, bounds, enums, and explicit
required fields. All actions accept optional `target_athlete_id` and resolve it
through the existing `principal` method.

## 18. Migration and versioning strategy

Most normalized Garmin and strength additions fit existing raw JSON storage and do
not require a server migration. Do not create a migration without a persisted need.

If implementation needs persisted inferred-link review state, exercise aliases, or
patch revisions, create `migrations/00006_coaching_normalization.sql`; do not edit
existing migrations. Prefer code-owned exercise catalog aliases in this iteration.

For mobile strength fields, add the next mobile SQLite migration in
`apps/android/app/src/main/kotlin/com/fitsync/nativeapp/data/Database.kt`, preserve old rows, and make reads tolerate absent
columns/JSON keys. Update OpenAPI first, regenerate the Go and TypeScript clients with
the repository's existing generation workflow, and never hand-edit generated output
without updating its source contract.

Bump the Fitnessd MCP implementation version after phases 1-4 and again when the new
tools are registered. Tool names remain stable once published.

## 19. Test plan

### 19.1 Pure decoder and analysis tests

Use table-driven synthetic cases for:

- main-only, aggregate-nap, detailed-nap, duplicate-nap, and missing-nap-source data;
- HRV/RHR missing, zero, stale at 1/2/3 days, and no observation within 90 days;
- every recovery threshold and unavailable rule;
- every hard-session rule and compact/summary projection invariance;
- rolling 7/14/28 windows, percentage denominator zero, double sessions, and rest
  days;
- metric units, sparse descriptor indices, bad cumulative distance, and GPS removal;
- zone hashes and changed zone definitions;
- alias collisions and known/unknown aliases;
- unilateral/bilateral/per-side volume examples;
- direct, indirect, and weighted muscle sets;
- e1RM eligibility, RIR comparison, rep PR, and insufficient-data trends;
- interval source precedence and unclassified fallback;
- readiness rule precedence and confidence.

### 19.2 Repository tests

Use migrated temporary SQLite databases with two athletes. Test:

- every read/write is athlete-scoped;
- timeline explicit link precedence, inferred uniqueness, ambiguity, statuses, and
  linked-pair de-duplication;
- signal `updated_at` comes from the correct Garmin record;
- last-observed queries are bounded and date-correct;
- patch operations are atomic and optimistic concurrency rejects stale writes;
- new JSON fields survive LWW reads/writes and old payloads still decode;
- pagination has no duplicates or gaps at equal dates.

### 19.3 MCP protocol tests

For every new action verify:

- discoverable tool name and description;
- correct read/write/destructive/idempotent annotations;
- strict nested schema and rejection of unknown properties;
- omitted target defaults to the authenticated athlete;
- cross-household targets do not reveal existence;
- stable output JSON for null, zero, missing, and stale;
- structural response bounds and no GPS/raw payload leakage.

### 19.4 Python and mobile tests

- curated sleep output uses the new names without changing raw upload bytes;
- activity-detail coordinate stripping preserves metric descriptor alignment;
- mobile API and SQLite round-trip all new strength fields;
- offline outbox retries do not erase fields added by patch/full updates;
- relevant mobile reducer/UI tests cover per-side and per-limb entry.

## 20. Verification commands

Repository instructions require routine verification through these entrypoints and
require them to run outside the sandbox.

During focused iteration, run the smallest relevant command after each change:

```bash
./scripts/test.sh --go
./scripts/test.sh --vet
./scripts/test.sh --python
```

When mobile schemas or behavior change:

```bash
./scripts/build-mobile.sh
./scripts/test-mobile-e2e.sh
```

At the end of every phase, and before handoff:

```bash
./scripts/test.sh
./scripts/build.sh
```

Run `./scripts/build.sh --docker` only when container/build inputs change. Do not
substitute ad-hoc `go test`, `go vet`, or Python commands for the repository
entrypoints in phase gates.

## 21. Implementation checklist for a smaller LLM

For each phase, follow this exact loop:

1. read the relevant contract section in this plan;
2. inspect the named existing types/functions and their tests;
3. add a failing synthetic regression test;
4. add or change typed domain structs;
5. implement normalization or pure analysis;
6. update repository composition with athlete/date bounds;
7. update MCP input structs, strict schema, handler, and registration if applicable;
8. update OpenAPI/mobile only when shared persisted/write shapes changed;
9. run focused repository scripts outside the sandbox;
10. fix failures without weakening assertions;
11. run the phase gate scripts outside the sandbox;
12. review `git diff` for real data, raw payloads, GPS, secrets, unrelated edits, and
    generated-file drift.

Never combine two phases merely because they touch the same file. In particular,
finish and verify sleep, hard-session, and metric-unit correctness before beginning
timeline or readiness work.

## 22. Definition of done

This iteration is complete when:

- a synced nap changes `total_sleep_24h` exactly once and recovery states which sleep
  value it evaluated;
- `hard_sessions_last_7_days` recognizes the synthetic VO2 and threshold cases and
  returns evidence for every hard label;
- metric distances are correctly unit-normalized and implausible values are not
  exposed as kilometres;
- daily health and recovery distinguish current, missing, stale, and zero values;
- every derived coaching value includes source/method/freshness information needed
  to audit it;
- planned and completed training are available in one bounded, de-duplicated
  timeline;
- exercise progress works across reviewed aliases and does not claim progress from
  ambiguous loads or incomparable RIR;
- strength sets have explicit repetition/load semantics and direct/indirect muscle
  roles;
- interval workouts have bounded lap/step summaries without requiring metric blobs;
- readiness returns a deterministic recommendation plus all reasons, counterpoints,
  unavailable rules, and underlying evidence;
- quick readiness and workout patch writes are atomic, athlete-scoped, idempotent,
  and sync-safe;
- old read aliases work for the documented compatibility release;
- no raw Garmin payload, GPS, secret, or cross-athlete data appears in responses,
  logs, fixtures, or Git;
- `./scripts/test.sh`, `./scripts/build.sh`, and applicable mobile checks pass outside
  the sandbox.
