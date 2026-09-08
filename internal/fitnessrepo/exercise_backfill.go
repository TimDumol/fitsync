package fitnessrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/timdumol/fitsync/internal/exercisecatalog"
)

// ExerciseBackfillReport deliberately contains aggregate data only. Exercise
// names and coaching text are not emitted by the maintenance command.
type ExerciseBackfillReport struct {
	BackupPath              string         `json:"backup_path,omitempty"`
	PlannedRows             int            `json:"planned_rows"`
	WorkoutRows             int            `json:"workout_rows"`
	ExactByKey              map[string]int `json:"exact_by_key"`
	UnknownNameRows         int            `json:"unknown_name_rows"`
	SuggestedRows           int            `json:"suggested_rows"`
	AmbiguousSuggestionRows int            `json:"ambiguous_suggestion_rows"`
	AppliedPlannedRows      int            `json:"applied_planned_rows,omitempty"`
	AppliedWorkoutRows      int            `json:"applied_workout_rows,omitempty"`
	ChangedActivePlans      int            `json:"changed_active_plans,omitempty"`
	ChangedWorkouts         int            `json:"changed_workouts,omitempty"`
}

type backfillPlannedRow struct {
	id, name, planID, planStatus string
	key                          string
}

type backfillWorkoutRow struct {
	id, sessionID, payload, key string
}

// BackfillExerciseIdentities evaluates null-keyed, non-deleted planned
// exercise steps and strength entries. When apply is true, it backs up the
// live database first, then applies only exact reviewed aliases atomically.
func BackfillExerciseIdentities(ctx context.Context, database *sql.DB, apply bool, backupPath string) (ExerciseBackfillReport, error) {
	if database == nil {
		return ExerciseBackfillReport{}, errors.New("invalid_input: database is required")
	}
	if apply {
		if strings.TrimSpace(backupPath) == "" || !filepath.IsAbs(backupPath) {
			return ExerciseBackfillReport{}, errors.New("invalid_input: --backup must be an absolute path when applying")
		}
		if err := onlineBackup(ctx, database, backupPath); err != nil {
			return ExerciseBackfillReport{}, err
		}
	}
	if !apply {
		return collectBackfill(ctx, database)
	}

	conn, err := database.Conn(ctx)
	if err != nil {
		return ExerciseBackfillReport{}, fmt.Errorf("internal_error: backfill connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return ExerciseBackfillReport{}, fmt.Errorf("conflict: could not lock database for backfill: %w", err)
	}
	rollback := func(cause error) (ExerciseBackfillReport, error) {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		return ExerciseBackfillReport{}, cause
	}
	planned, workouts, report, err := collectBackfillRows(ctx, conn)
	if err != nil {
		return rollback(err)
	}
	report.BackupPath = backupPath
	now := time.Now().UnixMilli()
	planIDs := map[string]bool{}
	workoutIDs := map[string]bool{}
	for _, row := range planned {
		if row.key == "" {
			continue
		}
		result, err := conn.ExecContext(ctx, "UPDATE planned_session_steps SET exercise_key=? WHERE id=? AND exercise_key IS NULL AND exercise_name=? AND deleted_at IS NULL", row.key, row.id, row.name)
		if err != nil {
			return rollback(fmt.Errorf("internal_error: planned step backfill: %w", err))
		}
		count, _ := result.RowsAffected()
		report.AppliedPlannedRows += int(count)
		if count > 0 && row.planStatus == "active" {
			planIDs[row.planID] = true
		}
	}
	for _, row := range workouts {
		if row.key == "" {
			continue
		}
		payload, err := payloadWithExerciseKey(row.payload, row.key)
		if err != nil {
			return rollback(err)
		}
		result, err := conn.ExecContext(ctx, "UPDATE workout_entries SET payload_raw=?,server_synced_at=? WHERE id=? AND entry_type='set' AND deleted_at IS NULL", payload, now, row.id)
		if err != nil {
			return rollback(fmt.Errorf("internal_error: workout entry backfill: %w", err))
		}
		count, _ := result.RowsAffected()
		report.AppliedWorkoutRows += int(count)
		if count > 0 {
			workoutIDs[row.sessionID] = true
		}
	}
	planList := sortedKeys(planIDs)
	for _, planID := range planList {
		sequence, err := allocateBackfillSequence(ctx, conn)
		if err != nil {
			return rollback(err)
		}
		if _, err := conn.ExecContext(ctx, "UPDATE training_plans SET sync_seq=? WHERE id=? AND status='active' AND deleted_at IS NULL", sequence, planID); err != nil {
			return rollback(fmt.Errorf("internal_error: active plan sync cursor: %w", err))
		}
	}
	workoutList := sortedKeys(workoutIDs)
	for _, workoutID := range workoutList {
		sequence, err := allocateBackfillSequence(ctx, conn)
		if err != nil {
			return rollback(err)
		}
		if _, err := conn.ExecContext(ctx, "UPDATE workout_sessions SET sync_seq=? WHERE id=? AND deleted_at IS NULL", sequence, workoutID); err != nil {
			return rollback(fmt.Errorf("internal_error: workout sync cursor: %w", err))
		}
	}
	report.ChangedActivePlans = len(planList)
	report.ChangedWorkouts = len(workoutList)
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return ExerciseBackfillReport{}, fmt.Errorf("internal_error: backfill commit: %w", err)
	}
	return report, nil
}

type backfillQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func collectBackfill(ctx context.Context, database *sql.DB) (ExerciseBackfillReport, error) {
	_, _, report, err := collectBackfillRows(ctx, database)
	return report, err
}

func collectBackfillRows(ctx context.Context, queryer backfillQueryer) ([]backfillPlannedRow, []backfillWorkoutRow, ExerciseBackfillReport, error) {
	report := ExerciseBackfillReport{ExactByKey: map[string]int{}}
	planned := []backfillPlannedRow{}
	rows, err := queryer.QueryContext(ctx, `SELECT s.id,s.exercise_name,p.id,p.status
		FROM planned_session_steps s
		JOIN planned_sessions ps ON ps.id=s.planned_session_id
		JOIN training_plans p ON p.id=ps.plan_id
		WHERE s.exercise_key IS NULL AND s.exercise_name IS NOT NULL AND trim(s.exercise_name)<>'' AND s.deleted_at IS NULL`)
	if err != nil {
		return nil, nil, report, fmt.Errorf("internal_error: planned backfill query: %w", err)
	}
	for rows.Next() {
		var row backfillPlannedRow
		if err := rows.Scan(&row.id, &row.name, &row.planID, &row.planStatus); err != nil {
			rows.Close()
			return nil, nil, report, fmt.Errorf("internal_error: planned backfill row: %w", err)
		}
		report.PlannedRows++
		row.key = classifyBackfillName(&report, row.name)
		planned = append(planned, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, report, fmt.Errorf("internal_error: planned backfill rows: %w", err)
	}
	rows.Close()

	workouts := []backfillWorkoutRow{}
	rows, err = queryer.QueryContext(ctx, `SELECT id,session_id,payload_raw
		FROM workout_entries
		WHERE entry_type='set' AND deleted_at IS NULL`)
	if err != nil {
		return nil, nil, report, fmt.Errorf("internal_error: workout backfill query: %w", err)
	}
	for rows.Next() {
		var id, sessionID, payload string
		if err := rows.Scan(&id, &sessionID, &payload); err != nil {
			rows.Close()
			return nil, nil, report, fmt.Errorf("internal_error: workout backfill row: %w", err)
		}
		key, name, eligible, err := rawBackfillIdentity(payload)
		if err != nil {
			rows.Close()
			return nil, nil, report, err
		}
		if !eligible || key != "" || name == "" {
			continue
		}
		report.WorkoutRows++
		resolved := classifyBackfillName(&report, name)
		workouts = append(workouts, backfillWorkoutRow{id: id, sessionID: sessionID, payload: payload, key: resolved})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, report, fmt.Errorf("internal_error: workout backfill rows: %w", err)
	}
	rows.Close()
	return planned, workouts, report, nil
}

func classifyBackfillName(report *ExerciseBackfillReport, name string) string {
	resolution, err := exercisecatalog.Resolve(nil, &name)
	if err == nil && resolution.Key != nil {
		report.ExactByKey[*resolution.Key]++
		return *resolution.Key
	}
	candidates := exercisecatalog.Search(name, 3)
	report.UnknownNameRows++
	if len(candidates) > 0 {
		report.SuggestedRows++
	}
	if len(candidates) > 1 {
		report.AmbiguousSuggestionRows++
	}
	return ""
}

func rawBackfillIdentity(raw string) (key, name string, eligible bool, err error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", "", false, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload == nil {
		return "", "", false, errors.New("internal_error: workout entry payload is not an object")
	}
	key, _, err = rawPayloadString(payload, "exercise_key")
	if err != nil {
		return "", "", false, errors.New("internal_error: workout exercise_key is not a string")
	}
	var namePresent bool
	name, namePresent, err = rawPayloadString(payload, "exercise_name")
	if err != nil {
		return "", "", false, errors.New("internal_error: workout exercise_name is not a string")
	}
	return key, name, namePresent, nil
}

func rawPayloadString(payload map[string]json.RawMessage, field string) (string, bool, error) {
	raw, ok := payload[field]
	if !ok || string(raw) == "null" {
		return "", ok, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, err
	}
	return value, true, nil
}

func payloadWithExerciseKey(raw, key string) (string, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload == nil {
		return "", errors.New("internal_error: workout entry payload is not an object")
	}
	payload["exercise_key"], _ = json.Marshal(key)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("internal_error: encode workout entry payload: %w", err)
	}
	return string(encoded), nil
}

func onlineBackup(ctx context.Context, database *sql.DB, path string) error {
	if _, err := os.Stat(path); err == nil {
		return errors.New("conflict: backup path already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("invalid_input: backup path: %w", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return fmt.Errorf("invalid_input: backup directory: %w", err)
	}
	quoted := strings.ReplaceAll(path, "'", "''")
	if _, err := database.ExecContext(ctx, "VACUUM INTO '"+quoted+"'"); err != nil {
		return fmt.Errorf("internal_error: online database backup: %w", err)
	}
	return nil
}

func allocateBackfillSequence(ctx context.Context, conn interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int64, error) {
	var sequence int64
	if err := conn.QueryRowContext(ctx, "UPDATE sync_clock SET value=value+1 WHERE id=1 RETURNING value").Scan(&sequence); err != nil {
		return 0, errors.New("internal_error: sync sequence allocation")
	}
	return sequence, nil
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
