// Package mcpserver exposes Fitnessd's bounded, athlete-scoped MCP actions.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	openapi_types "github.com/oapi-codegen/runtime/types"
	fitnessauth "github.com/timdumol/fitsync/internal/auth"
	"github.com/timdumol/fitsync/internal/fitnessdata"
	"github.com/timdumol/fitsync/internal/fitnessrepo"
	"github.com/timdumol/fitsync/internal/httpapi"
)

const maxToolResults = 100

type Server struct {
	api   *httpapi.API
	auth  *fitnessauth.Authenticator
	repo  *fitnessrepo.Repository
	clock func() time.Time
	mcp   *mcp.Server
}

type TrainingContextInput struct {
	TargetAthleteID string   `json:"target_athlete_id,omitempty"`
	AsOf            string   `json:"as_of,omitempty"`
	LookbackDays    int      `json:"lookback_days,omitempty"`
	Include         []string `json:"include,omitempty"`
}
type DailyHealthInput struct {
	TargetAthleteID string   `json:"target_athlete_id,omitempty"`
	From            string   `json:"from"`
	To              string   `json:"to"`
	Metrics         []string `json:"metrics,omitempty"`
	Order           string   `json:"order,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	Cursor          string   `json:"cursor,omitempty"`
}
type ActivityInput struct {
	TargetAthleteID    string   `json:"target_athlete_id,omitempty"`
	From               string   `json:"from"`
	To                 string   `json:"to"`
	Categories         []string `json:"categories,omitempty"`
	SourceTypes        []string `json:"source_types,omitempty"`
	Detail             string   `json:"detail,omitempty"`
	MinDurationMinutes *float64 `json:"min_duration_minutes,omitempty"`
	MinDistanceKM      *float64 `json:"min_distance_km,omitempty"`
	MinTrainingLoad    *float64 `json:"min_training_load,omitempty"`
	Order              string   `json:"order,omitempty"`
	Limit              int      `json:"limit,omitempty"`
	Cursor             string   `json:"cursor,omitempty"`
}
type ActivityDetailInput struct {
	TargetAthleteID         string   `json:"target_athlete_id,omitempty"`
	ActivityID              string   `json:"activity_id"`
	Include                 []string `json:"include,omitempty"`
	MetricResolutionSeconds int      `json:"metric_resolution_seconds,omitempty"`
	MaxMetricPoints         int      `json:"max_metric_points,omitempty"`
}
type StrengthInput struct {
	TargetAthleteID string   `json:"target_athlete_id,omitempty"`
	From            string   `json:"from"`
	To              string   `json:"to"`
	ExerciseKeys    []string `json:"exercise_keys,omitempty"`
	MuscleGroups    []string `json:"muscle_groups,omitempty"`
	IncludeEntries  bool     `json:"include_entries,omitempty"`
	Order           string   `json:"order,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	Cursor          string   `json:"cursor,omitempty"`
}

type WorkoutEntryInput struct {
	EntryID         string   `json:"entry_id"`
	SequenceNo      int      `json:"sequence_no"`
	EntryType       string   `json:"entry_type"`
	ExerciseKey     *string  `json:"exercise_key,omitempty"`
	ExerciseName    *string  `json:"exercise_name,omitempty"`
	UserLabel       *string  `json:"user_label,omitempty"`
	SetNumber       *int     `json:"set_number,omitempty"`
	Reps            *float64 `json:"reps,omitempty"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
	LoadKG          *float64 `json:"load_kg,omitempty"`
	LoadDescription *string  `json:"load_description,omitempty"`
	IsBodyweight    *bool    `json:"is_bodyweight,omitempty"`
	ToFailure       *bool    `json:"to_failure,omitempty"`
	RIR             *float64 `json:"rir,omitempty"`
	RIRMin          *float64 `json:"rir_min,omitempty"`
	RIRMax          *float64 `json:"rir_max,omitempty"`
	Laterality      *string  `json:"laterality,omitempty"`
	MuscleGroups    []string `json:"muscle_groups,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
	IsWarmup        bool     `json:"is_warmup,omitempty"`
}
type UpsertWorkoutInput struct {
	TargetAthleteID  string              `json:"target_athlete_id,omitempty"`
	WorkoutID        string              `json:"workout_id"`
	ClientUpdatedAt  int64               `json:"client_updated_at"`
	Date             string              `json:"date"`
	Title            string              `json:"title"`
	WorkoutType      string              `json:"workout_type"`
	Source           string              `json:"source"`
	StartedAt        string              `json:"started_at,omitempty"`
	DurationMinutes  *float64            `json:"duration_minutes,omitempty"`
	PerceivedEffort  *float64            `json:"perceived_effort,omitempty"`
	Notes            *string             `json:"notes,omitempty"`
	LinkedActivityID *string             `json:"linked_activity_id,omitempty"`
	Entries          []WorkoutEntryInput `json:"entries,omitempty"`
}
type DeleteWorkoutInput struct {
	TargetAthleteID string `json:"target_athlete_id,omitempty"`
	WorkoutID       string `json:"workout_id"`
	ClientUpdatedAt int64  `json:"client_updated_at"`
}
type CheckinsInput struct {
	TargetAthleteID string `json:"target_athlete_id,omitempty"`
	From            string `json:"from"`
	To              string `json:"to"`
	Order           string `json:"order,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	Cursor          string `json:"cursor,omitempty"`
}
type CheckinInput struct {
	TargetAthleteID string                 `json:"target_athlete_id,omitempty"`
	CheckinID       string                 `json:"checkin_id"`
	ClientUpdatedAt int64                  `json:"client_updated_at"`
	RecordedAt      string                 `json:"recorded_at"`
	Energy          *int                   `json:"energy,omitempty"`
	Fatigue         *int                   `json:"fatigue,omitempty"`
	Motivation      *int                   `json:"motivation,omitempty"`
	SleepQuality    *int                   `json:"sleep_quality,omitempty"`
	Stress          *int                   `json:"stress,omitempty"`
	Soreness        []fitnessrepo.PainArea `json:"soreness,omitempty"`
	Pain            []fitnessrepo.PainArea `json:"pain,omitempty"`
	Illness         fitnessrepo.Illness    `json:"illness,omitempty"`
	Notes           *string                `json:"notes,omitempty"`
}
type ProfileInput struct {
	TargetAthleteID       string                                      `json:"target_athlete_id,omitempty"`
	ClientUpdatedAt       int64                                       `json:"client_updated_at"`
	Timezone              string                                      `json:"timezone"`
	Units                 string                                      `json:"units"`
	BirthYear             *int                                        `json:"birth_year,omitempty"`
	Goals                 []fitnessrepo.ProfileGoal                   `json:"goals,omitempty"`
	TargetEvents          []fitnessrepo.TargetEvent                   `json:"target_events,omitempty"`
	WeeklyAvailability    map[string][]fitnessrepo.AvailabilityWindow `json:"weekly_availability,omitempty"`
	Preferences           fitnessrepo.Preferences                     `json:"preferences,omitempty"`
	HeartRateZones        []fitnessrepo.Zone                          `json:"heart_rate_zones,omitempty"`
	PaceZones             []fitnessrepo.Zone                          `json:"pace_zones,omitempty"`
	PowerZones            []fitnessrepo.Zone                          `json:"power_zones,omitempty"`
	Constraints           map[string]json.RawMessage                  `json:"constraints,omitempty"`
	ConditionsAndInjuries []fitnessrepo.Condition                     `json:"conditions_and_injuries,omitempty"`
}
type PlanInput struct {
	TargetAthleteID  string `json:"target_athlete_id,omitempty"`
	From             string `json:"from"`
	To               string `json:"to"`
	IncludeCompleted *bool  `json:"include_completed,omitempty"`
}
type PlannedSessionInput struct {
	TargetAthleteID string                     `json:"target_athlete_id,omitempty"`
	PlanID          string                     `json:"plan_id"`
	SessionID       string                     `json:"session_id"`
	ClientUpdatedAt int64                      `json:"client_updated_at"`
	Date            string                     `json:"date"`
	Sport           string                     `json:"sport"`
	SessionType     string                     `json:"session_type"`
	Priority        string                     `json:"priority"`
	Targets         fitnessrepo.PlannedTargets `json:"targets,omitempty"`
	Notes           *string                    `json:"notes,omitempty"`
}
type PlannedStatusInput struct {
	TargetAthleteID     string  `json:"target_athlete_id,omitempty"`
	SessionID           string  `json:"session_id"`
	ClientUpdatedAt     int64   `json:"client_updated_at"`
	Status              string  `json:"status"`
	CompletedActivityID *string `json:"completed_activity_id,omitempty"`
	CompletedWorkoutID  *string `json:"completed_workout_id,omitempty"`
}
type AnalysisInput struct {
	TargetAthleteID           string   `json:"target_athlete_id,omitempty"`
	From                      string   `json:"from"`
	To                        string   `json:"to"`
	CompareWithPreviousPeriod bool     `json:"compare_with_previous_period,omitempty"`
	GroupBy                   string   `json:"group_by,omitempty"`
	Categories                []string `json:"categories,omitempty"`
}

type SummaryOutput struct {
	Items      []fitnessdata.ActivitySummary `json:"items"`
	DataStatus fitnessrepo.DataStatus        `json:"data_status"`
	NextCursor *string                       `json:"next_cursor,omitempty"`
}
type HealthOutput struct {
	Items      []fitnessrepo.DailyHealthItem `json:"items"`
	DataStatus fitnessrepo.DataStatus        `json:"data_status"`
	NextCursor *string                       `json:"next_cursor,omitempty"`
}

func New(api *httpapi.API, authenticator *fitnessauth.Authenticator) (*Server, error) {
	return NewWithClock(api, authenticator, time.Now)
}
func NewWithClock(api *httpapi.API, authenticator *fitnessauth.Authenticator, clock func() time.Time) (*Server, error) {
	if api == nil || authenticator == nil {
		return nil, errors.New("MCP API and authenticator are required")
	}
	if clock == nil {
		clock = time.Now
	}
	s := &Server{api: api, auth: authenticator, clock: clock, repo: fitnessrepo.New(api.Database(), clock)}
	s.mcp = mcp.NewServer(&mcp.Implementation{Name: "fitnessd", Version: "1.0.0", Description: "Household fitness data"}, &mcp.ServerOptions{PageSize: maxToolResults})
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_training_context", Description: "Get a bounded decision-ready snapshot for fitness guidance, not diagnosis or emergency assessment.", Annotations: readAnnotations(), InputSchema: trainingContextSchema()}, s.trainingContext)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_daily_health", Description: "Read normalized daily sleep, recovery, and wellness metrics.", Annotations: readAnnotations(), InputSchema: dailyHealthSchema()}, s.dailyHealth)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_activities", Description: "Read normalized, bounded activity summaries for training analysis.", Annotations: readAnnotations(), InputSchema: activitySchema()}, s.activities)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_activity", Description: "Read one activity with bounded splits, zones, and GPS-free metrics.", Annotations: readAnnotations(), InputSchema: activityDetailSchema()}, s.activity)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_strength_workouts", Description: "Read structured strength sessions, entries, and muscle-group summaries.", Annotations: readAnnotations(), InputSchema: strengthSchema()}, s.strength)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "upsert_workout", Description: "Atomically create or replace one structured workout and its ordered entries.", Annotations: writeAnnotations(false), InputSchema: upsertWorkoutSchema()}, s.upsertWorkout)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "delete_workout", Description: "Tombstone one workout and its entries using last-write-wins.", Annotations: writeAnnotations(true), InputSchema: deleteWorkoutSchema()}, s.deleteWorkout)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_checkins", Description: "Read subjective readiness check-ins in a bounded date range.", Annotations: readAnnotations(), InputSchema: checkinsSchema()}, s.checkins)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "record_checkin", Description: "Create or replace one subjective readiness check-in.", Annotations: writeAnnotations(false), InputSchema: recordCheckinSchema()}, s.recordCheckin)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_athlete_profile", Description: "Read the editable athlete goals, zones, preferences, and constraints.", Annotations: readAnnotations(), InputSchema: targetOnlySchema()}, s.profile)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "update_athlete_profile", Description: "Replace the editable athlete profile atomically.", Annotations: writeAnnotations(false), InputSchema: profileSchema()}, s.updateProfile)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_training_plan", Description: "Read the active training plan and planned sessions.", Annotations: readAnnotations(), InputSchema: planSchema()}, s.plan)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "upsert_planned_session", Description: "Create or replace one planned training session.", Annotations: writeAnnotations(false), InputSchema: plannedSessionSchema()}, s.upsertPlannedSession)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "update_planned_session_status", Description: "Update planned-session status and owned completion links.", Annotations: writeAnnotations(true), InputSchema: plannedStatusSchema()}, s.updatePlannedStatus)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_training_analysis", Description: "Calculate bounded deterministic period and comparison aggregates.", Annotations: readAnnotations(), InputSchema: analysisSchema()}, s.analysis)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "update_workout", Description: "Deprecated low-level sync mutation; use upsert_workout or delete_workout.", Annotations: writeAnnotations(true), InputSchema: legacyUpdateSchema()}, s.updateWorkout)
	return s, nil
}

func (s *Server) Handler() http.Handler {
	stream := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return s.mcp }, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true, MaxRequestBodyBytes: 1 << 20})
	return s.auth.Middleware("", stream)
}
func (s *Server) principal(ctx context.Context, target, scope string) (fitnessauth.Principal, string, error) {
	p, ok := fitnessauth.PrincipalFromContext(ctx)
	if !ok {
		return fitnessauth.Principal{}, "", errors.New("invalid_input: authentication is required")
	}
	if !p.HasScope(scope) {
		return fitnessauth.Principal{}, "", fmt.Errorf("invalid_input: required scope is missing: %s", scope)
	}
	if target == "" {
		return p, p.AthleteID, nil
	}
	id, e := uuid.Parse(target)
	if e != nil {
		return fitnessauth.Principal{}, "", errors.New("invalid_input: target_athlete_id must be a UUID")
	}
	if e = s.auth.AuthorizeTarget(ctx, p, id.String()); e != nil {
		return fitnessauth.Principal{}, "", errors.New("not_found: athlete is not available")
	}
	return p, id.String(), nil
}

func (s *Server) trainingContext(ctx context.Context, _ *mcp.CallToolRequest, in TrainingContextInput) (*mcp.CallToolResult, fitnessrepo.TrainingContext, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.TrainingContext{}, e
	}
	lookback := in.LookbackDays
	if lookback == 0 {
		lookback = 28
	}
	value, err := s.repo.TrainingContext(ctx, id, in.AsOf, lookback, in.Include)
	return nil, value, err
}
func (s *Server) dailyHealth(ctx context.Context, _ *mcp.CallToolRequest, in DailyHealthInput) (*mcp.CallToolResult, HealthOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, HealthOutput{}, e
	}
	page, e := s.repo.DailyHealth(ctx, id, fitnessrepo.HealthQuery{From: in.From, To: in.To, Metrics: in.Metrics, Order: in.Order, Limit: in.Limit, Cursor: in.Cursor})
	if e != nil {
		return nil, HealthOutput{}, e
	}
	return nil, HealthOutput{Items: page.Items, DataStatus: page.DataStatus, NextCursor: page.NextCursor}, nil
}
func (s *Server) activities(ctx context.Context, _ *mcp.CallToolRequest, in ActivityInput) (*mcp.CallToolResult, SummaryOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, SummaryOutput{}, e
	}
	page, e := s.repo.Activities(ctx, id, fitnessrepo.ActivityQuery{From: in.From, To: in.To, Categories: in.Categories, SourceTypes: in.SourceTypes, Detail: in.Detail, MinDurationMinutes: in.MinDurationMinutes, MinDistanceKM: in.MinDistanceKM, MinTrainingLoad: in.MinTrainingLoad, Order: in.Order, Limit: in.Limit, Cursor: in.Cursor})
	if e != nil {
		return nil, SummaryOutput{}, e
	}
	return nil, SummaryOutput{Items: page.Items, DataStatus: page.DataStatus, NextCursor: page.NextCursor}, nil
}
func (s *Server) activity(ctx context.Context, _ *mcp.CallToolRequest, in ActivityDetailInput) (*mcp.CallToolResult, fitnessrepo.ActivityResult, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.ActivityResult{}, e
	}
	resolution := in.MetricResolutionSeconds
	if resolution == 0 {
		resolution = 30
	}
	points := in.MaxMetricPoints
	if points == 0 {
		points = 500
	}
	if in.ActivityID == "" || len(in.ActivityID) > 128 || resolution < 5 || resolution > 300 || points < 10 || points > 1000 {
		return nil, fitnessrepo.ActivityResult{}, errors.New("invalid_input: activity detail bounds are invalid")
	}
	value, err := s.repo.Activity(ctx, id, in.ActivityID, in.Include, resolution, points)
	return nil, value, err
}
func (s *Server) strength(ctx context.Context, _ *mcp.CallToolRequest, in StrengthInput) (*mcp.CallToolResult, fitnessrepo.Page[fitnessrepo.StrengthWorkout], error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.Page[fitnessrepo.StrengthWorkout]{}, e
	}
	value, err := s.repo.StrengthWorkouts(ctx, id, fitnessrepo.StrengthQuery{From: in.From, To: in.To, ExerciseKeys: in.ExerciseKeys, MuscleGroups: in.MuscleGroups, IncludeEntries: in.IncludeEntries, Order: in.Order, Limit: in.Limit, Cursor: in.Cursor})
	return nil, value, err
}

func (s *Server) upsertWorkout(ctx context.Context, _ *mcp.CallToolRequest, in UpsertWorkoutInput) (*mcp.CallToolResult, fitnessrepo.WorkoutWinner, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, fitnessrepo.WorkoutWinner{}, e
	}
	if in.WorkoutID == "" || in.ClientUpdatedAt < 0 || in.Date == "" || in.Title == "" || in.WorkoutType == "" || in.Source == "" || len(in.Entries) > 200 {
		return nil, fitnessrepo.WorkoutWinner{}, errors.New("invalid_input: workout fields or entry bounds are invalid")
	}
	var started *time.Time
	if in.StartedAt != "" {
		v, err := time.Parse(time.RFC3339, in.StartedAt)
		if err != nil {
			return nil, fitnessrepo.WorkoutWinner{}, errors.New("invalid_input: started_at must be RFC3339")
		}
		started = &v
	}
	entries := make([]fitnessrepo.StrengthEntry, 0, len(in.Entries))
	seen := map[int]bool{}
	for _, x := range in.Entries {
		if x.EntryID == "" || x.SequenceNo < 1 || seen[x.SequenceNo] || (x.EntryType != "set" && x.EntryType != "rest" && x.EntryType != "note") {
			return nil, fitnessrepo.WorkoutWinner{}, errors.New("invalid_input: entries are invalid")
		}
		seen[x.SequenceNo] = true
		entries = append(entries, fitnessrepo.StrengthEntry{EntryID: x.EntryID, SequenceNo: x.SequenceNo, EntryType: x.EntryType, ExerciseKey: x.ExerciseKey, ExerciseName: x.ExerciseName, UserLabel: x.UserLabel, SetNumber: x.SetNumber, Reps: x.Reps, DurationSeconds: x.DurationSeconds, LoadKG: x.LoadKG, LoadDescription: x.LoadDescription, IsBodyweight: x.IsBodyweight, ToFailure: x.ToFailure, RIR: x.RIR, RIRMin: x.RIRMin, RIRMax: x.RIRMax, Laterality: x.Laterality, MuscleGroups: x.MuscleGroups, Notes: x.Notes, IsWarmup: x.IsWarmup})
	}
	if in.LinkedActivityID != nil && !s.repo.OwnedActivity(ctx, id, *in.LinkedActivityID) {
		return nil, fitnessrepo.WorkoutWinner{}, errors.New("not_found: linked activity")
	}
	value, err := s.repo.UpsertWorkout(ctx, id, fitnessrepo.WorkoutWrite{ID: in.WorkoutID, ClientUpdatedAt: in.ClientUpdatedAt, Date: in.Date, Title: in.Title, WorkoutType: in.WorkoutType, Source: in.Source, StartedAt: started, DurationMinutes: in.DurationMinutes, PerceivedEffort: in.PerceivedEffort, Notes: in.Notes, LinkedActivityID: in.LinkedActivityID, Entries: entries})
	return nil, value, err
}
func (s *Server) deleteWorkout(ctx context.Context, _ *mcp.CallToolRequest, in DeleteWorkoutInput) (*mcp.CallToolResult, fitnessrepo.WorkoutWinner, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, fitnessrepo.WorkoutWinner{}, e
	}
	value, err := s.repo.DeleteWorkout(ctx, id, in.WorkoutID, in.ClientUpdatedAt)
	return nil, value, err
}
func (s *Server) checkins(ctx context.Context, _ *mcp.CallToolRequest, in CheckinsInput) (*mcp.CallToolResult, fitnessrepo.Page[fitnessrepo.Checkin], error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.Page[fitnessrepo.Checkin]{}, e
	}
	value, err := s.repo.Checkins(ctx, id, fitnessrepo.CheckinQuery{From: in.From, To: in.To, Order: in.Order, Limit: in.Limit, Cursor: in.Cursor})
	return nil, value, err
}
func (s *Server) recordCheckin(ctx context.Context, _ *mcp.CallToolRequest, in CheckinInput) (*mcp.CallToolResult, fitnessrepo.CheckinWinner, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, fitnessrepo.CheckinWinner{}, e
	}
	at, e := time.Parse(time.RFC3339, in.RecordedAt)
	if e != nil || at.After(s.clock().Add(24*time.Hour)) {
		return nil, fitnessrepo.CheckinWinner{}, errors.New("invalid_input: recorded_at must be RFC3339 and not more than 24 hours ahead")
	}
	if in.CheckinID == "" || in.ClientUpdatedAt < 0 {
		return nil, fitnessrepo.CheckinWinner{}, errors.New("invalid_input: check-in identity or clock is invalid")
	}
	if in.Energy == nil && in.Fatigue == nil && in.Motivation == nil && in.SleepQuality == nil && in.Stress == nil && len(in.Soreness) == 0 && len(in.Pain) == 0 && !in.Illness.Present && len(in.Illness.Symptoms) == 0 && in.Notes == nil {
		return nil, fitnessrepo.CheckinWinner{}, errors.New("invalid_input: at least one check-in field is required")
	}
	value, err := s.repo.UpsertCheckin(ctx, id, fitnessrepo.CheckinWrite{ID: in.CheckinID, ClientUpdatedAt: in.ClientUpdatedAt, RecordedAt: at, Energy: in.Energy, Fatigue: in.Fatigue, Motivation: in.Motivation, SleepQuality: in.SleepQuality, Stress: in.Stress, Soreness: in.Soreness, Pain: in.Pain, Illness: in.Illness, Notes: in.Notes})
	return nil, value, err
}
func (s *Server) profile(ctx context.Context, _ *mcp.CallToolRequest, in targetInput) (*mcp.CallToolResult, fitnessrepo.Profile, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.Profile{}, e
	}
	value, err := s.repo.Profile(ctx, id)
	return nil, value, err
}
func (s *Server) updateProfile(ctx context.Context, _ *mcp.CallToolRequest, in ProfileInput) (*mcp.CallToolResult, fitnessrepo.ProfileWinner, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, fitnessrepo.ProfileWinner{}, e
	}
	value, err := s.repo.UpsertProfile(ctx, id, fitnessrepo.ProfileWrite{ClientUpdatedAt: in.ClientUpdatedAt, Profile: fitnessrepo.Profile{Timezone: in.Timezone, Units: in.Units, BirthYear: in.BirthYear, Goals: in.Goals, TargetEvents: in.TargetEvents, WeeklyAvailability: in.WeeklyAvailability, Preferences: in.Preferences, HeartRateZones: in.HeartRateZones, PaceZones: in.PaceZones, PowerZones: in.PowerZones, Constraints: in.Constraints, ConditionsAndInjuries: in.ConditionsAndInjuries}})
	return nil, value, err
}
func (s *Server) plan(ctx context.Context, _ *mcp.CallToolRequest, in PlanInput) (*mcp.CallToolResult, fitnessrepo.PlanResult, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.PlanResult{}, e
	}
	include := true
	if in.IncludeCompleted != nil {
		include = *in.IncludeCompleted
	}
	value, err := s.repo.Plan(ctx, id, in.From, in.To, include)
	return nil, value, err
}
func (s *Server) upsertPlannedSession(ctx context.Context, _ *mcp.CallToolRequest, in PlannedSessionInput) (*mcp.CallToolResult, fitnessrepo.PlannedSessionWinner, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, fitnessrepo.PlannedSessionWinner{}, e
	}
	value, err := s.repo.UpsertPlannedSession(ctx, id, fitnessrepo.PlannedSessionWrite{ID: in.SessionID, PlanID: in.PlanID, ClientUpdatedAt: in.ClientUpdatedAt, Date: in.Date, Sport: in.Sport, SessionType: in.SessionType, Priority: in.Priority, Targets: in.Targets, Notes: in.Notes})
	return nil, value, err
}
func (s *Server) updatePlannedStatus(ctx context.Context, _ *mcp.CallToolRequest, in PlannedStatusInput) (*mcp.CallToolResult, fitnessrepo.PlannedSessionWinner, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, fitnessrepo.PlannedSessionWinner{}, e
	}
	value, err := s.repo.UpdatePlannedSessionStatus(ctx, id, fitnessrepo.PlannedSessionStatusWrite{ID: in.SessionID, ClientUpdatedAt: in.ClientUpdatedAt, Status: in.Status, ActivityID: in.CompletedActivityID, WorkoutID: in.CompletedWorkoutID})
	return nil, value, err
}
func (s *Server) analysis(ctx context.Context, _ *mcp.CallToolRequest, in AnalysisInput) (*mcp.CallToolResult, fitnessrepo.TrainingAnalysis, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.TrainingAnalysis{}, e
	}
	value, err := s.repo.TrainingAnalysis(ctx, id, fitnessrepo.AnalysisQuery{From: in.From, To: in.To, Compare: in.CompareWithPreviousPeriod, GroupBy: in.GroupBy, Categories: in.Categories})
	return nil, value, err
}

type targetInput struct {
	TargetAthleteID string `json:"target_athlete_id,omitempty"`
}
type UpdateWorkoutInput struct {
	TargetAthleteID string          `json:"target_athlete_id,omitempty"`
	Kind            string          `json:"kind"`
	ID              string          `json:"id"`
	ClientUpdatedAt int64           `json:"client_updated_at"`
	Deleted         bool            `json:"deleted,omitempty"`
	WorkoutDate     string          `json:"workout_date,omitempty"`
	Title           string          `json:"title,omitempty"`
	Source          string          `json:"source,omitempty"`
	SequenceNo      *int            `json:"sequence_no,omitempty"`
	EntryType       string          `json:"entry_type,omitempty"`
	SessionID       string          `json:"session_id,omitempty"`
	DocumentKey     string          `json:"document_key,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}
type UpdateWorkoutWinner struct {
	AthleteID       string `json:"athlete_id"`
	ClientUpdatedAt int64  `json:"client_updated_at"`
	Deleted         bool   `json:"deleted"`
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Payload         any    `json:"payload,omitempty"`
	ServerSyncedAt  int64  `json:"server_synced_at"`
}
type UpdateWorkoutOutput struct {
	Winner UpdateWorkoutWinner `json:"winner"`
}

func (s *Server) updateWorkout(ctx context.Context, _ *mcp.CallToolRequest, in UpdateWorkoutInput) (*mcp.CallToolResult, UpdateWorkoutOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, UpdateWorkoutOutput{}, e
	}
	parsed, e := uuid.Parse(id)
	if e != nil {
		return nil, UpdateWorkoutOutput{}, errors.New("invalid_input: resolved athlete ID")
	}
	kind := httpapi.SyncMutationKind(strings.ToLower(in.Kind))
	if kind != httpapi.SyncMutationKindSession && kind != httpapi.SyncMutationKindEntry && kind != httpapi.SyncMutationKindDocument {
		return nil, UpdateWorkoutOutput{}, errors.New("invalid_input: kind")
	}
	mutation := httpapi.SyncMutation{Kind: kind, Id: in.ID, ClientUpdatedAt: in.ClientUpdatedAt, Deleted: in.Deleted, AthleteId: openapi_types.UUID(parsed)}
	if in.WorkoutDate != "" {
		d, err := time.Parse("2006-01-02", in.WorkoutDate)
		if err != nil {
			return nil, UpdateWorkoutOutput{}, errors.New("invalid_input: workout_date")
		}
		v := openapi_types.Date{Time: d}
		mutation.WorkoutDate = &v
	}
	if in.Title != "" {
		mutation.Title = &in.Title
	}
	if in.Source != "" {
		mutation.Source = &in.Source
	}
	if in.SequenceNo != nil {
		mutation.SequenceNo = in.SequenceNo
	}
	if in.EntryType != "" {
		mutation.EntryType = &in.EntryType
	}
	if in.SessionID != "" {
		mutation.SessionId = &in.SessionID
	}
	if in.DocumentKey != "" {
		mutation.DocumentKey = &in.DocumentKey
	}
	if len(in.Payload) > 0 && !json.Valid(in.Payload) {
		return nil, UpdateWorkoutOutput{}, errors.New("invalid_input: payload")
	}
	if len(in.Payload) > 0 {
		mutation.Payload = in.Payload
	}
	w, e := s.api.ApplySyncMutations(ctx, []httpapi.SyncMutation{mutation})
	if e != nil {
		return nil, UpdateWorkoutOutput{}, e
	}
	return nil, UpdateWorkoutOutput{Winner: UpdateWorkoutWinner{AthleteID: w[0].AthleteId.String(), ClientUpdatedAt: w[0].ClientUpdatedAt, Deleted: w[0].Deleted, ID: w[0].Id, Kind: string(w[0].Kind), Payload: w[0].Payload, ServerSyncedAt: w[0].ServerSyncedAt}}, nil
}
