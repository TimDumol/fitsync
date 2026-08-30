-- +goose Up
-- The sequence is deliberately global.  It is the pull cursor for v2 and is
-- advanced only for top-level aggregate changes.
CREATE TABLE sync_clock (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    value INTEGER NOT NULL CHECK (value >= 0)
);
INSERT INTO sync_clock (id, value) VALUES (1, 0);

ALTER TABLE training_plans ADD COLUMN description TEXT;
ALTER TABLE training_plans ADD COLUMN status TEXT NOT NULL DEFAULT 'draft';
ALTER TABLE training_plans ADD COLUMN start_date TEXT;
ALTER TABLE training_plans ADD COLUMN end_date TEXT;
ALTER TABLE training_plans ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE training_plans ADD COLUMN source_ref TEXT;
ALTER TABLE training_plans ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE training_plans ADD COLUMN sync_seq INTEGER NOT NULL DEFAULT 0;

UPDATE training_plans
SET status = CASE WHEN active = 1 AND deleted_at IS NULL THEN 'active' ELSE 'archived' END;
UPDATE training_plans SET sync_seq = rowid;

ALTER TABLE planned_sessions ADD COLUMN title TEXT NOT NULL DEFAULT 'Workout';
ALTER TABLE planned_sessions ADD COLUMN description TEXT;
ALTER TABLE planned_sessions ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;

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

ALTER TABLE workout_sessions ADD COLUMN planned_session_id TEXT REFERENCES planned_sessions(id);
ALTER TABLE workout_sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'completed';
ALTER TABLE workout_sessions ADD COLUMN started_at_utc_ms INTEGER;
ALTER TABLE workout_sessions ADD COLUMN completed_at_utc_ms INTEGER;
ALTER TABLE workout_sessions ADD COLUMN sync_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workout_entries ADD COLUMN planned_step_id TEXT REFERENCES planned_session_steps(id);

UPDATE workout_sessions SET sync_seq =
    (SELECT COALESCE(MAX(sync_seq), 0) FROM training_plans) + rowid;

CREATE INDEX training_plans_sync_idx
    ON training_plans (athlete_id, sync_seq, id);
CREATE INDEX workout_sessions_sync_idx
    ON workout_sessions (athlete_id, sync_seq, id);
CREATE INDEX workout_sessions_planned_idx
    ON workout_sessions (athlete_id, planned_session_id);

UPDATE sync_clock SET value = MAX(
    (SELECT COALESCE(MAX(sync_seq), 0) FROM training_plans),
    (SELECT COALESCE(MAX(sync_seq), 0) FROM workout_sessions)
) WHERE id = 1;

-- +goose Down
DROP INDEX workout_sessions_planned_idx;
DROP INDEX workout_sessions_sync_idx;
DROP INDEX training_plans_sync_idx;
DROP TABLE planned_session_steps;
ALTER TABLE workout_entries DROP COLUMN planned_step_id;
ALTER TABLE workout_sessions DROP COLUMN sync_seq;
ALTER TABLE workout_sessions DROP COLUMN completed_at_utc_ms;
ALTER TABLE workout_sessions DROP COLUMN started_at_utc_ms;
ALTER TABLE workout_sessions DROP COLUMN status;
ALTER TABLE workout_sessions DROP COLUMN planned_session_id;
ALTER TABLE planned_sessions DROP COLUMN created_at;
ALTER TABLE planned_sessions DROP COLUMN description;
ALTER TABLE planned_sessions DROP COLUMN title;
ALTER TABLE training_plans DROP COLUMN sync_seq;
ALTER TABLE training_plans DROP COLUMN created_at;
ALTER TABLE training_plans DROP COLUMN source_ref;
ALTER TABLE training_plans DROP COLUMN source;
ALTER TABLE training_plans DROP COLUMN end_date;
ALTER TABLE training_plans DROP COLUMN start_date;
ALTER TABLE training_plans DROP COLUMN status;
ALTER TABLE training_plans DROP COLUMN description;
DROP TABLE sync_clock;
