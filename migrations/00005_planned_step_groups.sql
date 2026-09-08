-- +goose Up
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

-- +goose Down
DROP INDEX planned_step_groups_athlete_idempotency_idx;
DROP INDEX planned_step_groups_session_idx;
DROP TABLE planned_step_groups;
