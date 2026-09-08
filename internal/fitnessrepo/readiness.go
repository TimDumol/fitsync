package fitnessrepo

import (
	"context"
	"errors"
	"time"

	"github.com/timdumol/fitsync/internal/fitnessanalysis"
	"github.com/timdumol/fitsync/internal/fitnessdata"
)

const (
	readinessHardPrevious24Hours = 24 * time.Hour
	readinessHardPrevious48Hours = 48 * time.Hour
	readinessLongDurationMinutes = 90.0
	readinessLongDistanceKM      = 15.0
)

func (r *Repository) ReadinessForSession(ctx context.Context, athleteID string, q ReadinessQuery) (ReadinessResult, error) {
	if q.SessionDate == "" || q.ProposedSessionType == "" || q.ProposedDurationMinutes < 1 || q.ProposedDurationMinutes > 1440 {
		return ReadinessResult{}, errors.New("invalid_input: readiness session fields")
	}
	if q.ProposedIntensity != "easy" && q.ProposedIntensity != "moderate" && q.ProposedIntensity != "hard" && q.ProposedIntensity != "maximal" {
		return ReadinessResult{}, errors.New("invalid_input: proposed_intensity")
	}
	switch fitnessanalysis.NormalizeSessionType(q.ProposedSessionType) {
	case "running", "walking", "cycling", "swimming", "strength", "cardio", "mobility", "hiking", "multisport", "other", "intervals", "tempo", "threshold", "vo2", "race", "long", "easy", "recovery":
	default:
		return ReadinessResult{}, errors.New("invalid_input: proposed_session_type")
	}
	timezone, err := r.AthleteTimezone(ctx, athleteID)
	if err != nil {
		return ReadinessResult{}, err
	}
	location := mustLocation(timezone)
	if q.AsOf == "" {
		q.AsOf = r.clock().Format(time.RFC3339)
	}
	asOf, err := time.Parse(time.RFC3339, q.AsOf)
	if err != nil {
		return ReadinessResult{}, errors.New("invalid_input: as_of must be RFC3339")
	}
	if asOf.After(r.clock().Add(5 * time.Minute)) {
		return ReadinessResult{}, errors.New("invalid_input: as_of is in the future")
	}
	localDate := asOf.In(location).Format("2006-01-02")
	if q.SessionDate < localDate || q.SessionDate > dateShift(localDate, 14) {
		return ReadinessResult{}, errors.New("invalid_input: session_date must be within 14 days of as_of")
	}
	health, err := r.DailyHealth(ctx, athleteID, HealthQuery{From: dateShift(localDate, -90), To: localDate, Metrics: []string{"sleep", "hrv", "heart_rate", "stress", "body_battery"}, Order: "desc", Limit: 100})
	if err != nil {
		return ReadinessResult{}, err
	}
	activities, err := r.Activities(ctx, athleteID, ActivityQuery{From: dateShift(localDate, -28), To: localDate, Detail: "summary", Order: "desc", Limit: 100})
	if err != nil {
		return ReadinessResult{}, err
	}
	strength, err := r.StrengthWorkouts(ctx, athleteID, StrengthQuery{From: dateShift(localDate, -28), To: localDate, IncludeEntries: false, Order: "desc", Limit: 100})
	if err != nil {
		return ReadinessResult{}, err
	}
	nearby, err := r.Plan(ctx, athleteID, localDate, dateShift(localDate, 14), true)
	if err != nil {
		return ReadinessResult{}, err
	}
	checkin := latestCheckin(ctx, r.db, athleteID, localDate)
	recovery := classifyRecovery(health.Items, checkin, localDate)
	load := calculateLoadWithPlans(activities.Items, localDate, health.Items, nearby.Sessions, strength.Items)
	result := ReadinessResult{Recommendation: "proceed", Confidence: "unavailable", Reasons: []ReadinessReason{}, Counterpoints: []ReadinessReason{}, RulesEvaluated: []string{}, RulesUnavailable: []string{}, RelevantRecentSessions: []TimelineCompleted{}, NearbyPlan: nearby.Sessions, RecoverySignals: recovery, LoadContext: load, Method: Method{Name: "session_readiness_rules_v1", Version: 1, RequiredSamples: 5}}
	for _, activity := range activities.Items {
		if daysBetween(activity.Date, localDate) >= 0 && daysBetween(activity.Date, localDate) < 7 {
			result.RelevantRecentSessions = append(result.RelevantRecentSessions, TimelineCompleted{Kind: "garmin_activity", ID: activity.ActivityID, Date: activity.Date, Title: derefActivityName(activity.Name), Category: activity.Category, SessionType: activity.SourceType, DurationMinutes: activity.DurationMinutes, DistanceKM: activity.DistanceKM, GarminLoad: activity.TrainingLoad, ActivityID: stringPointerNonEmpty(activity.ActivityID)})
		}
	}
	for _, workout := range strength.Items {
		if daysBetween(workout.Date, localDate) >= 0 && daysBetween(workout.Date, localDate) < 7 {
			result.RelevantRecentSessions = append(result.RelevantRecentSessions, TimelineCompleted{Kind: "structured_workout", ID: workout.WorkoutID, Date: workout.Date, Title: workout.Title, Category: "strength", SessionType: workout.WorkoutType, DurationMinutes: workout.DurationMinutes, WorkoutID: stringPointerNonEmpty(workout.WorkoutID), PlannedSessionID: workout.PlannedSessionID})
		}
	}

	hard24 := false
	hard48 := false
	for _, activity := range activities.Items {
		if !fitnessanalysis.ClassifyHardActivity(activity, "").Hard {
			continue
		}
		if activityInWindow(activity, asOf, location, readinessHardPrevious24Hours) {
			hard24 = true
		}
		if activityInWindow(activity, asOf, location, readinessHardPrevious48Hours) {
			hard48 = true
		}
	}
	for _, workout := range strength.Items {
		if workout.PerceivedEffort == nil || *workout.PerceivedEffort < 8 || workout.LinkedActivityID != nil {
			continue
		}
		if workoutInWindow(workout, asOf, location, readinessHardPrevious24Hours) {
			hard24 = true
		}
		if workoutInWindow(workout, asOf, location, readinessHardPrevious48Hours) {
			hard48 = true
		}
	}
	strained := recovery != nil && recovery.Classification == "strained"
	low := recovery != nil && recovery.Classification == "low"
	illness := checkin != nil && checkin.Illness.Present
	pain := checkin != nil && meaningfulPain(checkin.Pain)
	keyPlan, nextDayKeyPlan := false, false
	for _, plan := range nearby.Sessions {
		if plan.Status == "cancelled" || plan.Priority != "key" {
			continue
		}
		hardOrLong := isHardOrLongPlan(plan)
		delta := daysBetween(localDate, plan.Date)
		if hardOrLong && delta >= 0 && delta <= 2 {
			keyPlan = true
		}
		if hardOrLong && plan.Date == dateShift(localDate, 1) {
			nextDayKeyPlan = true
		}
	}
	if illness {
		result.Recommendation = "rest"
		result.Reasons = append(result.Reasons, ReadinessReason{Rule: "illness_reported", Actual: true, Threshold: false, SourceIDs: checkinIDs(checkin), Date: checkin.LocalDate, Explanation: "A current check-in reports illness."})
	}
	if pain {
		result.Recommendation = "rest"
		result.Reasons = append(result.Reasons, ReadinessReason{Rule: "meaningful_pain_reported", Actual: true, Threshold: false, SourceIDs: checkinIDs(checkin), Date: checkin.LocalDate, Explanation: "A current check-in reports meaningful pain."})
	}
	if low {
		result.Recommendation = "rest"
		result.Reasons = append(result.Reasons, ReadinessReason{Rule: "recovery_low", Actual: recovery.Classification, Threshold: "low", Date: localDate, Explanation: "The deterministic recovery classification is low."})
	}
	if (q.ProposedIntensity == "easy" || q.ProposedIntensity == "moderate") && strained && load.ConsecutiveTrainingDays >= 6 && hard24 && result.Recommendation != "rest" {
		result.Recommendation = "rest"
		result.Reasons = append(result.Reasons, ReadinessReason{Rule: "strained_six_days_hard_24h", Actual: map[string]any{"recovery": recovery.Classification, "consecutive_training_days": load.ConsecutiveTrainingDays, "hard_previous_24h": hard24}, Threshold: map[string]any{"consecutive_training_days": 6, "hard_previous_24h": true}, Date: localDate, Explanation: "Strained recovery, six consecutive training days, and a recent hard session meet the rest rule."})
	}
	if (q.ProposedIntensity == "hard" || q.ProposedIntensity == "maximal") && result.Recommendation != "rest" && (strained && hard48 || keyPlan) {
		result.Recommendation = "replace_with_easy"
		rule := "strained_hard_session_previous_48h"
		if keyPlan && !hard48 {
			rule = "key_hard_or_long_plan_nearby"
		}
		result.Reasons = append(result.Reasons, ReadinessReason{Rule: rule, Actual: map[string]any{"strained": strained, "hard_previous_48h": hard48, "key_plan_nearby": keyPlan}, Threshold: true, Date: localDate, Explanation: "The proposed high-intensity session conflicts with nearby deterministic load or plan evidence."})
	}
	if result.Recommendation == "proceed" && (strained || load.ConsecutiveTrainingDays >= 6 || load.Classification == "elevated_vs_recent_range" || nextDayKeyPlan) {
		result.Recommendation = "reduce"
		result.Reasons = append(result.Reasons, ReadinessReason{Rule: "reduce_for_load_or_plan_context", Actual: map[string]any{"recovery": recovery.Classification, "consecutive_training_days": load.ConsecutiveTrainingDays, "load_classification": load.Classification, "key_plan_next_day": nextDayKeyPlan}, Threshold: "any_match", Date: localDate, Explanation: "A configured reducing rule matched; this is not a medical safety assessment."})
	}
	if result.Recommendation == "proceed" {
		result.Counterpoints = append(result.Counterpoints, ReadinessReason{Rule: "no_configured_reducing_rule", Actual: "none", Threshold: "none", Date: localDate, Explanation: "No configured deterministic rule objected to the proposal."})
	}
	result.RulesEvaluated = []string{"illness_reported", "meaningful_pain_reported", "recovery_low", "strained_six_days_hard_24h", "hard_proposal_recent_hard_or_plan", "reduce_for_load_or_plan_context"}
	current := currentHealthItem(health.Items, localDate)
	currentSignals := 0
	if current != nil && current.Sleep != nil && (current.Sleep.MainSleepHours != nil || current.Sleep.Score != nil) {
		currentSignals++
	} else {
		result.RulesUnavailable = append(result.RulesUnavailable, "sleep_current")
	}
	if current != nil && current.HRV != nil && current.HRV.OvernightAverage != nil {
		currentSignals++
	} else {
		result.RulesUnavailable = append(result.RulesUnavailable, "hrv_current")
	}
	if current != nil && current.HeartRate != nil && current.HeartRate.Resting != nil {
		currentSignals++
	} else {
		result.RulesUnavailable = append(result.RulesUnavailable, "resting_hr_current")
	}
	if len(activities.Items) > 0 {
		currentSignals++
	} else {
		result.RulesUnavailable = append(result.RulesUnavailable, "recent_activities")
	}
	currentSignals++ // Plan lookup itself is a current, bounded observation, even when empty.
	switch {
	case currentSignals >= 5:
		result.Confidence = "high"
	case currentSignals >= 3:
		result.Confidence = "medium"
	case currentSignals == 2:
		result.Confidence = "low"
	default:
		result.Confidence = "unavailable"
	}
	if currentSignals < 2 && result.Recommendation == "proceed" {
		result.Recommendation = "reduce"
		result.Reasons = append(result.Reasons, ReadinessReason{Rule: "incomplete_evidence", Actual: currentSignals, Threshold: 2, Date: localDate, Explanation: "Evidence is incomplete, so the bounded default is to reduce."})
	}
	return result, nil
}

func currentHealthItem(items []DailyHealthItem, date string) *DailyHealthItem {
	for index := range items {
		if items[index].Date == date {
			return &items[index]
		}
	}
	return nil
}
func derefActivityName(value *string) string {
	if value == nil {
		return "Garmin activity"
	}
	return *value
}
func stringPointerNonEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func checkinIDs(checkin *Checkin) []string {
	if checkin == nil {
		return nil
	}
	return []string{checkin.ID}
}
func meaningfulPain(values []PainArea) bool {
	for _, value := range values {
		if value.Severity >= 3 {
			return true
		}
	}
	return false
}
func isHardOrLongPlan(plan PlannedSession) bool {
	normalized := fitnessanalysis.NormalizeSessionType(plan.SessionType)
	if normalized == "intervals" || normalized == "tempo" || normalized == "threshold" || normalized == "vo2" || normalized == "race" || normalized == "long" {
		return true
	}
	return plan.Targets.DurationMinutes != nil && *plan.Targets.DurationMinutes >= readinessLongDurationMinutes || plan.Targets.DistanceKM != nil && *plan.Targets.DistanceKM >= readinessLongDistanceKM
}
func activityInWindow(activity fitnessdata.ActivitySummary, asOf time.Time, location *time.Location, window time.Duration) bool {
	if activity.StartLocal != nil {
		parsed, err := time.Parse(time.RFC3339, *activity.StartLocal)
		if err == nil {
			return !parsed.After(asOf) && asOf.Sub(parsed) <= window
		}
	}
	date, err := time.ParseInLocation("2006-01-02", activity.Date, location)
	if err != nil {
		return false
	}
	asOfDate := time.Date(asOf.In(location).Year(), asOf.In(location).Month(), asOf.In(location).Day(), 0, 0, 0, 0, location)
	return !date.After(asOfDate) && asOfDate.Sub(date) <= window
}

func workoutInWindow(workout StrengthWorkout, asOf time.Time, location *time.Location, window time.Duration) bool {
	if workout.StartedAt != nil {
		parsed, err := time.Parse(time.RFC3339, *workout.StartedAt)
		if err == nil {
			return !parsed.After(asOf) && asOf.Sub(parsed) <= window
		}
	}
	date, err := time.ParseInLocation("2006-01-02", workout.Date, location)
	if err != nil {
		return false
	}
	asOfDate := time.Date(asOf.In(location).Year(), asOf.In(location).Month(), asOf.In(location).Day(), 0, 0, 0, 0, location)
	return !date.After(asOfDate) && asOfDate.Sub(date) <= window
}
