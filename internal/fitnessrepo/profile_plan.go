package fitnessrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

func (r *Repository) Profile(ctx context.Context, athleteID string) (Profile, error) {
	var timezone string
	if err := r.db.QueryRowContext(ctx, "SELECT timezone FROM athletes WHERE id=?", athleteID).Scan(&timezone); err != nil {
		return Profile{}, errors.New("not_found: athlete")
	}
	profile := defaultProfile(timezone)
	var raw string
	var updated int64
	err := r.db.QueryRowContext(ctx, "SELECT content_raw,client_updated_at FROM athlete_profiles WHERE athlete_id=?", athleteID).Scan(&raw, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return profile, nil
	}
	if err != nil {
		return Profile{}, errors.New("internal_error: profile query")
	}
	if json.Unmarshal([]byte(raw), &profile) != nil {
		return Profile{}, errors.New("internal_error: profile decode")
	}
	profile.Timezone = timezone
	profile.UpdatedAt = updated
	profile.SensitiveFieldsPresent = len(profile.ConditionsAndInjuries) > 0
	return profile, nil
}

func defaultProfile(timezone string) Profile {
	return Profile{Timezone: timezone, Units: "metric", WeeklyAvailability: map[string][]AvailabilityWindow{}, Preferences: Preferences{}, Constraints: map[string]json.RawMessage{}, ConditionsAndInjuries: []Condition{}, SensitiveFieldsPresent: false}
}

func (r *Repository) UpsertProfile(ctx context.Context, athleteID string, in ProfileWrite) (ProfileWinner, error) {
	if _, err := time.LoadLocation(in.Profile.Timezone); err != nil {
		return ProfileWinner{}, errors.New("invalid_input: timezone must be a valid IANA timezone")
	}
	if in.Profile.Units != "metric" {
		return ProfileWinner{}, errors.New("invalid_input: units must be metric")
	}
	old := int64(-1)
	var rawOld string
	err := r.db.QueryRowContext(ctx, "SELECT client_updated_at,content_raw FROM athlete_profiles WHERE athlete_id=?", athleteID).Scan(&old, &rawOld)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ProfileWinner{}, errors.New("internal_error: profile lookup")
	}
	if exists && in.ClientUpdatedAt <= old {
		stored, e := r.Profile(ctx, athleteID)
		return ProfileWinner{Profile: stored, SubmittedVersionWon: false}, e
	}
	raw, _ := json.Marshal(in.Profile)
	clock := r.now()
	tx, err := r.begin(ctx)
	if err != nil {
		return ProfileWinner{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE athletes SET timezone=? WHERE id=?", in.Profile.Timezone, athleteID); err == nil {
		if exists {
			_, err = tx.ExecContext(ctx, "UPDATE athlete_profiles SET content_raw=?,client_updated_at=?,server_synced_at=? WHERE athlete_id=?", string(raw), in.ClientUpdatedAt, clock, athleteID)
		} else {
			_, err = tx.ExecContext(ctx, "INSERT INTO athlete_profiles(athlete_id,content_raw,client_updated_at,server_synced_at) VALUES(?,?,?,?)", athleteID, string(raw), in.ClientUpdatedAt, clock)
		}
	}
	if err != nil {
		_ = tx.Rollback()
		return ProfileWinner{}, normalizeWriteError(err)
	}
	if err := tx.Commit(); err != nil {
		return ProfileWinner{}, errors.New("internal_error: transaction commit")
	}
	stored, e := r.Profile(ctx, athleteID)
	return ProfileWinner{Profile: stored, SubmittedVersionWon: true}, e
}

func (r *Repository) Plan(ctx context.Context, athleteID, from, to string, includeCompleted bool) (PlanResult, error) {
	if err := validRange(from, to, 366); err != nil {
		return PlanResult{}, err
	}
	result, err := r.ActivePlan(ctx, athleteID, true)
	if err != nil || result.Plan == nil {
		return result, err
	}
	filtered := make([]PlannedSession, 0, len(result.Sessions))
	for _, session := range result.Sessions {
		if session.Date < from || session.Date > to || (!includeCompleted && session.Status == "completed") {
			continue
		}
		filtered = append(filtered, session)
	}
	result.Sessions = filtered
	result.Plan.Sessions = filtered
	return result, nil
}

func (r *Repository) UpsertPlannedSession(ctx context.Context, athleteID string, in PlannedSessionWrite) (PlannedSessionWinner, error) {
	if err := validateIdempotencyKey(in.IdempotencyKey); err != nil {
		return PlannedSessionWinner{}, err
	}
	if err := validateIdempotencyKey(in.PlanIdempotencyKey); err != nil {
		return PlannedSessionWinner{}, err
	}
	var owner string
	var old int64
	err := r.db.QueryRowContext(ctx, "SELECT athlete_id,client_updated_at FROM planned_sessions WHERE id=?", in.ID).Scan(&owner, &old)
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !created {
		return PlannedSessionWinner{}, errors.New("internal_error: planned session lookup")
	}
	if created {
		if err := requireUUIDv7(in.ID); err != nil {
			return PlannedSessionWinner{}, err
		}
	}
	if !created && owner != athleteID {
		return PlannedSessionWinner{}, errors.New("not_found: planned session")
	}
	planID := in.PlanID
	if created {
		var planOwner string
		err = r.db.QueryRowContext(ctx, "SELECT athlete_id FROM training_plans WHERE id=? AND deleted_at IS NULL", planID).Scan(&planOwner)
		if errors.Is(err, sql.ErrNoRows) {
			if e := requireUUIDv7(planID); e != nil {
				return PlannedSessionWinner{}, e
			}
			clock := r.now()
			if _, e := r.db.ExecContext(ctx, "INSERT INTO training_plans(id,athlete_id,title,goal_raw,status,source,active,client_updated_at,server_synced_at,created_at,idempotency_key) VALUES(?,?,?, '{}','active','manual',1,?,?,?,?)", planID, athleteID, "Training plan", in.ClientUpdatedAt, clock, clock, nullableStringValue(optionalString(in.PlanIdempotencyKey))); e != nil {
				return PlannedSessionWinner{}, normalizeWriteError(e)
			}
		} else if err != nil {
			return PlannedSessionWinner{}, errors.New("internal_error: plan lookup")
		} else if planOwner != athleteID {
			return PlannedSessionWinner{}, errors.New("not_found: plan")
		}
	}
	if !created && in.ClientUpdatedAt <= old {
		stored, e := r.readPlanned(ctx, athleteID, in.ID)
		return PlannedSessionWinner{Session: stored, SubmittedVersionWon: false}, e
	}
	raw, _ := json.Marshal(in.Targets)
	clock := r.now()
	if created {
		_, err = r.db.ExecContext(ctx, `INSERT INTO planned_sessions(id,plan_id,athlete_id,session_date,sport,session_type,priority,targets_raw,notes,status,client_updated_at,server_synced_at,idempotency_key) VALUES(?,?,?,?,?,?,?,?,?,'planned',?,?,?)`, in.ID, planID, athleteID, in.Date, in.Sport, in.SessionType, in.Priority, string(raw), nullableStringValue(in.Notes), in.ClientUpdatedAt, clock, nullableStringValue(optionalString(in.IdempotencyKey)))
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE planned_sessions SET plan_id=?,session_date=?,sport=?,session_type=?,priority=?,targets_raw=?,notes=?,client_updated_at=?,server_synced_at=?,deleted_at=NULL,idempotency_key=? WHERE id=?`, planID, in.Date, in.Sport, in.SessionType, in.Priority, string(raw), nullableStringValue(in.Notes), in.ClientUpdatedAt, clock, nullableStringValue(optionalString(in.IdempotencyKey)), in.ID)
	}
	if err != nil {
		return PlannedSessionWinner{}, normalizeWriteError(err)
	}
	if err := r.bumpPlanSequence(ctx, planID); err != nil {
		return PlannedSessionWinner{}, err
	}
	value, e := r.readPlanned(ctx, athleteID, in.ID)
	return PlannedSessionWinner{Session: value, Created: created, SubmittedVersionWon: true, ServerSyncedAt: clock}, e
}

func (r *Repository) UpdatePlannedSessionStatus(ctx context.Context, athleteID string, in PlannedSessionStatusWrite) (PlannedSessionWinner, error) {
	var owner string
	var old int64
	var planID string
	err := r.db.QueryRowContext(ctx, "SELECT athlete_id,client_updated_at,plan_id FROM planned_sessions WHERE id=? AND deleted_at IS NULL", in.ID).Scan(&owner, &old, &planID)
	if errors.Is(err, sql.ErrNoRows) {
		return PlannedSessionWinner{}, errors.New("not_found: planned session")
	}
	if err != nil {
		return PlannedSessionWinner{}, errors.New("internal_error: planned session lookup")
	}
	if owner != athleteID {
		return PlannedSessionWinner{}, errors.New("not_found: planned session")
	}
	if in.Status != "planned" && in.Status != "completed" && in.Status != "skipped" && in.Status != "cancelled" {
		return PlannedSessionWinner{}, errors.New("invalid_input: planned session status")
	}
	if oldStatus, err := r.plannedStatus(ctx, in.ID); err == nil {
		if oldStatus == "completed" && in.Status != "completed" {
			return PlannedSessionWinner{}, errors.New("conflict: completed planned sessions are immutable")
		}
		if in.Status == "cancelled" {
			var started int
			if e := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workout_sessions WHERE planned_session_id=? AND athlete_id=? AND status IN ('in_progress','completed') AND deleted_at IS NULL", in.ID, athleteID).Scan(&started); e != nil {
				return PlannedSessionWinner{}, errors.New("internal_error: planned session lifecycle")
			}
			if started > 0 {
				return PlannedSessionWinner{}, errors.New("conflict: started planned sessions cannot be cancelled")
			}
		}
	}
	if in.ClientUpdatedAt <= old {
		value, e := r.readPlanned(ctx, athleteID, in.ID)
		return PlannedSessionWinner{Session: value, SubmittedVersionWon: false}, e
	}
	var activity, workout any
	if in.Status == "completed" {
		if in.ActivityID == nil && in.WorkoutID == nil {
			return PlannedSessionWinner{}, errors.New("invalid_input: completed requires a completion link")
		}
		if in.ActivityID != nil {
			if !r.ownedActivity(ctx, athleteID, *in.ActivityID) {
				return PlannedSessionWinner{}, errors.New("not_found: completed activity")
			}
			activity = *in.ActivityID
		}
		if in.WorkoutID != nil {
			var workoutStatus string
			if err := r.db.QueryRowContext(ctx, "SELECT status FROM workout_sessions WHERE athlete_id=? AND id=? AND deleted_at IS NULL", athleteID, *in.WorkoutID).Scan(&workoutStatus); err != nil || workoutStatus != "completed" {
				return PlannedSessionWinner{}, errors.New("not_found: completed workout")
			}
			workout = *in.WorkoutID
		}
	}
	clock := r.now()
	if in.Status != "completed" {
		activity = nil
		workout = nil
	}
	_, err = r.db.ExecContext(ctx, "UPDATE planned_sessions SET status=?,completed_activity_id=?,completed_workout_id=?,client_updated_at=?,server_synced_at=? WHERE id=?", in.Status, activity, workout, in.ClientUpdatedAt, clock, in.ID)
	if err != nil {
		return PlannedSessionWinner{}, normalizeWriteError(err)
	}
	if err := r.bumpPlanSequence(ctx, planID); err != nil {
		return PlannedSessionWinner{}, err
	}
	value, e := r.readPlanned(ctx, athleteID, in.ID)
	return PlannedSessionWinner{Session: value, SubmittedVersionWon: true, ServerSyncedAt: clock}, e
}

func (r *Repository) plannedStatus(ctx context.Context, id string) (string, error) {
	var status string
	err := r.db.QueryRowContext(ctx, "SELECT status FROM planned_sessions WHERE id=?", id).Scan(&status)
	return status, err
}

func (r *Repository) readPlanned(ctx context.Context, athleteID, id string) (PlannedSession, error) {
	value, err := loadPlannedSession(ctx, r.db, athleteID, id, true)
	if errors.Is(err, sql.ErrNoRows) {
		return PlannedSession{}, errors.New("not_found: planned session")
	}
	if err != nil {
		return PlannedSession{}, errors.New("internal_error: planned session read")
	}
	return value, nil
}

func (r *Repository) ownedActivity(ctx context.Context, athleteID, id string) bool {
	var n int
	return r.db.QueryRowContext(ctx, "SELECT 1 FROM garmin_records WHERE athlete_id=? AND dataset='activities' AND (record_key=? OR json_extract(payload_raw,'$.activityId')=? OR json_extract(payload_raw,'$.activity_id')=?)", athleteID, id, id, id).Scan(&n) == nil
}
func (r *Repository) ownedWorkout(ctx context.Context, athleteID, id string) bool {
	var n int
	return r.db.QueryRowContext(ctx, "SELECT 1 FROM workout_sessions WHERE athlete_id=? AND id=? AND deleted_at IS NULL", athleteID, id).Scan(&n) == nil
}

// OwnedActivity reports whether an activity identifier belongs to an athlete.
func (r *Repository) OwnedActivity(ctx context.Context, athleteID, id string) bool {
	return r.ownedActivity(ctx, athleteID, id)
}

func (r *Repository) begin(ctx context.Context) (*sql.Tx, error) {
	db, ok := r.db.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return nil, errors.New("internal_error: database transaction unavailable")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.New("internal_error: database transaction")
	}
	return tx, nil
}

func requireID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return errors.New("invalid_input: id must be UUID")
	}
	return nil
}
func formatTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	s := value.UTC().Format(time.RFC3339)
	return &s
}
func mustLocation(name string) *time.Location {
	value, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return value
}
func nullableStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
func winnerClock(ctx context.Context, db DBTX, id string) int64 {
	var v int64
	_ = db.QueryRowContext(ctx, "SELECT COALESCE(server_synced_at,0) FROM workout_sessions WHERE id=?", id).Scan(&v)
	return v
}
