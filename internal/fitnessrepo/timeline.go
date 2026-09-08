package fitnessrepo

import (
	"context"
	"errors"
	"sort"
	"strings"
)

type timelineCandidate struct {
	item TimelineCompleted
	date string
}

// TrainingTimeline composes plan, Garmin, and structured-workout records into
// a read-only view. Inferred links are never persisted.
func (r *Repository) TrainingTimeline(ctx context.Context, athleteID string, q TimelineQuery) (TimelineResult, error) {
	if err := validRange(q.From, q.To, 366); err != nil {
		return TimelineResult{}, err
	}
	limit, err := boundedLimit(q.Limit)
	if err != nil {
		return TimelineResult{}, err
	}
	order := q.Order
	if order == "" {
		order = "asc"
	}
	if order != "asc" && order != "desc" {
		return TimelineResult{}, errors.New("invalid_input: order")
	}
	filters := filterHash(q.From, q.To, strings.Join([]string{order, boolString(q.IncludeUnplanned), boolString(q.IncludeEntries)}, "|"))
	cur, err := decodeCursor(q.Cursor, order, filters)
	if err != nil {
		return TimelineResult{}, err
	}
	activities, err := r.Activities(ctx, athleteID, ActivityQuery{From: q.From, To: q.To, Detail: "summary", Order: "asc", Limit: 100})
	if err != nil {
		return TimelineResult{}, err
	}
	workouts, err := r.StrengthWorkouts(ctx, athleteID, StrengthQuery{From: q.From, To: q.To, IncludeEntries: q.IncludeEntries, Order: "asc", Limit: 100})
	if err != nil {
		return TimelineResult{}, err
	}
	plan, err := r.Plan(ctx, athleteID, q.From, q.To, true)
	if err != nil {
		return TimelineResult{}, err
	}

	activityItems := make([]timelineCandidate, 0, len(activities.Items))
	for _, activity := range activities.Items {
		id := activity.ActivityID
		if id == "" {
			id = activity.RecordKey
		}
		activityID := id
		workoutID := activity.LinkedWorkoutID
		activityItems = append(activityItems, timelineCandidate{date: activity.Date, item: TimelineCompleted{Kind: "garmin_activity", ID: id, Date: activity.Date, Title: derefString(activity.Name), Category: activity.Category, SessionType: activity.SourceType, DurationMinutes: activity.DurationMinutes, DistanceKM: activity.DistanceKM, GarminLoad: activity.TrainingLoad, ActivityID: &activityID, WorkoutID: workoutID}})
	}
	workoutItems := make([]timelineCandidate, 0, len(workouts.Items))
	for _, workout := range workouts.Items {
		id := workout.WorkoutID
		workoutID := id
		workoutItems = append(workoutItems, timelineCandidate{date: workout.Date, item: TimelineCompleted{Kind: "structured_workout", ID: id, Date: workout.Date, Title: workout.Title, Category: "strength", SessionType: workout.WorkoutType, DurationMinutes: workout.DurationMinutes, ActivityID: workout.LinkedActivityID, WorkoutID: &workoutID, PlannedSessionID: workout.PlannedSessionID, Entries: workout.Entries}})
	}

	plannedByID := map[string]*PlannedSession{}
	for index := range plan.Sessions {
		plannedByID[plan.Sessions[index].ID] = &plan.Sessions[index]
	}
	usedActivities, usedWorkouts := map[string]bool{}, map[string]bool{}
	completedByDate := map[string][]TimelineCompleted{}
	linksByDate := map[string][]TimelineLink{}
	warnings := []Warning{}
	addPair := func(activity *timelineCandidate, workout *timelineCandidate, planned *PlannedSession, method string, score *float64, margin *float64, candidates []string) {
		if activity == nil && workout == nil {
			return
		}
		if activity != nil {
			usedActivities[activity.item.ID] = true
		}
		if workout != nil {
			usedWorkouts[workout.item.ID] = true
		}
		date := ""
		if activity != nil {
			date = activity.date
		} else {
			date = workout.date
		}
		item := TimelineCompleted{Kind: "linked_pair", ID: "", Date: date}
		if activity != nil {
			item.ActivityID = activity.item.ActivityID
			item.GarminLoad = activity.item.GarminLoad
			item.DistanceKM = activity.item.DistanceKM
			item.DurationMinutes = activity.item.DurationMinutes
			item.Category = activity.item.Category
			item.SessionType = activity.item.SessionType
			item.Title = activity.item.Title
		}
		if workout != nil {
			item.WorkoutID = workout.item.WorkoutID
			item.Entries = workout.item.Entries
			if item.Title == "" {
				item.Title = workout.item.Title
			}
			if item.DurationMinutes == nil {
				item.DurationMinutes = workout.item.DurationMinutes
			}
		}
		if planned != nil {
			item.PlannedSessionID = &planned.ID
		}
		item.ID = stablePairID(item.ActivityID, item.WorkoutID)
		completedByDate[date] = append(completedByDate[date], item)
		link := TimelineLink{Status: method, Method: method, Score: score, Margin: margin, CandidateIDs: candidates}
		if planned != nil {
			link.PlannedSessionID = &planned.ID
		}
		if activity != nil {
			link.ActivityID = activity.item.ActivityID
		}
		if workout != nil {
			link.WorkoutID = workout.item.WorkoutID
		}
		linksByDate[date] = append(linksByDate[date], link)
	}

	activityByID := map[string]*timelineCandidate{}
	for index := range activityItems {
		activityByID[activityItems[index].item.ID] = &activityItems[index]
	}
	workoutByID := map[string]*timelineCandidate{}
	for index := range workoutItems {
		workoutByID[workoutItems[index].item.ID] = &workoutItems[index]
	}
	for _, session := range plan.Sessions {
		var activity *timelineCandidate
		var workout *timelineCandidate
		if session.CompletedActivityID != nil {
			activity = activityByID[*session.CompletedActivityID]
		}
		if session.CompletedWorkoutID != nil {
			workout = workoutByID[*session.CompletedWorkoutID]
		}
		if activity != nil || workout != nil {
			addPair(activity, workout, &session, "matched_explicit", nil, nil, nil)
		}
	}
	for index := range workoutItems {
		if usedWorkouts[workoutItems[index].item.ID] {
			continue
		}
		planned := plannedByID[derefString(workoutItems[index].item.PlannedSessionID)]
		activity := activityByID[derefString(workoutItems[index].item.ActivityID)]
		if activity != nil && !usedActivities[activity.item.ID] {
			addPair(activity, &workoutItems[index], planned, "matched_explicit", nil, nil, nil)
			continue
		}
		if planned != nil {
			addPair(nil, &workoutItems[index], planned, "matched_explicit", nil, nil, nil)
		}
	}
	for index := range activityItems {
		if usedActivities[activityItems[index].item.ID] {
			continue
		}
		if activityItems[index].item.ActivityID != nil {
			if workout := workoutByID[derefString(activityItems[index].item.ActivityID)]; workout != nil && !usedWorkouts[workout.item.ID] {
				addPair(&activityItems[index], workout, plannedByID[derefString(workout.item.PlannedSessionID)], "matched_explicit", nil, nil, nil)
				continue
			}
		}
		var candidates []*timelineCandidate
		for index := range workoutItems {
			candidate := &workoutItems[index]
			if usedWorkouts[candidate.item.ID] || candidate.date != activityItems[index].date {
				continue
			}
			if compatibleCategory(activityItems[index].item.Category, candidate.item.Category) {
				candidates = append(candidates, candidate)
			}
		}
		if len(candidates) == 1 {
			score := inferredScore(activityItems[index].item, candidates[0].item)
			if score < 0.75 {
				completedByDate[activityItems[index].date] = append(completedByDate[activityItems[index].date], activityItems[index].item)
				continue
			}
			addPair(&activityItems[index], candidates[0], plannedByID[derefString(candidates[0].item.PlannedSessionID)], "matched_inferred", &score, nil, []string{candidates[0].item.ID})
		} else if len(candidates) > 1 {
			ids := make([]string, 0, len(candidates))
			type scoredCandidate struct {
				candidate *timelineCandidate
				score     float64
			}
			scored := make([]scoredCandidate, 0, len(candidates))
			for _, candidate := range candidates {
				ids = append(ids, candidate.item.ID)
				scored = append(scored, scoredCandidate{candidate: candidate, score: inferredScore(activityItems[index].item, candidate.item)})
			}
			sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
			if scored[0].score >= 0.75 && scored[0].score-scored[1].score >= 0.15 {
				best, score := scored[0].candidate, scored[0].score
				margin := score - scored[1].score
				addPair(&activityItems[index], best, plannedByID[derefString(best.item.PlannedSessionID)], "matched_inferred", &score, &margin, ids)
				continue
			}
			linksByDate[activityItems[index].date] = append(linksByDate[activityItems[index].date], TimelineLink{Status: "unlinked", Method: "ambiguous_inference", ActivityID: activityItems[index].item.ActivityID, CandidateIDs: ids})
			warnings = appendBoundedRepoWarnings(warnings, Warning{Code: "AMBIGUOUS_TIMELINE_MATCH", Severity: "warning", Message: "A completion had multiple compatible candidates and was left unlinked."})
			completedByDate[activityItems[index].date] = append(completedByDate[activityItems[index].date], activityItems[index].item)
		} else {
			completedByDate[activityItems[index].date] = append(completedByDate[activityItems[index].date], activityItems[index].item)
		}
	}
	for index := range workoutItems {
		if !usedWorkouts[workoutItems[index].item.ID] {
			completedByDate[workoutItems[index].date] = append(completedByDate[workoutItems[index].date], workoutItems[index].item)
		}
	}

	dates := make([]string, 0, daysBetween(q.From, q.To)+1)
	for date := q.From; date <= q.To; date = dateShift(date, 1) {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	if order == "desc" {
		reverseStrings(dates)
	}
	allDays := make([]TimelineDay, 0, len(dates))
	today := r.clock().In(mustLocation(mustTimezone(ctx, r, athleteID))).Format("2006-01-02")
	for _, date := range dates {
		day := TimelineDay{Date: date, Planned: []PlannedSession{}, Completed: []TimelineCompleted{}, Links: append([]TimelineLink(nil), linksByDate[date]...)}
		for _, session := range plan.Sessions {
			if session.Date == date && session.Status != "cancelled" {
				day.Planned = append(day.Planned, session)
			}
		}
		for _, item := range completedByDate[date] {
			if q.IncludeUnplanned || item.PlannedSessionID != nil || hasLinkedDateLink(day.Links, item) {
				day.Completed = append(day.Completed, item)
			}
		}
		for _, item := range day.Completed {
			addTimelineTotals(&day.Totals, item)
		}
		completedPlans := map[string]bool{}
		for _, item := range day.Completed {
			if item.PlannedSessionID != nil {
				completedPlans[*item.PlannedSessionID] = true
			}
		}
		switch {
		case len(day.Planned) == 0 && len(day.Completed) == 0:
			day.Status = "rest"
		case len(day.Planned) == 0:
			day.Status = "unplanned"
		case len(completedPlans) == len(day.Planned):
			day.Status = "complete"
		case date < today && len(completedPlans) == 0:
			day.Status = "missed"
		case date >= today:
			day.Status = "planned"
		default:
			day.Status = "partial"
		}
		allDays = append(allDays, day)
	}
	filtered := allDays[:0]
	for _, day := range allDays {
		if cur != nil && !afterCursor(day.Date, day.Date, cur.Date, cur.ID, order) {
			continue
		}
		filtered = append(filtered, day)
	}
	more := len(filtered) > limit
	if more {
		filtered = filtered[:limit]
	}
	var next *string
	if more && len(filtered) > 0 {
		value := encodeCursor(cursorData{Version: 1, Date: filtered[len(filtered)-1].Date, ID: filtered[len(filtered)-1].Date, Order: order, Filters: filters})
		next = &value
	}
	return TimelineResult{Days: filtered, NextCursor: next, DataStatus: r.dataStatus(ctx, athleteID, q.From, q.To), Warnings: warnings, Method: Method{Name: "timeline_reconciliation_v1", Version: 1, InputsUsed: len(activityItems) + len(workoutItems) + len(plan.Sessions), InputsAvailable: len(activityItems) + len(workoutItems) + len(plan.Sessions), Confidence: "medium"}}, nil
}

func stablePairID(activityID, workoutID *string) string {
	return "pair:" + derefString(activityID) + ":" + derefString(workoutID)
}
func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
func compatibleCategory(left, right string) bool {
	return left == right
}
func inferredScore(activity, workout TimelineCompleted) float64 {
	score := 0.5
	if activity.Date == workout.Date {
		score += 0.25
	}
	if activity.DurationMinutes != nil && workout.DurationMinutes != nil {
		delta := *activity.DurationMinutes - *workout.DurationMinutes
		if delta < 0 {
			delta = -delta
		}
		if delta <= 10 {
			score += 0.25
		}
	}
	return score
}

func appendBoundedRepoWarnings(target []Warning, values ...Warning) []Warning {
	for _, value := range values {
		if len(target) >= 20 {
			break
		}
		target = append(target, value)
	}
	return target
}
func addTimelineTotals(total *TimelineTotals, item TimelineCompleted) {
	if item.DurationMinutes != nil {
		addFloat(&total.DurationMinutes, *item.DurationMinutes)
	}
	if item.DistanceKM != nil {
		addFloat(&total.DistanceKM, *item.DistanceKM)
	}
	if item.GarminLoad != nil {
		addFloat(&total.GarminLoad, *item.GarminLoad)
	}
}
func hasLinkedDateLink(links []TimelineLink, item TimelineCompleted) bool {
	for _, link := range links {
		if item.ActivityID != nil && link.ActivityID != nil && *item.ActivityID == *link.ActivityID {
			return true
		}
		if item.WorkoutID != nil && link.WorkoutID != nil && *item.WorkoutID == *link.WorkoutID {
			return true
		}
	}
	return false
}
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func mustTimezone(ctx context.Context, r *Repository, athleteID string) string {
	value, err := r.AthleteTimezone(ctx, athleteID)
	if err != nil {
		return "UTC"
	}
	return value
}
