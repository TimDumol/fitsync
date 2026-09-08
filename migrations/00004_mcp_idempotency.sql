-- +goose Up
-- MCP creates UUIDv7 record IDs on the server. These caller-supplied keys are
-- the stable secondary identity clients can use when retrying a request.
ALTER TABLE training_plans ADD COLUMN idempotency_key TEXT;
ALTER TABLE planned_sessions ADD COLUMN idempotency_key TEXT;
ALTER TABLE planned_session_steps ADD COLUMN idempotency_key TEXT;
ALTER TABLE workout_sessions ADD COLUMN idempotency_key TEXT;
ALTER TABLE workout_entries ADD COLUMN idempotency_key TEXT;
ALTER TABLE checkins ADD COLUMN idempotency_key TEXT;

CREATE UNIQUE INDEX training_plans_idempotency_idx
    ON training_plans (athlete_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX planned_sessions_idempotency_idx
    ON planned_sessions (athlete_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX planned_steps_idempotency_idx
    ON planned_session_steps (athlete_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX workout_sessions_idempotency_idx
    ON workout_sessions (athlete_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX workout_entries_idempotency_idx
    ON workout_entries (athlete_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX checkins_idempotency_idx
    ON checkins (athlete_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
DROP INDEX checkins_idempotency_idx;
DROP INDEX workout_entries_idempotency_idx;
DROP INDEX workout_sessions_idempotency_idx;
DROP INDEX planned_steps_idempotency_idx;
DROP INDEX planned_sessions_idempotency_idx;
DROP INDEX training_plans_idempotency_idx;
ALTER TABLE checkins DROP COLUMN idempotency_key;
ALTER TABLE workout_entries DROP COLUMN idempotency_key;
ALTER TABLE workout_sessions DROP COLUMN idempotency_key;
ALTER TABLE planned_session_steps DROP COLUMN idempotency_key;
ALTER TABLE planned_sessions DROP COLUMN idempotency_key;
ALTER TABLE training_plans DROP COLUMN idempotency_key;
