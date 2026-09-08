package fitnessrepo

import (
	"context"
	"database/sql"
	"fmt"
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
	_, err := database.Exec(`INSERT INTO garmin_records (id,athlete_id,dataset,record_key,record_date,payload_format,payload_raw,payload_sha256,ingested_at) VALUES (?,?,?,?,?,?,?,?,?)`, v7(t), testAthlete, "activities", "run-1", "2026-08-28", "test", `{"activityId":"run-1","activityType":{"typeKey":"trail_running"},"duration":2700,"distanceKm":6,"activityTrainingLoad":120,"averageHR":150,"averageRunningCadenceInStepsPerMinute":170,"averagePower":250,"avgTemperature":20,"hrZone1Minutes":5,"isPR":true,"manufacturer":"Garmin"}`, strings.Repeat("a", 64), 10)
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
	compact, err := repository.Activities(context.Background(), testAthlete, ActivityQuery{From: "2026-08-28", To: "2026-08-28", Detail: "compact", Order: "asc", Limit: 10})
	if err != nil || compact.Items[0].AverageHR != nil || compact.Items[0].TrainingLoad == nil {
		t.Fatalf("compact activity = %#v, %v", compact, err)
	}
	summary, err := repository.Activities(context.Background(), testAthlete, ActivityQuery{From: "2026-08-28", To: "2026-08-28", Detail: "summary", Order: "asc", Limit: 10})
	if err != nil || summary.Items[0].AverageHR == nil || summary.Items[0].Cadence != nil {
		t.Fatalf("summary activity = %#v, %v", summary, err)
	}
	full, err := repository.Activities(context.Background(), testAthlete, ActivityQuery{From: "2026-08-28", To: "2026-08-28", Detail: "full", Order: "asc", Limit: 10})
	if err != nil || full.Items[0].Cadence == nil || full.Items[0].PersonalRecord == nil {
		t.Fatalf("full activity = %#v, %v", full, err)
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
	staleDelete, err := repository.DeleteWorkout(context.Background(), testAthlete, workoutID, 1)
	if err != nil || staleDelete.Deleted || staleDelete.SubmittedVersionWon {
		t.Fatalf("stale delete = %#v, %v", staleDelete, err)
	}
	deleted, err := repository.DeleteWorkout(context.Background(), testAthlete, workoutID, 2)
	if err != nil || !deleted.Deleted || !deleted.SubmittedVersionWon {
		t.Fatalf("deleted workout = %#v, %v", deleted, err)
	}
	repeatedDelete, err := repository.DeleteWorkout(context.Background(), testAthlete, workoutID, 2)
	if err != nil || !repeatedDelete.Deleted || repeatedDelete.SubmittedVersionWon {
		t.Fatalf("repeated delete = %#v, %v", repeatedDelete, err)
	}
}

func TestTrainingContextUsesRequestedSectionsAndFullLoadHistory(t *testing.T) {
	database := testDB(t)
	insertActivity := func(key, date string, load float64) {
		t.Helper()
		payload := `{"activityId":"` + key + `","activityType":{"typeKey":"running"},"duration":1800,"distanceKm":5,"activityTrainingLoad":` + strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", load), "0"), ".") + `}`
		_, err := database.Exec(`INSERT INTO garmin_records (id,athlete_id,dataset,record_key,record_date,payload_format,payload_raw,payload_sha256,ingested_at) VALUES (?,?,?,?,?,?,?,?,?)`, v7(t), testAthlete, "activities", key, date, "test", payload, strings.Repeat("b", 64), int64(load))
		if err != nil {
			t.Fatal(err)
		}
	}
	insertActivity("load-last", "2026-08-27", 10)
	insertActivity("load-previous", "2026-08-20", 20)
	insertActivity("load-old-a", "2026-08-10", 30)
	insertActivity("load-old-b", "2026-08-01", 40)
	repository := New(database, func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) })
	loadContext, err := repository.TrainingContext(context.Background(), testAthlete, "2026-08-28", 7, []string{"training_load"})
	if err != nil || loadContext.CurrentStatus.TrainingLoad == nil || loadContext.CurrentStatus.TrainingLoad.Last28Days == nil || *loadContext.CurrentStatus.TrainingLoad.Last28Days != 100 {
		t.Fatalf("load context = %#v, %v", loadContext, err)
	}
	if loadContext.CurrentStatus.Recovery != nil || loadContext.Profile != nil || loadContext.RecentActivities != nil || loadContext.PlannedSessions != nil {
		t.Fatalf("unrequested sections leaked = %#v", loadContext)
	}

	recoveryContext, err := repository.TrainingContext(context.Background(), testAthlete, "2026-08-28", 7, []string{"recovery"})
	if err != nil || recoveryContext.CurrentStatus.Recovery == nil || recoveryContext.CurrentStatus.TrainingLoad != nil || recoveryContext.Profile != nil || recoveryContext.RecentActivities != nil {
		t.Fatalf("recovery context = %#v, %v", recoveryContext, err)
	}
}

func TestTrainingAnalysisCategoryFilterExcludesStrength(t *testing.T) {
	database := testDB(t)
	repository := New(database, func() time.Time { return time.UnixMilli(1000) })
	workoutID, entryID := v7(t), v7(t)
	reps, load := 5.0, 80.0
	if _, err := repository.UpsertWorkout(context.Background(), testAthlete, WorkoutWrite{ID: workoutID, ClientUpdatedAt: 1, Date: "2026-08-28", Title: "Legs", WorkoutType: "strength", Source: "manual_mcp", Entries: []StrengthEntry{{EntryID: entryID, SequenceNo: 1, EntryType: "set", ExerciseKey: stringPointer("back_squat"), Reps: &reps, LoadKG: &load}}}); err != nil {
		t.Fatal(err)
	}
	value, err := repository.TrainingAnalysis(context.Background(), testAthlete, AnalysisQuery{From: "2026-08-28", To: "2026-08-28", Categories: []string{"running"}})
	if err != nil || value.Current.Strength.Sessions != 0 || value.Current.Strength.WorkingSets != 0 {
		t.Fatalf("filtered analysis = %#v, %v", value, err)
	}
}

func TestActivityDetailWarningsIdentifyEmptySections(t *testing.T) {
	database := testDB(t)
	activityID := "activity-with-empty-detail"
	if _, err := database.Exec(`INSERT INTO garmin_records (id,athlete_id,dataset,record_key,record_date,payload_format,payload_raw,payload_sha256,ingested_at) VALUES (?,?,?,?,?,?,?,?,?)`, v7(t), testAthlete, "activities", activityID, "2026-08-28", "test", `{"activityId":"activity-with-empty-detail","activityType":{"typeKey":"running"}}`, strings.Repeat("c", 64), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO garmin_records (id,athlete_id,dataset,record_key,record_date,payload_format,payload_raw,payload_sha256,ingested_at) VALUES (?,?,?,?,?,?,?,?,?)`, v7(t), testAthlete, "activity_details", activityID, "2026-08-28", "test", `{"activity_id":"activity-with-empty-detail","splits":[],"heart_rate_zones":[],"metric_time_series":[]}`, strings.Repeat("d", 64), 2); err != nil {
		t.Fatal(err)
	}
	repository := New(database, func() time.Time { return time.UnixMilli(1000) })
	value, err := repository.Activity(context.Background(), testAthlete, activityID, []string{"summary", "splits", "zones", "metrics"}, 30, 500)
	if err != nil || value.Detail == nil || len(value.Warnings) != 3 {
		t.Fatalf("empty detail = %#v, %v", value, err)
	}
	seen := map[string]bool{}
	for _, warning := range value.Warnings {
		seen[warning.Code] = true
	}
	for _, code := range []string{"MISSING_ACTIVITY_SPLITS", "MISSING_ACTIVITY_ZONES", "MISSING_ACTIVITY_METRICS"} {
		if !seen[code] {
			t.Fatalf("warnings = %#v", value.Warnings)
		}
	}
}

func TestStrengthSemanticsAndPatchAreExplicitAndAtomic(t *testing.T) {
	database := testDB(t)
	repository := New(database, func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) })
	workoutID, entryID := v7(t), v7(t)
	reps, perLimb, load := 12.0, true, 15.0
	laterality, basis := "unilateral", "per_limb"
	value, err := repository.UpsertWorkout(context.Background(), testAthlete, WorkoutWrite{ID: workoutID, ClientUpdatedAt: 1, Date: "2026-09-04", Title: "Pull", WorkoutType: "strength", Source: "mobile", Entries: []StrengthEntry{{EntryID: entryID, SequenceNo: 1, EntryType: "set", ExerciseKey: stringPointer("one_arm_dumbbell_row"), Reps: &reps, LoadKG: &load, Laterality: &laterality, RepsPerSide: &perLimb, LoadBasis: &basis, LoadPerLimbKG: &load}}})
	if err != nil || value.Workout.Summary.NormalizedVolumeLoadKG == nil || *value.Workout.Summary.NormalizedVolumeLoadKG != 360 || value.Workout.Entries[0].NormalizedReps == nil || *value.Workout.Entries[0].NormalizedReps != 24 {
		t.Fatalf("explicit strength = %#v, %v", value, err)
	}
	if got := value.Workout.Summary.MuscleVolume["back"]; got.DirectSets != 1 || got.IndirectSets != 0 || got.WeightedSets != 1 {
		t.Fatalf("direct muscle volume = %#v", value.Workout.Summary.MuscleVolume)
	}
	if got := value.Workout.Summary.MuscleVolume["biceps"]; got.IndirectSets != 1 || got.WeightedSets != 0.5 {
		t.Fatalf("indirect muscle volume = %#v", value.Workout.Summary.MuscleVolume)
	}

	if _, err := repository.PatchWorkout(context.Background(), testAthlete, workoutID, value.Workout.SyncSeq, 2, []WorkoutPatchOperation{{Op: "set_entry_rir", EntryID: &entryID, RIR: testFloatPointer(2)}, {Op: "set_entry_rir", EntryID: stringPointer("missing"), RIR: testFloatPointer(1)}}); err == nil {
		t.Fatal("invalid patch committed")
	}
	unchanged, err := repository.Workout(context.Background(), testAthlete, workoutID)
	if err != nil || unchanged.Entries[0].RIR != nil {
		t.Fatalf("atomic rollback = %#v, %v", unchanged, err)
	}
	patched, err := repository.PatchWorkout(context.Background(), testAthlete, workoutID, value.Workout.SyncSeq, 2, []WorkoutPatchOperation{{Op: "set_entry_rir", EntryID: &entryID, RIR: testFloatPointer(2)}})
	if err != nil || patched.Workout.Workout.Entries[0].RIR == nil || *patched.Workout.Workout.Entries[0].RIR != 2 {
		t.Fatalf("patched workout = %#v, %v", patched, err)
	}
}

func TestDailyHealthReturnsStableSignalsAndFreshness(t *testing.T) {
	database := testDB(t)
	repository := New(database, func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) })
	if _, err := repository.UpsertProfile(context.Background(), testAthlete, ProfileWrite{ClientUpdatedAt: 1, Profile: Profile{Timezone: "Europe/Madrid", Units: "metric"}}); err != nil {
		t.Fatal(err)
	}
	insert := func(dataset, key, date, raw string, ingested int64) {
		t.Helper()
		if _, err := database.Exec(`INSERT INTO garmin_records (id,athlete_id,dataset,record_key,record_date,payload_format,payload_raw,payload_sha256,ingested_at) VALUES (?,?,?,?,?,?,?,?,?)`, v7(t), testAthlete, dataset, key, date, "test", raw, strings.Repeat("e", 64), ingested); err != nil {
			t.Fatal(err)
		}
	}
	insert("sleep", "sleep-2026-09-04", "2026-09-04", `{"sleep":{"dailySleepDTO":{"sleepTimeSeconds":22608,"napTimeSeconds":1800,"sleepScores":{"overall":{"value":59}}}}}`, 1234)
	insert("body_battery_events", "bb-2026-09-04", "2026-09-04", `{"events":[{"eventType":"nap","eventStartTimeLocal":"2026-09-04T13:00:00+02:00","eventEndTimeLocal":"2026-09-04T13:30:00+02:00"}]}`, 1235)
	insert("hrv", "hrv-2026-09-01", "2026-09-01", `{"hrv":{"hrvSummary":{"overnightAverage":0}}}`, 1236)
	page, err := repository.DailyHealth(context.Background(), testAthlete, HealthQuery{From: "2026-09-04", To: "2026-09-04", Metrics: []string{"sleep"}, Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("health page = %#v, %v", page, err)
	}
	item := page.Items[0]
	if item.Sleep == nil || item.HRV == nil || item.HeartRate == nil || item.Stress == nil || item.BodyBattery == nil {
		t.Fatalf("stable core signals = %#v", item)
	}
	if item.Sleep.NapCount != 1 || item.Sleep.TotalSleep24H == nil || *item.Sleep.TotalSleep24H != 6.78 || item.Sleep.SignalStatus.Provenance.UpdatedAt != "1970-01-01T00:00:01Z" {
		t.Fatalf("sleep freshness = %#v", item.Sleep)
	}
	if item.HRV.SignalStatus.Available || !item.HRV.SignalStatus.Stale || item.HRV.SignalStatus.AgeDays == nil || *item.HRV.SignalStatus.AgeDays != 3 {
		t.Fatalf("stale HRV = %#v", item.HRV)
	}
	if item.SpO2 != nil {
		t.Fatal("optional SpO2 should remain omitted when not requested")
	}
	if !hasWarning(page.DataStatus.Warnings, "STALE_HRV") {
		t.Fatalf("health warnings = %#v", page.DataStatus.Warnings)
	}
}

func testFloatPointer(value float64) *float64 { return &value }
func hasWarning(values []Warning, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string { return &value }
