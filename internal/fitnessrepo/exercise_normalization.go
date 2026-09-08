package fitnessrepo

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/timdumol/fitsync/internal/exercisecatalog"
)

// EntryNormalization carries the repository's authoritative resolution for a
// write. It is intentionally transport-neutral so MCP can report it without
// implementing a second resolver.
type EntryNormalization struct {
	Ref        string
	Resolution exercisecatalog.Resolution
}

func ResolveExerciseIdentity(exerciseKey, exerciseName *string) (exercisecatalog.Resolution, error) {
	return exercisecatalog.Resolve(exerciseKey, exerciseName)
}

func (r *Repository) ResolveExerciseIdentity(exerciseKey, exerciseName *string) (exercisecatalog.Resolution, error) {
	return ResolveExerciseIdentity(exerciseKey, exerciseName)
}

func normalizePlannedStep(step PlannedStep) (PlannedStep, exercisecatalog.Resolution, error) {
	result := step
	result.Prescription = append(json.RawMessage(nil), step.Prescription...)
	result.ExerciseKey = cloneString(step.ExerciseKey)
	result.ExerciseName = cloneString(step.ExerciseName)
	if !hasExerciseIdentity(step.ExerciseKey, step.ExerciseName) {
		return result, exercisecatalog.Resolution{Status: exercisecatalog.StatusUnknown}, nil
	}
	resolution, err := exercisecatalog.Resolve(step.ExerciseKey, step.ExerciseName)
	if err != nil {
		return PlannedStep{}, exercisecatalog.Resolution{}, err
	}
	result.ExerciseKey = cloneString(resolution.Key)
	return result, resolution, nil
}

func normalizeStrengthEntry(entry StrengthEntry) (StrengthEntry, exercisecatalog.Resolution, error) {
	result := entry
	result.ExerciseKey = cloneString(entry.ExerciseKey)
	result.ExerciseName = cloneString(entry.ExerciseName)
	result.MuscleGroups = append([]string(nil), entry.MuscleGroups...)
	result.DirectMuscleGroups = append([]string(nil), entry.DirectMuscleGroups...)
	result.IndirectMuscleGroups = append([]string(nil), entry.IndirectMuscleGroups...)
	if entry.EntryType != "set" || !hasExerciseIdentity(entry.ExerciseKey, entry.ExerciseName) {
		return result, exercisecatalog.Resolution{Status: exercisecatalog.StatusUnknown}, nil
	}
	resolution, err := exercisecatalog.Resolve(entry.ExerciseKey, entry.ExerciseName)
	if err != nil {
		return StrengthEntry{}, exercisecatalog.Resolution{}, err
	}
	result.ExerciseKey = cloneString(resolution.Key)
	return result, resolution, nil
}

func normalizeWorkoutWrite(in WorkoutWrite) (WorkoutWrite, []EntryNormalization, error) {
	result := in
	result.Entries = make([]StrengthEntry, len(in.Entries))
	normalizations := make([]EntryNormalization, 0, len(in.Entries))
	for index, entry := range in.Entries {
		normalized, resolution, err := normalizeStrengthEntry(entry)
		if err != nil {
			return WorkoutWrite{}, nil, err
		}
		result.Entries[index] = normalized
		if entry.EntryType == "set" && hasExerciseIdentity(entry.ExerciseKey, entry.ExerciseName) {
			normalizations = append(normalizations, EntryNormalization{Ref: entry.EntryID, Resolution: resolution})
		}
	}
	return result, normalizations, nil
}

func normalizePlannedSteps(steps []PlannedStep) ([]PlannedStep, error) {
	result := make([]PlannedStep, len(steps))
	for index, step := range steps {
		normalized, _, err := normalizePlannedStep(step)
		if err != nil {
			return nil, err
		}
		result[index] = normalized
	}
	return result, nil
}

func hasExerciseIdentity(key, name *string) bool {
	return (key != nil && strings.TrimSpace(*key) != "") || (name != nil && strings.TrimSpace(*name) != "")
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// normalizeRawEntryPayload only owns the two top-level identity fields. All
// other JSON, including unknown fields used by older mobile clients, is left
// untouched.
func normalizeRawEntryPayload(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload == nil {
		return "", errors.New("invalid_input: entry payload must be an object")
	}
	key, keyPresent, err := rawOptionalString(payload, "exercise_key")
	if err != nil {
		return "", errors.New("invalid_input: exercise_key")
	}
	name, namePresent, err := rawOptionalString(payload, "exercise_name")
	if err != nil {
		return "", errors.New("invalid_input: exercise_name")
	}
	if !keyPresent && !namePresent {
		return raw, nil
	}
	resolution, err := exercisecatalog.Resolve(key, name)
	if err != nil {
		return "", err
	}
	if resolution.Key == nil {
		payload["exercise_key"] = []byte("null")
	} else {
		payload["exercise_key"], _ = json.Marshal(*resolution.Key)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", errors.New("internal_error: entry payload")
	}
	return string(encoded), nil
}

func rawOptionalString(payload map[string]json.RawMessage, field string) (*string, bool, error) {
	raw, present := payload[field]
	if !present || string(raw) == "null" {
		return nil, present, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, err
	}
	return &value, true, nil
}
