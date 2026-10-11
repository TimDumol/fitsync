package fitnessrepo

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPatchWorkoutLinksActivityWithSingleConnection(t *testing.T) {
	database := testDB(t)
	database.SetMaxOpenConns(1)
	activityID := "linked-run"
	if _, err := database.Exec(`INSERT INTO garmin_records (id,athlete_id,dataset,record_key,record_date,payload_format,payload_raw,payload_sha256,ingested_at) VALUES (?,?,?,?,?,?,?,?,?)`, v7(t), testAthlete, "activities", activityID, "2026-09-04", "test", `{"activityId":"linked-run"}`, strings.Repeat("a", 64), 1); err != nil {
		t.Fatal(err)
	}
	repository := New(database, func() time.Time { return time.UnixMilli(1000) })
	workoutID := v7(t)
	value, err := repository.UpsertWorkout(context.Background(), testAthlete, WorkoutWrite{ID: workoutID, ClientUpdatedAt: 1, Date: "2026-09-04", Title: "Run", WorkoutType: "strength", Source: "mobile"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	patched, err := repository.PatchWorkout(ctx, testAthlete, workoutID, value.Workout.SyncSeq, 2, []WorkoutPatchOperation{{Op: "set_linked_activity", ActivityID: &activityID}})
	if err != nil {
		t.Fatal(err)
	}
	if got := patched.Workout.Workout.LinkedActivityID; got == nil || *got != activityID {
		t.Fatalf("linked activity = %v", got)
	}
	otherAthlete := "00000000-0000-0000-0000-000000000003"
	foreignActivityID := "foreign-run"
	if _, err := database.Exec(`INSERT INTO garmin_records (id,athlete_id,dataset,record_key,record_date,payload_format,payload_raw,payload_sha256,ingested_at) VALUES (?,?,?,?,?,?,?,?,?)`, v7(t), otherAthlete, "activities", foreignActivityID, "2026-09-04", "test", `{"activityId":"foreign-run"}`, strings.Repeat("b", 64), 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"missing-run", foreignActivityID} {
		_, err := repository.PatchWorkout(ctx, testAthlete, workoutID, patched.Workout.Workout.SyncSeq, 3, []WorkoutPatchOperation{{Op: "append_note", Text: stringPointer("must roll back")}, {Op: "set_linked_activity", ActivityID: &id}})
		if err == nil || err.Error() != "not_found: linked activity" {
			t.Fatalf("link %q: %v", id, err)
		}
	}
	unchanged, err := repository.Workout(ctx, testAthlete, workoutID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Notes != nil || unchanged.SyncSeq != patched.Workout.Workout.SyncSeq || unchanged.LinkedActivityID == nil || *unchanged.LinkedActivityID != activityID {
		t.Fatalf("failed link changed workout: %#v", unchanged)
	}
}
