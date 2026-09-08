package fitnessrepo

import (
	"context"
	"encoding/json"
	"strconv"
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

func TestPlanIdempotencyKeysRoundTrip(t *testing.T) {
	repository := New(testDB(t), func() time.Time { return time.UnixMilli(20_000) })
	planID, sessionID, stepID := v7(t), v7(t), v7(t)
	exercise := "Split squat"
	value, err := repository.UpsertPlanDraft(context.Background(), testAthlete, PlanWrite{
		Plan:     TrainingPlan{ID: planID, IdempotencyKey: "plan-2026-09-04", Title: "Strength", Goal: json.RawMessage(`{"primary":"hypertrophy"}`)},
		Sessions: []PlannedSessionWrite{{ID: sessionID, PlanID: planID, IdempotencyKey: "session-2026-09-04", Date: "2026-09-04", Sport: "strength", SessionType: "full_body", Priority: "normal", Steps: []PlannedStep{{ID: stepID, IdempotencyKey: "step-split-squat", SequenceNo: 1, StepType: "exercise", ExerciseName: &exercise, Prescription: json.RawMessage(`{"sets":[{"reps_min":8}]}`)}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.Plan == nil || value.Plan.IdempotencyKey != "plan-2026-09-04" || value.Sessions[0].IdempotencyKey != "session-2026-09-04" || value.Sessions[0].Steps[0].IdempotencyKey != "step-split-squat" {
		t.Fatalf("idempotency keys were not retained: %#v", value)
	}
	for name, test := range map[string]struct {
		lookup func(context.Context, string, string) (string, error)
		key    string
		want   string
	}{
		"plan":    {repository.PlanIDForIdempotencyKey, "plan-2026-09-04", planID},
		"session": {repository.PlannedSessionIDForIdempotencyKey, "session-2026-09-04", sessionID},
		"step":    {repository.PlannedStepIDForIdempotencyKey, "step-split-squat", stepID},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := test.lookup(context.Background(), testAthlete, test.key)
			if err != nil || got != test.want {
				t.Fatalf("lookup = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestPlannedStepGroupsRoundTripIdempotencyAndReplacement(t *testing.T) {
	repository := New(testDB(t), func() time.Time { return time.UnixMilli(30_000) })
	planID, sessionID, firstStepID, secondStepID, groupID := v7(t), v7(t), v7(t), v7(t), v7(t)
	firstName, secondName := "Bench press", "Barbell row"
	firstPrescription := json.RawMessage(`{"sets":[{"reps_min":8,"reps_max":10,"rir":2,"rest_seconds":0},{"reps_min":8,"reps_max":10,"rir":2,"rest_seconds":0}]}`)
	secondPrescription := json.RawMessage(`{"sets":[{"reps_min":8,"reps_max":10,"rir":2,"rest_seconds":120},{"reps_min":8,"reps_max":10,"rir":2,"rest_seconds":120}]}`)
	groupNotes := "Keep transitions short"
	draft := PlanWrite{
		Plan: TrainingPlan{ID: planID, IdempotencyKey: "group-plan", Title: "Structured strength", Goal: json.RawMessage(`{"summary":"strength"}`)},
		Sessions: []PlannedSessionWrite{{ID: sessionID, PlanID: planID, IdempotencyKey: "group-session", Date: "2026-09-07", Title: "Upper body", Sport: "strength", SessionType: "full_body", Priority: "normal", Steps: []PlannedStep{
			{ID: firstStepID, IdempotencyKey: "group-step-a", SequenceNo: 1, StepType: "exercise", ExerciseName: &firstName, Prescription: firstPrescription},
			{ID: secondStepID, IdempotencyKey: "group-step-b", SequenceNo: 2, StepType: "exercise", ExerciseName: &secondName, Prescription: secondPrescription},
		}, Groups: []PlannedStepGroup{{ID: groupID, IdempotencyKey: "group-a", Label: "Superset A", Mode: "rounds", Rounds: 2, StepSequenceNos: []int{1, 2}, Notes: &groupNotes}}}},
	}
	value, err := repository.UpsertPlanDraft(context.Background(), testAthlete, draft)
	if err != nil {
		t.Fatal(err)
	}
	if value.Plan == nil || len(value.Sessions) != 1 || len(value.Sessions[0].Groups) != 1 {
		t.Fatalf("group draft = %#v", value)
	}
	group := value.Sessions[0].Groups[0]
	if group.ID != groupID || group.IdempotencyKey != "group-a" || group.Label != "Superset A" || group.Mode != "rounds" || group.Rounds != 2 || !sameString(group.Notes, &groupNotes) || len(group.StepSequenceNos) != 2 || group.StepSequenceNos[0] != 1 || group.StepSequenceNos[1] != 2 {
		t.Fatalf("group = %#v", group)
	}
	if got, err := repository.PlannedStepGroupIDForIdempotencyKey(context.Background(), testAthlete, "group-a"); err != nil || got != groupID {
		t.Fatalf("group idempotency lookup = %q, %v", got, err)
	}
	firstSeq := value.Plan.SyncSeq
	retried, err := repository.UpsertPlanDraft(context.Background(), testAthlete, draft)
	if err != nil || retried.Plan.SyncSeq != firstSeq {
		t.Fatalf("identical group retry = %#v, %v", retried, err)
	}
	changed := draft
	changed.Sessions = append([]PlannedSessionWrite(nil), draft.Sessions...)
	changed.Sessions[0] = draft.Sessions[0]
	changed.Sessions[0].Groups = []PlannedStepGroup{{ID: groupID, IdempotencyKey: "group-a", Label: "Superset A", Mode: "rounds", Rounds: 2, StepSequenceNos: []int{1, 2}, Notes: stringPointer("Longer transition")}}
	changedValue, err := repository.UpsertPlanDraft(context.Background(), testAthlete, changed)
	if err != nil || changedValue.Plan.SyncSeq <= firstSeq {
		t.Fatalf("changed group draft = %#v, %v", changedValue, err)
	}
	removed := changed
	removed.Sessions = append([]PlannedSessionWrite(nil), changed.Sessions...)
	removed.Sessions[0] = changed.Sessions[0]
	removed.Sessions[0].Groups = nil
	removedValue, err := repository.UpsertPlanDraft(context.Background(), testAthlete, removed)
	if err != nil || len(removedValue.Sessions[0].Groups) != 0 {
		t.Fatalf("removed group draft = %#v, %v", removedValue, err)
	}
	var deleted int
	if err := repository.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM planned_step_groups WHERE id=? AND deleted_at IS NOT NULL", groupID).Scan(&deleted); err != nil || deleted != 1 {
		t.Fatalf("deleted group count = %d, %v", deleted, err)
	}
}

func TestGroupedWorkoutComparisonSortsInterleavedEntriesByExecutionOrder(t *testing.T) {
	sessionID, planID, firstStepID, secondStepID := v7(t), v7(t), v7(t), v7(t)
	firstName, secondName := "Bench press", "Barbell row"
	session := PlannedSession{ID: sessionID, PlanID: planID, Steps: []PlannedStep{
		{ID: firstStepID, SequenceNo: 1, StepType: "exercise", ExerciseName: &firstName, Prescription: json.RawMessage(`{"sets":[{"reps_min":8},{"reps_min":8}]}`)},
		{ID: secondStepID, SequenceNo: 2, StepType: "exercise", ExerciseName: &secondName, Prescription: json.RawMessage(`{"sets":[{"reps_min":8},{"reps_min":8}]}`)},
	}}
	entries := []StrengthEntry{{PlannedStepID: &secondStepID, SequenceNo: 4, Reps: floatPointer(9)}, {PlannedStepID: &firstStepID, SequenceNo: 3, Reps: floatPointer(7)}, {PlannedStepID: &secondStepID, SequenceNo: 2, Reps: floatPointer(8)}, {PlannedStepID: &firstStepID, SequenceNo: 1, Reps: floatPointer(6)}}
	comparison := compareWorkout(session, entries)
	if len(comparison.Steps) != 2 || comparison.Steps[0].ActualSets != 2 || comparison.Steps[1].ActualSets != 2 {
		t.Fatalf("comparison = %#v", comparison)
	}
	if strings.Join([]string{fmtFloat(comparison.Steps[0].ActualReps[0]), fmtFloat(comparison.Steps[0].ActualReps[1]), fmtFloat(comparison.Steps[1].ActualReps[0]), fmtFloat(comparison.Steps[1].ActualReps[1])}, ",") != "6,7,8,9" {
		t.Fatalf("comparison order = %#v", comparison)
	}
}

func fmtFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func intPointer(value int) *int           { return &value }
func floatPointer(value float64) *float64 { return &value }
