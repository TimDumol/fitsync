package fitnessrepo

import (
	"encoding/json"
	"testing"

	"github.com/timdumol/fitsync/internal/exercisecatalog"
)

func TestNormalizePlannedStepPreservesNameAndFillsExactKey(t *testing.T) {
	name := "Bulgarian split squat"
	step, resolution, err := normalizePlannedStep(PlannedStep{ID: "step", ExerciseName: &name, Prescription: json.RawMessage(`{"sets":[]}`)})
	if err != nil || resolution.Status != exercisecatalog.StatusExactAlias || step.ExerciseKey == nil || *step.ExerciseKey != "bulgarian_split_squat" {
		t.Fatalf("normalized step = %#v, resolution = %#v, err = %v", step, resolution, err)
	}
	if step.ExerciseName == &name || step.ExerciseName == nil || *step.ExerciseName != name {
		t.Fatal("normalization did not preserve the supplied name without aliasing it")
	}
}

func TestNormalizeWorkoutWriteAllowsUnknownNamesAndRejectsUnknownKeys(t *testing.T) {
	name := "Pull-up or chin-up"
	entry := StrengthEntry{EntryID: "entry", EntryType: "set", ExerciseName: &name}
	value, reports, err := normalizeWorkoutWrite(WorkoutWrite{Entries: []StrengthEntry{entry}})
	if err != nil || len(reports) != 1 || reports[0].Resolution.Status != exercisecatalog.StatusUnknown || value.Entries[0].ExerciseKey != nil || value.Entries[0].ExerciseName == nil || *value.Entries[0].ExerciseName != name {
		t.Fatalf("unknown name normalization = %#v, %#v, %v", value, reports, err)
	}
	unknown := "made_up_key"
	if _, _, err := normalizeWorkoutWrite(WorkoutWrite{Entries: []StrengthEntry{{EntryID: "entry", EntryType: "set", ExerciseKey: &unknown}}}); err == nil || err.Error() != "invalid_input: unknown exercise_key" {
		t.Fatalf("unknown key error = %v", err)
	}
	deprecated := "dumbbell_row"
	value, _, err = normalizeWorkoutWrite(WorkoutWrite{Entries: []StrengthEntry{{EntryID: "entry", EntryType: "set", ExerciseKey: &deprecated}}})
	if err != nil || value.Entries[0].ExerciseKey == nil || *value.Entries[0].ExerciseKey != "one_arm_dumbbell_row" {
		t.Fatalf("deprecated key normalization = %#v, %v", value, err)
	}
}

func TestNormalizeRawEntryPayloadChangesOnlyIdentity(t *testing.T) {
	raw, err := normalizeRawEntryPayload(`{"exercise_name":"Push-up","notes":"keep this","custom":{"x":1}}`)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	if value["exercise_key"] != "push_up" || value["exercise_name"] != "Push-up" || value["notes"] != "keep this" {
		t.Fatalf("normalized payload = %#v", value)
	}
	if unchanged, err := normalizeRawEntryPayload(`[1,{"exercise_name":"Push-up"}]`); err != nil || unchanged != `[1,{"exercise_name":"Push-up"}]` {
		t.Fatalf("non-object payload = %q, %v", unchanged, err)
	}
}
