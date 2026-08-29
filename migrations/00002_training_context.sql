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
