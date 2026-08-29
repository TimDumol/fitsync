package importer

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/timdumol/fitsync/internal/db"
)

func TestLoadAndInsertSyntheticDumpIsIdempotent(t *testing.T) {
	dump, err := Load(filepath.Join("testdata", "synthetic-neon"))
	if err != nil {
		t.Fatal(err)
	}
	if dump.Counts.GarminRecords != 2 || dump.Counts.WorkoutSessions != 1 || dump.Counts.WorkoutEntries != 1 {
		t.Fatalf("unexpected fixture counts: %+v", dump.Counts)
	}
	if dump.Records[1].RecordDate == nil || *dump.Records[1].RecordDate != "2026-01-02" {
		t.Fatal("activity_details date was not derived from its matching activity")
	}

	database := testDB(t)
	if _, err := Insert(context.Background(), database, dump, "tim"); err != nil {
		t.Fatal(err)
	}
	if _, err := Insert(context.Background(), database, dump, "tim"); err != nil {
		t.Fatal(err)
	}
	var records, sessions, entries int
	if err := database.QueryRow("SELECT count(*) FROM garmin_records").Scan(&records); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT count(*) FROM workout_sessions").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT count(*) FROM workout_entries").Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if records != 2 || sessions != 1 || entries != 1 {
		t.Fatalf("counts after repeated import = %d/%d/%d", records, sessions, entries)
	}
	var entryID string
	if err := database.QueryRow("SELECT id FROM workout_entries").Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	if entryID != "42" {
		t.Fatalf("legacy entry id = %q, want 42", entryID)
	}
}

func TestLoadRejectsUnmatchedActivityDetails(t *testing.T) {
	if _, err := Load(filepath.Join("testdata", "unmatched-neon")); err == nil {
		t.Fatal("unmatched activity_details was accepted")
	}
}

func TestInsertRollsBackOnConstraintFailure(t *testing.T) {
	dump, err := Load(filepath.Join("testdata", "synthetic-neon"))
	if err != nil {
		t.Fatal(err)
	}
	database := testDB(t)
	_, err = database.Exec(`INSERT INTO workout_sessions
		(id, athlete_id, workout_date, title, source, source_ref, metadata_raw, client_updated_at, server_synced_at, created_at)
		VALUES ('legacy-workout-1', '00000000-0000-0000-0000-000000000002', '2026-01-01', 'before', 'manual', 'fixture', '{}', 1, 1, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO workout_entries
		(id, session_id, athlete_id, sequence_no, entry_type, payload_raw, client_updated_at, server_synced_at)
		VALUES ('existing-entry', 'legacy-workout-1', '00000000-0000-0000-0000-000000000002', 1, 'set', '{}', 1, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Insert(context.Background(), database, dump, "tim"); err == nil {
		t.Fatal("import unexpectedly succeeded despite ordering conflict")
	}
	var title string
	if err := database.QueryRow("SELECT title FROM workout_sessions WHERE id = 'legacy-workout-1'").Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "before" {
		t.Fatalf("rolled-back session title = %q", title)
	}
	var records int
	if err := database.QueryRow("SELECT count(*) FROM garmin_records").Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("rolled-back Garmin rows = %d", records)
	}
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}
