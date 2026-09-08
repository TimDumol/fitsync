package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/timdumol/fitsync/migrations"
)

func TestMigrateAndSeed(t *testing.T) {
	database, err := OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var athletes int
	if err := database.QueryRow("SELECT count(*) FROM athletes").Scan(&athletes); err != nil {
		t.Fatal(err)
	}
	if athletes != 2 {
		t.Fatalf("seeded athletes = %d, want 2", athletes)
	}
	var journalMode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	var foreignKeys int
	if err := database.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fitness.db")
	database, err := OpenAndMigrate(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = OpenAndMigrate(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
}

func TestTrainingContextMigrationDownAndUp(t *testing.T) {
	database := testDB(t)
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownContext(context.Background(), database, "."); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(context.Background(), database, "."); err != nil {
		t.Fatal(err)
	}
	var timezone string
	if err := database.QueryRow("SELECT timezone FROM athletes WHERE handle='tim'").Scan(&timezone); err != nil {
		t.Fatal(err)
	}
	if timezone != "UTC" {
		t.Fatalf("timezone = %q, want UTC", timezone)
	}
}

func TestPlannedStepGroupsMigrationPreservesLegacyPlanGraph(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(context.Background(), database, ".", 4); err != nil {
		t.Fatal(err)
	}
	planID := "00000000-0000-7000-8000-000000000010"
	sessionID := "00000000-0000-7000-8000-000000000011"
	stepID := "00000000-0000-7000-8000-000000000012"
	athleteID := "00000000-0000-0000-0000-000000000002"
	if _, err := database.Exec(`INSERT INTO training_plans
		(id, athlete_id, title, goal_raw, active, client_updated_at, server_synced_at)
		VALUES (?, ?, 'Legacy plan', '{}', 0, 1, 1)`, planID, athleteID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO planned_sessions
		(id, plan_id, athlete_id, session_date, sport, session_type, priority, targets_raw, status, client_updated_at, server_synced_at)
		VALUES (?, ?, ?, '2026-09-07', 'strength', 'full_body', 'normal', '{}', 'planned', 1, 1)`, sessionID, planID, athleteID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO planned_session_steps
		(id, planned_session_id, athlete_id, sequence_no, step_type, prescription_raw, created_at)
		VALUES (?, ?, ?, 1, 'exercise', '{"sets":[{"reps_min":8}]}', 1)`, stepID, sessionID, athleteID); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(context.Background(), database, "."); err != nil {
		t.Fatal(err)
	}
	var gotPlan, gotSession, gotStep, gotGroups int
	if err := database.QueryRow("SELECT count(*) FROM training_plans WHERE id=?", planID).Scan(&gotPlan); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT count(*) FROM planned_sessions WHERE id=?", sessionID).Scan(&gotSession); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT count(*) FROM planned_session_steps WHERE id=?", stepID).Scan(&gotStep); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT count(*) FROM planned_step_groups").Scan(&gotGroups); err != nil {
		t.Fatal(err)
	}
	if gotPlan != 1 || gotSession != 1 || gotStep != 1 || gotGroups != 0 {
		t.Fatalf("legacy graph after migration = plan %d, session %d, step %d, groups %d", gotPlan, gotSession, gotStep, gotGroups)
	}
}

func TestWorkoutAppMigrationBackfillsSequenceCursor(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(context.Background(), database, ".", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO training_plans(id,athlete_id,title,goal_raw,active,client_updated_at,server_synced_at) VALUES('00000000-0000-7000-8000-000000000010','00000000-0000-0000-0000-000000000002','Legacy plan','{}',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO workout_sessions(id,athlete_id,workout_date,title,source,metadata_raw,client_updated_at,server_synced_at,created_at) VALUES('00000000-0000-7000-8000-000000000011','00000000-0000-0000-0000-000000000002','2026-08-29','Legacy workout','mobile','{}',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpContext(context.Background(), database, "."); err != nil {
		t.Fatal(err)
	}
	var planSeq, workoutSeq, clock int64
	if err := database.QueryRow("SELECT sync_seq FROM training_plans WHERE id='00000000-0000-7000-8000-000000000010'").Scan(&planSeq); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT sync_seq FROM workout_sessions WHERE id='00000000-0000-7000-8000-000000000011'").Scan(&workoutSeq); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT value FROM sync_clock WHERE id=1").Scan(&clock); err != nil {
		t.Fatal(err)
	}
	if planSeq <= 0 || workoutSeq <= planSeq || clock != workoutSeq {
		t.Fatalf("backfilled sequences = plan %d, workout %d, clock %d", planSeq, workoutSeq, clock)
	}
}

func TestGarminConstraintsAndGeneratedSleepScores(t *testing.T) {
	database := testDB(t)
	athlete := "00000000-0000-0000-0000-000000000002"
	insert := func(id, payload string, offset any) error {
		_, err := database.Exec(`INSERT INTO garmin_records
			(id, athlete_id, dataset, record_key, record_date, tz_offset_seconds, payload_format, payload_raw, payload_sha256, ingested_at)
			VALUES (?, ?, 'synthetic', ?, '2026-01-02', ?, 'test', ?, ?, 1)`,
			id, athlete, id, offset, payload, fmt.Sprintf("%064d", 1))
		return err
	}
	if err := insert("score-a", `{"sleep":{"dailySleepDTO":{"sleepScores":{"overall":{"value":91}}}}}`, nil); err != nil {
		t.Fatal(err)
	}
	if err := insert("score-b", `{"sleep":{"dailySleepDTO":{"sleepScore":82}}}`, nil); err != nil {
		t.Fatal(err)
	}
	if err := insert("score-c", `{"sleep_score":73}`, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query("SELECT sleep_score FROM garmin_records WHERE id LIKE 'score-%' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []float64
	for rows.Next() {
		var score sql.NullFloat64
		if err := rows.Scan(&score); err != nil {
			t.Fatal(err)
		}
		got = append(got, score.Float64)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []float64{91, 82, 73}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sleep score %d = %v, want %v", i, got[i], want[i])
		}
	}
	if err := insert("bad-json", `{not-json}`, nil); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
	if err := insert("bad-offset", `{}`, 64801); err == nil {
		t.Fatal("invalid timezone offset was accepted")
	}
	if err := insert("duplicate", `{}`, nil); err != nil {
		t.Fatal(err)
	}
	if err := insert("duplicate", `{}`, nil); err == nil {
		t.Fatal("duplicate Garmin key was accepted")
	}
}

func TestIdentityUniquenessAndForeignKeys(t *testing.T) {
	database := testDB(t)
	athlete := "00000000-0000-0000-0000-000000000002"
	args := []any{"identity-a", athlete, "issuer", "subject", 1, 1}
	if _, err := database.Exec(`INSERT INTO athlete_identities
		(id, athlete_id, issuer, subject, approved_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`, args...); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO athlete_identities
		(id, athlete_id, issuer, subject, approved_at, created_at) VALUES ('identity-b', ?, 'issuer', 'subject', 1, 1)`, athlete); err == nil {
		t.Fatal("duplicate identity was accepted")
	}
	if _, err := database.Exec(`INSERT INTO garmin_records
		(id, athlete_id, dataset, record_key, record_date, payload_format, payload_raw, payload_sha256, ingested_at)
		VALUES ('orphan', '00000000-0000-0000-0000-000000000099', 'x', 'x', '2026-01-01', 'test', '{}', ?, 1)`, fmt.Sprintf("%064d", 1)); err == nil {
		t.Fatal("orphan Garmin row was accepted")
	}
	if _, err := database.Exec(`INSERT INTO identity_audit_log
		(id, athlete_id, identity_id, event, occurred_at) VALUES ('audit-orphan', ?, 'identity-a', 'approve', 1)`, "00000000-0000-0000-0000-000000000099"); err == nil {
		t.Fatal("orphan audit row was accepted")
	}
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}
