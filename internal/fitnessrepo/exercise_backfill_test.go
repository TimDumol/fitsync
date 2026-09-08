package fitnessrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackfillExerciseIdentitiesAppliesReviewedAliasesAtomically(t *testing.T) {
	database := testDB(t)
	planID, plannedSessionID, stepID := v7(t), v7(t), v7(t)
	workoutID, entryID := v7(t), v7(t)
	if _, err := database.Exec(`INSERT INTO training_plans
		(id,athlete_id,title,goal_raw,active,status,client_updated_at,server_synced_at)
		VALUES(?,?,?,?,1,'active',1,1)`, planID, testAthlete, "Backfill plan", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO planned_sessions
		(id,plan_id,athlete_id,session_date,sport,session_type,priority,targets_raw,status,client_updated_at,server_synced_at)
		VALUES(?,?,?,?,?,?,?,?,'planned',1,1)`, plannedSessionID, planID, testAthlete, "2026-09-05", "strength", "upper", "normal", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO planned_session_steps
		(id,planned_session_id,athlete_id,sequence_no,step_type,exercise_name,prescription_raw,created_at)
		VALUES(?,?,?,?,?,?,?,1)`, stepID, plannedSessionID, testAthlete, 1, "exercise", "Bench Press", "{} "); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO workout_sessions
		(id,athlete_id,workout_date,title,source,metadata_raw,client_updated_at,server_synced_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,1)`, workoutID, testAthlete, "2026-09-05", "Backfill workout", "manual", "{}", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO workout_entries
		(id,session_id,athlete_id,sequence_no,entry_type,payload_raw,client_updated_at,server_synced_at)
		VALUES(?,?,?,?,?,?,?,1)`, entryID, workoutID, testAthlete, 1, "set", `{"exercise_name":"Dumbbell Row","notes":"preserve"}`, 1); err != nil {
		t.Fatal(err)
	}

	report, err := BackfillExerciseIdentities(context.Background(), database, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.PlannedRows != 1 || report.WorkoutRows != 1 || report.ExactByKey["bench_press"] != 1 || report.ExactByKey["one_arm_dumbbell_row"] != 1 {
		t.Fatalf("dry-run report = %#v", report)
	}

	backupPath := filepath.Join(t.TempDir(), "fitness-backup.db")
	report, err = BackfillExerciseIdentities(context.Background(), database, true, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.AppliedPlannedRows != 1 || report.AppliedWorkoutRows != 1 || report.ChangedActivePlans != 1 || report.ChangedWorkouts != 1 {
		t.Fatalf("apply report = %#v", report)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup stat: %v", err)
	}

	var planKey, workoutPayload string
	if err := database.QueryRow("SELECT exercise_key FROM planned_session_steps WHERE id=?", stepID).Scan(&planKey); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT payload_raw FROM workout_entries WHERE id=?", entryID).Scan(&workoutPayload); err != nil {
		t.Fatal(err)
	}
	if planKey != "bench_press" || !strings.Contains(workoutPayload, `"exercise_key":"one_arm_dumbbell_row"`) || !strings.Contains(workoutPayload, `"notes":"preserve"`) {
		t.Fatalf("backfilled values = %q, %q", planKey, workoutPayload)
	}

	report, err = BackfillExerciseIdentities(context.Background(), database, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.PlannedRows != 0 || report.WorkoutRows != 0 {
		t.Fatalf("post-apply dry-run report = %#v", report)
	}
}
