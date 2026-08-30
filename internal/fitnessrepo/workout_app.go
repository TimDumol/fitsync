package fitnessrepo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PlanPublishResult is returned by both REST and MCP so the caller can show
// which previous routine was archived.
type PlanPublishResult struct {
	Plan           PlanResult `json:"plan"`
	ArchivedPlanID *string    `json:"archived_plan_id,omitempty"`
}

type storedAggregate struct {
	id      string
	kind    string
	seq     int64
	deleted bool
}

func allocateSyncSeq(ctx context.Context, tx *sql.Tx) (int64, error) {
	var value int64
	if err := tx.QueryRowContext(ctx, "UPDATE sync_clock SET value=value+1 WHERE id=1 RETURNING value").Scan(&value); err != nil {
		return 0, errors.New("internal_error: sync sequence allocation")
	}
	return value, nil
}

func (r *Repository) bumpPlanSequence(ctx context.Context, planID string) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	fail := func(e error) error {
		_ = tx.Rollback()
		return e
	}
	seq, err := allocateSyncSeq(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE training_plans SET sync_seq=? WHERE id=?", seq, planID); err != nil {
		return fail(errors.New("internal_error: plan sequence update"))
	}
	if err := tx.Commit(); err != nil {
		return errors.New("internal_error: transaction commit")
	}
	return nil
}

func nullableText(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func parseDatePointer(value *string) error {
	if value == nil || *value == "" {
		return nil
	}
	return validDate(*value)
}

func validDate(value string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return errors.New("invalid_input: date must be YYYY-MM-DD")
	}
	return nil
}

func (r *Repository) UpsertPlanDraft(ctx context.Context, athleteID string, in PlanWrite) (PlanResult, error) {
	if in.Steps != nil {
		for i := range in.Sessions {
			if len(in.Sessions[i].Steps) == 0 && in.Steps[in.Sessions[i].ID] != nil {
				in.Sessions[i].Steps = in.Steps[in.Sessions[i].ID]
			}
		}
	}
	if err := validatePlanWrite(athleteID, in); err != nil {
		return PlanResult{}, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return PlanResult{}, err
	}
	fail := func(e error) (PlanResult, error) {
		_ = tx.Rollback()
		return PlanResult{}, e
	}
	var owner, status string
	var deleted sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT athlete_id,status,deleted_at FROM training_plans WHERE id=?", in.Plan.ID).Scan(&owner, &status, &deleted)
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !created {
		return fail(errors.New("internal_error: plan lookup"))
	}
	if !created {
		if owner != athleteID {
			return fail(errors.New("not_found: plan"))
		}
		if deleted.Valid || status != "draft" {
			return fail(errors.New("conflict: only an un-deleted draft plan can be replaced"))
		}
	}
	if !created {
		current, e := loadPlanAggregate(ctx, tx, athleteID, in.Plan.ID, true)
		if e != nil {
			return fail(e)
		}
		if samePlanDraft(current, in) {
			if e := tx.Commit(); e != nil {
				return PlanResult{}, errors.New("internal_error: transaction commit")
			}
			return current, nil
		}
	}
	now := r.now()
	goal := rawOrEmptyObject(in.Plan.Goal)
	if created {
		if _, err := tx.ExecContext(ctx, `INSERT INTO training_plans
			(id,athlete_id,title,description,goal_raw,status,start_date,end_date,source,source_ref,active,client_updated_at,server_synced_at,created_at,sync_seq)
			VALUES(?,?,?,?,?,'draft',?,?,?, ?,0,?,?,?,0)`,
			in.Plan.ID, athleteID, in.Plan.Title, nullableText(in.Plan.Description), string(goal), nullableText(in.Plan.StartDate), nullableText(in.Plan.EndDate), "mcp", nullableText(in.Plan.SourceRef), now, now, now); err != nil {
			return fail(normalizeWriteError(err))
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE training_plans SET title=?,description=?,goal_raw=?,status='draft',start_date=?,end_date=?,source=?,source_ref=?,active=0,client_updated_at=?,server_synced_at=?,deleted_at=NULL WHERE id=?`,
		in.Plan.Title, nullableText(in.Plan.Description), string(goal), nullableText(in.Plan.StartDate), nullableText(in.Plan.EndDate), "mcp", nullableText(in.Plan.SourceRef), now, now, in.Plan.ID); err != nil {
		return fail(normalizeWriteError(err))
	}

	// A draft replacement may only touch sessions that have never been
	// started.  Soft-deleting the old graph keeps synchronization deterministic.
	var locked int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM planned_sessions s
		WHERE s.plan_id=? AND s.deleted_at IS NULL AND
		(s.status IN ('completed','cancelled') OR EXISTS (
			SELECT 1 FROM workout_sessions w WHERE w.planned_session_id=s.id
			AND w.deleted_at IS NULL AND w.status IN ('in_progress','completed')))`, in.Plan.ID).Scan(&locked); err != nil {
		return fail(errors.New("internal_error: plan session lock check"))
	}
	if locked > 0 {
		return fail(errors.New("conflict: started or completed planned sessions cannot be replaced"))
	}
	if _, err := tx.ExecContext(ctx, "UPDATE planned_session_steps SET deleted_at=? WHERE planned_session_id IN (SELECT id FROM planned_sessions WHERE plan_id=? AND athlete_id=?) AND deleted_at IS NULL", now, in.Plan.ID, athleteID); err != nil {
		return fail(errors.New("internal_error: plan step tombstones"))
	}
	if _, err := tx.ExecContext(ctx, "UPDATE planned_sessions SET deleted_at=? WHERE plan_id=? AND athlete_id=? AND deleted_at IS NULL", now, in.Plan.ID, athleteID); err != nil {
		return fail(errors.New("internal_error: plan session tombstones"))
	}

	for _, session := range in.Sessions {
		var existingOwner, existingPlan string
		err := tx.QueryRowContext(ctx, "SELECT athlete_id,plan_id FROM planned_sessions WHERE id=?", session.ID).Scan(&existingOwner, &existingPlan)
		existing := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fail(errors.New("internal_error: planned session lookup"))
		}
		if existing && (existingOwner != athleteID || (existingPlan != in.Plan.ID && existingPlan != "")) {
			return fail(errors.New("conflict: planned session belongs to another plan or athlete"))
		}
		title := session.Title
		if title == "" {
			title = "Workout"
		}
		if existing {
			_, err = tx.ExecContext(ctx, `UPDATE planned_sessions SET plan_id=?,athlete_id=?,session_date=?,title=?,description=?,sport=?,session_type=?,priority=?,targets_raw=?,notes=?,status='planned',client_updated_at=?,server_synced_at=?,deleted_at=NULL WHERE id=?`,
				in.Plan.ID, athleteID, session.Date, title, nullableText(optionalString(session.Description)), session.Sport, session.SessionType, session.Priority, mustJSON(session.Targets), nullableText(session.Notes), now, now, session.ID)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO planned_sessions
				(id,plan_id,athlete_id,session_date,title,description,sport,session_type,priority,targets_raw,notes,status,client_updated_at,server_synced_at,created_at)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,'planned',?,?,?)`,
				session.ID, in.Plan.ID, athleteID, session.Date, title, nullableText(optionalString(session.Description)), session.Sport, session.SessionType, session.Priority, mustJSON(session.Targets), nullableText(session.Notes), now, now, now)
		}
		if err != nil {
			return fail(normalizeWriteError(err))
		}
		for _, step := range session.Steps {
			var stepOwner string
			stepErr := tx.QueryRowContext(ctx, "SELECT athlete_id FROM planned_session_steps WHERE id=?", step.ID).Scan(&stepOwner)
			if stepErr == nil && stepOwner != athleteID {
				return fail(errors.New("conflict: planned step belongs to another athlete"))
			}
			if stepErr != nil && !errors.Is(stepErr, sql.ErrNoRows) {
				return fail(errors.New("internal_error: planned step lookup"))
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO planned_session_steps
				(id,planned_session_id,athlete_id,sequence_no,step_type,exercise_key,exercise_name,prescription_raw,notes,created_at,deleted_at)
				VALUES(?,?,?,?,?,?,?,?,?,?,NULL)
				ON CONFLICT(id) DO UPDATE SET planned_session_id=excluded.planned_session_id,athlete_id=excluded.athlete_id,
				sequence_no=excluded.sequence_no,step_type=excluded.step_type,exercise_key=excluded.exercise_key,
				exercise_name=excluded.exercise_name,prescription_raw=excluded.prescription_raw,notes=excluded.notes,
				created_at=excluded.created_at,deleted_at=NULL`,
				step.ID, session.ID, athleteID, step.SequenceNo, step.StepType, nullableStringValue(step.ExerciseKey), nullableStringValue(step.ExerciseName), rawOrEmptyObject(step.Prescription), nullableStringValue(step.Notes), now)
			if err != nil {
				return fail(normalizeWriteError(err))
			}
		}
	}
	seq, err := allocateSyncSeq(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE training_plans SET sync_seq=? WHERE id=?", seq, in.Plan.ID); err != nil {
		return fail(errors.New("internal_error: plan sequence update"))
	}
	if err := tx.Commit(); err != nil {
		return PlanResult{}, errors.New("internal_error: transaction commit")
	}
	return r.PlanByID(ctx, athleteID, in.Plan.ID, true)
}

func (r *Repository) PublishPlan(ctx context.Context, athleteID, planID string) (PlanPublishResult, error) {
	if err := requireUUID(planID); err != nil {
		return PlanPublishResult{}, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return PlanPublishResult{}, err
	}
	fail := func(e error) (PlanPublishResult, error) {
		_ = tx.Rollback()
		return PlanPublishResult{}, e
	}
	var owner, status string
	var deleted sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT athlete_id,status,deleted_at FROM training_plans WHERE id=?", planID).Scan(&owner, &status, &deleted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fail(errors.New("not_found: plan"))
		}
		return fail(errors.New("internal_error: plan lookup"))
	}
	if owner != athleteID || deleted.Valid {
		return fail(errors.New("not_found: plan"))
	}
	if status == "active" {
		if err := tx.Commit(); err != nil {
			return PlanPublishResult{}, errors.New("internal_error: transaction commit")
		}
		plan, e := r.PlanByID(ctx, athleteID, planID, true)
		return PlanPublishResult{Plan: plan}, e
	}
	if status != "draft" {
		return fail(errors.New("conflict: only a draft plan can be published"))
	}
	var oldID string
	err = tx.QueryRowContext(ctx, "SELECT id FROM training_plans WHERE athlete_id=? AND status='active' AND active=1 AND deleted_at IS NULL AND id<>?", athleteID, planID).Scan(&oldID)
	hasOld := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fail(errors.New("internal_error: active plan lookup"))
	}
	var oldPtr *string
	if hasOld {
		oldPtr = &oldID
		oldSeq, e := allocateSyncSeq(ctx, tx)
		if e != nil {
			return fail(e)
		}
		if _, e = tx.ExecContext(ctx, "UPDATE training_plans SET status='archived',active=0,sync_seq=? WHERE id=?", oldSeq, oldID); e != nil {
			return fail(errors.New("internal_error: archive active plan"))
		}
	}
	newSeq, err := allocateSyncSeq(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE training_plans SET status='active',active=1,sync_seq=? WHERE id=?", newSeq, planID); err != nil {
		return fail(normalizeWriteError(err))
	}
	if err := tx.Commit(); err != nil {
		return PlanPublishResult{}, errors.New("internal_error: transaction commit")
	}
	plan, err := r.PlanByID(ctx, athleteID, planID, true)
	return PlanPublishResult{Plan: plan, ArchivedPlanID: oldPtr}, err
}

func (r *Repository) SetPlannedSessionStatus(ctx context.Context, athleteID, sessionID, status string) (PlannedSession, error) {
	if err := requireUUID(sessionID); err != nil {
		return PlannedSession{}, err
	}
	if status != "skipped" {
		return PlannedSession{}, errors.New("invalid_input: only skipped is supported by this command")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return PlannedSession{}, err
	}
	var owner, planID, current string
	var deleted sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT athlete_id,plan_id,status,deleted_at FROM planned_sessions WHERE id=?", sessionID).Scan(&owner, &planID, &current, &deleted); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return PlannedSession{}, errors.New("not_found: planned session")
		}
		return PlannedSession{}, errors.New("internal_error: planned session lookup")
	}
	if owner != athleteID || deleted.Valid {
		_ = tx.Rollback()
		return PlannedSession{}, errors.New("not_found: planned session")
	}
	if current == "skipped" {
		_ = tx.Commit()
		return r.readPlanned(ctx, athleteID, sessionID)
	}
	if current != "planned" {
		_ = tx.Rollback()
		return PlannedSession{}, errors.New("conflict: planned session cannot be skipped in its current state")
	}
	var started int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM workout_sessions WHERE planned_session_id=? AND athlete_id=? AND status IN ('in_progress','completed') AND deleted_at IS NULL", sessionID, athleteID).Scan(&started); err != nil {
		_ = tx.Rollback()
		return PlannedSession{}, errors.New("internal_error: planned session lifecycle")
	}
	if started > 0 {
		_ = tx.Rollback()
		return PlannedSession{}, errors.New("conflict: started planned sessions cannot be skipped")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE planned_sessions SET status='skipped' WHERE id=?", sessionID); err != nil {
		_ = tx.Rollback()
		return PlannedSession{}, errors.New("internal_error: planned session status")
	}
	seq, err := allocateSyncSeq(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return PlannedSession{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE training_plans SET sync_seq=? WHERE id=?", seq, planID); err != nil {
		_ = tx.Rollback()
		return PlannedSession{}, errors.New("internal_error: plan sequence update")
	}
	if err := tx.Commit(); err != nil {
		return PlannedSession{}, errors.New("internal_error: transaction commit")
	}
	return r.readPlanned(ctx, athleteID, sessionID)
}

func (r *Repository) PlanByID(ctx context.Context, athleteID, planID string, includeSteps bool) (PlanResult, error) {
	if err := requireUUID(planID); err != nil {
		return PlanResult{}, err
	}
	result, err := loadPlanAggregate(ctx, r.db, athleteID, planID, includeSteps)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanResult{}, errors.New("not_found: plan")
	}
	return result, err
}

func (r *Repository) ActivePlan(ctx context.Context, athleteID string, includeSteps bool) (PlanResult, error) {
	var id string
	if err := r.db.QueryRowContext(ctx, "SELECT id FROM training_plans WHERE athlete_id=? AND status='active' AND deleted_at IS NULL", athleteID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PlanResult{Sessions: []PlannedSession{}}, nil
		}
		return PlanResult{}, errors.New("internal_error: active plan lookup")
	}
	return r.PlanByID(ctx, athleteID, id, includeSteps)
}

func (r *Repository) PlannedSessions(ctx context.Context, athleteID, from, to string, includeSteps bool) ([]PlannedSession, error) {
	if err := validRange(from, to, 366); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM planned_sessions
		WHERE athlete_id=? AND deleted_at IS NULL AND session_date BETWEEN ? AND ?
		ORDER BY session_date,id`, athleteID, from, to)
	if err != nil {
		return nil, errors.New("internal_error: planned session query")
	}
	defer rows.Close()
	result := []PlannedSession{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, errors.New("internal_error: planned session row")
		}
		value, err := r.loadPlannedSession(ctx, r.db, athleteID, id, includeSteps)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func loadPlanAggregate(ctx context.Context, db DBTX, athleteID, planID string, includeSteps bool) (PlanResult, error) {
	var plan TrainingPlan
	var description, startDate, endDate, sourceRef sql.NullString
	var goalRaw string
	if err := db.QueryRowContext(ctx, `SELECT id,athlete_id,title,description,goal_raw,status,start_date,end_date,source,source_ref,created_at,sync_seq
		FROM training_plans WHERE id=? AND athlete_id=? AND deleted_at IS NULL`, planID, athleteID).Scan(
		&plan.ID, &plan.AthleteID, &plan.Title, &description, &goalRaw, &plan.Status, &startDate, &endDate, &plan.Source, &sourceRef, &plan.CreatedAt, &plan.SyncSeq); err != nil {
		return PlanResult{}, err
	}
	plan.Goal = json.RawMessage(goalRaw)
	if description.Valid {
		plan.Description = &description.String
	}
	if startDate.Valid {
		plan.StartDate = &startDate.String
	}
	if endDate.Valid {
		plan.EndDate = &endDate.String
	}
	if sourceRef.Valid {
		plan.SourceRef = &sourceRef.String
	}
	result := PlanResult{Plan: &plan, Sessions: []PlannedSession{}}
	rows, err := db.QueryContext(ctx, `SELECT id FROM planned_sessions WHERE plan_id=? AND athlete_id=? AND deleted_at IS NULL ORDER BY session_date,id`, planID, athleteID)
	if err != nil {
		return PlanResult{}, errors.New("internal_error: planned session query")
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return PlanResult{}, errors.New("internal_error: planned session row")
		}
		session, err := loadPlannedSession(ctx, db, athleteID, id, includeSteps)
		if err != nil {
			return PlanResult{}, err
		}
		result.Sessions = append(result.Sessions, session)
	}
	if err := rows.Err(); err != nil {
		return PlanResult{}, errors.New("internal_error: planned session rows")
	}
	plan.Sessions = result.Sessions
	return result, nil
}

func (r *Repository) loadPlannedSession(ctx context.Context, db DBTX, athleteID, id string, includeSteps bool) (PlannedSession, error) {
	return loadPlannedSession(ctx, db, athleteID, id, includeSteps)
}

func loadPlannedSession(ctx context.Context, db DBTX, athleteID, id string, includeSteps bool) (PlannedSession, error) {
	var session PlannedSession
	var description, notes, activity, workout, targetsRaw sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT id,plan_id,athlete_id,session_date,title,description,sport,session_type,priority,targets_raw,notes,status,completed_activity_id,completed_workout_id
		FROM planned_sessions WHERE id=? AND athlete_id=? AND deleted_at IS NULL`, id, athleteID).Scan(
		&session.ID, &session.PlanID, &session.AthleteID, &session.Date, &session.Title, &description, &session.Sport, &session.SessionType, &session.Priority, &targetsRaw, &notes, &session.Status, &activity, &workout); err != nil {
		return PlannedSession{}, err
	}
	if description.Valid {
		session.Description = &description.String
	}
	if notes.Valid {
		session.Notes = &notes.String
	}
	if activity.Valid {
		session.CompletedActivityID = &activity.String
	}
	if workout.Valid {
		session.CompletedWorkoutID = &workout.String
	}
	if err := json.Unmarshal([]byte(targetsRaw.String), &session.Targets); err != nil {
		// Targets are known JSON at write time; retain a zero value for legacy
		// malformed rows rather than exposing raw storage.
		session.Targets = PlannedTargets{}
	}
	var completedWorkout string
	if err := db.QueryRowContext(ctx, "SELECT id FROM workout_sessions WHERE planned_session_id=? AND athlete_id=? AND status='completed' AND deleted_at IS NULL ORDER BY completed_at_utc_ms DESC,id LIMIT 1", id, athleteID).Scan(&completedWorkout); err == nil {
		session.Status = "completed"
		session.CompletedWorkoutID = &completedWorkout
	}
	if includeSteps {
		steps, err := loadSteps(ctx, db, athleteID, id)
		if err != nil {
			return PlannedSession{}, err
		}
		session.Steps = steps
	}
	return session, nil
}

func loadSteps(ctx context.Context, db DBTX, athleteID, sessionID string) ([]PlannedStep, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,planned_session_id,athlete_id,sequence_no,step_type,exercise_key,exercise_name,prescription_raw,notes
		FROM planned_session_steps WHERE planned_session_id=? AND athlete_id=? AND deleted_at IS NULL ORDER BY sequence_no,id`, sessionID, athleteID)
	if err != nil {
		return nil, errors.New("internal_error: planned step query")
	}
	defer rows.Close()
	result := []PlannedStep{}
	for rows.Next() {
		var step PlannedStep
		var key, name, notes sql.NullString
		if err := rows.Scan(&step.ID, &step.PlannedSessionID, &step.AthleteID, &step.SequenceNo, &step.StepType, &key, &name, &step.Prescription, &notes); err != nil {
			return nil, errors.New("internal_error: planned step row")
		}
		if key.Valid {
			step.ExerciseKey = &key.String
		}
		if name.Valid {
			step.ExerciseName = &name.String
		}
		if notes.Valid {
			step.Notes = &notes.String
		}
		result = append(result, step)
	}
	return result, rows.Err()
}

func (r *Repository) UpsertWorkoutAggregate(ctx context.Context, athleteID string, in WorkoutWrite) (WorkoutWinner, error) {
	return r.upsertWorkoutAggregate(ctx, athleteID, in, false)
}

func (r *Repository) CompleteWorkout(ctx context.Context, athleteID string, in WorkoutWrite) (WorkoutWinner, error) {
	in.Status = "completed"
	return r.upsertWorkoutAggregate(ctx, athleteID, in, true)
}

func (r *Repository) upsertWorkoutAggregate(ctx context.Context, athleteID string, in WorkoutWrite, completing bool) (WorkoutWinner, error) {
	if err := validateWorkoutWrite(in); err != nil {
		return WorkoutWinner{}, err
	}
	if in.Status == "" {
		in.Status = "in_progress"
	}
	if completing && in.Status != "completed" {
		return WorkoutWinner{}, errors.New("invalid_input: completion requires completed status")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return WorkoutWinner{}, err
	}
	fail := func(e error) (WorkoutWinner, error) { _ = tx.Rollback(); return WorkoutWinner{}, e }
	var owner, oldStatus string
	var oldClient, oldSeq int64
	var oldDeleted sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT athlete_id,status,client_updated_at,sync_seq,deleted_at FROM workout_sessions WHERE id=?", in.ID).Scan(&owner, &oldStatus, &oldClient, &oldSeq, &oldDeleted)
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !created {
		return fail(errors.New("internal_error: workout lookup"))
	}
	if created {
		if err := requireUUIDv7(in.ID); err != nil {
			return fail(err)
		}
		if completing {
			return fail(errors.New("conflict: workout must be in_progress before completion"))
		}
	} else if owner != athleteID {
		return fail(errors.New("not_found: workout"))
	} else if oldDeleted.Valid {
		return fail(errors.New("conflict: deleted workouts are immutable"))
	}
	if !created && oldStatus == "discarded" {
		return fail(errors.New("conflict: discarded workouts are immutable"))
	}
	if completing && !created && oldStatus != "in_progress" && oldStatus != "completed" {
		return fail(errors.New("conflict: only in-progress workouts can be completed"))
	}
	if in.PlannedSessionID != nil {
		if err := validatePlannedLink(ctx, tx, athleteID, *in.PlannedSessionID, in.Entries); err != nil {
			return fail(err)
		}
	}
	if !created && oldStatus == "completed" {
		if in.Status != "completed" {
			return fail(errors.New("conflict: completed workouts are immutable"))
		}
		// A retry is a successful no-op when the complete aggregate is the same.
		stored, e := loadWorkoutAggregate(ctx, tx, athleteID, in.ID)
		if e != nil {
			return fail(e)
		}
		if sameWorkoutAggregate(stored, in) {
			if e := tx.Commit(); e != nil {
				return WorkoutWinner{}, errors.New("internal_error: transaction commit")
			}
			return WorkoutWinner{Workout: stored, SubmittedVersionWon: false, ServerSyncedAt: oldSeq}, nil
		}
		return fail(errors.New("conflict: completed workouts are immutable"))
	}
	if !created && in.ClientUpdatedAt <= oldClient {
		stored, e := loadWorkoutAggregate(ctx, tx, athleteID, in.ID)
		if e != nil {
			return fail(e)
		}
		if in.ClientUpdatedAt == oldClient && oldStatus != "completed" && !sameWorkoutAggregate(stored, in) {
			return fail(errors.New("conflict: workout version already exists"))
		}
		if e := tx.Commit(); e != nil {
			return WorkoutWinner{}, errors.New("internal_error: transaction commit")
		}
		return WorkoutWinner{Workout: stored, SubmittedVersionWon: false, ServerSyncedAt: oldSeq}, nil
	}
	now := r.now()
	started := in.StartedAt
	if started == nil && in.Status == "in_progress" {
		value := r.clock()
		started = &value
	}
	completed := in.CompletedAt
	if in.Status == "completed" && completed == nil {
		value := r.clock()
		completed = &value
	}
	meta := workoutMetadata{WorkoutType: in.WorkoutType, StartedAt: formatTime(started), DurationMinutes: in.DurationMinutes, PerceivedEffort: in.PerceivedEffort, LinkedActivityID: in.LinkedActivityID, Notes: in.Notes}
	raw, _ := json.Marshal(meta)
	var seq int64
	if created {
		seq, err = allocateSyncSeq(ctx, tx)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO workout_sessions
				(id,athlete_id,workout_date,title,source,metadata_raw,client_updated_at,server_synced_at,created_at,planned_session_id,status,started_at_utc_ms,completed_at_utc_ms,sync_seq)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.ID, athleteID, in.Date, in.Title, in.Source, string(raw), in.ClientUpdatedAt, now, now, nullableStringValue(in.PlannedSessionID), in.Status, millis(started), millis(completed), seq)
		}
	} else {
		seq, err = allocateSyncSeq(ctx, tx)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE workout_sessions SET workout_date=?,title=?,source=?,metadata_raw=?,client_updated_at=?,server_synced_at=?,deleted_at=NULL,planned_session_id=?,status=?,started_at_utc_ms=?,completed_at_utc_ms=?,sync_seq=? WHERE id=?`,
				in.Date, in.Title, in.Source, string(raw), in.ClientUpdatedAt, now, nullableStringValue(in.PlannedSessionID), in.Status, millis(started), millis(completed), seq, in.ID)
		}
	}
	if err != nil {
		return fail(normalizeWriteError(err))
	}
	if _, err := tx.ExecContext(ctx, "UPDATE workout_entries SET deleted_at=? WHERE session_id=? AND athlete_id=? AND deleted_at IS NULL", now, in.ID, athleteID); err != nil {
		return fail(errors.New("internal_error: workout entries"))
	}
	for _, entry := range in.Entries {
		var entryOwner string
		err := tx.QueryRowContext(ctx, "SELECT athlete_id FROM workout_entries WHERE id=?", entry.EntryID).Scan(&entryOwner)
		if err == nil && entryOwner != athleteID {
			return fail(errors.New("conflict: workout entry belongs to another athlete"))
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fail(errors.New("internal_error: workout entry lookup"))
		}
		payload := entryPayload{ExerciseKey: entry.ExerciseKey, ExerciseName: entry.ExerciseName, UserLabel: entry.UserLabel, SetNumber: entry.SetNumber, Reps: entry.Reps, DurationSeconds: entry.DurationSeconds, LoadKG: entry.LoadKG, LoadDescription: entry.LoadDescription, IsBodyweight: entry.IsBodyweight, ToFailure: entry.ToFailure, RIR: entry.RIR, RIRMin: entry.RIRMin, RIRMax: entry.RIRMax, Laterality: entry.Laterality, MuscleGroups: entry.MuscleGroups, Notes: entry.Notes, IsWarmup: entry.IsWarmup}
		entryRaw, _ := json.Marshal(payload)
		if _, err := tx.ExecContext(ctx, `INSERT INTO workout_entries
			(id,session_id,athlete_id,sequence_no,entry_type,payload_raw,client_updated_at,server_synced_at,deleted_at,planned_step_id)
			VALUES(?,?,?,?,?,?,?,?,NULL,?) ON CONFLICT(id) DO UPDATE SET session_id=excluded.session_id,athlete_id=excluded.athlete_id,
			sequence_no=excluded.sequence_no,entry_type=excluded.entry_type,payload_raw=excluded.payload_raw,client_updated_at=excluded.client_updated_at,
			server_synced_at=excluded.server_synced_at,deleted_at=NULL,planned_step_id=excluded.planned_step_id`,
			entry.EntryID, in.ID, athleteID, entry.SequenceNo, entry.EntryType, string(entryRaw), in.ClientUpdatedAt, now, nullableStringValue(entry.PlannedStepID)); err != nil {
			return fail(normalizeWriteError(err))
		}
	}
	if completing && in.PlannedSessionID != nil {
		if _, err := tx.ExecContext(ctx, "UPDATE planned_sessions SET status='completed',completed_workout_id=? WHERE id=? AND athlete_id=?", in.ID, *in.PlannedSessionID, athleteID); err != nil {
			return fail(errors.New("internal_error: planned session completion"))
		}
		planSeq, err := allocateSyncSeq(ctx, tx)
		if err != nil {
			return fail(err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE training_plans SET sync_seq=? WHERE id=(SELECT plan_id FROM planned_sessions WHERE id=? AND athlete_id=?)", planSeq, *in.PlannedSessionID, athleteID); err != nil {
			return fail(errors.New("internal_error: containing plan sequence"))
		}
	}
	if err := tx.Commit(); err != nil {
		return WorkoutWinner{}, errors.New("internal_error: transaction commit")
	}
	value, err := r.Workout(ctx, athleteID, in.ID)
	if err != nil {
		return WorkoutWinner{}, err
	}
	return WorkoutWinner{Workout: value, Created: created, SubmittedVersionWon: true, ServerSyncedAt: seq}, nil
}

func (r *Repository) Workout(ctx context.Context, athleteID, workoutID string) (StrengthWorkout, error) {
	if err := requireUUID(workoutID); err != nil {
		return StrengthWorkout{}, err
	}
	value, err := loadWorkoutAggregate(ctx, r.db, athleteID, workoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return StrengthWorkout{}, errors.New("not_found: workout")
	}
	return value, err
}

func loadWorkoutAggregate(ctx context.Context, db DBTX, athleteID, workoutID string) (StrengthWorkout, error) {
	var date, title, source, metadata, status string
	var planned sql.NullString
	var started, completed sql.NullInt64
	var seq int64
	if err := db.QueryRowContext(ctx, `SELECT workout_date,title,source,metadata_raw,planned_session_id,status,started_at_utc_ms,completed_at_utc_ms,sync_seq
		FROM workout_sessions WHERE id=? AND athlete_id=? AND deleted_at IS NULL`, workoutID, athleteID).Scan(&date, &title, &source, &metadata, &planned, &status, &started, &completed, &seq); err != nil {
		return StrengthWorkout{}, err
	}
	w := decodeWorkout(workoutID, date, title, source, metadata)
	if w == nil {
		return StrengthWorkout{}, errors.New("internal_error: workout payload")
	}
	w.AthleteID, w.Status, w.SyncSeq = athleteID, status, seq
	if planned.Valid {
		w.PlannedSessionID = &planned.String
	}
	if started.Valid {
		value := time.UnixMilli(started.Int64).UTC().Format(time.RFC3339)
		w.StartedAt = &value
	}
	if completed.Valid {
		value := time.UnixMilli(completed.Int64).UTC().Format(time.RFC3339)
		w.CompletedAt = &value
	}
	entries, err := loadEntries(ctx, db, athleteID, workoutID)
	if err != nil {
		return StrengthWorkout{}, err
	}
	w.Entries, w.Summary = entries, summarizeStrength(entries)
	if w.PlannedSessionID != nil {
		session, err := loadPlannedSession(ctx, db, athleteID, *w.PlannedSessionID, true)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return StrengthWorkout{}, err
		}
		if err == nil {
			w.PlannedSession = &session
			w.Comparison = compareWorkout(session, entries)
		}
	}
	return *w, nil
}

func loadEntries(ctx context.Context, db DBTX, athleteID, workoutID string) ([]StrengthEntry, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,sequence_no,entry_type,payload_raw,planned_step_id FROM workout_entries
		WHERE athlete_id=? AND session_id=? AND deleted_at IS NULL ORDER BY sequence_no,id`, athleteID, workoutID)
	if err != nil {
		return nil, errors.New("internal_error: entry query")
	}
	defer rows.Close()
	result := []StrengthEntry{}
	for rows.Next() {
		var id, typ, raw string
		var seq int
		var planned sql.NullString
		if err := rows.Scan(&id, &seq, &typ, &raw, &planned); err != nil {
			return nil, errors.New("internal_error: entry row")
		}
		var p entryPayload
		if json.Unmarshal([]byte(raw), &p) != nil {
			continue
		}
		groups := p.MuscleGroups
		value := StrengthEntry{EntryID: id, SequenceNo: seq, EntryType: typ, ExerciseKey: p.ExerciseKey, ExerciseName: p.ExerciseName, UserLabel: p.UserLabel, SetNumber: p.SetNumber, Reps: p.Reps, DurationSeconds: p.DurationSeconds, LoadKG: p.LoadKG, LoadDescription: p.LoadDescription, IsBodyweight: p.IsBodyweight, ToFailure: p.ToFailure, RIR: p.RIR, RIRMin: p.RIRMin, RIRMax: p.RIRMax, Laterality: p.Laterality, MuscleGroups: groups, Notes: p.Notes, IsWarmup: p.IsWarmup}
		if planned.Valid {
			value.PlannedStepID = &planned.String
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (r *Repository) PullChanges(ctx context.Context, athleteID string, after int64, limit int) (SyncPage, error) {
	if after < 0 {
		return SyncPage{}, errors.New("invalid_input: after must be non-negative")
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 50 {
		return SyncPage{}, errors.New("invalid_input: limit must be between 1 and 50")
	}
	tx, err := r.beginRead(ctx)
	if err != nil {
		return SyncPage{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,sync_seq,deleted FROM (
		SELECT id,'training_plan' AS kind,sync_seq,deleted_at IS NOT NULL AS deleted FROM training_plans WHERE athlete_id=? AND sync_seq>?
		UNION ALL
		SELECT id,'workout' AS kind,sync_seq,deleted_at IS NOT NULL AS deleted FROM workout_sessions WHERE athlete_id=? AND sync_seq>?
	) ORDER BY sync_seq,kind,id LIMIT ?`, athleteID, after, athleteID, after, limit+1)
	if err != nil {
		return SyncPage{}, errors.New("internal_error: sync query")
	}
	defer rows.Close()
	aggregates := []storedAggregate{}
	for rows.Next() {
		var item storedAggregate
		var deleted int
		if err := rows.Scan(&item.id, &item.kind, &item.seq, &deleted); err != nil {
			return SyncPage{}, errors.New("internal_error: sync row")
		}
		item.deleted = deleted != 0
		aggregates = append(aggregates, item)
	}
	if err := rows.Err(); err != nil {
		return SyncPage{}, errors.New("internal_error: sync rows")
	}
	more := len(aggregates) > limit
	if more {
		aggregates = aggregates[:limit]
	}
	page := SyncPage{Changes: []SyncChange{}, NextAfter: after, HasMore: more}
	for _, item := range aggregates {
		change := SyncChange{SyncSeq: item.seq, Kind: item.kind, Operation: "upsert", EntityID: item.id}
		if item.deleted {
			change.Operation = "delete"
		} else if item.kind == "training_plan" {
			value, err := loadPlanAggregate(ctx, tx, athleteID, item.id, true)
			if err != nil {
				return SyncPage{}, err
			}
			change.Entity = *value.Plan
		} else {
			value, err := loadWorkoutAggregate(ctx, tx, athleteID, item.id)
			if err != nil {
				return SyncPage{}, err
			}
			change.Entity = value
		}
		page.Changes = append(page.Changes, change)
		page.NextAfter = item.seq
	}
	return page, nil
}

func (r *Repository) beginRead(ctx context.Context) (*sql.Tx, error) {
	db, ok := r.db.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return nil, errors.New("internal_error: database transaction unavailable")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.New("internal_error: database transaction")
	}
	return tx, nil
}

func validatePlanWrite(athleteID string, in PlanWrite) error {
	if athleteID == "" {
		return errors.New("invalid_input: athlete ID")
	}
	if err := requireUUIDv7(in.Plan.ID); err != nil {
		return err
	}
	if in.Plan.Title == "" || len(in.Plan.Title) > 512 {
		return errors.New("invalid_input: plan title")
	}
	if in.Plan.Description != nil && len(*in.Plan.Description) > 4000 {
		return errors.New("invalid_input: plan description")
	}
	if in.Plan.SourceRef != nil && len(*in.Plan.SourceRef) > 512 {
		return errors.New("invalid_input: plan source_ref")
	}
	if len(in.Sessions) > 120 {
		return errors.New("invalid_input: at most 120 sessions per plan")
	}
	goal := rawOrEmptyObject(in.Plan.Goal)
	if len(goal) > 64*1024 || !isJSONObject(goal) {
		return errors.New("invalid_input: goal must be valid JSON")
	}
	if err := parseDatePointer(in.Plan.StartDate); err != nil {
		return err
	}
	if err := parseDatePointer(in.Plan.EndDate); err != nil {
		return err
	}
	if in.Plan.StartDate != nil && in.Plan.EndDate != nil && *in.Plan.StartDate > *in.Plan.EndDate {
		return errors.New("invalid_input: plan end_date must not be before start_date")
	}
	seenSessions := map[string]bool{}
	for _, session := range in.Sessions {
		if err := requireUUIDv7(session.ID); err != nil {
			return err
		}
		if seenSessions[session.ID] {
			return errors.New("invalid_input: duplicate session ID")
		}
		seenSessions[session.ID] = true
		if session.PlanID != "" && session.PlanID != in.Plan.ID {
			return errors.New("invalid_input: session plan_id does not match plan")
		}
		if err := validDate(session.Date); err != nil {
			return err
		}
		if session.Title != "" && len(session.Title) > 512 || len(session.Description) > 4000 || len(session.Sport) > 64 || len(session.SessionType) > 64 || len(session.Priority) > 32 {
			return errors.New("invalid_input: session text is too long")
		}
		if len(session.Steps) > 50 {
			return errors.New("invalid_input: at most 50 steps per session")
		}
		seenSteps := map[int]bool{}
		seenStepIDs := map[string]bool{}
		for _, step := range session.Steps {
			if err := validateStep(step); err != nil {
				return err
			}
			if seenSteps[step.SequenceNo] || seenStepIDs[step.ID] {
				return errors.New("invalid_input: duplicate step sequence or ID")
			}
			seenSteps[step.SequenceNo], seenStepIDs[step.ID] = true, true
		}
	}
	return nil
}

func validateStep(step PlannedStep) error {
	if err := requireUUIDv7(step.ID); err != nil {
		return err
	}
	if step.SequenceNo < 1 || step.SequenceNo > 50 {
		return errors.New("invalid_input: step sequence is out of bounds")
	}
	switch step.StepType {
	case "warmup", "exercise", "interval", "rest", "cooldown", "note":
	default:
		return errors.New("invalid_input: step_type")
	}
	if step.ExerciseName != nil && len(*step.ExerciseName) > 256 || step.ExerciseKey != nil && len(*step.ExerciseKey) > 128 || step.Notes != nil && len(*step.Notes) > 4000 {
		return errors.New("invalid_input: step text is too long")
	}
	if step.StepType == "exercise" && (step.ExerciseName == nil || strings.TrimSpace(*step.ExerciseName) == "") {
		return errors.New("invalid_input: exercise_name is required for exercise steps")
	}
	prescription := rawOrEmptyObject(step.Prescription)
	if len(prescription) > 64*1024 {
		return errors.New("invalid_input: prescription is too large")
	}
	var value Prescription
	decoder := json.NewDecoder(bytes.NewReader(prescription))
	decoder.DisallowUnknownFields()
	trimmed := bytes.TrimSpace(prescription)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("invalid_input: prescription must be an object")
	}
	if err := decoder.Decode(&value); err != nil {
		return errors.New("invalid_input: prescription must be an object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid_input: prescription must be one JSON object")
	}
	if len(value.Sets) > 20 {
		return errors.New("invalid_input: at most 20 prescribed sets per step")
	}
	for _, set := range value.Sets {
		if set.RepsMin != nil && (*set.RepsMin < 0 || *set.RepsMin > 1000) || set.RepsMax != nil && (*set.RepsMax < 0 || *set.RepsMax > 1000) {
			return errors.New("invalid_input: reps out of bounds")
		}
		if set.RepsMin != nil && set.RepsMax != nil && *set.RepsMin > *set.RepsMax {
			return errors.New("invalid_input: reps_min exceeds reps_max")
		}
		if set.LoadKG != nil && (*set.LoadKG < 0 || *set.LoadKG > 2000) || set.RIR != nil && (*set.RIR < 0 || *set.RIR > 10) || set.RPE != nil && (*set.RPE < 1 || *set.RPE > 10) || set.DurationSeconds != nil && (*set.DurationSeconds < 0 || *set.DurationSeconds > 86400) || set.RestSeconds != nil && (*set.RestSeconds < 0 || *set.RestSeconds > 86400) {
			return errors.New("invalid_input: prescription set value is out of bounds")
		}
	}
	if value.Rounds != nil && (*value.Rounds < 0 || *value.Rounds > 1000) || value.DistanceMeters != nil && (*value.DistanceMeters < 0 || *value.DistanceMeters > 1000000) || value.DurationSeconds != nil && (*value.DurationSeconds < 0 || *value.DurationSeconds > 86400) {
		return errors.New("invalid_input: interval prescription is out of bounds")
	}
	if value.PaceMinPerKMMin != nil && value.PaceMinPerKMMax != nil && *value.PaceMinPerKMMin > *value.PaceMinPerKMMax {
		return errors.New("invalid_input: pace minimum exceeds maximum")
	}
	return nil
}

func validateWorkoutWrite(in WorkoutWrite) error {
	if err := requireUUIDv7(in.ID); err != nil {
		return err
	}
	if in.ClientUpdatedAt < 0 || in.Title == "" || len(in.Title) > 512 || in.Source == "" || len(in.Source) > 128 {
		return errors.New("invalid_input: workout metadata")
	}
	if err := validDate(in.Date); err != nil {
		return err
	}
	if in.Status != "" && in.Status != "in_progress" && in.Status != "completed" && in.Status != "discarded" {
		return errors.New("invalid_input: workout status")
	}
	if len(in.Entries) > 200 {
		return errors.New("invalid_input: at most 200 entries")
	}
	seen := map[int]bool{}
	for _, entry := range in.Entries {
		if err := requireUUIDv7(entry.EntryID); err != nil {
			return err
		}
		if entry.SequenceNo < 1 || seen[entry.SequenceNo] {
			return errors.New("invalid_input: duplicate or invalid entry sequence")
		}
		seen[entry.SequenceNo] = true
		if entry.EntryType != "set" && entry.EntryType != "rest" && entry.EntryType != "note" {
			return errors.New("invalid_input: entry_type")
		}
		if entry.PlannedStepID != nil {
			if err := requireUUID(*entry.PlannedStepID); err != nil {
				return err
			}
		}
	}
	if in.PlannedSessionID != nil {
		if err := requireUUID(*in.PlannedSessionID); err != nil {
			return err
		}
	}
	return nil
}

func validatePlannedLink(ctx context.Context, tx *sql.Tx, athleteID, sessionID string, entries []StrengthEntry) error {
	var owner string
	if err := tx.QueryRowContext(ctx, "SELECT athlete_id FROM planned_sessions WHERE id=? AND deleted_at IS NULL", sessionID).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("not_found: planned session")
		}
		return errors.New("internal_error: planned session lookup")
	}
	if owner != athleteID {
		return errors.New("not_found: planned session")
	}
	for _, entry := range entries {
		if entry.PlannedStepID == nil {
			continue
		}
		var stepOwner, stepSession string
		if err := tx.QueryRowContext(ctx, "SELECT athlete_id,planned_session_id FROM planned_session_steps WHERE id=? AND deleted_at IS NULL", *entry.PlannedStepID).Scan(&stepOwner, &stepSession); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("not_found: planned step")
			}
			return errors.New("internal_error: planned step lookup")
		}
		if stepOwner != athleteID || stepSession != sessionID {
			return errors.New("conflict: planned step does not belong to the planned session")
		}
	}
	return nil
}

func sameWorkoutAggregate(stored StrengthWorkout, in WorkoutWrite) bool {
	if stored.WorkoutID != in.ID || stored.Date != in.Date || stored.Title != in.Title || stored.Source != in.Source || stored.WorkoutType != in.WorkoutType || stored.Status != in.Status {
		return false
	}
	if deref(stored.PlannedSessionID) != deref(in.PlannedSessionID) {
		return false
	}
	if (in.StartedAt != nil && !sameString(stored.StartedAt, formatTime(in.StartedAt))) || (in.CompletedAt != nil && !sameString(stored.CompletedAt, formatTime(in.CompletedAt))) || !sameFloat(stored.DurationMinutes, in.DurationMinutes) || !sameFloat(stored.PerceivedEffort, in.PerceivedEffort) || !sameString(stored.Notes, in.Notes) || !sameString(stored.LinkedActivityID, in.LinkedActivityID) {
		return false
	}
	return len(stored.Entries) == len(in.Entries) && sameEntries(stored.Entries, in.Entries)
}

func samePlanDraft(stored PlanResult, in PlanWrite) bool {
	if stored.Plan == nil || stored.Plan.ID != in.Plan.ID || stored.Plan.Title != in.Plan.Title ||
		!sameString(stored.Plan.Description, in.Plan.Description) ||
		!sameRawJSON(stored.Plan.Goal, rawOrEmptyObject(in.Plan.Goal)) ||
		!sameString(stored.Plan.StartDate, in.Plan.StartDate) || !sameString(stored.Plan.EndDate, in.Plan.EndDate) ||
		!sameString(stored.Plan.SourceRef, in.Plan.SourceRef) || len(stored.Sessions) != len(in.Sessions) {
		return false
	}
	byID := make(map[string]PlannedSession, len(stored.Sessions))
	for _, session := range stored.Sessions {
		byID[session.ID] = session
	}
	for _, input := range in.Sessions {
		storedSession, ok := byID[input.ID]
		if !ok || storedSession.PlanID != in.Plan.ID || storedSession.Date != input.Date ||
			storedSession.Sport != input.Sport || storedSession.SessionType != input.SessionType ||
			storedSession.Priority != input.Priority || !reflect.DeepEqual(storedSession.Targets, input.Targets) ||
			!sameString(storedSession.TitlePtr(), optionalString(input.TitleOrDefault())) ||
			!sameString(storedSession.Description, optionalString(input.Description)) ||
			!sameString(storedSession.Notes, input.Notes) {
			return false
		}
		steps := input.Steps
		if len(storedSession.Steps) != len(steps) {
			return false
		}
		for i := range steps {
			left, right := storedSession.Steps[i], steps[i]
			if left.ID != right.ID || left.SequenceNo != right.SequenceNo || left.StepType != right.StepType ||
				!sameString(left.ExerciseKey, right.ExerciseKey) || !sameString(left.ExerciseName, right.ExerciseName) ||
				!sameRawJSON(left.Prescription, rawOrEmptyObject(right.Prescription)) || !sameString(left.Notes, right.Notes) {
				return false
			}
		}
	}
	return true
}

func (s PlannedSession) TitlePtr() *string {
	if s.Title == "" {
		return nil
	}
	return &s.Title
}

func (in PlannedSessionWrite) TitleOrDefault() string {
	if in.Title == "" {
		return "Workout"
	}
	return in.Title
}

func sameRawJSON(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return bytes.Equal(left, right)
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func isJSONObject(value []byte) bool {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(trimmed, &object) == nil
}

func sameEntries(left, right []StrengthEntry) bool {
	for i := range left {
		if left[i].EntryID != right[i].EntryID || left[i].SequenceNo != right[i].SequenceNo || left[i].EntryType != right[i].EntryType || !sameString(left[i].PlannedStepID, right[i].PlannedStepID) || !sameString(left[i].ExerciseKey, right[i].ExerciseKey) || !sameString(left[i].ExerciseName, right[i].ExerciseName) || !sameString(left[i].UserLabel, right[i].UserLabel) || !sameInt(left[i].SetNumber, right[i].SetNumber) || !sameFloat(left[i].Reps, right[i].Reps) || !sameFloat(left[i].DurationSeconds, right[i].DurationSeconds) || !sameFloat(left[i].LoadKG, right[i].LoadKG) || !sameString(left[i].LoadDescription, right[i].LoadDescription) || !sameBool(left[i].IsBodyweight, right[i].IsBodyweight) || !sameBool(left[i].ToFailure, right[i].ToFailure) || !sameFloat(left[i].RIR, right[i].RIR) || !sameFloat(left[i].RIRMin, right[i].RIRMin) || !sameFloat(left[i].RIRMax, right[i].RIRMax) || !sameString(left[i].Laterality, right[i].Laterality) || !sameString(left[i].Notes, right[i].Notes) || left[i].IsWarmup != right[i].IsWarmup || strings.Join(left[i].MuscleGroups, "\x00") != strings.Join(right[i].MuscleGroups, "\x00") {
			return false
		}
	}
	return true
}

func sameString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func sameFloat(left, right *float64) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func sameInt(left, right *int) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func sameBool(left, right *bool) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func compareWorkout(session PlannedSession, entries []StrengthEntry) *WorkoutComparison {
	comparison := &WorkoutComparison{Steps: []StepComparison{}}
	byStep := map[string][]StrengthEntry{}
	for _, entry := range entries {
		if entry.PlannedStepID != nil {
			byStep[*entry.PlannedStepID] = append(byStep[*entry.PlannedStepID], entry)
		}
	}
	for _, step := range session.Steps {
		value := StepComparison{PlannedStepID: step.ID, ExerciseKey: step.ExerciseKey, ExerciseName: step.ExerciseName}
		var prescription Prescription
		_ = json.Unmarshal(step.Prescription, &prescription)
		value.PlannedSets = len(prescription.Sets)
		for _, set := range prescription.Sets {
			if set.RepsMin != nil {
				value.PlannedReps = append(value.PlannedReps, *set.RepsMin)
			} else {
				value.PlannedReps = append(value.PlannedReps, 0)
			}
			if set.LoadKG != nil {
				value.PlannedLoadKG = append(value.PlannedLoadKG, *set.LoadKG)
			} else {
				value.PlannedLoadKG = append(value.PlannedLoadKG, 0)
			}
		}
		for _, entry := range byStep[step.ID] {
			value.ActualSets++
			if entry.Reps != nil {
				value.ActualReps = append(value.ActualReps, *entry.Reps)
			}
			if entry.LoadKG != nil {
				value.ActualLoadKG = append(value.ActualLoadKG, *entry.LoadKG)
			}
		}
		comparison.Steps = append(comparison.Steps, value)
	}
	return comparison
}

func sourceOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func rawOrEmptyObject(value json.RawMessage) []byte {
	if len(value) == 0 {
		return []byte("{}")
	}
	return value
}
func mustJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func millis(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UnixMilli()
}
func requireUUID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return errors.New("invalid_input: id must be UUID")
	}
	return nil
}
