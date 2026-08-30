package fitnessrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/timdumol/fitsync/internal/exercisecatalog"
	"github.com/timdumol/fitsync/internal/fitnessdata"
)

func New(database *sql.DB, clock Clock) *Repository {
	if clock == nil {
		clock = time.Now
	}
	return &Repository{db: database, clock: clock}
}

func (r *Repository) now() int64 { return r.clock().UnixMilli() }

func (r *Repository) AthleteTimezone(ctx context.Context, athleteID string) (string, error) {
	var timezone string
	err := r.db.QueryRowContext(ctx, "SELECT timezone FROM athletes WHERE id=?", athleteID).Scan(&timezone)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("not_found: athlete")
	}
	if err != nil {
		return "", fmt.Errorf("internal_error: athlete lookup")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		timezone = "UTC"
	}
	return timezone, nil
}

func (r *Repository) DailyHealth(ctx context.Context, athleteID string, q HealthQuery) (Page[DailyHealthItem], error) {
	if err := validRange(q.From, q.To, 366); err != nil {
		return Page[DailyHealthItem]{}, err
	}
	limit, err := boundedLimit(q.Limit)
	if err != nil {
		return Page[DailyHealthItem]{}, err
	}
	order := q.Order
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return Page[DailyHealthItem]{}, errors.New("invalid_input: order must be asc or desc")
	}
	cur, err := decodeCursor(q.Cursor, order, filterHash(q.From, q.To, strings.Join(q.Metrics, ",")))
	if err != nil {
		return Page[DailyHealthItem]{}, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT record_date, dataset, payload_raw FROM garmin_records WHERE athlete_id=? AND record_date BETWEEN ? AND ? ORDER BY record_date, record_key`, athleteID, q.From, q.To)
	if err != nil {
		return Page[DailyHealthItem]{}, fmt.Errorf("internal_error: health query")
	}
	defer rows.Close()
	byDate := map[string]*DailyHealthItem{}
	for rows.Next() {
		var date, dataset, raw string
		if err := rows.Scan(&date, &dataset, &raw); err != nil {
			return Page[DailyHealthItem]{}, fmt.Errorf("internal_error: health row")
		}
		item := byDate[date]
		if item == nil {
			item = &DailyHealthItem{Date: date, Completeness: map[string]bool{}}
			byDate[date] = item
		}
		payload := json.RawMessage(raw)
		switch dataset {
		case "sleep":
			if value, e := fitnessdata.DecodeSleep(dataset, payload); e == nil {
				item.Sleep = &value.Value
				item.Completeness["sleep"] = true
			}
			if value, e := fitnessdata.DecodeHRV(dataset, payload); e == nil && value.Value.OvernightAverage != nil {
				item.HRV = &value.Value
				item.Completeness["hrv"] = true
			}
			if value, e := fitnessdata.DecodeRecovery(dataset, payload); e == nil {
				mergeHealthRecovery(item, value.Value)
			}
		case "hrv":
			if value, e := fitnessdata.DecodeHRV(dataset, payload); e == nil {
				item.HRV = &value.Value
				item.Completeness["hrv"] = true
			}
		case "daily_recovery", "body_battery_events":
			if value, e := fitnessdata.DecodeRecovery(dataset, payload); e == nil {
				mergeHealthRecovery(item, value.Value)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Page[DailyHealthItem]{}, fmt.Errorf("internal_error: health rows")
	}
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		if cur != nil && ((order == "asc" && date <= cur.Date) || (order == "desc" && date >= cur.Date)) {
			continue
		}
		dates = append(dates, date)
	}
	sort.Strings(dates)
	if order == "desc" {
		reverseStrings(dates)
	}
	more := len(dates) > limit
	if more {
		dates = dates[:limit]
	}
	items := make([]DailyHealthItem, 0, len(dates))
	for _, date := range dates {
		item := *byDate[date]
		if len(q.Metrics) > 0 {
			wanted := map[string]bool{}
			for _, metric := range q.Metrics {
				wanted[metric] = true
			}
			if !wanted["sleep"] {
				item.Sleep = nil
			}
			if !wanted["hrv"] {
				item.HRV = nil
			}
			if !wanted["heart_rate"] {
				item.HeartRate = nil
			}
			if !wanted["stress"] {
				item.Stress = nil
			}
			if !wanted["body_battery"] {
				item.BodyBattery = nil
			}
			if !wanted["spo2"] {
				item.SpO2 = nil
			}
			if !wanted["daily_activity"] {
				item.DailyActivity = nil
			}
			for metric := range item.Completeness {
				if !wanted[metric] {
					delete(item.Completeness, metric)
				}
			}
		}
		items = append(items, item)
	}
	var next *string
	if more && len(items) > 0 {
		encoded := encodeCursor(cursorData{Version: 1, Date: items[len(items)-1].Date, ID: items[len(items)-1].Date, Order: order, Filters: filterHash(q.From, q.To, strings.Join(q.Metrics, ","))})
		next = &encoded
	}
	return Page[DailyHealthItem]{Items: items, NextCursor: next, DataStatus: r.dataStatus(ctx, athleteID, q.From, q.To)}, nil
}

func mergeHealthRecovery(item *DailyHealthItem, value fitnessdata.DailyHealth) {
	if value.HeartRate != nil {
		item.HeartRate = value.HeartRate
		item.Completeness["heart_rate"] = true
	}
	if value.Stress != nil {
		item.Stress = value.Stress
		item.Completeness["stress"] = true
	}
	if value.BodyBattery != nil {
		item.BodyBattery = value.BodyBattery
		item.Completeness["body_battery"] = true
	}
	if value.SpO2 != nil {
		item.SpO2 = value.SpO2
		item.Completeness["spo2"] = true
	}
	if value.DailyActivity != nil {
		item.DailyActivity = value.DailyActivity
		item.Completeness["daily_activity"] = true
	}
}

func (r *Repository) Activities(ctx context.Context, athleteID string, q ActivityQuery) (Page[fitnessdata.ActivitySummary], error) {
	if err := validRange(q.From, q.To, 366); err != nil {
		return Page[fitnessdata.ActivitySummary]{}, err
	}
	limit, err := boundedLimit(q.Limit)
	if err != nil {
		return Page[fitnessdata.ActivitySummary]{}, err
	}
	order := q.Order
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return Page[fitnessdata.ActivitySummary]{}, errors.New("invalid_input: order must be asc or desc")
	}
	filters := filterHash(q.From, q.To, strings.Join(q.Categories, ",")+"|"+strings.Join(q.SourceTypes, ",")+"|"+q.Detail)
	cur, err := decodeCursor(q.Cursor, order, filters)
	if err != nil {
		return Page[fitnessdata.ActivitySummary]{}, err
	}
	queryOrder := "ASC"
	if order == "desc" {
		queryOrder = "DESC"
	}
	rows, err := r.db.QueryContext(ctx, "SELECT id, record_key, record_date, payload_raw, ingested_at FROM garmin_records WHERE athlete_id=? AND dataset='activities' AND record_date BETWEEN ? AND ? ORDER BY record_date "+queryOrder+", record_key "+queryOrder+" LIMIT 501", athleteID, q.From, q.To)
	if err != nil {
		return Page[fitnessdata.ActivitySummary]{}, errors.New("internal_error: activity query")
	}
	defer rows.Close()
	items := make([]fitnessdata.ActivitySummary, 0, limit+1)
	for rows.Next() {
		var recordID, key, date, raw string
		var ingestedAt int64
		if err := rows.Scan(&recordID, &key, &date, &raw, &ingestedAt); err != nil {
			return Page[fitnessdata.ActivitySummary]{}, errors.New("internal_error: activity row")
		}
		if cur != nil && !afterCursor(date, key, cur.Date, cur.ID, order) {
			continue
		}
		value, err := fitnessdata.DecodeActivity("activities", json.RawMessage(raw))
		if err != nil {
			continue
		}
		value.Value.Date = date
		if value.Value.ActivityID == "" {
			value.Value.ActivityID = key
		}
		value.Value.ID, value.Value.Dataset, value.Value.RecordKey = recordID, "activities", key
		value.Value.IngestedAt = &ingestedAt
		if !containsOrEmpty(q.Categories, value.Value.Category) || !containsOrEmpty(q.SourceTypes, value.Value.SourceType) || (q.MinDurationMinutes != nil && (value.Value.DurationMinutes == nil || *value.Value.DurationMinutes < *q.MinDurationMinutes)) || (q.MinDistanceKM != nil && (value.Value.DistanceKM == nil || *value.Value.DistanceKM < *q.MinDistanceKM)) || (q.MinTrainingLoad != nil && (value.Value.TrainingLoad == nil || *value.Value.TrainingLoad < *q.MinTrainingLoad)) {
			continue
		}
		items = append(items, value.Value)
		if len(items) >= limit+1 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return Page[fitnessdata.ActivitySummary]{}, errors.New("internal_error: activity rows")
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	var next *string
	if more && len(items) > 0 {
		last := items[len(items)-1]
		encoded := encodeCursor(cursorData{Version: 1, Date: last.Date, ID: last.ActivityID, Order: order, Filters: filters})
		next = &encoded
	}
	return Page[fitnessdata.ActivitySummary]{Items: items, NextCursor: next, DataStatus: r.dataStatus(ctx, athleteID, q.From, q.To)}, nil
}

func (r *Repository) Activity(ctx context.Context, athleteID, activityID string, include []string, resolution, maxPoints int) (ActivityResult, error) {
	var key, date, raw string
	err := r.db.QueryRowContext(ctx, `SELECT record_key,record_date,payload_raw FROM garmin_records WHERE athlete_id=? AND dataset='activities' AND (record_key=? OR json_extract(payload_raw,'$.activityId')=? OR json_extract(payload_raw,'$.activity_id')=?) LIMIT 1`, athleteID, activityID, activityID, activityID).Scan(&key, &date, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ActivityResult{}, errors.New("not_found: activity")
	}
	if err != nil {
		return ActivityResult{}, errors.New("internal_error: activity lookup")
	}
	value, err := fitnessdata.DecodeActivity("activities", json.RawMessage(raw))
	if err != nil {
		return ActivityResult{}, errors.New("internal_error: activity decode")
	}
	value.Value.Date = date
	if value.Value.ActivityID == "" {
		value.Value.ActivityID = key
	}
	result := ActivityResult{Summary: value.Value}
	if has(include, "summary") || len(include) == 0 {
	}
	if has(include, "splits") || has(include, "zones") || has(include, "metrics") {
		var detailRaw string
		err := r.db.QueryRowContext(ctx, `SELECT payload_raw FROM garmin_records WHERE athlete_id=? AND dataset='activity_details' AND (record_key=? OR json_extract(payload_raw,'$.activity_id')=? OR json_extract(payload_raw,'$.activityId')=?) LIMIT 1`, athleteID, key, value.Value.ActivityID, value.Value.ActivityID).Scan(&detailRaw)
		if errors.Is(err, sql.ErrNoRows) {
			result.Warnings = []Warning{{Code: "MISSING_ACTIVITY_DETAIL", Severity: "info", Message: "Activity detail is unavailable."}}
		} else if err != nil {
			return ActivityResult{}, errors.New("internal_error: detail lookup")
		} else {
			detail, e := fitnessdata.DecodeActivityDetail("activity_details", json.RawMessage(detailRaw))
			if e != nil {
				return ActivityResult{}, errors.New("internal_error: detail decode")
			}
			d := detail.Value
			d.Summary = value.Value
			if resolution <= 0 {
				resolution = 30
			}
			if maxPoints <= 0 {
				maxPoints = 500
			}
			d.ResolutionSeconds = resolution
			d.Metrics, d.Truncated = downsample(d.Metrics, resolution, maxPoints)
			d.ReturnedPoints = len(d.Metrics)
			result.Detail = &d
		}
	}
	return result, nil
}

func downsample(points []fitnessdata.MetricPoint, resolution, maxPoints int) ([]fitnessdata.MetricPoint, bool) {
	if len(points) == 0 {
		return points, false
	}
	bucket := func(size int) []fitnessdata.MetricPoint {
		type aggregate struct {
			point  fitnessdata.MetricPoint
			count  int
			sums   [8]float64
			counts [8]int
		}
		m := map[int]*aggregate{}
		for _, p := range points {
			key := int(p.ElapsedSeconds) / size
			a := m[key]
			if a == nil {
				a = &aggregate{point: p}
				m[key] = a
			}
			a.point.ElapsedSeconds = p.ElapsedSeconds
			values := []*float64{p.HeartRate, p.PaceMinPerKM, p.SpeedKPH, p.Cadence, p.Power, p.ElevationM, p.Respiration, p.Temperature}
			for i, value := range values {
				if value != nil {
					a.sums[i] += *value
					a.counts[i]++
				}
			}
			a.point.DistanceKM = p.DistanceKM
			a.count++
		}
		keys := make([]int, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		out := make([]fitnessdata.MetricPoint, 0, len(keys))
		for _, k := range keys {
			a := m[k]
			if a.counts[0] > 0 {
				v := a.sums[0] / float64(a.counts[0])
				a.point.HeartRate = &v
			}
			if a.counts[1] > 0 {
				v := a.sums[1] / float64(a.counts[1])
				a.point.PaceMinPerKM = &v
			}
			if a.counts[2] > 0 {
				v := a.sums[2] / float64(a.counts[2])
				a.point.SpeedKPH = &v
			}
			if a.counts[3] > 0 {
				v := a.sums[3] / float64(a.counts[3])
				a.point.Cadence = &v
			}
			if a.counts[4] > 0 {
				v := a.sums[4] / float64(a.counts[4])
				a.point.Power = &v
			}
			if a.counts[5] > 0 {
				v := a.sums[5] / float64(a.counts[5])
				a.point.ElevationM = &v
			}
			if a.counts[6] > 0 {
				v := a.sums[6] / float64(a.counts[6])
				a.point.Respiration = &v
			}
			if a.counts[7] > 0 {
				v := a.sums[7] / float64(a.counts[7])
				a.point.Temperature = &v
			}
			out = append(out, a.point)
		}
		return out
	}
	out := bucket(resolution)
	truncated := false
	if len(out) > maxPoints {
		size := int(points[len(points)-1].ElapsedSeconds)/maxPoints + 1
		if size < resolution {
			size = resolution
		}
		out = bucket(size)
		truncated = len(out) > maxPoints
		if truncated {
			out = out[:maxPoints]
		}
	}
	return out, truncated
}

func (r *Repository) StrengthWorkouts(ctx context.Context, athleteID string, q StrengthQuery) (Page[StrengthWorkout], error) {
	if err := validRange(q.From, q.To, 730); err != nil {
		return Page[StrengthWorkout]{}, err
	}
	limit, err := boundedLimit(q.Limit)
	if err != nil {
		return Page[StrengthWorkout]{}, err
	}
	order := q.Order
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return Page[StrengthWorkout]{}, errors.New("invalid_input: order must be asc or desc")
	}
	filters := filterHash(q.From, q.To, strings.Join(q.ExerciseKeys, ",")+"|"+strings.Join(q.MuscleGroups, ","))
	cur, err := decodeCursor(q.Cursor, order, filters)
	if err != nil {
		return Page[StrengthWorkout]{}, err
	}
	rows, err := r.db.QueryContext(ctx, "SELECT id,workout_date FROM workout_sessions WHERE athlete_id=? AND deleted_at IS NULL AND workout_date BETWEEN ? AND ? ORDER BY workout_date "+map[bool]string{true: "ASC", false: "DESC"}[order == "asc"]+",id "+map[bool]string{true: "ASC", false: "DESC"}[order == "asc"], athleteID, q.From, q.To)
	if err != nil {
		return Page[StrengthWorkout]{}, errors.New("internal_error: workout query")
	}
	defer rows.Close()
	items := make([]StrengthWorkout, 0, limit+1)
	for rows.Next() {
		var id, date string
		if err := rows.Scan(&id, &date); err != nil {
			return Page[StrengthWorkout]{}, errors.New("internal_error: workout row")
		}
		if cur != nil && !afterCursor(date, id, cur.Date, cur.ID, order) {
			continue
		}
		value, err := loadWorkoutAggregate(ctx, r.db, athleteID, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return Page[StrengthWorkout]{}, err
		}
		workout := &value
		if workout.Summary.WorkingSets == 0 && workout.LinkedActivityID == nil && workout.WorkoutType != "strength" {
			continue
		}
		if !strengthMatches(workout, q) {
			continue
		}
		if !q.IncludeEntries {
			workout.Entries = nil
		}
		items = append(items, *workout)
		if len(items) >= limit+1 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return Page[StrengthWorkout]{}, errors.New("internal_error: workout rows")
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	var next *string
	if more {
		encoded := encodeCursor(cursorData{Version: 1, Date: items[len(items)-1].Date, ID: items[len(items)-1].WorkoutID, Order: order, Filters: filters})
		next = &encoded
	}
	return Page[StrengthWorkout]{Items: items, NextCursor: next}, nil
}

type workoutMetadata struct {
	WorkoutType      string   `json:"workout_type"`
	StartedAt        *string  `json:"started_at"`
	DurationMinutes  *float64 `json:"duration_minutes"`
	PerceivedEffort  *float64 `json:"perceived_effort"`
	LinkedActivityID *string  `json:"linked_activity_id"`
	GarminActivityID *string  `json:"garmin_activity_id"`
	Notes            *string  `json:"notes"`
}
type entryPayload struct {
	ExerciseKey     *string  `json:"exercise_key"`
	ExerciseName    *string  `json:"exercise_name"`
	UserLabel       *string  `json:"user_label"`
	SetNumber       *int     `json:"set_number"`
	Reps            *float64 `json:"reps"`
	DurationSeconds *float64 `json:"duration_seconds"`
	LoadKG          *float64 `json:"load_kg"`
	LoadDescription *string  `json:"load_description"`
	IsBodyweight    *bool    `json:"is_bodyweight"`
	ToFailure       *bool    `json:"to_failure"`
	RIR             *float64 `json:"rir"`
	RIRMin          *float64 `json:"rir_min"`
	RIRMax          *float64 `json:"rir_max"`
	Laterality      *string  `json:"laterality"`
	MuscleGroups    []string `json:"muscle_groups"`
	Notes           *string  `json:"notes"`
	IsWarmup        bool     `json:"is_warmup"`
}

func decodeWorkout(id, date, title, source, raw string) *StrengthWorkout {
	var m workoutMetadata
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	linked := m.LinkedActivityID
	if linked == nil {
		linked = m.GarminActivityID
	}
	return &StrengthWorkout{WorkoutID: id, Date: date, Title: title, Source: source, WorkoutType: m.WorkoutType, StartedAt: m.StartedAt, DurationMinutes: m.DurationMinutes, PerceivedEffort: m.PerceivedEffort, Notes: m.Notes, LinkedActivityID: linked}
}
func (r *Repository) entries(ctx context.Context, athleteID, sessionID string, include bool) ([]StrengthEntry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,sequence_no,entry_type,payload_raw FROM workout_entries WHERE athlete_id=? AND session_id=? AND deleted_at IS NULL ORDER BY sequence_no,id`, athleteID, sessionID)
	if err != nil {
		return nil, errors.New("internal_error: entry query")
	}
	defer rows.Close()
	if !include {
		return nil, nil
	}
	var result []StrengthEntry
	for rows.Next() {
		var id, typ, raw string
		var seq int
		if err := rows.Scan(&id, &seq, &typ, &raw); err != nil {
			return nil, errors.New("internal_error: entry row")
		}
		var p entryPayload
		if json.Unmarshal([]byte(raw), &p) != nil {
			continue
		}
		groups := p.MuscleGroups
		if len(groups) == 0 && p.ExerciseKey != nil {
			groups = exercisecatalog.Groups(*p.ExerciseKey)
		}
		result = append(result, StrengthEntry{EntryID: id, SequenceNo: seq, EntryType: typ, ExerciseKey: p.ExerciseKey, ExerciseName: p.ExerciseName, UserLabel: p.UserLabel, SetNumber: p.SetNumber, Reps: p.Reps, DurationSeconds: p.DurationSeconds, LoadKG: p.LoadKG, LoadDescription: p.LoadDescription, IsBodyweight: p.IsBodyweight, ToFailure: p.ToFailure, RIR: p.RIR, RIRMin: p.RIRMin, RIRMax: p.RIRMax, Laterality: p.Laterality, MuscleGroups: groups, Notes: p.Notes, IsWarmup: p.IsWarmup})
	}
	return result, rows.Err()
}
func summarizeStrength(entries []StrengthEntry) StrengthSummary {
	sum := StrengthSummary{MuscleGroupSets: map[string]int{}}
	var volume float64
	hasVolume := false
	for _, e := range entries {
		if e.EntryType != "set" || e.IsWarmup {
			continue
		}
		sum.WorkingSets++
		if e.Reps != nil && e.LoadKG != nil {
			volume += *e.Reps * *e.LoadKG
			hasVolume = true
		}
		groups := e.MuscleGroups
		if len(groups) == 0 && e.ExerciseKey != nil {
			groups = exercisecatalog.Groups(*e.ExerciseKey)
		}
		for _, g := range groups {
			sum.MuscleGroupSets[g]++
		}
		if (e.ToFailure != nil && *e.ToFailure) || (e.RIR != nil && *e.RIR <= 1) || (e.RIRMax != nil && *e.RIRMax <= 1) {
			sum.HighEffortSets++
		}
	}
	if hasVolume {
		sum.VolumeLoadKG = &volume
	}
	return sum
}
func strengthMatches(w *StrengthWorkout, q StrengthQuery) bool {
	if len(q.ExerciseKeys) == 0 && len(q.MuscleGroups) == 0 {
		return true
	}
	for _, e := range w.Entries {
		if containsOrEmpty(q.ExerciseKeys, deref(e.ExerciseKey)) && (len(q.MuscleGroups) == 0 || anyIn(e.MuscleGroups, q.MuscleGroups)) {
			return true
		}
	}
	return false
}

func (r *Repository) UpsertWorkout(ctx context.Context, athleteID string, in WorkoutWrite) (WorkoutWinner, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return WorkoutWinner{}, err
	}
	rollback := func(e error) (WorkoutWinner, error) { _ = tx.Rollback(); return WorkoutWinner{}, e }
	clock := r.now()
	var existingClient int64
	var existingAthlete string
	err = tx.QueryRowContext(ctx, "SELECT athlete_id,client_updated_at FROM workout_sessions WHERE id=?", in.ID).Scan(&existingAthlete, &existingClient)
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !created {
		return rollback(errors.New("internal_error: workout lookup"))
	}
	if created {
		if err := requireUUIDv7(in.ID); err != nil {
			return rollback(err)
		}
		metadata := workoutMetadata{WorkoutType: in.WorkoutType, StartedAt: formatTime(in.StartedAt), DurationMinutes: in.DurationMinutes, PerceivedEffort: in.PerceivedEffort, LinkedActivityID: in.LinkedActivityID, Notes: in.Notes}
		raw, _ := json.Marshal(metadata)
		if _, err := tx.ExecContext(ctx, `INSERT INTO workout_sessions(id,athlete_id,workout_date,title,source,metadata_raw,client_updated_at,server_synced_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, in.ID, athleteID, in.Date, in.Title, in.Source, string(raw), in.ClientUpdatedAt, clock, clock); err != nil {
			return rollback(normalizeWriteError(err))
		}
	} else {
		if existingAthlete != athleteID {
			return rollback(errors.New("conflict: workout belongs to another athlete"))
		}
		if in.ClientUpdatedAt <= existingClient {
			if err := tx.Commit(); err != nil {
				return WorkoutWinner{}, errors.New("internal_error: transaction commit")
			}
			w, err := r.loadWorkoutWithDB(ctx, athleteID, in.ID)
			if err != nil {
				return WorkoutWinner{}, err
			}
			return WorkoutWinner{Workout: w, SubmittedVersionWon: false, ServerSyncedAt: winnerClock(ctx, r.db, in.ID)}, nil
		}
		metadata := workoutMetadata{WorkoutType: in.WorkoutType, StartedAt: formatTime(in.StartedAt), DurationMinutes: in.DurationMinutes, PerceivedEffort: in.PerceivedEffort, LinkedActivityID: in.LinkedActivityID, Notes: in.Notes}
		raw, _ := json.Marshal(metadata)
		if _, err := tx.ExecContext(ctx, `UPDATE workout_sessions SET workout_date=?,title=?,source=?,metadata_raw=?,client_updated_at=?,server_synced_at=?,deleted_at=NULL WHERE id=?`, in.Date, in.Title, in.Source, string(raw), in.ClientUpdatedAt, clock, in.ID); err != nil {
			return rollback(normalizeWriteError(err))
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE workout_entries SET deleted_at=? WHERE session_id=? AND athlete_id=? AND deleted_at IS NULL", clock, in.ID, athleteID); err != nil {
		return rollback(errors.New("internal_error: workout entries"))
	}
	for _, entry := range in.Entries {
		if err := requireUUIDv7(entry.EntryID); err != nil {
			return rollback(err)
		}
		var entryOwner string
		entryLookup := tx.QueryRowContext(ctx, "SELECT athlete_id FROM workout_entries WHERE id=?", entry.EntryID).Scan(&entryOwner)
		if entryLookup == nil && entryOwner != athleteID {
			return rollback(errors.New("conflict: workout entry belongs to another athlete"))
		}
		if entryLookup != nil && !errors.Is(entryLookup, sql.ErrNoRows) {
			return rollback(errors.New("internal_error: workout entry lookup"))
		}
		payload := entryPayload{ExerciseKey: entry.ExerciseKey, ExerciseName: entry.ExerciseName, UserLabel: entry.UserLabel, SetNumber: entry.SetNumber, Reps: entry.Reps, DurationSeconds: entry.DurationSeconds, LoadKG: entry.LoadKG, LoadDescription: entry.LoadDescription, IsBodyweight: entry.IsBodyweight, ToFailure: entry.ToFailure, RIR: entry.RIR, RIRMin: entry.RIRMin, RIRMax: entry.RIRMax, Laterality: entry.Laterality, MuscleGroups: entry.MuscleGroups, Notes: entry.Notes, IsWarmup: entry.IsWarmup}
		raw, _ := json.Marshal(payload)
		if _, err := tx.ExecContext(ctx, `INSERT INTO workout_entries(id,session_id,athlete_id,sequence_no,entry_type,payload_raw,client_updated_at,server_synced_at,deleted_at) VALUES(?,?,?,?,?,?,?, ?,NULL) ON CONFLICT(id) DO UPDATE SET session_id=excluded.session_id,athlete_id=excluded.athlete_id,sequence_no=excluded.sequence_no,entry_type=excluded.entry_type,payload_raw=excluded.payload_raw,client_updated_at=excluded.client_updated_at,server_synced_at=excluded.server_synced_at,deleted_at=NULL`, entry.EntryID, in.ID, athleteID, entry.SequenceNo, entry.EntryType, string(raw), in.ClientUpdatedAt, clock); err != nil {
			return rollback(normalizeWriteError(err))
		}
	}
	seq, err := allocateSyncSeq(ctx, tx)
	if err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE workout_sessions SET sync_seq=? WHERE id=?", seq, in.ID); err != nil {
		return rollback(errors.New("internal_error: workout sequence"))
	}
	if err := tx.Commit(); err != nil {
		return WorkoutWinner{}, errors.New("internal_error: transaction commit")
	}
	w, err := r.loadWorkoutWithDB(ctx, athleteID, in.ID)
	if err != nil {
		return WorkoutWinner{}, err
	}
	return WorkoutWinner{Workout: w, Created: created, SubmittedVersionWon: true, ServerSyncedAt: seq}, nil
}

func (r *Repository) DeleteWorkout(ctx context.Context, athleteID, id string, clientUpdatedAt int64) (WorkoutWinner, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return WorkoutWinner{}, err
	}
	var owner string
	var old int64
	if err := tx.QueryRowContext(ctx, "SELECT athlete_id,client_updated_at FROM workout_sessions WHERE id=?", id).Scan(&owner, &old); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return WorkoutWinner{}, errors.New("not_found: workout")
		}
		return WorkoutWinner{}, errors.New("internal_error: workout lookup")
	}
	if owner != athleteID {
		_ = tx.Rollback()
		return WorkoutWinner{}, errors.New("not_found: workout")
	}
	if clientUpdatedAt <= old {
		_ = tx.Commit()
		w, e := r.loadWorkoutWithDB(ctx, athleteID, id)
		return WorkoutWinner{Workout: w, SubmittedVersionWon: false, ServerSyncedAt: winnerClock(ctx, r.db, id)}, e
	}
	clock := r.now()
	if _, err := tx.ExecContext(ctx, "UPDATE workout_sessions SET deleted_at=?,client_updated_at=?,server_synced_at=? WHERE id=?", clock, clientUpdatedAt, clock, id); err != nil {
		_ = tx.Rollback()
		return WorkoutWinner{}, errors.New("internal_error: workout delete")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE workout_entries SET deleted_at=?,server_synced_at=? WHERE session_id=? AND athlete_id=? AND deleted_at IS NULL", clock, clock, id, athleteID); err != nil {
		_ = tx.Rollback()
		return WorkoutWinner{}, errors.New("internal_error: entry delete")
	}
	seq, err := allocateSyncSeq(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return WorkoutWinner{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE workout_sessions SET sync_seq=? WHERE id=?", seq, id); err != nil {
		_ = tx.Rollback()
		return WorkoutWinner{}, errors.New("internal_error: workout sequence")
	}
	if err := tx.Commit(); err != nil {
		return WorkoutWinner{}, errors.New("internal_error: transaction commit")
	}
	w, e := r.loadWorkoutWithDB(ctx, athleteID, id)
	return WorkoutWinner{Workout: w, SubmittedVersionWon: true, ServerSyncedAt: seq}, e
}

func (r *Repository) loadWorkoutWithDB(ctx context.Context, athleteID, id string) (StrengthWorkout, error) {
	var date, title, source, raw string
	if err := r.db.QueryRowContext(ctx, "SELECT workout_date,title,source,metadata_raw FROM workout_sessions WHERE id=? AND athlete_id=?", id, athleteID).Scan(&date, &title, &source, &raw); err != nil {
		return StrengthWorkout{}, errors.New("internal_error: workout read")
	}
	w := decodeWorkout(id, date, title, source, raw)
	if w == nil {
		return StrengthWorkout{}, errors.New("internal_error: workout payload")
	}
	entries, err := r.entries(ctx, athleteID, id, true)
	if err != nil {
		return StrengthWorkout{}, err
	}
	w.Entries = entries
	w.Summary = summarizeStrength(entries)
	return *w, nil
}

func (r *Repository) UpsertCheckin(ctx context.Context, athleteID string, in CheckinWrite) (CheckinWinner, error) {
	timezone, err := r.AthleteTimezone(ctx, athleteID)
	if err != nil {
		return CheckinWinner{}, err
	}
	local := in.RecordedAt.In(mustLocation(timezone)).Format("2006-01-02")
	raw, _ := json.Marshal(checkinContent{Energy: in.Energy, Fatigue: in.Fatigue, Motivation: in.Motivation, SleepQuality: in.SleepQuality, Stress: in.Stress, Soreness: in.Soreness, Pain: in.Pain, Illness: in.Illness, Notes: in.Notes})
	clock := r.now()
	tx, err := r.begin(ctx)
	if err != nil {
		return CheckinWinner{}, err
	}
	var oldAthlete string
	var oldClock int64
	err = tx.QueryRowContext(ctx, "SELECT athlete_id,client_updated_at FROM checkins WHERE id=?", in.ID).Scan(&oldAthlete, &oldClock)
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !created {
		_ = tx.Rollback()
		return CheckinWinner{}, errors.New("internal_error: checkin lookup")
	}
	if created {
		if e := requireUUIDv7(in.ID); e != nil {
			_ = tx.Rollback()
			return CheckinWinner{}, e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO checkins(id,athlete_id,recorded_at_utc_ms,local_date,timezone,content_raw,client_updated_at,server_synced_at) VALUES(?,?,?,?,?,?,?,?)`, in.ID, athleteID, in.RecordedAt.UnixMilli(), local, timezone, string(raw), in.ClientUpdatedAt, clock)
	} else if oldAthlete != athleteID {
		_ = tx.Rollback()
		return CheckinWinner{}, errors.New("not_found: checkin")
	} else if in.ClientUpdatedAt > oldClock {
		_, err = tx.ExecContext(ctx, `UPDATE checkins SET recorded_at_utc_ms=?,local_date=?,timezone=?,content_raw=?,client_updated_at=?,server_synced_at=?,deleted_at=NULL WHERE id=?`, in.RecordedAt.UnixMilli(), local, timezone, string(raw), in.ClientUpdatedAt, clock, in.ID)
	}
	if err != nil {
		_ = tx.Rollback()
		return CheckinWinner{}, normalizeWriteError(err)
	}
	if err := tx.Commit(); err != nil {
		return CheckinWinner{}, errors.New("internal_error: transaction commit")
	}
	value, e := r.readCheckin(ctx, athleteID, in.ID)
	if e != nil {
		return CheckinWinner{}, e
	}
	return CheckinWinner{Checkin: value, Created: created, SubmittedVersionWon: created || in.ClientUpdatedAt > oldClock, ServerSyncedAt: clock}, nil
}

func (r *Repository) Checkins(ctx context.Context, athleteID string, q CheckinQuery) (Page[Checkin], error) {
	if err := validRange(q.From, q.To, 366); err != nil {
		return Page[Checkin]{}, err
	}
	limit, err := boundedLimit(q.Limit)
	if err != nil {
		return Page[Checkin]{}, err
	}
	order := q.Order
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return Page[Checkin]{}, errors.New("invalid_input: order")
	}
	filters := filterHash(q.From, q.To, "")
	cur, err := decodeCursor(q.Cursor, order, filters)
	if err != nil {
		return Page[Checkin]{}, err
	}
	direction := "DESC"
	if order == "asc" {
		direction = "ASC"
	}
	rows, err := r.db.QueryContext(ctx, "SELECT id,recorded_at_utc_ms,local_date,timezone,content_raw FROM checkins WHERE athlete_id=? AND deleted_at IS NULL AND local_date BETWEEN ? AND ? ORDER BY local_date "+direction+",id "+direction+" LIMIT ?", athleteID, q.From, q.To, limit+1)
	if err != nil {
		return Page[Checkin]{}, errors.New("internal_error: checkin query")
	}
	defer rows.Close()
	result := make([]Checkin, 0, limit+1)
	for rows.Next() {
		var id, date, timezone, raw string
		var at int64
		if e := rows.Scan(&id, &at, &date, &timezone, &raw); e != nil {
			return Page[Checkin]{}, errors.New("internal_error: checkin row")
		}
		if cur != nil && !afterCursor(date, id, cur.Date, cur.ID, order) {
			continue
		}
		value := decodeCheckin(id, at, date, timezone, raw)
		result = append(result, value)
	}
	if e := rows.Err(); e != nil {
		return Page[Checkin]{}, errors.New("internal_error: checkin rows")
	}
	more := len(result) > limit
	if more {
		result = result[:limit]
	}
	var next *string
	if more {
		encoded := encodeCursor(cursorData{1, result[len(result)-1].LocalDate, result[len(result)-1].ID, order, filters})
		next = &encoded
	}
	return Page[Checkin]{Items: result, NextCursor: next}, nil
}

func (r *Repository) readCheckin(ctx context.Context, athleteID, id string) (Checkin, error) {
	var at int64
	var date, timezone, raw string
	if err := r.db.QueryRowContext(ctx, "SELECT recorded_at_utc_ms,local_date,timezone,content_raw FROM checkins WHERE id=? AND athlete_id=?", id, athleteID).Scan(&at, &date, &timezone, &raw); err != nil {
		return Checkin{}, errors.New("internal_error: checkin read")
	}
	return decodeCheckin(id, at, date, timezone, raw), nil
}

type checkinContent struct {
	Energy       *int       `json:"energy,omitempty"`
	Fatigue      *int       `json:"fatigue,omitempty"`
	Motivation   *int       `json:"motivation,omitempty"`
	SleepQuality *int       `json:"sleep_quality,omitempty"`
	Stress       *int       `json:"stress,omitempty"`
	Soreness     []PainArea `json:"soreness,omitempty"`
	Pain         []PainArea `json:"pain,omitempty"`
	Illness      Illness    `json:"illness"`
	Notes        *string    `json:"notes,omitempty"`
}

func decodeCheckin(id string, at int64, date, timezone, raw string) Checkin {
	var c checkinContent
	_ = json.Unmarshal([]byte(raw), &c)
	return Checkin{ID: id, RecordedAt: time.UnixMilli(at).UTC().Format(time.RFC3339), LocalDate: date, Timezone: timezone, Energy: c.Energy, Fatigue: c.Fatigue, Motivation: c.Motivation, SleepQuality: c.SleepQuality, Stress: c.Stress, Soreness: c.Soreness, Pain: c.Pain, Illness: c.Illness, Notes: c.Notes}
}
