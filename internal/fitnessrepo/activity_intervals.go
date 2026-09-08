package fitnessrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/timdumol/fitsync/internal/fitnessdata"
)

func (r *Repository) ActivityIntervals(ctx context.Context, athleteID string, q ActivityIntervalsQuery) (ActivityIntervalsResult, error) {
	if q.ActivityID == "" || len(q.ActivityID) > 128 {
		return ActivityIntervalsResult{}, errors.New("invalid_input: activity_id")
	}
	if q.MaxSegments == 0 {
		q.MaxSegments = 50
	}
	if q.MaxSegments < 1 || q.MaxSegments > 100 {
		return ActivityIntervalsResult{}, errors.New("invalid_input: max_segments")
	}
	include := []string{"summary"}
	if q.IncludeLaps {
		include = append(include, "splits")
	}
	activity, err := r.Activity(ctx, athleteID, q.ActivityID, include, 30, q.MaxSegments)
	if err != nil {
		return ActivityIntervalsResult{}, err
	}
	result := ActivityIntervalsResult{ActivityID: q.ActivityID, Segments: []IntervalSegment{}, Warnings: append([]Warning(nil), activity.Warnings...), Method: Method{Name: "activity_intervals_v1", Version: 1, Confidence: "low", ValidSamples: 0, RequiredSamples: 1}}
	if activity.Detail == nil || !q.IncludeLaps {
		result.Warnings = append(result.Warnings, Warning{Code: "MISSING_ACTIVITY_SPLITS", Severity: "info", Message: "Typed activity laps or splits are unavailable."})
		return result, nil
	}
	steps := r.linkedIntervalSteps(ctx, athleteID, q.ActivityID, activity.Summary.Date)
	for index, split := range activity.Detail.Splits {
		if index >= q.MaxSegments {
			break
		}
		classification, source, confidence := classifyIntervalSplit(split, steps, index)
		segment := IntervalSegment{Sequence: index + 1, SourceID: split.SourceID, Classification: classification, StartElapsed: split.StartElapsed, DurationSeconds: split.DurationSeconds, DistanceKM: split.DistanceKM, PaceMinPerKM: split.PaceMinPerKM, AverageHR: split.AverageHR, EndHR: split.EndHR, MaxHR: split.MaxHR, AveragePower: split.AveragePower, MaxPower: split.MaxPower, Cadence: split.Cadence, ClassificationSource: source, Confidence: confidence}
		if segment.SourceID == "" {
			segment.SourceID = fmt.Sprintf("lap-%d", segment.Sequence)
		}
		if segment.DurationSeconds != nil {
			segment.DurationDisplay = fitnessdata.FormatDuration(*segment.DurationSeconds)
		}
		if segment.PaceMinPerKM != nil {
			segment.PaceDisplay = fitnessdata.FormatPace(*segment.PaceMinPerKM)
		}
		result.Segments = append(result.Segments, segment)
	}
	for index := range result.Segments {
		if !q.IncludeRecoveries || index == 0 || result.Segments[index].Classification != "recovery" || result.Segments[index-1].Classification != "work" {
			continue
		}
		before, after := result.Segments[index-1].EndHR, result.Segments[index].EndHR
		if before != nil && after != nil && *before >= *after {
			drop := *before - *after
			result.Segments[index].RecoveryHRDrop = &drop
		}
	}
	result.Method.ValidSamples = len(result.Segments)
	if len(result.Segments) > 0 {
		result.Method.Confidence = "medium"
	}
	return result, nil
}

func classifyIntervalSplit(split fitnessdata.Split, steps []fitnessdata.PlannedIntervalStep, index int) (string, string, string) {
	if split.StepType != "" {
		return normalizeIntervalType(split.StepType), "garmin_split_step", "medium"
	}
	if index < len(steps) {
		return normalizeIntervalType(steps[index].StepType), "linked_structured_workout", "high"
	}
	return "unclassified", "garmin_lap", "low"
}

func normalizeIntervalType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "warmup", "warm_up":
		return "warmup"
	case "work", "interval", "exercise":
		return "work"
	case "recovery", "rest":
		return "recovery"
	case "cooldown", "cool_down":
		return "cooldown"
	default:
		return "unclassified"
	}
}

func (r *Repository) linkedIntervalSteps(ctx context.Context, athleteID, activityID, date string) []fitnessdata.PlannedIntervalStep {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM workout_sessions WHERE athlete_id=? AND workout_date=? AND deleted_at IS NULL ORDER BY id LIMIT 100`, athleteID, date)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			continue
		}
		workout, err := r.Workout(ctx, athleteID, id)
		if err != nil || workout.LinkedActivityID == nil || *workout.LinkedActivityID != activityID || workout.PlannedSession == nil {
			continue
		}
		result := make([]fitnessdata.PlannedIntervalStep, 0, len(workout.PlannedSession.Steps))
		for _, step := range workout.PlannedSession.Steps {
			result = append(result, fitnessdata.PlannedIntervalStep{StepType: step.StepType})
		}
		return result
	}
	return nil
}
