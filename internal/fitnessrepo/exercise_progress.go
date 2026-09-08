package fitnessrepo

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/timdumol/fitsync/internal/exercisecatalog"
	"github.com/timdumol/fitsync/internal/fitnessdata"
)

func (r *Repository) ExerciseProgress(ctx context.Context, athleteID string, q ExerciseProgressQuery) (ExerciseProgress, error) {
	if err := validRange(q.From, q.To, 730); err != nil {
		return ExerciseProgress{}, err
	}
	limit, err := boundedLimit(q.Limit)
	if err != nil {
		return ExerciseProgress{}, err
	}
	if limit > 100 {
		limit = 100
	}
	canonical, known := exercisecatalog.NormalizeExercise(q.ExerciseKey)
	output := ExerciseProgress{Sessions: []ExerciseProgressSession{}, RepPRs: []ExerciseRepPR{}, Method: Method{Name: "exercise_progress_v1", Version: 1, Confidence: "unavailable"}}
	if !known {
		return ExerciseProgress{}, errors.New("invalid_input: unknown exercise_key")
	}
	definition, _ := exercisecatalog.Definition(canonical)
	aliasUsed := ""
	if strings.ToLower(strings.TrimSpace(q.ExerciseKey)) != canonical {
		aliasUsed = q.ExerciseKey
	}
	output.Exercise = ExerciseDefinitionOutput{Key: definition.Key, Aliases: definition.Aliases, DirectMuscles: definition.DirectMuscles, IndirectMuscles: definition.IndirectMuscles, SupportsE1RM: definition.SupportsE1RM, AliasUsed: aliasUsed, NormalizationStatus: "normalized"}
	// Fetch the bounded workout page before filtering entries so reviewed aliases
	// stored as exercise_name are not lost by the SQL-side canonical-key filter.
	page, err := r.StrengthWorkouts(ctx, athleteID, StrengthQuery{From: q.From, To: q.To, IncludeEntries: true, Order: "asc", Limit: limit, Cursor: q.Cursor})
	if err != nil {
		return ExerciseProgress{}, err
	}
	setCount := 0
	for _, workout := range page.Items {
		session := ExerciseProgressSession{WorkoutID: workout.WorkoutID, Date: workout.Date, Title: workout.Title, Sets: []ExerciseProgressSet{}}
		for _, entry := range workout.Entries {
			if setCount >= 200 {
				break
			}
			if entry.EntryType != "set" || (!q.IncludeWarmups && entry.IsWarmup) || !entryMatchesExercise(entry, canonical) {
				continue
			}
			applyNormalizedEntry(&entry)
			set := progressSet(entry, definition.SupportsE1RM)
			session.Sets = append(session.Sets, set)
			setCount++
			if entry.LoadKG != nil && entry.Reps != nil && entry.LoadSemantics != "ambiguous_legacy" {
				value := *entry.LoadKG * *entry.Reps
				addFloat(&session.VolumeLoadKG, value)
			}
			if entry.NormalizedVolumeLoadKG != nil {
				addFloat(&session.NormalizedVolumeLoadKG, *entry.NormalizedVolumeLoadKG)
			}
		}
		if len(session.Sets) == 0 {
			continue
		}
		sort.SliceStable(session.Sets, func(i, j int) bool { return session.Sets[i].EntryID < session.Sets[j].EntryID })
		for index := range session.Sets {
			if session.Sets[index].LoadKG != nil && (session.BestSet == nil || bestProgressSet(session.Sets[index], *session.BestSet)) {
				value := session.Sets[index]
				session.BestSet = &value
			}
		}
		output.Sessions = append(output.Sessions, session)
		if setCount >= 200 {
			break
		}
	}
	for _, session := range output.Sessions {
		for _, set := range session.Sets {
			if set.Reps == nil || set.LoadKG == nil || set.LoadSemantics == "ambiguous_legacy" {
				continue
			}
			reps := int(*set.Reps + 0.5)
			if float64(reps) != *set.Reps || reps < 1 || reps > 100 || *set.LoadKG <= 0 {
				continue
			}
			rirMin, rirMax := set.RIR, set.RIR
			output.RepPRs = addRepPR(output.RepPRs, reps, *set.LoadKG, rirMin, rirMax, set.EntryID, session.Date)
		}
	}
	sort.Slice(output.RepPRs, func(i, j int) bool {
		if output.RepPRs[i].Reps != output.RepPRs[j].Reps {
			return output.RepPRs[i].Reps < output.RepPRs[j].Reps
		}
		return output.RepPRs[i].LoadKG > output.RepPRs[j].LoadKG
	})
	if len(output.Sessions) > 0 {
		output.Method.Confidence = "medium"
		output.Method.ValidSamples = len(output.Sessions)
	}
	output.Trend = compareProgress(output.Sessions)
	output.NextCursor = page.NextCursor
	output.Method.RequiredSamples = 2
	return output, nil
}

func entryMatchesExercise(entry StrengthEntry, canonical string) bool {
	if entry.ExerciseKey != nil {
		if normalized, ok := exercisecatalog.NormalizeExercise(*entry.ExerciseKey); ok && normalized == canonical {
			return true
		}
	}
	if entry.ExerciseName != nil {
		normalized, ok := exercisecatalog.NormalizeExercise(*entry.ExerciseName)
		return ok && normalized == canonical
	}
	return false
}

func progressSet(entry StrengthEntry, supportsE1RM bool) ExerciseProgressSet {
	set := ExerciseProgressSet{EntryID: entry.EntryID, Reps: entry.Reps, NormalizedReps: entry.NormalizedReps, LoadKG: entry.LoadPerLimbKG, LoadBasis: entry.LoadBasis, LoadSemantics: entry.LoadSemantics, RIR: entry.RIR}
	if entry.TotalExternalLoadKG != nil {
		set.LoadKG = entry.TotalExternalLoadKG
	}
	if set.LoadKG == nil && entry.AddedLoadKG != nil {
		set.LoadKG = entry.AddedLoadKG
	}
	if entry.NormalizedVolumeLoadKG != nil && entry.NormalizedReps != nil && *entry.NormalizedReps > 0 {
		value := *entry.NormalizedVolumeLoadKG / *entry.NormalizedReps
		set.LoadKG = &value
	}
	if entry.DurationSeconds != nil {
		set.DurationDisplay = fitnessdata.FormatDuration(*entry.DurationSeconds)
	}
	if supportsE1RM && set.LoadKG != nil && entry.Reps != nil && *set.LoadKG > 0 && !boolValue(entry.IsBodyweight) && entry.LoadSemantics != "ambiguous_legacy" {
		reps := *entry.Reps
		if reps >= 1 && reps <= 12 {
			value := *set.LoadKG * (1 + reps/30)
			set.Estimated1RMKG = &value
		}
	}
	return set
}

func boolValue(value *bool) bool { return value != nil && *value }
func bestProgressSet(left, right ExerciseProgressSet) bool {
	if left.Estimated1RMKG != nil && right.Estimated1RMKG != nil {
		return *left.Estimated1RMKG > *right.Estimated1RMKG
	}
	if left.LoadKG != nil && right.LoadKG != nil {
		return *left.LoadKG > *right.LoadKG
	}
	return false
}
func addRepPR(values []ExerciseRepPR, reps int, load float64, min, max *float64, entryID, date string) []ExerciseRepPR {
	for index := range values {
		if values[index].Reps == reps && rirComparable(values[index].RIRMin, min) {
			if load > values[index].LoadKG {
				values[index] = ExerciseRepPR{Reps: reps, LoadKG: load, RIRMin: min, RIRMax: max, EntryID: entryID, Date: date}
			}
			return values
		}
	}
	return append(values, ExerciseRepPR{Reps: reps, LoadKG: load, RIRMin: min, RIRMax: max, EntryID: entryID, Date: date})
}
func rirComparable(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	delta := *left - *right
	if delta < 0 {
		delta = -delta
	}
	return delta <= 1
}
func compareProgress(sessions []ExerciseProgressSession) ExerciseTrendResult {
	result := ExerciseTrendResult{Label: "insufficient_data", Method: "same_reps_rir_comparison_v1"}
	var latest, previous *ExerciseProgressSet
	var latestReps int
	for sessionIndex := len(sessions) - 1; sessionIndex >= 0 && latest == nil; sessionIndex-- {
		for setIndex := len(sessions[sessionIndex].Sets) - 1; setIndex >= 0; setIndex-- {
			set := sessions[sessionIndex].Sets[setIndex]
			if set.LoadKG != nil && set.Reps != nil && set.LoadSemantics != "ambiguous_legacy" {
				latest = &set
				latestReps = int(*set.Reps + 0.5)
				break
			}
		}
	}
	if latest == nil {
		return result
	}
	for sessionIndex := len(sessions) - 1; sessionIndex >= 0 && previous == nil; sessionIndex-- {
		for setIndex := len(sessions[sessionIndex].Sets) - 1; setIndex >= 0; setIndex-- {
			set := sessions[sessionIndex].Sets[setIndex]
			if set.LoadKG == nil || set.Reps == nil || int(*set.Reps+0.5) != latestReps || !rirComparable(set.RIR, latest.RIR) {
				continue
			}
			if sessionIndex == len(sessions)-1 && set.EntryID == latest.EntryID {
				continue
			}
			previous = &set
			break
		}
	}
	if previous == nil {
		return result
	}
	result.Samples, result.ComparableReps = 2, &latestReps
	result.LatestLoadKG, result.PreviousLoadKG = latest.LoadKG, previous.LoadKG
	change := *latest.LoadKG - *previous.LoadKG
	result.ChangeKG = &change
	switch {
	case change > 0:
		result.Label = "progressing"
	case change < 0:
		result.Label = "regressing"
	default:
		result.Label = "stable"
	}
	return result
}
