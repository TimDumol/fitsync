package fitnessrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/timdumol/fitsync/internal/exercisecatalog"
)

func (r *Repository) PatchWorkout(ctx context.Context, athleteID, workoutID string, expectedSyncSeq, clientUpdatedAt int64, operations []WorkoutPatchOperation) (PatchWorkoutResult, error) {
	if len(operations) == 0 || len(operations) > 50 {
		return PatchWorkoutResult{}, errors.New("invalid_input: operations must contain 1 to 50 items")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return PatchWorkoutResult{}, err
	}
	fail := func(e error) (PatchWorkoutResult, error) { _ = tx.Rollback(); return PatchWorkoutResult{}, e }
	var owner string
	var currentSeq, currentClient int64
	var metadataRaw string
	if err := tx.QueryRowContext(ctx, "SELECT athlete_id,sync_seq,client_updated_at,metadata_raw FROM workout_sessions WHERE id=? AND deleted_at IS NULL", workoutID).Scan(&owner, &currentSeq, &currentClient, &metadataRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fail(errors.New("not_found: workout"))
		}
		return fail(errors.New("internal_error: workout lookup"))
	}
	if owner != athleteID {
		return fail(errors.New("not_found: workout"))
	}
	if expectedSyncSeq != currentSeq {
		_ = tx.Rollback()
		current, readErr := r.Workout(ctx, athleteID, workoutID)
		if readErr != nil {
			return PatchWorkoutResult{}, readErr
		}
		winner := WorkoutWinner{Workout: current, SubmittedVersionWon: false, ServerSyncedAt: currentSeq}
		return PatchWorkoutResult{Conflict: &winner}, nil
	}
	if clientUpdatedAt <= currentClient {
		_ = tx.Rollback()
		current, readErr := r.Workout(ctx, athleteID, workoutID)
		if readErr != nil {
			return PatchWorkoutResult{}, readErr
		}
		winner := WorkoutWinner{Workout: current, SubmittedVersionWon: false, ServerSyncedAt: currentSeq}
		return PatchWorkoutResult{Conflict: &winner}, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataRaw), &metadata); err != nil {
		return fail(errors.New("internal_error: workout metadata"))
	}
	for _, operation := range operations {
		switch operation.Op {
		case "set_linked_activity":
			if operation.ActivityID == nil || *operation.ActivityID == "" || !r.OwnedActivity(ctx, athleteID, *operation.ActivityID) {
				return fail(errors.New("not_found: linked activity"))
			}
			metadata["linked_activity_id"] = *operation.ActivityID
		case "append_note":
			if operation.Text == nil || strings.TrimSpace(*operation.Text) == "" || len(*operation.Text) > 4000 {
				return fail(errors.New("invalid_input: note"))
			}
			old, _ := metadata["notes"].(string)
			if old != "" {
				old += "\n"
			}
			metadata["notes"] = old + *operation.Text
		case "set_entry_rir", "set_entry_exercise_key", "set_entry_load_semantics", "set_entry_muscle_roles":
			if operation.EntryID == nil || *operation.EntryID == "" {
				return fail(errors.New("invalid_input: entry_id"))
			}
			var raw string
			if err := tx.QueryRowContext(ctx, "SELECT payload_raw FROM workout_entries WHERE id=? AND session_id=? AND athlete_id=? AND deleted_at IS NULL", *operation.EntryID, workoutID, athleteID).Scan(&raw); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fail(errors.New("not_found: workout entry"))
				}
				return fail(errors.New("internal_error: workout entry lookup"))
			}
			var payload map[string]any
			if json.Unmarshal([]byte(raw), &payload) != nil {
				return fail(errors.New("internal_error: workout entry payload"))
			}
			switch operation.Op {
			case "set_entry_rir":
				if operation.RIR == nil || *operation.RIR < 0 || *operation.RIR > 10 {
					return fail(errors.New("invalid_input: rir"))
				}
				payload["rir"] = *operation.RIR
			case "set_entry_exercise_key":
				if operation.ExerciseKey == nil || len(*operation.ExerciseKey) > 128 {
					return fail(errors.New("invalid_input: exercise_key"))
				}
				var currentName *string
				if rawName, ok := payload["exercise_name"]; ok && rawName != nil {
					name, ok := rawName.(string)
					if !ok {
						return fail(errors.New("invalid_input: exercise_name"))
					}
					currentName = &name
				}
				resolution, resolveErr := exercisecatalog.Resolve(operation.ExerciseKey, currentName)
				if resolveErr != nil {
					return fail(resolveErr)
				}
				if resolution.Key == nil {
					return fail(errors.New("invalid_input: exercise_key"))
				}
				payload["exercise_key"] = *resolution.Key
			case "set_entry_load_semantics":
				if operation.LoadBasis == nil {
					return fail(errors.New("invalid_input: load_basis"))
				}
				payload["load_basis"] = *operation.LoadBasis
				if operation.Laterality != nil {
					payload["laterality"] = *operation.Laterality
				}
				if operation.RepsPerSide != nil {
					payload["reps_per_side"] = *operation.RepsPerSide
				}
				if operation.LoadPerLimbKG != nil {
					payload["load_per_limb_kg"] = *operation.LoadPerLimbKG
				}
				if operation.TotalExternalLoadKG != nil {
					payload["total_external_load_kg"] = *operation.TotalExternalLoadKG
				}
				if operation.AddedLoadKG != nil {
					payload["added_load_kg"] = *operation.AddedLoadKG
				}
				candidateRaw, marshalErr := json.Marshal(payload)
				if marshalErr != nil {
					return fail(errors.New("internal_error: workout entry payload"))
				}
				var candidate StrengthEntry
				if json.Unmarshal(candidateRaw, &candidate) != nil {
					return fail(errors.New("internal_error: workout entry payload"))
				}
				if err := validateStrengthEntry(candidate); err != nil {
					return fail(err)
				}
			case "set_entry_muscle_roles":
				payload["direct_muscle_groups"] = operation.DirectMuscleGroups
				payload["indirect_muscle_groups"] = operation.IndirectMuscleGroups
			default:
				return fail(errors.New("invalid_input: unsupported patch operation"))
			}
			updated, _ := json.Marshal(payload)
			if _, err := tx.ExecContext(ctx, "UPDATE workout_entries SET payload_raw=?,client_updated_at=?,server_synced_at=? WHERE id=? AND session_id=? AND athlete_id=?", string(updated), clientUpdatedAt, r.now(), *operation.EntryID, workoutID, athleteID); err != nil {
				return fail(errors.New("internal_error: workout entry update"))
			}
		default:
			return fail(errors.New("invalid_input: unsupported patch operation"))
		}
	}
	updatedMetadata, _ := json.Marshal(metadata)
	newSeq, err := allocateSyncSeq(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE workout_sessions SET metadata_raw=?,client_updated_at=?,server_synced_at=?,sync_seq=? WHERE id=? AND athlete_id=?", string(updatedMetadata), clientUpdatedAt, r.now(), newSeq, workoutID, athleteID); err != nil {
		return fail(errors.New("internal_error: workout update"))
	}
	if err := tx.Commit(); err != nil {
		return PatchWorkoutResult{}, errors.New("internal_error: transaction commit")
	}
	value, err := r.Workout(ctx, athleteID, workoutID)
	if err != nil {
		return PatchWorkoutResult{}, err
	}
	return PatchWorkoutResult{Workout: WorkoutWinner{Workout: value, SubmittedVersionWon: true, ServerSyncedAt: newSeq}}, nil
}
