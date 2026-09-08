# Fitness MCP action-set implementation plan

## 1. Goal

Expand Fitnessd's MCP surface so an assistant can answer questions such as:

- What kind of run should I do tomorrow?
- Should I do strength today?
- How has my recent training been?
- How has my recovery been?
- Am I progressing toward an event or weekly goal?
- Which muscle groups have I neglected or trained hard recently?
- How closely did completed training match the plan?

The server supplies bounded facts, trends, completeness information, and transparent deterministic classifications. The MCP client supplies the prose explanation and recommendation. Fitnessd must not generate a medical diagnosis or hide a recommendation inside an unexplained score.

This document is normative. Field names, bounds, null handling, authorization, ordering, and calculation rules below are part of the contract.

## 2. Existing behavior and compatibility

The current MCP server registers:

- `get_daily_health`
- `get_activities`
- `update_workout`

Keep all three names available during migration.

- Enrich `get_daily_health` and `get_activities` in place. Existing top-level `items` remains, and existing summary fields may remain as additive compatibility fields for one release.
- Keep `update_workout` as a deprecated low-level mutation. New clients should use `upsert_workout` and `delete_workout`.
- Do not expose `payload_raw`, FIT archives, GPS coordinates, database IDs unrelated to the requested entity, or raw Garmin response collections through MCP.
- REST behavior is out of scope except for internal service methods shared with MCP. Do not weaken the existing REST authorization or raw-payload bounds.

## 3. Complete action inventory

Implement these 15 actions:

| Action | Scope | Mutation | Purpose |
|---|---|---:|---|
| `get_training_context` | `fitness:read` | no | One-call context for a near-term training decision |
| `get_daily_health` | `fitness:read` | no | Daily sleep, recovery, and wellness trends |
| `get_activities` | `fitness:read` | no | Bounded activity history and training metrics |
| `get_activity` | `fitness:read` | no | Drill into one activity, including bounded details |
| `get_strength_workouts` | `fitness:read` | no | Read manual/Garmin-linked strength sessions and sets |
| `upsert_workout` | `fitness:write` | yes | Atomically create or replace a structured workout |
| `delete_workout` | `fitness:write` | yes | Tombstone a workout and its entries |
| `get_checkins` | `fitness:read` | no | Read subjective readiness history |
| `record_checkin` | `fitness:write` | yes | Create or replace a subjective check-in |
| `get_athlete_profile` | `fitness:read` | no | Read goals, zones, availability, preferences, constraints |
| `update_athlete_profile` | `fitness:write` | yes | Replace the editable athlete profile |
| `get_training_plan` | `fitness:read` | no | Read planned sessions and completion linkage |
| `upsert_planned_session` | `fitness:write` | yes | Create or replace one planned session |
| `update_planned_session_status` | `fitness:write` | yes | Change status/link completion without replacing targets |
| `get_training_analysis` | `fitness:read` | no | Period and comparison-period aggregates |

All actions accept optional `target_athlete_id`. An omitted target means the authenticated athlete. Resolve and authorize it through the existing `principal` method. Never reveal whether an unauthorized athlete exists.

## 4. Shared contract

### 4.1 Dates, timestamps, and timezone

- A date is exactly `YYYY-MM-DD` and is interpreted in the athlete profile's IANA timezone.
- A timestamp is RFC 3339 with an explicit offset. Normalize stored timestamps to UTC milliseconds, but return RFC 3339 plus the relevant local date.
- Default timezone is `UTC` until the athlete saves a valid IANA timezone.
- Load timezones using Go's `time.LoadLocation`. Reject unknown zones.
- `as_of` defaults to the current date in the athlete timezone, not the server timezone.

### 4.2 Pagination and ordering

Every list action supports:

```json
{
  "order": "desc",
  "limit": 50,
  "cursor": null
}
```

- `order`: `asc` or `desc`; default `desc`.
- `limit`: 1-100; default 50.
- Cursor is opaque base64url JSON containing version, sort date/timestamp, stable ID, order, and a hash of filters.
- Reject a cursor if its version, order, or filter hash does not match the request.
- Query with a stable `(date_or_timestamp, id)` ordering. Fetch `limit + 1` rows and emit `next_cursor` only when another row exists.
- Analysis and context actions are not paginated; they have strict date/lookback bounds instead.

### 4.3 Nulls, units, and missing data

- Unknown numbers are JSON `null`, never `0`.
- Durations use minutes unless the field explicitly ends in `_seconds` or `_hours`.
- Distances use kilometres, loads use kilograms, pace uses minutes per kilometre, heart rate uses bpm, power uses watts, and ratios are unitless.
- Do not infer a zero-value day when a source record is absent.
- Each response includes `data_status`:

```json
{
  "last_sync_at": "2026-08-28T07:15:00Z",
  "complete_through": "2026-08-27",
  "missing_dates": ["2026-08-24"],
  "warnings": [
    {"code": "MISSING_HRV", "severity": "info", "message": "HRV is unavailable for 3 of 28 days."}
  ]
}
```

`last_sync_at` is the maximum `garmin_records.ingested_at` included for the athlete. `complete_through` is the latest date for which a sleep record and a daily recovery record both exist. If neither dataset is expected or available, return `null`. Limit `missing_dates` to 31 entries; add warning `MISSING_DATES_TRUNCATED` when necessary.

### 4.4 Provenance and calculated values

Values derived by Fitnessd include:

```json
{
  "method": "median_previous_28_valid_days",
  "valid_samples": 24,
  "required_samples": 14,
  "confidence": "high"
}
```

Allowed confidence values are `low`, `medium`, `high`, and `unavailable`.

- High: at least 80% of expected samples are present.
- Medium: at least 50% are present.
- Low: at least the method-specific minimum is present but less than 50%.
- Unavailable: method-specific minimum is not met.

Never return a trend or classification without returning its supporting values and method metadata.

### 4.5 Errors

Handlers return concise tool errors without sensitive values. Use these stable prefixes:

- `invalid_input:` schema-valid JSON with a semantic error
- `not_found:` requested authorized entity does not exist
- `conflict:` stale `client_updated_at` or uniqueness conflict
- `unavailable:` required source data is unavailable
- `internal_error:` unexpected storage/decoding failure

Do not include raw payloads, SQL, tokens, other athlete IDs, or health values in logs or error strings.

### 4.6 MCP annotations

- Read actions: `ReadOnlyHint=true`, `IdempotentHint=true`, `DestructiveHint=false`, `OpenWorldHint=false`.
- Upserts: `ReadOnlyHint=false`, `IdempotentHint=true`, `DestructiveHint=false`, `OpenWorldHint=false`.
- Deletes/status cancellation: `ReadOnlyHint=false`, `IdempotentHint=true`, `DestructiveHint=true`, `OpenWorldHint=false`.
- Every input schema uses `additionalProperties: false` at every object level, bounded strings/arrays, enums, and explicit required fields.

## 5. Canonical normalized types

Do not pass arbitrary Garmin JSON into MCP structs. Add decoder functions that normalize both formats already present in `garmin_records`:

1. Direct Fitnessd ingestion, generally Garmin camelCase raw payloads.
2. Imported legacy records, generally curated snake_case payloads.

Add `internal/fitnessdata` with these files:

```text
internal/fitnessdata/
  types.go
  sleep.go
  recovery.go
  activity.go
  activity_detail.go
  training.go
  decoder_test.go
```

Each decoder accepts `(dataset string, payload json.RawMessage)` and returns a typed normalized value plus field-level availability. It must tolerate missing optional fields and reject invalid JSON. Do not silently coerce strings to numbers except empty string to null for legacy compatibility.

### 5.1 `DailyHealth`

```go
type DailyHealth struct {
    Date          string
    Sleep         *SleepSummary
    HRV           *HRVSummary
    HeartRate     *HeartRateSummary
    Stress        *StressSummary
    BodyBattery   *BodyBatterySummary
    SpO2          *SpO2Summary
    DailyActivity *DailyActivitySummary
    Completeness  map[string]bool
}
```

Normalize sleep from either the raw envelope (`sleep.dailySleepDTO`, `hrv.hrvSummary`, and `resting_heart_rate`) or flat legacy keys. Normalize daily recovery from either raw `summary`, `stress`, `heart_rate`, `intensity`, `body_battery` members or flat legacy keys.

### 5.2 `ActivitySummary`

Include:

- `activity_id`, date, local/GMT start time, name, type, parent type
- distance, duration, moving/elapsed time, pace and speed
- elevation gain/loss
- average/max HR and five HR-zone minute values
- calories
- aerobic/anaerobic training effect, label, activity training load
- VO2 max
- cadence, power, grade-adjusted pace, respiration, running dynamics
- temperature, water loss, Body Battery change, steps, intensity minutes
- fastest 1K/mile, PR flag, lap count, manufacturer
- perceived effort and notes from a linked manual session when available
- `linked_workout_id`

Support Garmin raw camelCase names shown in `garmin_sync.py` and the snake_case names emitted by `summarize_activity`.

Normalize activity types to stable broad categories while retaining source type:

```text
running, walking, cycling, swimming, strength, cardio, mobility,
hiking, multisport, other
```

For example, `trail_running` has category `running` and source type `trail_running`. Keep the mapping in a tested Go map; unknown types map to `other` without being discarded.

### 5.3 Strength session and entry

Existing `workout_sessions.metadata_raw` stores session-only fields. Existing `workout_entries.payload_raw` stores entry detail. Use these canonical payloads:

Session metadata:

```json
{
  "workout_type": "strength",
  "started_at": "2026-08-28T18:00:00+02:00",
  "duration_minutes": 50,
  "perceived_effort": 7,
  "linked_activity_id": "12345",
  "notes": "Shoulder felt fine"
}
```

Entry payload:

```json
{
  "exercise_key": "back_squat",
  "exercise_name": "Back Squat",
  "user_label": null,
  "set_number": 1,
  "reps": 5,
  "duration_seconds": null,
  "load_kg": 80,
  "load_description": "80 kg",
  "is_bodyweight": false,
  "to_failure": false,
  "rir": 2,
  "laterality": "bilateral",
  "muscle_groups": ["quads", "glutes", "hamstrings"],
  "notes": null
}
```

Legacy `rir_min`/`rir_max` decodes as `rir_min` and `rir_max`; set singular `rir` only when they are equal.

## 6. Persistence migration

Create `migrations/00002_training_context.sql`. Do not rewrite migration 00001.

```sql
-- +goose Up
ALTER TABLE athletes ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';

CREATE TABLE athlete_profiles (
    athlete_id TEXT PRIMARY KEY REFERENCES athletes(id),
    content_raw TEXT NOT NULL CHECK (json_valid(content_raw)),
    client_updated_at INTEGER NOT NULL,
    server_synced_at INTEGER NOT NULL
);

CREATE TABLE checkins (
    id TEXT PRIMARY KEY,
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    recorded_at_utc_ms INTEGER NOT NULL,
    local_date TEXT NOT NULL,
    timezone TEXT NOT NULL,
    content_raw TEXT NOT NULL CHECK (json_valid(content_raw)),
    client_updated_at INTEGER NOT NULL,
    server_synced_at INTEGER NOT NULL,
    deleted_at INTEGER,
    UNIQUE (athlete_id, recorded_at_utc_ms)
);
CREATE INDEX checkins_athlete_date_idx
    ON checkins (athlete_id, local_date, recorded_at_utc_ms, id);

CREATE TABLE training_plans (
    id TEXT PRIMARY KEY,
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    title TEXT NOT NULL,
    goal_raw TEXT NOT NULL CHECK (json_valid(goal_raw)),
    active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    client_updated_at INTEGER NOT NULL,
    server_synced_at INTEGER NOT NULL,
    deleted_at INTEGER
);
CREATE UNIQUE INDEX one_active_plan_per_athlete
    ON training_plans (athlete_id) WHERE active = 1 AND deleted_at IS NULL;

CREATE TABLE planned_sessions (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES training_plans(id),
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    session_date TEXT NOT NULL,
    sport TEXT NOT NULL,
    session_type TEXT NOT NULL,
    priority TEXT NOT NULL,
    targets_raw TEXT NOT NULL CHECK (json_valid(targets_raw)),
    notes TEXT,
    status TEXT NOT NULL CHECK (status IN ('planned','completed','skipped','cancelled')),
    completed_activity_id TEXT,
    completed_workout_id TEXT,
    client_updated_at INTEGER NOT NULL,
    server_synced_at INTEGER NOT NULL,
    deleted_at INTEGER,
    FOREIGN KEY (completed_workout_id, athlete_id)
        REFERENCES workout_sessions(id, athlete_id)
);
CREATE INDEX planned_sessions_athlete_date_idx
    ON planned_sessions (athlete_id, session_date, id);

-- +goose Down
DROP TABLE planned_sessions;
DROP TABLE training_plans;
DROP TABLE checkins;
DROP TABLE athlete_profiles;
ALTER TABLE athletes DROP COLUMN timezone;
```

Before using this verbatim, verify the SQLite version supports partial unique indexes and `DROP COLUMN` (the modern SQLite bundled through `modernc.org/sqlite` should). Add a migration test that runs up, down, and up again so an unsupported runtime fails during development rather than deployment. The composite workout foreign key is valid because migration 00001 declares `UNIQUE (id, athlete_id)`.

Profile, check-in, goal, and target objects remain JSON so the contract can evolve, but their row identity, ownership, dates, status, and clocks are normalized and indexed.

All newly created IDs must be UUIDv7. All writes use `client_updated_at` and the existing last-write-wins rule:

- New record: accept a UUIDv7 and required fields.
- Existing record: update only if submitted `client_updated_at` is greater.
- Equal or older timestamp: return the existing winner and `submitted_version_won: false`; do not report a tool error.
- Ownership never changes.

Extend the mobile sync clock union and pull/push support for the four new entity types in a later compatible OpenAPI revision. MCP implementation does not have to wait for mobile sync support, but the migration must not reuse `app_documents` for these entities.

## 7. Action contracts

The schemas below omit `target_athlete_id` from repeated examples. Add it to every input as an optional UUID string.

### 7.1 `get_training_context`

Description: `Get a bounded decision-ready snapshot of recovery, recent training, goals, subjective check-ins, and nearby planned sessions.`

Input:

```json
{
  "as_of": "2026-08-28",
  "lookback_days": 28,
  "include": ["recovery", "training_load", "recent_activities", "strength", "plan", "checkins", "goals"]
}
```

- `as_of`: optional date.
- `lookback_days`: 7-90, default 28.
- `include`: unique enum array, 1-7 items; default all values shown above.

Output has:

- athlete ID, `as_of`, timezone, `generated_at`, and `data_status`
- profile goals and nearest future target event
- `current_status.recovery`
- `current_status.training_load`
- aggregates under `recent_training.runs` and `recent_training.strength`
- at most 10 latest activities in compact form
- latest check-in on or before `as_of`, no older than seven days
- plan sessions for `as_of - 1 day` through `as_of + 7 days`
- deterministic flags

Recovery object:

```json
{
  "classification": "normal",
  "confidence": "medium",
  "sleep_score": 82,
  "sleep_hours": 7.6,
  "overnight_hrv": 48,
  "hrv_baseline": 46,
  "hrv_ratio": 1.043,
  "resting_heart_rate": 51,
  "rhr_baseline": 52,
  "rhr_delta": -1,
  "body_battery_at_wake": 76,
  "average_stress_yesterday": 24,
  "subjective_fatigue": 2,
  "soreness": [],
  "method": {
    "name": "rule_based_recovery_v1",
    "valid_primary_signals": 4,
    "negative_signals": []
  }
}
```

Use the most recent night ending on `as_of`. Baselines use valid values from the 28 dates before that night and require at least 7 values. Baseline is the median, not mean.

`rule_based_recovery_v1` primary negative signals are:

- `sleep_score_low`: score below 60
- `sleep_duration_low`: total sleep below 6 hours
- `hrv_suppressed`: overnight HRV below 85% of baseline
- `rhr_elevated`: RHR at least 7 bpm over baseline
- `body_battery_low`: Body Battery at wake below 35
- `high_fatigue`: latest check-in fatigue is 4 or 5
- `illness_present`: latest check-in says illness is present
- `meaningful_pain`: any pain severity is 3 or more

Classification:

- `unknown`: fewer than two primary objective signals (sleep score/duration, HRV, RHR, Body Battery) are available and no subjective warning exists.
- `low`: illness, meaningful pain, or at least three negative signals.
- `strained`: one or two negative signals.
- `normal`: zero negative signals.

Do not return `excellent`; absence of a negative signal is not proof that maximal training is appropriate. Confidence is high with at least four objective signals, medium with two or three, low with one plus a subjective signal, otherwise unavailable.

Training load object:

```json
{
  "last_7_days": 315,
  "previous_7_days": 270,
  "last_28_days": 1120,
  "chronic_weekly_average": 280,
  "acute_chronic_ratio": 1.125,
  "hard_sessions_last_7_days": 2,
  "consecutive_training_days": 3,
  "classification": "within_recent_range",
  "method": {"name": "garmin_activity_load_sum_v1", "activities_with_load": 12, "total_activities": 14}
}
```

- Sum non-null Garmin `activity_training_load`; never substitute duration or calories.
- Chronic weekly average is the sum of the last 28 days divided by 4.
- Ratio is last-seven-day load divided by chronic weekly average.
- Ratio/classification is null/`unavailable` if fewer than 14 of 28 dates have expected synchronized activity coverage, fewer than four activities have load, or chronic average is zero.
- Descriptive labels: `<0.8 under_recent_range`, `0.8-1.3 within_recent_range`, `>1.3 elevated_vs_recent_range`. Do not label this ratio an injury-risk score.
- A hard session is one with aerobic or anaerobic effect at least 3.5, at least 20 minutes in Z4+Z5, session type in `intervals`, `tempo`, `threshold`, `race`, or manual perceived effort at least 8.
- Consecutive training days counts backwards from `as_of`; a day counts when it has at least 20 activity/workout minutes. Do not double-count linked Garmin and manual sessions.

Flags use stable codes. Implement at least:

```text
ILLNESS_REPORTED, PAIN_REPORTED, RECOVERY_LOW, RECOVERY_STRAINED,
LOAD_ELEVATED, BACK_TO_BACK_HARD_DAYS, NO_CURRENT_CHECKIN,
NO_CURRENT_INJURY_DATA, STALE_SYNC, INCOMPLETE_RECOVERY_DATA,
PLANNED_HARD_SESSION, STRENGTH_MUSCLE_GROUP_RECENTLY_TRAINED
```

Flags describe facts. They do not say `safe`, `unsafe`, or prescribe treatment.

### 7.2 `get_daily_health`

Description: `Read normalized daily sleep, recovery, and wellness metrics with missing-data indicators.`

Input:

```json
{
  "from": "2026-08-01",
  "to": "2026-08-28",
  "metrics": ["sleep", "hrv", "heart_rate", "stress", "body_battery", "spo2", "daily_activity"],
  "order": "desc",
  "limit": 28,
  "cursor": null
}
```

- Date range is required and at most 366 inclusive days.
- `metrics` is a unique 1-7 enum array; default all.
- Return one merged item per date, not one item per dataset.
- Join decoded `sleep`, `daily_recovery`, and `body_battery_events` in Go by local date.

Each item may contain:

- `sleep`: main/nap/total hours, deep/light/REM/awake hours, score, qualifier, sleep stress
- `hrv`: overnight average, weekly average, Garmin status
- `heart_rate`: resting, minimum, maximum
- `stress`: average, maximum, qualifier, high/medium/low/rest minutes
- `body_battery`: at wake, highest, lowest, most recent, charged, drained
- `spo2`: sleep average/lowest and daily average/lowest
- `daily_activity`: steps/goal, distance, active/highly active/sedentary time, calories, intensity minutes, floors, waking respiration
- `completeness`: requested metric name to boolean

Return `next_cursor` and response-level `data_status`.

### 7.3 `get_activities`

Description: `Read normalized activity summaries for training volume, intensity, and load analysis.`

Input:

```json
{
  "from": "2026-08-01",
  "to": "2026-08-28",
  "categories": ["running", "strength"],
  "source_types": [],
  "detail": "summary",
  "min_duration_minutes": null,
  "min_distance_km": null,
  "min_training_load": null,
  "order": "desc",
  "limit": 50,
  "cursor": null
}
```

- Required date range, maximum 366 days.
- Category/source arrays: unique, maximum 20.
- Detail enum: `compact`, `summary`, `full`; default `summary`.
- Numeric filters are optional and non-negative.
- Query dataset `activities`; decode before applying filters that cannot be expressed safely in SQL.
- To fill a page after decoded filtering, scan in chunks of at most 500 database rows and stop after the requested `limit + 1` matches. Cursor tracks the last scanned database row, not merely the last match.

Compact fields: ID, date/start time, name, category/source type, duration, distance, training load, linked workout ID.

Summary additionally includes pace/speed, HR, calories, elevation, training effect, VO2 max, zone minutes, perceived effort, and notes.

Full additionally includes all normalized running dynamics/power/environment fields and lap count, but not metric time series. Time series belong to `get_activity`.

### 7.4 `get_activity`

Description: `Read one activity with optional bounded splits, zones, and downsampled metric series; GPS is excluded.`

Input:

```json
{
  "activity_id": "12345",
  "include": ["summary", "splits", "zones", "metrics"],
  "metric_resolution_seconds": 30,
  "max_metric_points": 500
}
```

- `activity_id`: required, 1-128 characters.
- `include`: unique enum array, default `summary`; maximum four.
- Resolution: 5-300 seconds, default 30.
- Points: 10-1000, default 500.

Find the athlete-owned `activities` record by either generated `activity_id`, `record_key`, raw `activityId`, or legacy `activity_id`. Then find matching `activity_details` by record key/payload ID.

Output:

- `summary`: full activity summary
- `splits`: at most 200 ordered split/lap objects
- `zones`: HR and power zone boundaries/minutes when available
- `metrics`: an array of points containing elapsed seconds and available HR, pace/speed, cadence, power, elevation, respiration, and temperature
- `metrics_metadata`: source point count, returned point count, resolution, and truncation flag

Downsampling algorithm:

1. Remove latitude, longitude, map, route, polyline, start/end coordinate, and location fields recursively before any response construction.
2. Bucket points by `floor(elapsed_seconds / resolution)`.
3. For numeric physiological fields return the arithmetic mean per bucket; for cumulative distance return the last value; for categorical fields return the last value.
4. If still above `max_metric_points`, increase resolution to `ceil(duration_seconds / max_metric_points)` and rebucket.
5. If elapsed time is missing, uniformly sample indices, always retaining first and last.

Return `not_found` for an absent authorized activity. An activity without detail still returns summary and warnings for requested missing sections.

### 7.5 `get_strength_workouts`

Description: `Read structured strength sessions, exercises, sets, effort, and muscle-group summaries.`

Input:

```json
{
  "from": "2026-07-01",
  "to": "2026-08-28",
  "exercise_keys": [],
  "muscle_groups": [],
  "include_entries": true,
  "order": "desc",
  "limit": 30,
  "cursor": null
}
```

- Required range, maximum 730 days.
- Filter arrays unique, maximum 30 values, each 1-128 characters.
- Include only non-deleted sessions and entries.
- A strength session has metadata `workout_type=strength`, a strength-like Garmin link, or at least one `set` entry.
- Apply exercise/muscle filters with `EXISTS`; a session matches when any non-deleted entry matches.

Return session identity/date/title/source, start/duration/RPE/notes, linked activity metrics, entries when requested, and summary:

- `working_sets`: count `entry_type=set`; exclude warmups when payload has `is_warmup=true`
- `volume_load_kg`: sum `reps * load_kg`; null if no entry has both fields
- `muscle_group_sets`: each set contributes 1 to every resolved primary muscle group; do not fractionally divide it
- `high_effort_sets`: `to_failure=true`, `rir<=1`, or `rir_max<=1`

Add `internal/exercisecatalog/catalog.go` with a small initial mapping for known exercise keys. Resolution order is entry `muscle_groups`, catalog mapping, then empty. Never guess from free-text exercise name.

### 7.6 `upsert_workout`

Description: `Atomically create or replace one structured workout and its ordered entries.`

Input:

```json
{
  "workout_id": "018f...uuidv7...",
  "client_updated_at": 1787900000000,
  "date": "2026-08-28",
  "title": "Upper Body",
  "workout_type": "strength",
  "source": "manual_mcp",
  "started_at": "2026-08-28T18:00:00+02:00",
  "duration_minutes": 50,
  "perceived_effort": 7,
  "notes": "Shoulder felt fine",
  "linked_activity_id": null,
  "entries": []
}
```

Validation:

- `workout_id` is optional for new records; when omitted, the server generates a UUIDv7. An optional `idempotency_key` is a caller-owned secondary key for safe retries.
- `client_updated_at` non-negative.
- Date/title/workout type/source required; title max 512, notes max 4000.
- Workout type enum: `strength`, `circuit`, `mobility`, `cardio`, `run`, `other`.
- Duration 0-1440; RPE integer 1-10.
- Entries max 200. New `entry_id` values are optional and are generated as UUIDv7 values when omitted. Each has an optional `idempotency_key`, unique positive `sequence_no`, entry type `set`, `rest`, or `note`, and a type-appropriate bounded payload.
- Set: exercise key/name required; reps 0-1000; duration 0-86400; load 0-2000; RIR values 0-10; muscle groups maximum 10.
- Rest: duration 0-86400.
- Note: text max 4000.
- If linked activity is supplied, verify it belongs to the target athlete.

Perform one transaction. Upsert the session using existing last-write-wins logic. If the submitted session wins, replace the submitted entries and tombstone prior non-submitted entries using the same server clock. If it loses, make no entry changes. Do not partially apply entries.

Output:

```json
{
  "workout": {"workout_id": "...", "entries": []},
  "created": false,
  "submitted_version_won": true,
  "server_synced_at": 1787900000123
}
```

### 7.7 `delete_workout`

Input requires `workout_id` and `client_updated_at`. In one transaction, verify ownership, apply last-write-wins, set the session and all live entries' `deleted_at`, and return:

```json
{
  "workout_id": "...",
  "deleted": true,
  "submitted_version_won": true,
  "server_synced_at": 1787900000123
}
```

Deleting an already deleted workout is successful and idempotent. An older clock returns the current winner with `submitted_version_won=false`.

### 7.8 `get_checkins`

Input uses required `from`/`to` (maximum 366 days), order, limit, and cursor. Return live check-ins with:

- ID, recorded timestamp, local date, timezone
- energy, fatigue, motivation, sleep quality, and stress: integer 1-5 or null
- soreness/pain arrays of `{area, severity}`; severity 1-5
- illness `{present, symptoms}`
- notes

At most 20 soreness/pain entries and 20 symptoms per check-in.

### 7.9 `record_checkin`

Input:

```json
{
  "checkin_id": "018f...uuidv7...",
  "client_updated_at": 1787900000000,
  "recorded_at": "2026-08-28T07:30:00+02:00",
  "energy": 4,
  "fatigue": 2,
  "motivation": 4,
  "sleep_quality": 4,
  "stress": 2,
  "soreness": [{"area": "calves", "severity": 2}],
  "pain": [],
  "illness": {"present": false, "symptoms": []},
  "notes": null
}
```

- ID must be existing or UUIDv7.
- At least one subjective field must be non-null/non-empty.
- Scale values 1-5; area/symptom strings 1-128; notes max 4000.
- Convert `recorded_at` to UTC, derive local date using athlete timezone, and reject timestamps more than 24 hours in the future.
- Last-write-wins output includes `checkin`, `created`, `submitted_version_won`, and `server_synced_at`.

### 7.10 `get_athlete_profile`

No input besides optional target. Return:

```json
{
  "timezone": "Europe/Madrid",
  "units": "metric",
  "birth_year": 1990,
  "goals": [],
  "target_events": [],
  "weekly_availability": {},
  "preferences": {},
  "heart_rate_zones": {},
  "pace_zones": {},
  "power_zones": {},
  "constraints": {},
  "conditions_and_injuries": [],
  "updated_at": 1787900000123
}
```

Return an empty default profile rather than `not_found` when no row exists. Include `sensitive_fields_present`, but never put conditions/injuries into logs.

### 7.11 `update_athlete_profile`

This is full replacement, not JSON merge. Require `client_updated_at`, timezone, and units. Bounds:

- units: currently only `metric`
- birth year: 1900 through current year, optional
- goals: max 10; name max 256; optional target value/unit/date
- target events: max 20; name max 256; sport/category enum; date; optional distance
- weekly availability: seven known weekday keys, each max 5 bounded time windows
- preferences: run days 0-7, strength sessions 0-7, preferred weekdays, back-to-back-hard-days boolean
- zones: monotonically increasing, non-overlapping, maximum 10 zones per system
- constraints: max 20 typed booleans/numbers/strings
- conditions/injuries: max 20, label max 256, status enum `active`, `managed`, `resolved`, optional notes max 1000

Update `athletes.timezone` and `athlete_profiles` in one transaction. Output the stored winner and `submitted_version_won`.

### 7.12 `get_training_plan`

Input requires `from` and `to`, maximum 366 days, plus `include_completed` default true. Return the active live plan and live sessions in date/ID order. If no active plan exists, return `plan: null`, `sessions: []`.

Each session contains ID, plan ID, date, sport, type, priority, targets, notes, status, completed activity/workout IDs, and an optional compact completion summary.

### 7.13 `upsert_planned_session`

Input:

```json
{
  "plan_id": "018f...",
  "session_id": "018f...",
  "client_updated_at": 1787900000000,
  "date": "2026-08-29",
  "sport": "running",
  "session_type": "easy",
  "priority": "normal",
  "targets": {
    "duration_minutes": 45,
    "distance_km": null,
    "heart_rate_zone": "z2",
    "pace_min_per_km_min": null,
    "pace_min_per_km_max": null,
    "sets": null,
    "reps": null,
    "load_kg": null
  },
  "notes": null
}
```

- New `plan_id` and `session_id` values may be omitted; the server generates UUIDv7 values. Use `plan_idempotency_key` and `idempotency_key` as caller-owned secondary keys when a retry must address the same plan or session.
- If `plan_id` is new, create a minimal active plan titled `Training plan` with `{}` goal. If another active plan exists, require its ID rather than silently replacing it.
- Sport enum uses normalized activity categories.
- Session type 1-64 characters from a documented open enum; initially recognize `easy`, `long`, `recovery`, `tempo`, `threshold`, `intervals`, `race`, `strength`, `mobility`, `rest`, `other`.
- Priority: `optional`, `normal`, `key`.
- Targets are optional but at least one target or notes must be present. Numeric targets non-negative and bounded to plausible storage limits, not physiological claims.
- New sessions start `planned`. This action cannot mark completion.

Return session, created, submitted-version-won, and server clock.

### 7.14 `update_planned_session_status`

Input:

```json
{
  "session_id": "018f...",
  "client_updated_at": 1787900000000,
  "status": "completed",
  "completed_activity_id": "12345",
  "completed_workout_id": null
}
```

- Status enum: `planned`, `completed`, `skipped`, `cancelled`.
- `completed` requires an owned activity or workout link; allow both only when they are linked representations of the same session.
- Other statuses clear completion links.
- Verify ownership of every link.
- Apply last-write-wins and return the stored session winner.

### 7.15 `get_training_analysis`

Description: `Calculate bounded period, comparison, consistency, running, strength, load, and recovery aggregates from normalized data.`

Input:

```json
{
  "from": "2026-07-01",
  "to": "2026-08-28",
  "compare_with_previous_period": true,
  "group_by": "week",
  "categories": ["running", "strength"]
}
```

- Required inclusive range, maximum 366 days.
- `group_by`: `day`, `week`, `month`; default `week`.
- Categories unique, maximum 12; default all.
- Previous period is the equal-length inclusive period immediately before `from`.

Return:

- period and optional comparison period boundaries
- at most 366 daily, 54 weekly, or 13 monthly buckets
- activity count, active days, duration, distance, Garmin load by category
- running distance/duration/frequency, longest run, intensity distribution, load, and comparable-HR pace trend
- strength sessions, working sets, volume load, sets by muscle group, exercise trends
- recovery sleep/score means and HRV/RHR trends
- plan consistency counts and completion rate
- current vs comparison absolute and percentage changes
- data warnings and method metadata

Aggregation rules:

- ISO weeks start Monday in athlete timezone.
- A linked manual workout and Garmin activity count as one session. Prefer Garmin duration/load and manual exercises/RPE.
- Intensity distribution uses HR-zone minutes. If zones are absent, those minutes are `unclassified`; do not infer zones from average HR.
- Means ignore nulls and report sample count. A sum is null if every contributing value is null.
- Percentage change is null when comparison is null or zero.
- HRV/RHR trend compares the median of the last seven valid days in the period with the first seven valid days. Require at least four values in each window. Labels: change within 5% `stable`; otherwise `increasing` or `decreasing`. Return numeric percentage too; do not claim whether the direction is good or bad.
- Comparable-HR pace trend uses runs 20-120 minutes, 3-25 km, and with average HR. Partition into first and last thirds of the period. Pair groups by average HR within 5 bpm, compare median speed (not pace), require at least three runs in each group. Labels: speed change within 2% `stable`, above 2% `improving`, below -2% `declining`; otherwise `unavailable`. Return method and sample counts.
- Strength exercise trend groups sets by `exercise_key`. For each exercise, calculate best estimated 1RM per period only for 1-12 reps and positive external load using Epley: `load_kg * (1 + reps/30)`. Do not calculate bodyweight exercise 1RM. Return at most 20 exercises ranked by recent set count.
- Plan completion rate is completed divided by planned sessions whose date is on or before `to`; cancelled sessions are excluded from numerator and denominator. Report skipped separately.

### 7.16 Structured plan draft IDs and JSON objects

`upsert_training_plan_draft` follows the same create-ID rule: new `plan_id`,
session `session_id`, and step `step_id` values are optional and generated as
UUIDv7 values by the server. Each plan, session, and step may carry an
`idempotency_key`; these keys are stored separately from UUID primary IDs and
are unique for live records belonging to an athlete.

`goal` and `steps[].prescription` are JSON objects in both input and output.
The MCP output schema exposes them as objects (and permits null for
forward-compatible reads), never as byte arrays.

## 8. Service architecture

Keep MCP transport code small. Use this structure:

```text
internal/fitnessdata/       Garmin normalization
internal/fitnessrepo/       SQL queries and write transactions
internal/fitnessanalysis/   deterministic aggregation/classification
internal/mcpserver/         schemas, authorization, mapping, tool registration
```

Suggested interfaces:

```go
type Repository interface {
    DailyHealth(ctx context.Context, athleteID string, q DailyHealthQuery) (Page[DailyHealth], DataStatus, error)
    Activities(ctx context.Context, athleteID string, q ActivityQuery) (Page[ActivitySummary], DataStatus, error)
    Activity(ctx context.Context, athleteID, activityID string) (ActivityBundle, error)
    StrengthWorkouts(ctx context.Context, athleteID string, q StrengthQuery) (Page[Workout], error)
    UpsertWorkout(ctx context.Context, athleteID string, in WorkoutWrite) (WorkoutWinner, error)
    DeleteWorkout(ctx context.Context, athleteID string, in DeleteWrite) (WorkoutWinner, error)
    Checkins(ctx context.Context, athleteID string, q CheckinQuery) (Page[Checkin], error)
    UpsertCheckin(ctx context.Context, athleteID string, in CheckinWrite) (CheckinWinner, error)
    Profile(ctx context.Context, athleteID string) (Profile, error)
    UpsertProfile(ctx context.Context, athleteID string, in ProfileWrite) (ProfileWinner, error)
    Plan(ctx context.Context, athleteID string, from, to string, includeCompleted bool) (PlanResult, error)
    UpsertPlannedSession(ctx context.Context, athleteID string, in PlannedSessionWrite) (PlannedSessionWinner, error)
    UpdatePlannedSessionStatus(ctx context.Context, athleteID string, in PlannedStatusWrite) (PlannedSessionWinner, error)
}
```

`fitnessanalysis` consumes normalized repository results. It must not execute SQL or parse raw JSON. Inject a clock into MCP/service constructors so default dates and future-timestamp validation are deterministic in tests.

The existing `httpapi.API` may temporarily implement/delegate to the repository, but do not add all logic to `internal/mcpserver/server.go`.

## 9. Input/output schema implementation

Define named Go input and output structs for every action. Do not use `map[string]any` for outputs or domain values. Raw JSON is allowed only in persistence decoders.

Build strict MCP input schemas in separate files:

```text
internal/mcpserver/schema_common.go
internal/mcpserver/schema_health.go
internal/mcpserver/schema_activity.go
internal/mcpserver/schema_workout.go
internal/mcpserver/schema_profile.go
internal/mcpserver/schema_plan.go
internal/mcpserver/schema_analysis.go
```

The MCP SDK's reflected schema support may be used only if tests prove it emits:

- correct required properties
- bounds and enums
- `additionalProperties: false` for nested objects
- nullable optional values accepted by ChatGPT MCP clients

Otherwise construct schemas explicitly as the current implementation does. Add an output schema when supported by the pinned SDK; the Go output structs remain authoritative regardless.

Keep tool responses below approximately 200 KB. Enforce this structurally through result limits and metric point bounds rather than marshaling then truncating arbitrary JSON.

## 10. Safety and privacy behavior

- Recommendations are made by the MCP client, but response descriptions must say that the data is for fitness guidance, not diagnosis or emergency assessment.
- When a check-in reports chest pain, fainting, severe breathing difficulty, or another configured urgent symptom, emit `URGENT_SYMPTOM_REPORTED` with the user's recorded text omitted from the flag. Do not diagnose.
- Do not calculate readiness for an athlete other than the authorized target.
- Profile conditions/injuries are returned only from `get_athlete_profile` and `get_training_context`; omit them from generic activity responses.
- GPS and raw activity archives are never returned by this action set.
- Tool logging records action name, principal/target IDs, date range, result count, latency, and error code only. Do not log input/output bodies or health values.
- All writes require `fitness:write`; reads require `fitness:read`.

## 11. Tests

### 11.1 Decoder fixtures

Create small synthetic fixtures only. Never copy real health payloads.

For sleep, activity, recovery, training snapshot, and activity detail, test:

- raw camelCase form
- legacy snake_case form
- missing optional members
- explicit zeros preserved as zero
- malformed JSON rejected
- unknown activity type becomes `other` with source type retained
- coordinates removed recursively

### 11.2 Repository tests

Use migrated temporary SQLite databases and two athletes. Test:

- household authorization remains in MCP layer
- all reads are athlete-scoped
- ascending/descending pagination has no duplicates or gaps
- cursor/filter mismatch is rejected
- deleted rows are absent
- stale writes return existing winner without mutation
- UUIDv7 required for new records
- workout+entry upsert is atomic
- delete tombstones session and entries
- activity/manual linkage avoids double counting
- active plan uniqueness
- completion links cannot cross athletes
- null values remain null

### 11.3 Analysis table tests

Use table-driven tests for every threshold boundary:

- recovery at sleep score 59/60, HRV ratio 0.849/0.85, RHR delta 6/7, Body Battery 34/35
- recovery unknown/normal/strained/low and confidence levels
- load ratio 0.799/0.8 and 1.3/1.301
- hard-session rules individually
- consecutive training days with linked duplicate records
- median baselines with missing days
- daily/weekly/monthly bucket boundaries in Europe/Madrid, including DST
- HRV/RHR trend minimum sample count
- comparable-HR pace trend minimum samples and thresholds
- Epley calculation and bodyweight exclusion
- completion rate excludes future/cancelled sessions

### 11.4 MCP protocol tests

Expand `internal/mcpserver/server_test.go` to initialize the SDK client and assert:

- exactly the 15 new actions plus deprecated `update_workout` are discoverable
- annotations are correct
- every input schema rejects unknown properties
- bounds/enums are present
- omitted target resolves to self
- authorized household target works
- unauthorized/non-household target returns the same generic error
- read/write scope separation
- default timezone/date behavior with injected clock
- response item and byte bounds
- cancellation propagates into repository queries
- no output contains known synthetic coordinate keys or raw payloads

### 11.5 End-to-end scenarios

Build synthetic 35-day histories and verify:

1. Tomorrow-run context includes latest recovery, last 7/28-day run/load totals, latest hard sessions, tomorrow's plan, goals, and warnings.
2. Strength-today context includes latest strength date, muscle-group sets, high-effort sets, soreness/pain, and today's plan.
3. Recent-training analysis compares equal periods without double-counting linked sessions.
4. Recovery analysis distinguishes missing data from poor recovery.
5. A stale Garmin sync produces `STALE_SYNC` and reduces confidence.
6. An athlete with no profile/check-ins still gets useful objective context with explicit warnings.

## 12. Implementation sequence

Implement in reviewable stages; keep tests passing after each stage.

1. Add migration 00002, migration idempotency tests, injected clock, shared date/cursor helpers, and normalized domain types.
2. Add raw/legacy decoders and decoder fixtures.
3. Replace `get_daily_health` output with merged normalized days and pagination.
4. Replace `get_activities`; add `get_activity` and coordinate-stripped details.
5. Add strength repository reads, exercise catalog, `get_strength_workouts`, atomic `upsert_workout`, and `delete_workout`.
6. Add profile and check-in persistence/read/write actions.
7. Add plan persistence/read/write/status actions.
8. Add deterministic analysis package and `get_training_analysis`.
9. Compose existing primitives into `get_training_context`; add flags and completeness metadata.
10. Add full MCP protocol/end-to-end coverage, update README action documentation, and mark `update_workout` deprecated in its description.

For focused iteration run the relevant Go tests with `./scripts/test.sh --go`. Before handoff run:

```text
./scripts/test.sh
./scripts/build.sh
```

Do not add generated databases, real Garmin payloads, FIT files, tokens, health data, or build output to the repository.

## 13. Definition of done

The action set is complete when:

- All actions in section 3 are discoverable with strict schemas and correct annotations.
- The four motivating questions are covered by the end-to-end tests in section 11.5.
- Raw and legacy records produce the same normalized output.
- Recent-first queries cannot silently return old records because of missing pagination.
- Every classification exposes supporting values, method, sample counts, and confidence.
- Missing data is distinguishable from zero and from an adverse value.
- Manual and Garmin-linked sessions are not double-counted.
- Writes are athlete-scoped, UUIDv7-based, atomic, idempotent, and last-write-wins.
- GPS, raw Garmin payloads, archives, secrets, and cross-athlete data never appear in MCP results or logs.
- `./scripts/test.sh` and `./scripts/build.sh` pass.
