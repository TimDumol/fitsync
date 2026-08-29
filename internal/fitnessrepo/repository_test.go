package fitnessrepo

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/timdumol/fitsync/internal/db"
)

const testAthlete = "00000000-0000-0000-0000-000000000002"

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func v7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func TestRepositoryNormalizedReadsAndAtomicWrites(t *testing.T) {
	database := testDB(t)
	_, err := database.Exec(`INSERT INTO garmin_records (id,athlete_id,dataset,record_key,record_date,payload_format,payload_raw,payload_sha256,ingested_at) VALUES (?,?,?,?,?,?,?,?,?)`, v7(t), testAthlete, "activities", "run-1", "2026-08-28", "test", `{"activityId":"run-1","activityType":{"typeKey":"trail_running"},"duration":2700,"distanceKm":6,"activityTrainingLoad":120,"averageHR":150}`, strings.Repeat("a", 64), 10)
	if err != nil {
		t.Fatal(err)
	}
	repository := New(database, func() time.Time { return time.UnixMilli(1000) })
	activities, err := repository.Activities(context.Background(), testAthlete, ActivityQuery{From: "2026-08-28", To: "2026-08-28", Order: "asc", Limit: 10})
	if err != nil || len(activities.Items) != 1 {
		t.Fatalf("activities = %#v, %v", activities, err)
	}
	if activities.Items[0].Category != "running" || activities.Items[0].DurationMinutes == nil || *activities.Items[0].DurationMinutes != 45 {
		t.Fatalf("normalized activity = %#v", activities.Items[0])
	}

	workoutID, entryID := v7(t), v7(t)
	reps, load := 5.0, 80.0
	win, err := repository.UpsertWorkout(context.Background(), testAthlete, WorkoutWrite{ID: workoutID, ClientUpdatedAt: 1, Date: "2026-08-28", Title: "Legs", WorkoutType: "strength", Source: "manual_mcp", Entries: []StrengthEntry{{EntryID: entryID, SequenceNo: 1, EntryType: "set", ExerciseKey: stringPointer("back_squat"), Reps: &reps, LoadKG: &load}}})
	if err != nil || !win.SubmittedVersionWon || win.Workout.Summary.WorkingSets != 1 {
		t.Fatalf("workout = %#v, %v", win, err)
	}
	strength, err := repository.StrengthWorkouts(context.Background(), testAthlete, StrengthQuery{From: "2026-08-28", To: "2026-08-28", IncludeEntries: true, Limit: 10})
	if err != nil || len(strength.Items) != 1 || strength.Items[0].Summary.VolumeLoadKG == nil || *strength.Items[0].Summary.VolumeLoadKG != 400 {
		t.Fatalf("strength = %#v, %v", strength, err)
	}
	stale, err := repository.UpsertWorkout(context.Background(), testAthlete, WorkoutWrite{ID: workoutID, ClientUpdatedAt: 1, Date: "2026-08-28", Title: "stale", WorkoutType: "strength", Source: "manual_mcp"})
	if err != nil || stale.SubmittedVersionWon || stale.Workout.Title != "Legs" {
		t.Fatalf("stale workout = %#v, %v", stale, err)
	}

	profile, err := repository.UpsertProfile(context.Background(), testAthlete, ProfileWrite{ClientUpdatedAt: 2, Profile: Profile{Timezone: "Europe/Madrid", Units: "metric"}})
	if err != nil || !profile.SubmittedVersionWon || profile.Profile.Timezone != "Europe/Madrid" {
		t.Fatalf("profile = %#v, %v", profile, err)
	}
	checkinID := v7(t)
	energy := 4
	if _, err := repository.UpsertCheckin(context.Background(), testAthlete, CheckinWrite{ID: checkinID, ClientUpdatedAt: 3, RecordedAt: time.Date(2026, 8, 28, 7, 30, 0, 0, time.FixedZone("CEST", 7200)), Energy: &energy, Illness: Illness{Present: false}}); err != nil {
		t.Fatal(err)
	}
	checkins, err := repository.Checkins(context.Background(), testAthlete, CheckinQuery{From: "2026-08-28", To: "2026-08-28", Limit: 10})
	if err != nil || len(checkins.Items) != 1 || checkins.Items[0].Energy == nil || *checkins.Items[0].Energy != 4 {
		t.Fatalf("checkins = %#v, %v", checkins, err)
	}
}

func stringPointer(value string) *string { return &value }
