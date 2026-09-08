package fitnessrepo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const maxIdempotencyKeyLength = 256

func validateIdempotencyKey(key string) error {
	if key != "" && (len(key) > maxIdempotencyKeyLength || strings.TrimSpace(key) == "") {
		return errors.New("invalid_input: idempotency_key must be 1-256 non-whitespace characters")
	}
	return nil
}

func lookupIdempotencyKey(ctx context.Context, db DBTX, table, athleteID, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	var id string
	query := "SELECT id FROM " + table + " WHERE athlete_id=? AND idempotency_key=? AND deleted_at IS NULL"
	if err := db.QueryRowContext(ctx, query, athleteID, key).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", errors.New("internal_error: idempotency lookup")
	}
	return id, nil
}

// PlanIDForIdempotencyKey returns the live plan associated with key, if any.
func (r *Repository) PlanIDForIdempotencyKey(ctx context.Context, athleteID, key string) (string, error) {
	if err := validateIdempotencyKey(key); err != nil {
		return "", err
	}
	return lookupIdempotencyKey(ctx, r.db, "training_plans", athleteID, key)
}

// PlannedSessionIDForIdempotencyKey returns the live session associated with key, if any.
func (r *Repository) PlannedSessionIDForIdempotencyKey(ctx context.Context, athleteID, key string) (string, error) {
	if err := validateIdempotencyKey(key); err != nil {
		return "", err
	}
	return lookupIdempotencyKey(ctx, r.db, "planned_sessions", athleteID, key)
}

// PlannedStepIDForIdempotencyKey returns the live step associated with key, if any.
func (r *Repository) PlannedStepIDForIdempotencyKey(ctx context.Context, athleteID, key string) (string, error) {
	if err := validateIdempotencyKey(key); err != nil {
		return "", err
	}
	return lookupIdempotencyKey(ctx, r.db, "planned_session_steps", athleteID, key)
}

// PlannedStepGroupIDForIdempotencyKey returns the live group associated with key, if any.
func (r *Repository) PlannedStepGroupIDForIdempotencyKey(ctx context.Context, athleteID, key string) (string, error) {
	if err := validateIdempotencyKey(key); err != nil {
		return "", err
	}
	return lookupIdempotencyKey(ctx, r.db, "planned_step_groups", athleteID, key)
}

// WorkoutIDForIdempotencyKey returns the live workout associated with key, if any.
func (r *Repository) WorkoutIDForIdempotencyKey(ctx context.Context, athleteID, key string) (string, error) {
	if err := validateIdempotencyKey(key); err != nil {
		return "", err
	}
	return lookupIdempotencyKey(ctx, r.db, "workout_sessions", athleteID, key)
}

// WorkoutEntryIDForIdempotencyKey returns the live entry associated with key, if any.
func (r *Repository) WorkoutEntryIDForIdempotencyKey(ctx context.Context, athleteID, key string) (string, error) {
	if err := validateIdempotencyKey(key); err != nil {
		return "", err
	}
	return lookupIdempotencyKey(ctx, r.db, "workout_entries", athleteID, key)
}

// CheckinIDForIdempotencyKey returns the live check-in associated with key, if any.
func (r *Repository) CheckinIDForIdempotencyKey(ctx context.Context, athleteID, key string) (string, error) {
	if err := validateIdempotencyKey(key); err != nil {
		return "", err
	}
	return lookupIdempotencyKey(ctx, r.db, "checkins", athleteID, key)
}
