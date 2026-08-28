"""Upload curated Garmin exports to a Neon Postgres project."""

from __future__ import annotations

import hashlib
import json
import os
from dataclasses import dataclass
from datetime import date
from pathlib import Path
from typing import Any, Iterable
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


NEON_API_BASE = "https://console.neon.tech/api/v2"


class NeonSyncError(RuntimeError):
    """Raised for a safe-to-display Neon configuration or sync failure."""


@dataclass(frozen=True)
class NeonTarget:
    connection_uri: str
    project_id: str | None
    project_name: str | None
    database_name: str | None
    role_name: str | None


@dataclass(frozen=True)
class ExportRecord:
    dataset: str
    record_key: str
    record_date: date | None
    payload: dict[str, Any]
    payload_sha256: str


def _api_get(api_key: str, path: str, params: dict[str, Any] | None = None) -> Any:
    url = f"{NEON_API_BASE}{path}"
    if params:
        url += "?" + urlencode(params)
    request = Request(
        url,
        headers={"Authorization": f"Bearer {api_key}", "Accept": "application/json"},
    )
    try:
        # The host is fixed by NEON_API_BASE; only the path and query vary.
        # foxguard: ignore[py/no-ssrf]
        with urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except HTTPError as exc:
        try:
            detail = json.loads(exc.read().decode("utf-8")).get("message")
        except (json.JSONDecodeError, UnicodeDecodeError, AttributeError):
            detail = None
        suffix = f": {detail}" if detail else ""
        raise NeonSyncError(f"Neon API returned HTTP {exc.code}{suffix}") from exc
    except URLError as exc:
        raise NeonSyncError(f"Could not reach the Neon API: {exc.reason}") from exc
    except json.JSONDecodeError as exc:
        raise NeonSyncError("Neon API returned invalid JSON") from exc


def _choose_named(
    values: list[dict[str, Any]],
    requested: str | None,
    *,
    kind: str,
    preferred: str | None = None,
) -> dict[str, Any]:
    if requested:
        for value in values:
            if requested in {str(value.get("id") or ""), str(value.get("name") or "")}:
                return value
        raise NeonSyncError(f"Configured Neon {kind} was not found: {requested}")
    if preferred:
        for value in values:
            if value.get("name") == preferred:
                return value
    if len(values) == 1:
        return values[0]
    choices = ", ".join(
        str(value.get("name") or value.get("id") or "unnamed") for value in values
    )
    raise NeonSyncError(
        f"Multiple Neon {kind}s are available ({choices}); configure the intended one"
    )


def resolve_neon_target(environ: dict[str, str] | None = None) -> NeonTarget:
    env = environ if environ is not None else os.environ
    direct_uri = env.get("NEON_DATABASE_URL")
    if direct_uri:
        return NeonTarget(direct_uri, None, None, None, None)

    api_key = env.get("NEON_API_KEY")
    if not api_key:
        raise NeonSyncError(
            "Set NEON_API_KEY, or set NEON_DATABASE_URL to a scoped Postgres connection string"
        )

    configured_project_id = env.get("NEON_PROJECT_ID")
    if configured_project_id:
        # Project-scoped API keys cannot list every project in an account. When
        # the caller already supplied the project ID, stay entirely within that
        # project's API scope instead of attempting account-wide discovery.
        project_id = configured_project_id
        project = {
            "id": project_id,
            "name": env.get("NEON_PROJECT_NAME") or project_id,
        }
    else:
        projects_payload = _api_get(api_key, "/projects", {"limit": 400})
        projects = (
            projects_payload.get("projects")
            if isinstance(projects_payload, dict)
            else None
        )
        if not isinstance(projects, list) or not projects:
            raise NeonSyncError("The Neon API key cannot see any projects")
        project = _choose_named(
            projects,
            env.get("NEON_PROJECT_NAME"),
            kind="projects",
        )
        project_id = str(project.get("id") or "")
        if not project_id:
            raise NeonSyncError("Selected Neon project has no project ID")

    branches_payload = _api_get(api_key, f"/projects/{project_id}/branches")
    branches = branches_payload.get("branches") if isinstance(branches_payload, dict) else None
    if not isinstance(branches, list) or not branches:
        raise NeonSyncError("Selected Neon project has no branches")
    branch = next(
        (item for item in branches if item.get("primary") is True),
        branches[0],
    )
    branch_id = str(branch.get("id") or "")

    databases_payload = _api_get(
        api_key, f"/projects/{project_id}/branches/{branch_id}/databases"
    )
    databases = (
        databases_payload.get("databases") if isinstance(databases_payload, dict) else None
    )
    if not isinstance(databases, list) or not databases:
        raise NeonSyncError("Selected Neon branch has no databases")
    database = _choose_named(
        databases,
        env.get("NEON_DATABASE_NAME"),
        kind="databases",
        preferred="neondb",
    )
    database_name = str(database.get("name") or "")
    role_name = env.get("NEON_ROLE_NAME") or database.get("owner_name")
    if not role_name:
        raise NeonSyncError("Set NEON_ROLE_NAME because the database owner could not be discovered")

    uri_payload = _api_get(
        api_key,
        f"/projects/{project_id}/connection_uri",
        {
            "branch_id": branch_id,
            "database_name": database_name,
            "role_name": role_name,
            "pooled": "true",
        },
    )
    connection_uri = uri_payload.get("uri") if isinstance(uri_payload, dict) else None
    if not isinstance(connection_uri, str) or not connection_uri.startswith("postgres"):
        raise NeonSyncError("Neon did not return a PostgreSQL connection URI")
    return NeonTarget(
        connection_uri=connection_uri,
        project_id=project_id,
        project_name=str(project.get("name") or project_id),
        database_name=database_name,
        role_name=str(role_name),
    )


def _record_date(payload: dict[str, Any]) -> date | None:
    for key in ("date", "captured_date", "start_time_local", "calendarDate"):
        value = payload.get(key)
        if not isinstance(value, str) or len(value) < 10:
            continue
        try:
            return date.fromisoformat(value[:10])
        except ValueError:
            continue
    return None


def _make_record(dataset: str, key: Any, payload: dict[str, Any]) -> ExportRecord:
    canonical = json.dumps(payload, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    return ExportRecord(
        dataset=dataset,
        record_key=str(key),
        record_date=_record_date(payload),
        payload=payload,
        payload_sha256=hashlib.sha256(canonical.encode("utf-8")).hexdigest(),
    )


def _records_from_file(path: Path, dataset: str, key_name: str) -> Iterable[ExportRecord]:
    if not path.exists():
        return []
    payload = json.loads(path.read_text(encoding="utf-8-sig"))
    values = payload if isinstance(payload, list) else [payload]
    records: list[ExportRecord] = []
    for index, value in enumerate(values):
        if not isinstance(value, dict):
            continue
        key = value.get(key_name) or value.get("date") or value.get("captured_date") or index
        records.append(_make_record(dataset, key, value))
    return records


def collect_export_records(data_dir: Path) -> list[ExportRecord]:
    specs = (
        ("sleep.json", "sleep", "date"),
        ("activities.json", "activities", "activity_id"),
        ("daily_recovery.json", "daily_recovery", "date"),
        ("body_battery_events.json", "body_battery_events", "date"),
        ("training_snapshot.json", "training_snapshot", "captured_date"),
        ("body_measurements.json", "body_measurements", "captured_date"),
    )
    records = [
        record
        for filename, dataset, key_name in specs
        for record in _records_from_file(data_dir / filename, dataset, key_name)
    ]
    details_dir = data_dir / "activity_details"
    if details_dir.exists():
        for path in sorted(details_dir.glob("*.json")):
            payload = json.loads(path.read_text(encoding="utf-8-sig"))
            if isinstance(payload, dict):
                records.append(
                    _make_record(
                        "activity_details", payload.get("activity_id") or path.stem, payload
                    )
                )
    return records


SCHEMA_SQL = """
CREATE SCHEMA IF NOT EXISTS fitness;
CREATE SCHEMA IF NOT EXISTS fitness_agent;

CREATE TABLE IF NOT EXISTS fitness.records (
    dataset text NOT NULL,
    record_key text NOT NULL,
    record_date date,
    payload jsonb NOT NULL,
    payload_sha256 text NOT NULL,
    synced_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (dataset, record_key)
);

CREATE INDEX IF NOT EXISTS records_dataset_date_idx
    ON fitness.records (dataset, record_date DESC);

CREATE TABLE IF NOT EXISTS fitness.workout_sessions (
    workout_id text PRIMARY KEY,
    workout_date date NOT NULL,
    started_at timestamptz,
    time_is_approximate boolean NOT NULL DEFAULT false,
    title text NOT NULL,
    workout_type text NOT NULL,
    source text NOT NULL,
    source_ref text UNIQUE,
    garmin_activity_id bigint,
    default_rest_seconds_min integer,
    default_rest_seconds_max integer,
    notes text,
    raw_log text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (default_rest_seconds_min IS NULL OR default_rest_seconds_min >= 0),
    CHECK (default_rest_seconds_max IS NULL OR default_rest_seconds_max >= 0),
    CHECK (
        default_rest_seconds_min IS NULL
        OR default_rest_seconds_max IS NULL
        OR default_rest_seconds_min <= default_rest_seconds_max
    )
);

CREATE INDEX IF NOT EXISTS workout_sessions_date_idx
    ON fitness.workout_sessions (workout_date DESC);

CREATE INDEX IF NOT EXISTS workout_sessions_garmin_activity_idx
    ON fitness.workout_sessions (garmin_activity_id)
    WHERE garmin_activity_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS fitness.workout_entries (
    entry_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workout_id text NOT NULL REFERENCES fitness.workout_sessions (workout_id)
        ON DELETE CASCADE,
    sequence_no integer NOT NULL,
    entry_type text NOT NULL CHECK (entry_type IN ('exercise_set', 'rest', 'note')),
    exercise_key text,
    exercise_name text,
    user_label text,
    set_number integer,
    reps integer,
    duration_seconds integer,
    load_kg numeric(8, 3),
    load_description text,
    is_bodyweight boolean,
    to_failure boolean,
    rir_min integer,
    rir_max integer,
    laterality text,
    notes text,
    UNIQUE (workout_id, sequence_no),
    CHECK (sequence_no > 0),
    CHECK (set_number IS NULL OR set_number > 0),
    CHECK (reps IS NULL OR reps >= 0),
    CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
    CHECK (load_kg IS NULL OR load_kg >= 0),
    CHECK (rir_min IS NULL OR rir_min >= 0),
    CHECK (rir_max IS NULL OR rir_max >= 0),
    CHECK (rir_min IS NULL OR rir_max IS NULL OR rir_min <= rir_max),
    CHECK (entry_type <> 'exercise_set' OR exercise_key IS NOT NULL),
    CHECK (entry_type <> 'rest' OR duration_seconds IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS workout_entries_exercise_idx
    ON fitness.workout_entries (exercise_key, workout_id);

CREATE OR REPLACE VIEW fitness_agent.sleep_daily AS
SELECT
    record_date AS date,
    NULLIF(payload->>'total_sleep_hours', '')::double precision AS main_sleep_hours,
    NULLIF(payload->>'nap_hours', '')::double precision AS nap_hours,
    NULLIF(payload->>'total_sleep_including_naps_hours', '')::double precision
        AS total_sleep_including_naps_hours,
    NULLIF(payload->>'sleep_score', '')::double precision AS sleep_score,
    NULLIF(payload->>'overnight_hrv', '')::double precision AS overnight_hrv,
    NULLIF(payload->>'resting_heart_rate', '')::double precision AS resting_heart_rate,
    payload
FROM fitness.records
WHERE dataset = 'sleep';

CREATE OR REPLACE VIEW fitness_agent.activities AS
SELECT
    record_key::bigint AS activity_id,
    record_date AS date,
    payload->>'activity_name' AS activity_name,
    payload->>'activity_type' AS activity_type,
    NULLIF(payload->>'distance_km', '')::double precision AS distance_km,
    NULLIF(payload->>'duration_minutes', '')::double precision AS duration_minutes,
    NULLIF(payload->>'average_pace_min_per_km', '')::double precision AS average_pace_min_per_km,
    NULLIF(payload->>'average_heart_rate', '')::double precision AS average_heart_rate,
    NULLIF(payload->>'activity_training_load', '')::double precision AS activity_training_load,
    payload
FROM fitness.records
WHERE dataset = 'activities'
  AND COALESCE(payload->>'source', '') <> 'manual_chat_log';

CREATE OR REPLACE VIEW fitness_agent.workout_sessions AS
SELECT
    workout.workout_id,
    workout.workout_date,
    workout.started_at,
    workout.time_is_approximate,
    workout.title,
    workout.workout_type,
    workout.source,
    workout.source_ref,
    workout.garmin_activity_id,
    workout.default_rest_seconds_min,
    workout.default_rest_seconds_max,
    workout.notes,
    workout.raw_log,
    activity.duration_minutes AS garmin_duration_minutes,
    activity.average_heart_rate AS garmin_average_heart_rate,
    activity.activity_training_load AS garmin_training_load,
    workout.created_at,
    workout.updated_at
FROM fitness.workout_sessions AS workout
LEFT JOIN fitness_agent.activities AS activity
    ON activity.activity_id = workout.garmin_activity_id;

CREATE OR REPLACE VIEW fitness_agent.workout_entries AS
SELECT
    entry.entry_id,
    entry.workout_id,
    session.workout_date,
    session.started_at,
    entry.sequence_no,
    entry.entry_type,
    entry.exercise_key,
    entry.exercise_name,
    entry.user_label,
    entry.set_number,
    entry.reps,
    entry.duration_seconds,
    entry.load_kg,
    entry.load_description,
    entry.is_bodyweight,
    entry.to_failure,
    entry.rir_min,
    entry.rir_max,
    entry.laterality,
    entry.notes
FROM fitness.workout_entries AS entry
JOIN fitness.workout_sessions AS session USING (workout_id);

CREATE OR REPLACE VIEW fitness_agent.daily_recovery AS
SELECT
    record_date AS date,
    NULLIF(payload->>'steps', '')::integer AS steps,
    NULLIF(payload->>'resting_heart_rate', '')::double precision AS resting_heart_rate,
    NULLIF(payload->>'average_stress', '')::double precision AS average_stress,
    NULLIF(payload->>'body_battery_at_wake', '')::double precision AS body_battery_at_wake,
    NULLIF(payload->>'moderate_intensity_minutes', '')::double precision AS moderate_intensity_minutes,
    NULLIF(payload->>'vigorous_intensity_minutes', '')::double precision AS vigorous_intensity_minutes,
    payload
FROM fitness.records
WHERE dataset = 'daily_recovery';

CREATE OR REPLACE VIEW fitness_agent.body_battery_events AS
SELECT record_date AS date, payload
FROM fitness.records
WHERE dataset = 'body_battery_events';

CREATE OR REPLACE VIEW fitness_agent.activity_details AS
SELECT record_key::bigint AS activity_id, record_date AS date, payload
FROM fitness.records
WHERE dataset = 'activity_details';

CREATE OR REPLACE VIEW fitness_agent.training_snapshot AS
SELECT record_date AS captured_date, payload
FROM fitness.records
WHERE dataset = 'training_snapshot';

CREATE OR REPLACE VIEW fitness_agent.body_measurements AS
SELECT record_date AS captured_date, payload
FROM fitness.records
WHERE dataset = 'body_measurements';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fitness_agent_reader') THEN
        CREATE ROLE fitness_agent_reader NOLOGIN;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA fitness_agent TO fitness_agent_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA fitness_agent TO fitness_agent_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA fitness_agent
    GRANT SELECT ON TABLES TO fitness_agent_reader;
"""


UPSERT_SQL = """
INSERT INTO fitness.records
    (dataset, record_key, record_date, payload, payload_sha256, synced_at)
VALUES (%s, %s, %s, %s, %s, now())
ON CONFLICT (dataset, record_key) DO UPDATE SET
    record_date = EXCLUDED.record_date,
    payload = EXCLUDED.payload,
    payload_sha256 = EXCLUDED.payload_sha256,
    synced_at = now()
WHERE fitness.records.payload_sha256 IS DISTINCT FROM EXCLUDED.payload_sha256
"""


def upload_records(target: NeonTarget, records: list[ExportRecord]) -> dict[str, Any]:
    try:
        import psycopg
        from psycopg.types.json import Jsonb
    except ImportError as exc:
        raise NeonSyncError(
            "Neon upload requires psycopg; run pip install -r requirements.txt"
        ) from exc

    counts: dict[str, int] = {}
    try:
        with psycopg.connect(target.connection_uri, connect_timeout=30) as connection:
            with connection.cursor() as cursor:
                cursor.execute(SCHEMA_SQL)
                for record in records:
                    cursor.execute(
                        UPSERT_SQL,
                        (
                            record.dataset,
                            record.record_key,
                            record.record_date,
                            Jsonb(record.payload),
                            record.payload_sha256,
                        ),
                    )
                    counts[record.dataset] = counts.get(record.dataset, 0) + 1
    except psycopg.Error as exc:
        message = getattr(exc, "diag", None)
        primary = getattr(message, "message_primary", None) if message else None
        raise NeonSyncError(f"Neon PostgreSQL sync failed: {primary or exc.__class__.__name__}") from exc

    return {
        "project_id": target.project_id,
        "project_name": target.project_name,
        "database_name": target.database_name,
        "role_name": target.role_name,
        "records_seen": len(records),
        "datasets": counts,
    }


def sync_garmin_directory_to_neon(
    data_dir: Path, environ: dict[str, str] | None = None
) -> dict[str, Any]:
    records = collect_export_records(data_dir)
    if not records:
        raise NeonSyncError(f"No curated Garmin JSON exports found in {data_dir}")
    target = resolve_neon_target(environ)
    return upload_records(target, records)
