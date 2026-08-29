-- +goose Up
CREATE TABLE households (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE athletes (
    id TEXT PRIMARY KEY,
    handle TEXT NOT NULL UNIQUE,
    household_id TEXT NOT NULL REFERENCES households(id),
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
    created_at INTEGER NOT NULL
);

CREATE TABLE athlete_identities (
    id TEXT PRIMARY KEY,
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    signed_username TEXT,
    approved_at INTEGER NOT NULL,
    disabled_at INTEGER,
    created_at INTEGER NOT NULL,
    UNIQUE (issuer, subject)
);

CREATE TABLE pending_identities (
    id TEXT PRIMARY KEY,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    signed_username TEXT,
    first_seen_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    UNIQUE (issuer, subject)
);

CREATE TABLE identity_audit_log (
    id TEXT PRIMARY KEY,
    athlete_id TEXT REFERENCES athletes(id),
    identity_id TEXT REFERENCES athlete_identities(id),
    event TEXT NOT NULL CHECK (event IN ('approve', 'disable', 'rebind')),
    actor_issuer TEXT,
    actor_subject TEXT,
    occurred_at INTEGER NOT NULL
);

CREATE TABLE garmin_records (
    id TEXT PRIMARY KEY,
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    dataset TEXT NOT NULL,
    record_key TEXT NOT NULL,
    record_date TEXT NOT NULL CHECK (length(record_date) = 10 AND substr(record_date, 5, 1) = '-' AND substr(record_date, 8, 1) = '-'),
    occurred_at_utc_ms INTEGER,
    tz_offset_seconds INTEGER CHECK (tz_offset_seconds IS NULL OR (tz_offset_seconds BETWEEN -64800 AND 64800)),
    payload_format TEXT NOT NULL,
    payload_raw TEXT NOT NULL CHECK (json_valid(payload_raw)),
    payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64),
    ingested_at INTEGER NOT NULL,
    activity_id TEXT GENERATED ALWAYS AS (json_extract(payload_raw, '$.activityId')) VIRTUAL,
    sleep_score REAL GENERATED ALWAYS AS (COALESCE(
        json_extract(payload_raw, '$.sleep.dailySleepDTO.sleepScores.overall.value'),
        json_extract(payload_raw, '$.sleep.dailySleepDTO.sleepScore'),
        json_extract(payload_raw, '$.sleep_score')
    )) VIRTUAL,
    UNIQUE (athlete_id, dataset, record_key)
);

CREATE INDEX garmin_records_sleep_score_idx ON garmin_records (athlete_id, record_date, sleep_score);
CREATE INDEX garmin_records_activity_idx ON garmin_records (athlete_id, record_date, activity_id);

CREATE TABLE garmin_archives (
    id TEXT PRIMARY KEY,
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    activity_record_id TEXT REFERENCES garmin_records(id),
    storage_path TEXT GENERATED ALWAYS AS ('archives/' || id || '.bin') STORED,
    original_filename TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    content_sha256 TEXT NOT NULL CHECK (length(content_sha256) = 64),
    content_type TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE workout_sessions (
    id TEXT PRIMARY KEY,
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    workout_date TEXT NOT NULL,
    title TEXT NOT NULL,
    source TEXT NOT NULL,
    source_ref TEXT,
    metadata_raw TEXT NOT NULL CHECK (json_valid(metadata_raw)),
    client_updated_at INTEGER NOT NULL,
    server_synced_at INTEGER NOT NULL,
    deleted_at INTEGER,
    created_at INTEGER NOT NULL,
    UNIQUE (id, athlete_id)
);

CREATE TABLE workout_entries (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    sequence_no INTEGER NOT NULL CHECK (sequence_no > 0),
    entry_type TEXT NOT NULL,
    payload_raw TEXT NOT NULL CHECK (json_valid(payload_raw)),
    client_updated_at INTEGER NOT NULL,
    server_synced_at INTEGER NOT NULL,
    deleted_at INTEGER,
    FOREIGN KEY (session_id, athlete_id) REFERENCES workout_sessions(id, athlete_id),
    UNIQUE (session_id, sequence_no)
);

CREATE TABLE app_documents (
    id TEXT PRIMARY KEY,
    athlete_id TEXT NOT NULL REFERENCES athletes(id),
    document_key TEXT NOT NULL,
    content_raw TEXT NOT NULL CHECK (json_valid(content_raw)),
    client_updated_at INTEGER NOT NULL,
    server_synced_at INTEGER NOT NULL,
    deleted_at INTEGER,
    UNIQUE (athlete_id, document_key)
);

INSERT INTO households (id, name, created_at)
VALUES ('00000000-0000-0000-0000-000000000001', 'Household', 0);

INSERT INTO athletes (id, handle, household_id, active, created_at)
VALUES
    ('00000000-0000-0000-0000-000000000002', 'tim', '00000000-0000-0000-0000-000000000001', 0, 0),
    ('00000000-0000-0000-0000-000000000003', 'jopi', '00000000-0000-0000-0000-000000000001', 0, 0);

-- +goose Down
DROP TABLE app_documents;
DROP TABLE workout_entries;
DROP TABLE workout_sessions;
DROP TABLE garmin_archives;
DROP TABLE garmin_records;
DROP TABLE identity_audit_log;
DROP TABLE pending_identities;
DROP TABLE athlete_identities;
DROP TABLE athletes;
DROP TABLE households;
