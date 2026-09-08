package fitnessrepo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/timdumol/fitsync/internal/fitnessanalysis"
	"github.com/timdumol/fitsync/internal/fitnessdata"
)

func (r *Repository) TrainingContext(ctx context.Context, athleteID, asOf string, lookback int, include []string) (TrainingContext, error) {
	if asOf == "" {
		timezone, e := r.AthleteTimezone(ctx, athleteID)
		if e != nil {
			return TrainingContext{}, e
		}
		asOf = r.clock().In(mustLocation(timezone)).Format("2006-01-02")
	}
	if err := validRange(asOf, asOf, 1); err != nil {
		return TrainingContext{}, err
	}
	if lookback < 7 || lookback > 90 {
		return TrainingContext{}, errors.New("invalid_input: lookback_days must be between 7 and 90")
	}
	allSections := allContextSections(include)
	wants := func(section string) bool { return allSections || hasString(include, section) }
	wantRecovery := wants("recovery")
	wantLoad := wants("training_load")
	wantActivities := wants("recent_activities")
	wantStrength := wants("strength")
	wantPlan := wants("plan")
	wantCheckins := wants("checkins")
	wantGoals := wants("goals")
	timezone, err := r.AthleteTimezone(ctx, athleteID)
	if err != nil {
		return TrainingContext{}, err
	}
	start := dateShift(asOf, -lookback)

	health := Page[DailyHealthItem]{}
	if wantRecovery {
		healthFrom := dateShift(asOf, -maxInt(lookback, 28))
		health, err = r.DailyHealth(ctx, athleteID, HealthQuery{From: healthFrom, To: asOf, Metrics: []string{"sleep", "hrv", "heart_rate", "stress", "body_battery"}, Order: "desc", Limit: 100})
		if err != nil {
			return TrainingContext{}, err
		}
	}

	activities := Page[fitnessdata.ActivitySummary]{}
	var manualStrength []StrengthWorkout
	if wantLoad || wantActivities {
		activityFrom := start
		if wantLoad {
			activityFrom = dateShift(asOf, -maxInt(lookback, 28))
		}
		// Summary is the analysis projection. Compact presentation data clears
		// the very effects/zones needed to classify hard sessions.
		activities, err = r.Activities(ctx, athleteID, ActivityQuery{From: activityFrom, To: asOf, Detail: "summary", Order: "desc", Limit: 100})
		if err != nil {
			return TrainingContext{}, err
		}
		if wantLoad {
			// Manual perceived effort is an analysis input. Join it onto the
			// internal summary projection before any presentation projection is
			// applied, while keeping the Garmin activity as the counted session.
			manual, manualErr := r.StrengthWorkouts(ctx, athleteID, StrengthQuery{From: activityFrom, To: asOf, IncludeEntries: false, Order: "asc", Limit: 100})
			if manualErr == nil {
				manualStrength = manual.Items
				for index := range activities.Items {
					for _, workout := range manual.Items {
						if workout.LinkedActivityID == nil || *workout.LinkedActivityID != activities.Items[index].ActivityID {
							continue
						}
						activities.Items[index].PerceivedEffort = workout.PerceivedEffort
						activities.Items[index].LinkedWorkoutID = &workout.WorkoutID
					}
				}
			}
		}
	}

	var profile Profile
	if wantGoals {
		profile, err = r.Profile(ctx, athleteID)
		if err != nil {
			return TrainingContext{}, err
		}
	}

	var plan PlanResult
	if wantPlan {
		plan, err = r.Plan(ctx, athleteID, dateShift(asOf, -1), dateShift(asOf, 7), true)
		if err != nil {
			return TrainingContext{}, err
		}
	}

	var latest *Checkin
	if wantRecovery || wantCheckins {
		latest = latestCheckin(ctx, r.db, athleteID, asOf)
	}
	recentActivities := activitiesInRange(activities.Items, start, asOf)
	context := TrainingContext{AthleteID: athleteID, AsOf: asOf, Timezone: timezone, GeneratedAt: r.clock().UTC().Format(time.RFC3339), DataStatus: health.DataStatus, Flags: []string{}}
	if wantGoals {
		goalsProfile := profile
		if !allSections {
			goalsProfile = contextGoalsProfile(profile)
		}
		context.Profile = &goalsProfile
		context.NearestTargetEvent = nearestEvent(profile.TargetEvents, asOf)
	}
	if wantRecovery {
		context.CurrentStatus.Recovery = classifyRecovery(health.Items, latest, asOf)
	}
	if wantLoad {
		loadActivities := activitiesInRange(activities.Items, dateShift(asOf, -28), asOf)
		var planned []PlannedSession
		if wantPlan {
			planned = plan.Sessions
		}
		context.CurrentStatus.TrainingLoad = calculateLoadWithPlans(loadActivities, asOf, health.Items, planned, manualStrength)
		strengthContext := aggregateStrengthForContext(ctx, r, athleteID, dateShift(asOf, -28), asOf)
		context.CurrentStatus.TrainingLoad.StrengthContext = StrengthContext{Sessions: strengthContext.Sessions, WorkingSets: strengthContext.WorkingSets, HighEffortSets: strengthContext.HighEffortSets, Method: "structured_strength_summary_v1"}
	}
	if wantActivities {
		context.RecentActivities = limitActivities(recentActivities, 10)
		context.RecentTraining.Runs = aggregateActivities(recentActivities, "running")
	}
	if wantStrength {
		context.RecentTraining.Strength = aggregateStrengthForContext(ctx, r, athleteID, start, asOf)
	}
	if wantPlan {
		context.PlannedSessions = plan.Sessions
	}
	if wantCheckins {
		context.LatestCheckin = latest
	}
	if (wantRecovery || wantCheckins) && latest == nil {
		context.Flags = append(context.Flags, "NO_CURRENT_CHECKIN")
	}
	if wantGoals && len(profile.ConditionsAndInjuries) == 0 {
		context.Flags = append(context.Flags, "NO_CURRENT_INJURY_DATA")
	}
	if wantRecovery && len(health.DataStatus.MissingDates) > 0 {
		context.Flags = append(context.Flags, "INCOMPLETE_RECOVERY_DATA")
	}
	if wantRecovery && health.DataStatus.LastSyncAt != nil {
		if synced, parseErr := time.Parse(time.RFC3339, *health.DataStatus.LastSyncAt); parseErr == nil && r.clock().Sub(synced) > 48*time.Hour {
			context.Flags = append(context.Flags, "STALE_SYNC")
		}
	}
	if wantRecovery && context.CurrentStatus.Recovery != nil {
		if context.CurrentStatus.Recovery.Classification == "low" {
			context.Flags = append(context.Flags, "RECOVERY_LOW")
		}
		if context.CurrentStatus.Recovery.Classification == "strained" {
			context.Flags = append(context.Flags, "RECOVERY_STRAINED")
		}
		if latest != nil && latest.Illness.Present {
			context.Flags = append(context.Flags, "ILLNESS_REPORTED")
		}
		if latest != nil && len(latest.Pain) > 0 {
			context.Flags = append(context.Flags, "PAIN_REPORTED")
		}
	}
	if wantLoad && context.CurrentStatus.TrainingLoad != nil && context.CurrentStatus.TrainingLoad.Classification == "elevated_vs_recent_range" {
		context.Flags = append(context.Flags, "LOAD_ELEVATED")
	}
	for _, s := range context.PlannedSessions {
		if s.Date == dateShift(asOf, 1) && (s.SessionType == "intervals" || s.SessionType == "tempo" || s.SessionType == "threshold" || s.SessionType == "race") {
			context.Flags = append(context.Flags, "PLANNED_HARD_SESSION")
		}
	}
	return context, nil
}

func aggregateStrengthForContext(ctx context.Context, r *Repository, athleteID, from, to string) StrengthAggregate {
	page, e := r.StrengthWorkouts(ctx, athleteID, StrengthQuery{From: from, To: to, IncludeEntries: true, Order: "desc", Limit: 100})
	if e != nil {
		return StrengthAggregate{}
	}
	agg := StrengthAggregate{MuscleGroupSets: map[string]int{}}
	for _, w := range page.Items {
		agg.Sessions++
		agg.WorkingSets += w.Summary.WorkingSets
		if agg.LatestDate == nil || w.Date > *agg.LatestDate {
			d := w.Date
			agg.LatestDate = &d
		}
		// Strength duration is not part of the load aggregate.
		if w.Summary.VolumeLoadKG != nil {
			addFloat(&agg.VolumeLoadKG, *w.Summary.VolumeLoadKG)
		}
		agg.HighEffortSets += w.Summary.HighEffortSets
		for g, n := range w.Summary.MuscleGroupSets {
			agg.MuscleGroupSets[g] += n
		}
	}
	return agg
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func allContextSections(include []string) bool {
	if len(include) == 0 {
		return true
	}
	for _, section := range []string{"recovery", "training_load", "recent_activities", "strength", "plan", "checkins", "goals"} {
		if !hasString(include, section) {
			return false
		}
	}
	return true
}

func activitiesInRange(items []fitnessdata.ActivitySummary, from, to string) []fitnessdata.ActivitySummary {
	result := make([]fitnessdata.ActivitySummary, 0, len(items))
	for _, item := range items {
		if item.Date >= from && item.Date <= to {
			result = append(result, item)
		}
	}
	return result
}

func limitActivities(items []fitnessdata.ActivitySummary, limit int) []fitnessdata.ActivitySummary {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func contextGoalsProfile(profile Profile) Profile {
	profile.BirthYear = nil
	profile.TargetEvents = nil
	profile.WeeklyAvailability = nil
	profile.Preferences = Preferences{}
	profile.HeartRateZones = nil
	profile.PaceZones = nil
	profile.PowerZones = nil
	profile.Constraints = nil
	profile.ConditionsAndInjuries = nil
	profile.SensitiveFieldsPresent = false
	return profile
}

func classifyRecovery(items []DailyHealthItem, checkin *Checkin, asOf string) *RecoveryStatus {
	method := RecoveryMethod{Name: "rule_based_recovery_v2", Version: 2, Evaluations: []RecoveryEvaluation{}, UnavailableRules: []string{}}
	empty := func() *RecoveryStatus {
		return &RecoveryStatus{Classification: "unknown", Confidence: "unavailable", Sleep: RecoverySleepEvidence{SourceStatus: "unavailable"}, HRV: RecoverySignalEvidence{SourceStatus: "unavailable"}, RestingHeartRateSignal: RecoverySignalEvidence{SourceStatus: "unavailable"}, Method: method}
	}
	if len(items) == 0 {
		method.UnavailableRules = []string{"sleep_score_low", "sleep_duration_low", "hrv_suppressed", "rhr_elevated", "body_battery_low"}
		return empty()
	}
	var current *DailyHealthItem
	for i := range items {
		if items[i].Date <= asOf {
			current = &items[i]
			break
		}
	}
	if current == nil {
		method.UnavailableRules = []string{"sleep_score_low", "sleep_duration_low", "hrv_suppressed", "rhr_elevated", "body_battery_low"}
		return empty()
	}
	result := &RecoveryStatus{Classification: "normal", Confidence: "unavailable", Sleep: RecoverySleepEvidence{SourceStatus: "unavailable"}, HRV: RecoverySignalEvidence{SourceStatus: "unavailable"}, RestingHeartRateSignal: RecoverySignalEvidence{SourceStatus: "unavailable"}, Method: method}
	objective := 0
	negative := []string{}
	addEvaluation := func(rule string, value any, operator string, threshold any, matched bool) {
		result.Method.Evaluations = append(result.Method.Evaluations, RecoveryEvaluation{Rule: rule, Value: value, Operator: operator, Threshold: threshold, Matched: matched})
		if matched {
			negative = append(negative, rule)
		}
	}
	addUnavailable := func(rule string) { result.Method.UnavailableRules = append(result.Method.UnavailableRules, rule) }
	if current.Sleep != nil {
		result.SleepScore = current.Sleep.Score
		totalSleep := current.Sleep.TotalSleep24H
		if totalSleep == nil {
			totalSleep = current.Sleep.TotalHours
		}
		result.SleepHours = totalSleep
		result.Sleep = RecoverySleepEvidence{MainHours: current.Sleep.MainSleepHours, NapHours: current.Sleep.NapSleepHours, Total24H: totalSleep, Score: current.Sleep.Score, Available: current.Sleep.SignalStatus.Available, ObservedDate: current.Sleep.SignalStatus.ObservedDate, LastObservedDate: current.Sleep.SignalStatus.LastObservedDate, AgeDays: current.Sleep.SignalStatus.AgeDays, Stale: current.Sleep.SignalStatus.Stale, SourceStatus: current.Sleep.SignalStatus.SourceStatus}
		if !result.Sleep.Available && (result.Sleep.MainHours != nil || result.Sleep.Score != nil) {
			result.Sleep.Available = true
			result.Sleep.ObservedDate = pointerToString(current.Date)
		}
		if current.Sleep.Score != nil {
			objective++
			addEvaluation("sleep_score_low", *current.Sleep.Score, "<", 60.0, *current.Sleep.Score < 60)
		} else {
			addUnavailable("sleep_score_low")
		}
		if totalSleep != nil {
			objective++
			addEvaluation("sleep_duration_low", *totalSleep, "<", 6.0, *totalSleep < 6)
		} else {
			addUnavailable("sleep_duration_low")
		}
	} else {
		addUnavailable("sleep_score_low")
		addUnavailable("sleep_duration_low")
	}
	if current.HRV != nil && current.HRV.OvernightAverage != nil {
		result.OvernightHRV = current.HRV.OvernightAverage
		values := priorHealthNumbers(items, current.Date, func(i DailyHealthItem) *float64 {
			if i.HRV != nil {
				return i.HRV.OvernightAverage
			}
			return nil
		})
		if base := medianPrevious(values, current.Date); base != nil && *base != 0 {
			result.HRVBaseline = base
			ratio := *result.OvernightHRV / *base
			result.HRVRatio = &ratio
			addEvaluation("hrv_suppressed", ratio, "<", .85, ratio < .85)
		} else {
			addUnavailable("hrv_suppressed")
		}
		result.HRV = RecoverySignalEvidence{Value: current.HRV.OvernightAverage, Baseline: result.HRVBaseline, Ratio: result.HRVRatio, Available: current.HRV.SignalStatus.Available, ObservedDate: current.HRV.SignalStatus.ObservedDate, LastObservedDate: current.HRV.SignalStatus.LastObservedDate, AgeDays: current.HRV.SignalStatus.AgeDays, Stale: current.HRV.SignalStatus.Stale, SourceStatus: current.HRV.SignalStatus.SourceStatus}
		if !result.HRV.Available {
			result.HRV.Available = true
			result.HRV.ObservedDate = pointerToString(current.Date)
		}
		objective++
	} else {
		addUnavailable("hrv_suppressed")
	}
	if current.HeartRate != nil && current.HeartRate.Resting != nil {
		result.RestingHeartRate = current.HeartRate.Resting
		values := priorHealthNumbers(items, current.Date, func(i DailyHealthItem) *float64 {
			if i.HeartRate != nil {
				return i.HeartRate.Resting
			}
			return nil
		})
		if base := medianPrevious(values, current.Date); base != nil {
			result.RHRBaseline = base
			delta := *result.RestingHeartRate - *base
			result.RHRDelta = &delta
			addEvaluation("rhr_elevated", delta, ">=", 7.0, delta >= 7)
		} else {
			addUnavailable("rhr_elevated")
		}
		result.RestingHeartRateSignal = RecoverySignalEvidence{Value: current.HeartRate.Resting, Baseline: result.RHRBaseline, Delta: result.RHRDelta, Available: current.HeartRate.SignalStatus.Available, ObservedDate: current.HeartRate.SignalStatus.ObservedDate, LastObservedDate: current.HeartRate.SignalStatus.LastObservedDate, AgeDays: current.HeartRate.SignalStatus.AgeDays, Stale: current.HeartRate.SignalStatus.Stale, SourceStatus: current.HeartRate.SignalStatus.SourceStatus}
		if !result.RestingHeartRateSignal.Available {
			result.RestingHeartRateSignal.Available = true
			result.RestingHeartRateSignal.ObservedDate = pointerToString(current.Date)
		}
		objective++
	} else {
		addUnavailable("rhr_elevated")
	}
	if current.BodyBattery != nil && current.BodyBattery.AtWake != nil {
		result.BodyBatteryAtWake = current.BodyBattery.AtWake
		objective++
		addEvaluation("body_battery_low", *result.BodyBatteryAtWake, "<", 35.0, *result.BodyBatteryAtWake < 35)
	} else {
		addUnavailable("body_battery_low")
	}
	if checkin != nil {
		result.SubjectiveFatigue = checkin.Fatigue
		result.Soreness = checkin.Soreness
		if checkin.Fatigue != nil {
			objective++
			addEvaluation("high_fatigue", *checkin.Fatigue, ">=", 4.0, *checkin.Fatigue >= 4)
		}
		if checkin.Illness.Present {
			addEvaluation("illness_present", true, "=", true, true)
		}
		for _, p := range checkin.Pain {
			if p.Severity >= 3 {
				addEvaluation("meaningful_pain", p.Severity, ">=", 3, true)
				break
			}
		}
	}
	result.Method.InputsUsed = objective
	result.Method.InputsAvailable = 5
	result.Method.ValidPrimarySignals = objective
	result.Method.NegativeSignals = negative
	if objective < 2 && len(negative) == 0 {
		result.Classification = "unknown"
	} else if hasString(negative, "illness_present") || hasString(negative, "meaningful_pain") || len(negative) >= 3 {
		result.Classification = "low"
	} else if len(negative) > 0 {
		result.Classification = "strained"
	}
	if objective >= 4 {
		result.Confidence = "high"
	} else if objective >= 2 {
		result.Confidence = "medium"
	} else if objective == 1 && len(negative) > 0 {
		result.Confidence = "low"
	}
	result.Method.Confidence = result.Confidence
	return result
}

func calculateLoad(activities []fitnessdata.ActivitySummary, asOf string, health []DailyHealthItem) *TrainingLoadStatus {
	return calculateLoadWithPlans(activities, asOf, health, nil)
}

func calculateLoadWithPlans(activities []fitnessdata.ActivitySummary, asOf string, health []DailyHealthItem, planned []PlannedSession, structured ...[]StrengthWorkout) *TrainingLoadStatus {
	result := &TrainingLoadStatus{Classification: "unavailable", HardSessions: []HardSessionEvidence{}, DoubleSessionEvidence: []DoubleSessionDay{}, Method: LoadMethod{Name: "garmin_activity_load_sum_v2", Confidence: "unavailable"}}
	// Activity records can be repeated when a source exposes both a summary
	// and a detail projection. A stable activity ID is the de-duplication key.
	unique := make([]fitnessdata.ActivitySummary, 0, len(activities))
	seen := map[string]bool{}
	for _, activity := range activities {
		key := activity.ActivityID
		if key == "" {
			key = activity.RecordKey
		}
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		unique = append(unique, activity)
	}
	activities = unique
	linkedActivityIndex := map[string]int{}
	for index, activity := range activities {
		if activity.ActivityID != "" {
			linkedActivityIndex[activity.ActivityID] = index
		}
	}
	manual := []StrengthWorkout{}
	if len(structured) > 0 {
		manual = structured[0]
	}
	// Join linked manual effort onto the Garmin activity before classification.
	// An unlinked manual workout remains its own evidence item below.
	for _, workout := range manual {
		if workout.LinkedActivityID == nil || workout.PerceivedEffort == nil {
			continue
		}
		if index, ok := linkedActivityIndex[*workout.LinkedActivityID]; ok {
			activities[index].PerceivedEffort = workout.PerceivedEffort
			if activities[index].LinkedWorkoutID == nil {
				id := workout.WorkoutID
				activities[index].LinkedWorkoutID = &id
			}
		}
	}
	for _, a := range activities {
		result.Method.TotalActivities++
		if a.TrainingLoad != nil {
			result.Method.ActivitiesWithLoad++
		}
	}
	var last7, prev7, last28 float64
	var has28, has7, hasPrev bool
	var runningDistance [3]float64
	var runningDuration [3]float64
	var runningDistanceKnown [3]bool
	var runningDurationKnown [3]bool
	var runningLoad float64
	runningLoadKnown := false
	currentRunning, previousRunning := 0.0, 0.0
	currentRunningKnown, previousRunningKnown := false, false
	byDate := map[string][]fitnessdata.ActivitySummary{}
	for _, a := range activities {
		if a.DurationMinutes != nil && *a.DurationMinutes >= 20 {
			byDate[a.Date] = append(byDate[a.Date], a)
		}
		if a.TrainingLoad == nil {
			// Distance and duration remain useful even when Garmin load is
			// unavailable.
		} else {
			days := daysBetween(a.Date, asOf)
			if days >= 0 && days < 7 {
				last7 += *a.TrainingLoad
				has7 = true
			}
			if days >= 7 && days < 14 {
				prev7 += *a.TrainingLoad
				hasPrev = true
			}
			if days >= 0 && days < 28 {
				last28 += *a.TrainingLoad
				has28 = true
			}
			if a.Category == "running" {
				runningLoad += *a.TrainingLoad
				runningLoadKnown = true
			}
		}
		if a.Category == "running" {
			days := daysBetween(a.Date, asOf)
			for index, window := range []int{7, 14, 28} {
				if days >= 0 && days < window {
					if a.DistanceKM != nil {
						runningDistance[index] += *a.DistanceKM
						runningDistanceKnown[index] = true
					}
					if a.DurationMinutes != nil {
						runningDuration[index] += *a.DurationMinutes
						runningDurationKnown[index] = true
					}
				}
			}
			if days >= 0 && days < 7 && a.DistanceKM != nil {
				currentRunning += *a.DistanceKM
				currentRunningKnown = true
			}
			if days >= 7 && days < 14 && a.DistanceKM != nil {
				previousRunning += *a.DistanceKM
				previousRunningKnown = true
			}
		}
	}
	if has7 {
		result.Last7Days = &last7
	}
	if hasPrev {
		result.Previous7Days = &prev7
	}
	if has28 {
		result.Last28Days = &last28
	}
	chronic := last28 / 4
	if result.Method.ActivitiesWithLoad >= 4 && chronic > 0 {
		result.ChronicWeeklyAverage = &chronic
		ratio := last7 / chronic
		result.AcuteChronicRatio = &ratio
		if ratio < .8 {
			result.Classification = "under_recent_range"
		} else if ratio <= 1.3 {
			result.Classification = "within_recent_range"
		} else {
			result.Classification = "elevated_vs_recent_range"
		}
	}
	plannedTypes := map[string]string{}
	plannedWorkoutTypes := map[string]string{}
	for _, session := range planned {
		if session.CompletedActivityID != nil {
			plannedTypes[*session.CompletedActivityID] = session.SessionType
		}
		if session.CompletedWorkoutID != nil {
			plannedWorkoutTypes[*session.CompletedWorkoutID] = session.SessionType
		}
	}
	for _, a := range activities {
		if daysBetween(a.Date, asOf) < 0 || daysBetween(a.Date, asOf) >= 7 {
			continue
		}
		plannedType := plannedTypes[a.ActivityID]
		if plannedType == "" && a.LinkedWorkoutID != nil {
			plannedType = plannedWorkoutTypes[*a.LinkedWorkoutID]
		}
		classification := fitnessanalysis.ClassifyHardActivity(a, plannedType)
		evidence := HardSessionEvidence{ActivityID: a.ActivityID, Date: a.Date, Hard: classification.Hard, Method: classification.Method, Reasons: make([]HardSessionReason, 0, len(classification.Reasons))}
		for _, reason := range classification.Reasons {
			evidence.Reasons = append(evidence.Reasons, HardSessionReason{Rule: reason.Rule, Value: reason.Value, Threshold: reason.Threshold})
		}
		if evidence.Hard {
			result.HardSessionsLast7Days++
		}
		if evidence.Hard {
			result.HardSessions = append(result.HardSessions, evidence)
		}
	}
	for _, workout := range manual {
		if workout.LinkedActivityID != nil || workout.PerceivedEffort == nil || daysBetween(workout.Date, asOf) < 0 || daysBetween(workout.Date, asOf) >= 7 || *workout.PerceivedEffort < 8 {
			continue
		}
		value := *workout.PerceivedEffort
		result.HardSessionsLast7Days++
		result.HardSessions = append(result.HardSessions, HardSessionEvidence{WorkoutID: workout.WorkoutID, Date: workout.Date, Hard: true, Method: "hard_session_v2", Reasons: []HardSessionReason{{Rule: "perceived_effort", Value: value, Threshold: 8}}})
	}
	result.ConsecutiveTrainingDays = consecutiveDays(activities, asOf)
	for i := range runningDistance {
		if runningDistanceKnown[i] {
			value := runningDistance[i]
			if i == 0 {
				result.RunningDistance7Days = &value
			}
			if i == 1 {
				result.RunningDistance14Days = &value
			}
			if i == 2 {
				result.RunningDistance28Days = &value
			}
		}
	}
	for i := range runningDuration {
		if runningDurationKnown[i] {
			value := runningDuration[i]
			if i == 0 {
				result.RunningDuration7Days = &value
			}
			if i == 1 {
				result.RunningDuration14Days = &value
			}
			if i == 2 {
				result.RunningDuration28Days = &value
			}
		}
	}
	if currentRunningKnown && previousRunningKnown && previousRunning != 0 {
		value := (currentRunning - previousRunning) / previousRunning * 100
		result.RunningDistanceChangePercent = &value
	}
	if runningLoadKnown {
		result.RunningGarminLoad = &runningLoad
	}
	if has7 {
		value := last7
		result.TotalGarminLoad = &value
	}
	for date, sessions := range byDate {
		if len(sessions) < 2 {
			continue
		}
		ids, categories := make([]string, 0, len(sessions)), make([]string, 0, len(sessions))
		for _, session := range sessions {
			ids = append(ids, session.ActivityID)
			categories = append(categories, session.Category)
		}
		result.DoubleSessionEvidence = append(result.DoubleSessionEvidence, DoubleSessionDay{Date: date, SessionIDs: ids, Categories: categories})
	}
	sort.Slice(result.DoubleSessionEvidence, func(i, j int) bool {
		return result.DoubleSessionEvidence[i].Date < result.DoubleSessionEvidence[j].Date
	})
	for _, day := range result.DoubleSessionEvidence {
		distance := daysBetween(day.Date, asOf)
		if distance >= 0 && distance < 7 {
			result.DoubleSessionDays7++
		}
		if distance >= 0 && distance < 28 {
			result.DoubleSessionDays28++
		}
	}
	for distance := 0; distance < 90; distance++ {
		if len(byDate[dateShift(asOf, -distance)]) == 0 {
			result.DaysSinceRestDay = &distance
			break
		}
	}
	result.Method.InputsUsed = len(activities)
	result.Method.InputsAvailable = result.Method.ActivitiesWithLoad
	if result.Method.ActivitiesWithLoad > 0 {
		result.Method.Confidence = "medium"
	}
	return result
}

func hardActivity(a fitnessdata.ActivitySummary) bool {
	return fitnessanalysis.ClassifyHardActivity(a, "").Hard
}

func pointerToString(value string) *string {
	return &value
}
func consecutiveDays(activities []fitnessdata.ActivitySummary, asOf string) int {
	days := map[string]bool{}
	for _, a := range activities {
		if a.DurationMinutes != nil && *a.DurationMinutes >= 20 {
			days[a.Date] = true
		}
	}
	count := 0
	for date := asOf; ; date = dateShift(date, -1) {
		if !days[date] {
			break
		}
		count++
	}
	return count
}
func aggregateActivities(items []fitnessdata.ActivitySummary, category string) TrainingAggregate {
	result := TrainingAggregate{}
	for _, a := range items {
		if category != "" && a.Category != category {
			continue
		}
		result.Sessions++
		if a.DurationMinutes != nil {
			addFloat(&result.DurationMinutes, *a.DurationMinutes)
		}
		if a.DistanceKM != nil {
			addFloat(&result.DistanceKM, *a.DistanceKM)
		}
		if a.TrainingLoad != nil {
			addFloat(&result.Load, *a.TrainingLoad)
		}
		if result.LatestDate == nil || a.Date > *result.LatestDate {
			d := a.Date
			result.LatestDate = &d
		}
	}
	return result
}

func (r *Repository) TrainingAnalysis(ctx context.Context, athleteID string, q AnalysisQuery) (TrainingAnalysis, error) {
	if err := validRange(q.From, q.To, 366); err != nil {
		return TrainingAnalysis{}, err
	}
	if q.GroupBy == "" {
		q.GroupBy = "week"
	}
	if q.GroupBy != "day" && q.GroupBy != "week" && q.GroupBy != "month" {
		return TrainingAnalysis{}, errors.New("invalid_input: group_by")
	}
	activities, err := r.Activities(ctx, athleteID, ActivityQuery{From: q.From, To: q.To, Categories: q.Categories, Order: "asc", Limit: 100})
	if err != nil {
		return TrainingAnalysis{}, err
	}
	period := AnalysisAggregates{}
	bucketMap := map[string]*AnalysisBucket{}
	activeDates := map[string]bool{}
	for _, a := range activities.Items {
		period.ActivityCount++
		activeDates[a.Date] = true
		if a.DurationMinutes != nil {
			addFloat(&period.DurationMinutes, *a.DurationMinutes)
		}
		if a.DistanceKM != nil {
			addFloat(&period.DistanceKM, *a.DistanceKM)
		}
		if a.TrainingLoad != nil {
			addFloat(&period.GarminLoad, *a.TrainingLoad)
		}
		if a.Category == "running" {
			period.Running.Frequency++
			if a.DistanceKM != nil {
				addFloat(&period.Running.DistanceKM, *a.DistanceKM)
				if period.Running.LongestRunKM == nil || *a.DistanceKM > *period.Running.LongestRunKM {
					v := *a.DistanceKM
					period.Running.LongestRunKM = &v
				}
			}
			if a.DurationMinutes != nil {
				addFloat(&period.Running.DurationMinutes, *a.DurationMinutes)
			}
		}
		key := bucketKey(a.Date, q.GroupBy)
		b := bucketMap[key]
		if b == nil {
			start, end := bucketBounds(a.Date, q.GroupBy)
			b = &AnalysisBucket{Start: start, End: end}
			bucketMap[key] = b
		}
		b.Activities++
		if a.DurationMinutes != nil {
			addFloat(&b.DurationMinutes, *a.DurationMinutes)
		}
		if a.DistanceKM != nil {
			addFloat(&b.DistanceKM, *a.DistanceKM)
		}
		if a.TrainingLoad != nil {
			addFloat(&b.GarminLoad, *a.TrainingLoad)
		}
	}
	period.ActiveDays = len(activeDates)
	if len(q.Categories) == 0 || hasString(q.Categories, "strength") {
		strength, _ := r.StrengthWorkouts(ctx, athleteID, StrengthQuery{From: q.From, To: q.To, IncludeEntries: true, Order: "asc", Limit: 100})
		for _, w := range strength.Items {
			period.Strength.Sessions++
			period.Strength.WorkingSets += w.Summary.WorkingSets
			if w.Summary.VolumeLoadKG != nil {
				addFloat(&period.Strength.VolumeLoadKG, *w.Summary.VolumeLoadKG)
			}
			for g, n := range w.Summary.MuscleGroupSets {
				period.Strength.MuscleGroupSets = ensureMap(period.Strength.MuscleGroupSets)
				period.Strength.MuscleGroupSets[g] += n
			}
		}
	}
	health, _ := r.DailyHealth(ctx, athleteID, HealthQuery{From: q.From, To: q.To, Order: "asc", Limit: 100})
	for _, h := range health.Items {
		if h.Sleep != nil {
			if h.Sleep.TotalHours != nil {
				addFloat(&period.Recovery.SleepHours, *h.Sleep.TotalHours)
			}
			if h.Sleep.Score != nil {
				addFloat(&period.Recovery.SleepScore, *h.Sleep.Score)
			}
			period.Recovery.SleepSamples++
		}
	}
	planned, _ := r.Plan(ctx, athleteID, q.From, q.To, true)
	for _, s := range planned.Sessions {
		if s.Date > q.To || s.Status == "cancelled" {
			continue
		}
		period.PlanConsistency.Planned++
		if s.Status == "completed" {
			period.PlanConsistency.Completed++
		}
		if s.Status == "skipped" {
			period.PlanConsistency.Skipped++
		}
	}
	if period.PlanConsistency.Planned > 0 {
		rate := float64(period.PlanConsistency.Completed) / float64(period.PlanConsistency.Planned)
		period.PlanConsistency.CompletionRate = &rate
	}
	buckets := make([]AnalysisBucket, 0, len(bucketMap))
	for _, b := range bucketMap {
		buckets = append(buckets, *b)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Start < buckets[j].Start })
	result := TrainingAnalysis{From: q.From, To: q.To, Buckets: buckets, Current: period, Methods: map[string]Method{"activity_load": {Name: "garmin_activity_load_sum_v1", ValidSamples: countLoads(activities.Items), RequiredSamples: 1}, "means": {Name: "null_ignoring_means", ValidSamples: len(health.Items)}}}
	if q.Compare {
		compareFrom := dateShift(q.From, -daysBetween(q.From, q.To)-1)
		compareTo := dateShift(q.From, -1)
		result.Comparison = &AnalysisPeriod{From: compareFrom, To: compareTo}
		compare, ce := r.TrainingAnalysis(ctx, athleteID, AnalysisQuery{From: compareFrom, To: compareTo, GroupBy: q.GroupBy, Categories: q.Categories})
		if ce == nil {
			result.ComparisonAggregates = &compare.Current
			result.Changes = aggregateChanges(period, compare.Current)
		}
	}
	return result, nil
}

func ensureMap(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}
func countLoads(items []fitnessdata.ActivitySummary) int {
	n := 0
	for _, a := range items {
		if a.TrainingLoad != nil {
			n++
		}
	}
	return n
}
func aggregateChanges(a, b AnalysisAggregates) map[string]Change {
	result := map[string]Change{}
	for name, x := range map[string][2]*float64{"activity_count": [2]*float64{floatPtr(float64(a.ActivityCount)), floatPtr(float64(b.ActivityCount))}, "duration_minutes": [2]*float64{a.DurationMinutes, b.DurationMinutes}, "distance_km": [2]*float64{a.DistanceKM, b.DistanceKM}, "garmin_load": [2]*float64{a.GarminLoad, b.GarminLoad}} {
		if x[0] == nil || x[1] == nil {
			continue
		}
		v := *x[0] - *x[1]
		change := Change{Absolute: &v}
		if *x[1] != 0 {
			p := v / *x[1] * 100
			change.Percent = &p
		}
		result[name] = change
	}
	return result
}
func floatPtr(v float64) *float64 { return &v }
func addFloat(target **float64, value float64) {
	if *target == nil {
		v := 0.0
		*target = &v
	}
	**target += value
}
func dateShift(value string, days int) string {
	parsed, _ := time.Parse("2006-01-02", value)
	return parsed.AddDate(0, 0, days).Format("2006-01-02")
}
func daysBetween(date, asOf string) int {
	a, e := time.Parse("2006-01-02", date)
	b, e2 := time.Parse("2006-01-02", asOf)
	if e != nil || e2 != nil {
		return 1 << 30
	}
	return int(b.Sub(a).Hours() / 24)
}
func nearestEvent(events []TargetEvent, asOf string) *TargetEvent {
	var best *TargetEvent
	for _, event := range events {
		if event.Date < asOf {
			continue
		}
		if best == nil || event.Date < best.Date {
			copy := event
			best = &copy
		}
	}
	return best
}
func latestCheckin(ctx context.Context, db DBTX, athleteID, asOf string) *Checkin {
	var id, date, timezone, raw string
	var at int64
	if db.QueryRowContext(ctx, "SELECT id,recorded_at_utc_ms,local_date,timezone,content_raw FROM checkins WHERE athlete_id=? AND deleted_at IS NULL AND local_date<=? ORDER BY local_date DESC,recorded_at_utc_ms DESC LIMIT 1", athleteID, asOf).Scan(&id, &at, &date, &timezone, &raw) != nil {
		return nil
	}
	value := decodeCheckin(id, at, date, timezone, raw)
	if daysBetween(date, asOf) > 7 {
		return nil
	}
	return &value
}
func validHealthNumbers(items []DailyHealthItem, fn func(DailyHealthItem) *float64) []float64 {
	result := []float64{}
	for _, item := range items {
		if v := fn(item); v != nil {
			result = append(result, *v)
		}
	}
	return result
}

func priorHealthNumbers(items []DailyHealthItem, before string, fn func(DailyHealthItem) *float64) []float64 {
	result := []float64{}
	for _, item := range items {
		if item.Date < before {
			if value := fn(item); value != nil {
				result = append(result, *value)
			}
		}
	}
	return result
}
func medianPrevious(values []float64, currentDate string) *float64 {
	_ = currentDate
	if len(values) < 7 {
		return nil
	}
	sort.Float64s(values)
	v := values[len(values)/2]
	return &v
}
func hasString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func bucketKey(date, group string) string {
	if group == "day" {
		return date
	}
	parsed, _ := time.Parse("2006-01-02", date)
	if group == "month" {
		return parsed.Format("2006-01")
	}
	year, week := parsed.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}
func bucketBounds(date, group string) (string, string) {
	parsed, _ := time.Parse("2006-01-02", date)
	switch group {
	case "day":
		return date, date
	case "month":
		start := time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start.Format("2006-01-02"), start.AddDate(0, 1, -1).Format("2006-01-02")
	default:
		weekday := (int(parsed.Weekday()) + 6) % 7
		start := parsed.AddDate(0, 0, -weekday)
		return start.Format("2006-01-02"), start.AddDate(0, 0, 6).Format("2006-01-02")
	}
}
