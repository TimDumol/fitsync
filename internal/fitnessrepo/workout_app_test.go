package fitnessrepo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlanDraftPublishWorkoutCompletionAndPull(t *testing.T) {
	database := testDB(t)
	clock := func() time.Time { return time.UnixMilli(10_000) }
	repository := New(database, clock)
	planID, sessionID, stepID := v7(t), v7(t), v7(t)
	exercise := "Back Squat"
	prescription, _ := json.Marshal(Prescription{Sets: []PrescriptionSet{{RepsMin: intPointer(5), RepsMax: intPointer(5), LoadKG: floatPointer(80), RIR: floatPointer(2), RestSeconds: intPointer(150)}}})
	plan, err := repository.UpsertPlanDraft(context.Background(), testAthlete, PlanWrite{
		Plan:     TrainingPlan{ID: planID, Title: "Foundation", Goal: json.RawMessage(`{"summary":"strength"}`), StartDate: stringPointer("2026-09-01"), EndDate: stringPointer("2026-09-28")},
		Sessions: []PlannedSessionWrite{{ID: sessionID, PlanID: planID, Date: "2026-09-01", Title: "Strength A", Sport: "strength", SessionType: "full_body", Priority: "high", Steps: []PlannedStep{{ID: stepID, SequenceNo: 1, StepType: "exercise", ExerciseName: &exercise, Prescription: prescription}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Plan == nil || plan.Plan.Status != "draft" || len(plan.Sessions) != 1 || len(plan.Sessions[0].Steps) != 1 {
		t.Fatalf("draft = %#v", plan)
	}

	published, err := repository.PublishPlan(context.Background(), testAthlete, planID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Plan.Plan == nil || published.Plan.Plan.Status != "active" || published.ArchivedPlanID != nil {
		t.Fatalf("published = %#v", published)
	}
	repeated, err := repository.PublishPlan(context.Background(), testAthlete, planID)
	if err != nil || repeated.Plan.Plan.SyncSeq != published.Plan.Plan.SyncSeq {
		t.Fatalf("repeated publish = %#v, %v", repeated, err)
	}

	workoutID, entryID := v7(t), v7(t)
	reps, load, rir := 5.0, 82.5, 1.0
	started := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC)
	workout, err := repository.UpsertWorkoutAggregate(context.Background(), testAthlete, WorkoutWrite{ID: workoutID, ClientUpdatedAt: 1, Date: "2026-09-01", Title: "Strength A", WorkoutType: "strength", Source: "mobile", Status: "in_progress", PlannedSessionID: &sessionID, StartedAt: &started, Entries: []StrengthEntry{{EntryID: entryID, PlannedStepID: &stepID, SequenceNo: 1, EntryType: "set", ExerciseName: &exercise, Reps: &reps, LoadKG: &load, RIR: &rir}}})
	if err != nil {
		t.Fatal(err)
	}
	if workout.Workout.Status != "in_progress" || workout.Workout.Entries[0].PlannedStepID == nil {
		t.Fatalf("started workout = %#v", workout)
	}
	completed, err := repository.CompleteWorkout(context.Background(), testAthlete, WorkoutWrite{ID: workoutID, ClientUpdatedAt: 2, Date: "2026-09-01", Title: "Strength A", WorkoutType: "strength", Source: "mobile", Status: "completed", PlannedSessionID: &sessionID, StartedAt: &started, Entries: []StrengthEntry{{EntryID: entryID, PlannedStepID: &stepID, SequenceNo: 1, EntryType: "set", ExerciseName: &exercise, Reps: &reps, LoadKG: &load, RIR: &rir}}})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Workout.Status != "completed" || completed.Workout.PlannedSession == nil || completed.Workout.Comparison == nil {
		t.Fatalf("completed workout = %#v", completed)
	}
	retried, err := repository.CompleteWorkout(context.Background(), testAthlete, WorkoutWrite{ID: workoutID, ClientUpdatedAt: 2, Date: "2026-09-01", Title: "Strength A", WorkoutType: "strength", Source: "mobile", Status: "completed", PlannedSessionID: &sessionID, Entries: []StrengthEntry{{EntryID: entryID, PlannedStepID: &stepID, SequenceNo: 1, EntryType: "set", ExerciseName: &exercise, Reps: &reps, LoadKG: &load, RIR: &rir}}})
	if err != nil || retried.SubmittedVersionWon {
		t.Fatalf("repeated completion = %#v, %v", retried, err)
	}
	session, err := repository.PlannedSessions(context.Background(), testAthlete, "2026-09-01", "2026-09-01", true)
	if err != nil || len(session) != 1 || session[0].Status != "completed" || session[0].CompletedWorkoutID == nil {
		t.Fatalf("completed session = %#v, %v", session, err)
	}

	page, err := repository.PullChanges(context.Background(), testAthlete, 0, 50)
	if err != nil || len(page.Changes) < 2 {
		t.Fatalf("pull = %#v, %v", page, err)
	}
	seenPlan, seenWorkout := false, false
	for _, change := range page.Changes {
		seenPlan = seenPlan || strings.Contains(change.Kind, "training_plan")
		seenWorkout = seenWorkout || strings.Contains(change.Kind, "workout")
	}
	if !seenPlan || !seenWorkout {
		t.Fatalf("pull kinds = %#v", page.Changes)
	}
	if page.NextAfter <= 0 {
		t.Fatalf("next cursor = %d", page.NextAfter)
	}
	if next, err := repository.PullChanges(context.Background(), testAthlete, page.NextAfter, 50); err != nil || len(next.Changes) != 0 || next.NextAfter != page.NextAfter {
		t.Fatalf("empty pull = %#v, %v", next, err)
	}
}

func intPointer(value int) *int           { return &value }
func floatPointer(value float64) *float64 { return &value }
