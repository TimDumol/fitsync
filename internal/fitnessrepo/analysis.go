package fitnessrepo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

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
	timezone, err := r.AthleteTimezone(ctx, athleteID)
	if err != nil {
		return TrainingContext{}, err
	}
	start := dateShift(asOf, -lookback)
	health, err := r.DailyHealth(ctx, athleteID, HealthQuery{From: start, To: asOf, Metrics: []string{"sleep", "hrv", "heart_rate", "stress", "body_battery"}, Order: "desc", Limit: 100})
	if err != nil {
		return TrainingContext{}, err
	}
	activities, err := r.Activities(ctx, athleteID, ActivityQuery{From: start, To: asOf, Detail: "compact", Order: "desc", Limit: 100})
	if err != nil {
		return TrainingContext{}, err
	}
	profile, err := r.Profile(ctx, athleteID)
	if err != nil {
		return TrainingContext{}, err
	}
	plan, err := r.Plan(ctx, athleteID, dateShift(asOf, -1), dateShift(asOf, 7), true)
	if err != nil {
		return TrainingContext{}, err
	}
	latest := latestCheckin(ctx, r.db, athleteID, asOf)
	context := TrainingContext{AthleteID: athleteID, AsOf: asOf, Timezone: timezone, GeneratedAt: r.clock().UTC().Format(time.RFC3339), DataStatus: health.DataStatus, Profile: &profile, PlannedSessions: plan.Sessions, LatestCheckin: latest, RecentActivities: activities.Items, Flags: []string{}}
	context.NearestTargetEvent = nearestEvent(profile.TargetEvents, asOf)
	context.CurrentStatus.Recovery = classifyRecovery(health.Items, latest, asOf)
	context.CurrentStatus.TrainingLoad = calculateLoad(activities.Items, asOf, health.Items)
	context.RecentTraining.Runs = aggregateActivities(activities.Items, "running")
	strength := aggregateStrengthForContext(ctx, r, athleteID, start, asOf)
	context.RecentTraining.Strength = strength
	if latest == nil {
		context.Flags = append(context.Flags, "NO_CURRENT_CHECKIN")
	}
	if len(profile.ConditionsAndInjuries) == 0 {
		context.Flags = append(context.Flags, "NO_CURRENT_INJURY_DATA")
	}
	if len(health.DataStatus.MissingDates) > 0 {
		context.Flags = append(context.Flags, "INCOMPLETE_RECOVERY_DATA")
	}
	if health.DataStatus.LastSyncAt != nil {
		if synced, parseErr := time.Parse(time.RFC3339, *health.DataStatus.LastSyncAt); parseErr == nil && r.clock().Sub(synced) > 48*time.Hour {
			context.Flags = append(context.Flags, "STALE_SYNC")
		}
	}
	if context.CurrentStatus.Recovery != nil {
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
	if context.CurrentStatus.TrainingLoad != nil && context.CurrentStatus.TrainingLoad.Classification == "elevated_vs_recent_range" {
		context.Flags = append(context.Flags, "LOAD_ELEVATED")
	}
	for _, s := range plan.Sessions {
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

func classifyRecovery(items []DailyHealthItem, checkin *Checkin, asOf string) *RecoveryStatus {
	if len(items) == 0 {
		return &RecoveryStatus{Classification: "unknown", Confidence: "unavailable", Method: RecoveryMethod{Name: "rule_based_recovery_v1"}}
	}
	var current *DailyHealthItem
	for i := range items {
		if items[i].Date <= asOf {
			current = &items[i]
			break
		}
	}
	if current == nil {
		return &RecoveryStatus{Classification: "unknown", Confidence: "unavailable", Method: RecoveryMethod{Name: "rule_based_recovery_v1"}}
	}
	result := &RecoveryStatus{Classification: "normal", Confidence: "unavailable", Method: RecoveryMethod{Name: "rule_based_recovery_v1"}}
	objective := 0
	negative := []string{}
	if current.Sleep != nil {
		result.SleepScore = current.Sleep.Score
		result.SleepHours = current.Sleep.TotalHours
		if current.Sleep.Score != nil {
			objective++
			if *current.Sleep.Score < 60 {
				negative = append(negative, "sleep_score_low")
			}
		}
		if current.Sleep.TotalHours != nil {
			objective++
			if *current.Sleep.TotalHours < 6 {
				negative = append(negative, "sleep_duration_low")
			}
		}
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
			if ratio < .85 {
				negative = append(negative, "hrv_suppressed")
			}
		}
		objective++
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
			if delta >= 7 {
				negative = append(negative, "rhr_elevated")
			}
		}
		objective++
	}
	if current.BodyBattery != nil && current.BodyBattery.AtWake != nil {
		result.BodyBatteryAtWake = current.BodyBattery.AtWake
		objective++
		if *result.BodyBatteryAtWake < 35 {
			negative = append(negative, "body_battery_low")
		}
	}
	if checkin != nil {
		result.SubjectiveFatigue = checkin.Fatigue
		result.Soreness = checkin.Soreness
		if checkin.Fatigue != nil && *checkin.Fatigue >= 4 {
			negative = append(negative, "high_fatigue")
		}
		if checkin.Illness.Present {
			negative = append(negative, "illness_present")
		}
		for _, p := range checkin.Pain {
			if p.Severity >= 3 {
				negative = append(negative, "meaningful_pain")
				break
			}
		}
	}
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
	return result
}

func calculateLoad(activities []fitnessdata.ActivitySummary, asOf string, health []DailyHealthItem) *TrainingLoadStatus {
	result := &TrainingLoadStatus{Classification: "unavailable", Method: LoadMethod{Name: "garmin_activity_load_sum_v1"}}
	for _, a := range activities {
		result.Method.TotalActivities++
		if a.TrainingLoad != nil {
			result.Method.ActivitiesWithLoad++
		}
	}
	var last7, prev7, last28 float64
	var has28, has7, hasPrev bool
	for _, a := range activities {
		if a.TrainingLoad == nil {
			continue
		}
		days := daysBetween(a.Date, asOf)
		if days >= 0 && days < 7 {
			last7 += *a.TrainingLoad
			has7 = true
		} else if days >= 7 && days < 14 {
			prev7 += *a.TrainingLoad
			hasPrev = true
		}
		if days >= 0 && days < 28 {
			last28 += *a.TrainingLoad
			has28 = true
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
	for _, a := range activities {
		if daysBetween(a.Date, asOf) >= 0 && daysBetween(a.Date, asOf) < 7 && hardActivity(a) {
			result.HardSessionsLast7Days++
		}
	}
	result.ConsecutiveTrainingDays = consecutiveDays(activities, asOf)
	return result
}

func hardActivity(a fitnessdata.ActivitySummary) bool {
	if a.PerceivedEffort != nil && *a.PerceivedEffort >= 8 {
		return true
	}
	for _, effect := range []*float64{a.AerobicEffect, a.AnaerobicEffect} {
		if effect != nil && *effect >= 3.5 {
			return true
		}
	}
	return a.SourceType == "intervals" || a.SourceType == "tempo" || a.SourceType == "threshold" || a.SourceType == "race"
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
