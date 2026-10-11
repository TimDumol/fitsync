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
	"github.com/timdumol/fitsync/internal/exercisecatalog"
	"github.com/timdumol/fitsync/internal/fitnessdata"
	"github.com/timdumol/fitsync/internal/fitnessrepo"
	"github.com/timdumol/fitsync/internal/httpapi"
)

const maxToolResults = 100

var mcpActionNames = []string{
	"get_training_context",
	"get_daily_health",
	"get_activities",
	"get_activity",
	"get_activity_intervals",
	"get_training_timeline",
	"get_readiness_for_session",
	"get_strength_workouts",
	"get_exercise_progress",
	"resolve_exercises",
	"upsert_workout",
	"patch_workout",
	"delete_workout",
	"get_checkins",
	"record_checkin",
	"record_quick_readiness",
	"get_athlete_profile",
	"update_athlete_profile",
	"get_training_plan",
	"upsert_training_plan_draft",
	"publish_training_plan",
	"get_workout",
	"upsert_planned_session",
	"update_planned_session_status",
	"get_training_analysis",
	"update_workout",
}

var mcpReadActionNames = []string{
	"get_training_context",
	"get_daily_health",
	"get_activities",
	"get_activity",
	"get_activity_intervals",
	"get_training_timeline",
	"get_readiness_for_session",
	"get_strength_workouts",
	"get_exercise_progress",
	"resolve_exercises",
	"get_checkins",
	"get_athlete_profile",
	"get_training_plan",
	"get_workout",
	"get_training_analysis",
}

var mcpWriteActionNames = []string{
	"upsert_workout",
	"patch_workout",
	"delete_workout",
	"record_checkin",
	"record_quick_readiness",
	"update_athlete_profile",
	"upsert_training_plan_draft",
	"publish_training_plan",
	"upsert_planned_session",
	"update_planned_session_status",
	"update_workout",
}

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
type ActivityIntervalsInput struct {
	TargetAthleteID   string `json:"target_athlete_id,omitempty"`
	ActivityID        string `json:"activity_id"`
	IncludeLaps       bool   `json:"include_laps,omitempty"`
	IncludeRecoveries bool   `json:"include_recoveries,omitempty"`
	MaxSegments       int    `json:"max_segments,omitempty"`
}
type TimelineInput struct {
	TargetAthleteID  string `json:"target_athlete_id,omitempty"`
	From             string `json:"from"`
	To               string `json:"to"`
	IncludeUnplanned bool   `json:"include_unplanned,omitempty"`
	IncludeEntries   bool   `json:"include_entries,omitempty"`
	Order            string `json:"order,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Cursor           string `json:"cursor,omitempty"`
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

type ExerciseProgressInput struct {
	TargetAthleteID string `json:"target_athlete_id,omitempty"`
	ExerciseKey     string `json:"exercise_key"`
	From            string `json:"from"`
	To              string `json:"to"`
	IncludeWarmups  bool   `json:"include_warmups,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	Cursor          string `json:"cursor,omitempty"`
}
type ResolveExercisesInput struct {
	TargetAthleteID string                 `json:"target_athlete_id,omitempty"`
	Exercises       []ResolveExerciseInput `json:"exercises"`
	MaxCandidates   int                    `json:"max_candidates,omitempty"`
}
type ResolveExerciseInput struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}
type ExerciseCandidateOutput struct {
	ExerciseKey   string `json:"exercise_key"`
	CanonicalName string `json:"canonical_name"`
	Description   string `json:"description"`
	MatchedAlias  string `json:"matched_alias"`
	Score         int    `json:"score"`
}
type ResolveExerciseResult struct {
	Ref           string                    `json:"ref"`
	Name          string                    `json:"name"`
	Status        string                    `json:"status"`
	ExerciseKey   *string                   `json:"exercise_key"`
	CanonicalName string                    `json:"canonical_name,omitempty"`
	MatchedAlias  string                    `json:"matched_alias,omitempty"`
	Candidates    []ExerciseCandidateOutput `json:"candidates"`
}
type ResolveExercisesOutput struct {
	CatalogRevision int                     `json:"catalog_revision"`
	Results         []ResolveExerciseResult `json:"results"`
}
type ReadinessInput struct {
	TargetAthleteID         string `json:"target_athlete_id,omitempty"`
	AsOf                    string `json:"as_of,omitempty"`
	SessionDate             string `json:"session_date"`
	ProposedSessionType     string `json:"proposed_session_type"`
	ProposedDurationMinutes int    `json:"proposed_duration_minutes"`
	ProposedIntensity       string `json:"proposed_intensity"`
}

type WorkoutEntryInput struct {
	EntryID              string   `json:"entry_id"`
	IdempotencyKey       string   `json:"idempotency_key,omitempty"`
	SequenceNo           int      `json:"sequence_no"`
	EntryType            string   `json:"entry_type"`
	ExerciseKey          *string  `json:"exercise_key,omitempty"`
	ExerciseName         *string  `json:"exercise_name,omitempty"`
	UserLabel            *string  `json:"user_label,omitempty"`
	SetNumber            *int     `json:"set_number,omitempty"`
	Reps                 *float64 `json:"reps,omitempty"`
	DurationSeconds      *float64 `json:"duration_seconds,omitempty"`
	LoadKG               *float64 `json:"load_kg,omitempty"`
	LoadDescription      *string  `json:"load_description,omitempty"`
	IsBodyweight         *bool    `json:"is_bodyweight,omitempty"`
	ToFailure            *bool    `json:"to_failure,omitempty"`
	RIR                  *float64 `json:"rir,omitempty"`
	RIRMin               *float64 `json:"rir_min,omitempty"`
	RIRMax               *float64 `json:"rir_max,omitempty"`
	Laterality           *string  `json:"laterality,omitempty"`
	RepsPerSide          *bool    `json:"reps_per_side,omitempty"`
	LoadBasis            *string  `json:"load_basis,omitempty"`
	LoadPerLimbKG        *float64 `json:"load_per_limb_kg,omitempty"`
	TotalExternalLoadKG  *float64 `json:"total_external_load_kg,omitempty"`
	AddedLoadKG          *float64 `json:"added_load_kg,omitempty"`
	MuscleGroups         []string `json:"muscle_groups,omitempty"`
	DirectMuscleGroups   []string `json:"direct_muscle_groups,omitempty"`
	IndirectMuscleGroups []string `json:"indirect_muscle_groups,omitempty"`
	Notes                *string  `json:"notes,omitempty"`
	Completed            *bool    `json:"completed,omitempty"`
	IsWarmup             bool     `json:"is_warmup,omitempty"`
}
type UpsertWorkoutInput struct {
	TargetAthleteID  string              `json:"target_athlete_id,omitempty"`
	WorkoutID        string              `json:"workout_id"`
	IdempotencyKey   string              `json:"idempotency_key,omitempty"`
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
type PatchWorkoutInput struct {
	TargetAthleteID string                       `json:"target_athlete_id,omitempty"`
	WorkoutID       string                       `json:"workout_id"`
	ExpectedSyncSeq int64                        `json:"expected_sync_seq"`
	ClientUpdatedAt int64                        `json:"client_updated_at"`
	Operations      []PatchWorkoutOperationInput `json:"operations"`
}
type PatchWorkoutOperationInput struct {
	Op                   string   `json:"op"`
	ActivityID           *string  `json:"activity_id,omitempty"`
	EntryID              *string  `json:"entry_id,omitempty"`
	RIR                  *float64 `json:"rir,omitempty"`
	ExerciseKey          *string  `json:"exercise_key,omitempty"`
	Laterality           *string  `json:"laterality,omitempty"`
	RepsPerSide          *bool    `json:"reps_per_side,omitempty"`
	LoadBasis            *string  `json:"load_basis,omitempty"`
	LoadPerLimbKG        *float64 `json:"load_per_limb_kg,omitempty"`
	TotalExternalLoadKG  *float64 `json:"total_external_load_kg,omitempty"`
	AddedLoadKG          *float64 `json:"added_load_kg,omitempty"`
	DirectMuscleGroups   []string `json:"direct_muscle_groups,omitempty"`
	IndirectMuscleGroups []string `json:"indirect_muscle_groups,omitempty"`
	Text                 *string  `json:"text,omitempty"`
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
	IdempotencyKey  string                 `json:"idempotency_key,omitempty"`
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
type QuickReadinessInput struct {
	TargetAthleteID string                 `json:"target_athlete_id,omitempty"`
	CheckinID       string                 `json:"checkin_id,omitempty"`
	IdempotencyKey  string                 `json:"idempotency_key,omitempty"`
	ClientUpdatedAt int64                  `json:"client_updated_at"`
	RecordedAt      string                 `json:"recorded_at"`
	Energy          int                    `json:"energy"`
	LegFreshness    int                    `json:"leg_freshness"`
	SleepQuality    int                    `json:"sleep_quality"`
	PainPresent     bool                   `json:"pain_present"`
	Pain            []fitnessrepo.PainArea `json:"pain,omitempty"`
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
	PlanID           string `json:"plan_id,omitempty"`
	IncludeSteps     *bool  `json:"include_steps,omitempty"`
	IncludeCompleted *bool  `json:"include_completed,omitempty"`
}
type PlannedSessionInput struct {
	TargetAthleteID    string                     `json:"target_athlete_id,omitempty"`
	PlanID             string                     `json:"plan_id"`
	PlanIdempotencyKey string                     `json:"plan_idempotency_key,omitempty"`
	SessionID          string                     `json:"session_id"`
	IdempotencyKey     string                     `json:"idempotency_key,omitempty"`
	ClientUpdatedAt    int64                      `json:"client_updated_at"`
	Date               string                     `json:"date"`
	Sport              string                     `json:"sport"`
	SessionType        string                     `json:"session_type"`
	Priority           string                     `json:"priority"`
	Targets            fitnessrepo.PlannedTargets `json:"targets,omitempty"`
	Notes              *string                    `json:"notes,omitempty"`
	Title              string                     `json:"title,omitempty"`
	Description        string                     `json:"description,omitempty"`
	Steps              []fitnessrepo.PlannedStep  `json:"steps,omitempty"`
}
type PlanDraftInput struct {
	TargetAthleteID string                  `json:"target_athlete_id,omitempty"`
	PlanID          string                  `json:"plan_id"`
	IdempotencyKey  string                  `json:"idempotency_key,omitempty"`
	Title           string                  `json:"title"`
	Description     *string                 `json:"description,omitempty"`
	Goal            json.RawMessage         `json:"goal,omitempty"`
	StartDate       *string                 `json:"start_date,omitempty"`
	EndDate         *string                 `json:"end_date,omitempty"`
	SourceRef       *string                 `json:"source_ref,omitempty"`
	Sessions        []PlanDraftSessionInput `json:"sessions"`
}
type PlanDraftSessionInput struct {
	SessionID      string                     `json:"session_id"`
	IdempotencyKey string                     `json:"idempotency_key,omitempty"`
	Date           string                     `json:"date"`
	Title          string                     `json:"title"`
	Description    *string                    `json:"description,omitempty"`
	Sport          string                     `json:"sport"`
	SessionType    string                     `json:"session_type"`
	Priority       string                     `json:"priority"`
	Targets        fitnessrepo.PlannedTargets `json:"targets,omitempty"`
	Notes          *string                    `json:"notes,omitempty"`
	Steps          []fitnessrepo.PlannedStep  `json:"steps"`
	Groups         []PlannedStepGroupInput    `json:"groups,omitempty"`
}
type PlannedStepGroupInput struct {
	ID              string  `json:"group_id,omitempty"`
	IdempotencyKey  string  `json:"idempotency_key,omitempty"`
	Label           string  `json:"label,omitempty"`
	Mode            string  `json:"mode"`
	Rounds          int     `json:"rounds"`
	StepSequenceNos []int   `json:"step_sequence_nos"`
	Notes           *string `json:"notes,omitempty"`
}
type PlanDraftOutput struct {
	Plan          planResultOutput    `json:"plan"`
	SessionCount  int                 `json:"session_count"`
	StepCount     int                 `json:"step_count"`
	GroupCount    int                 `json:"group_count"`
	SyncSeq       int64               `json:"sync_seq"`
	Normalization normalizationReport `json:"normalization"`
}

type normalizationItem struct {
	Ref          string                    `json:"ref"`
	ExerciseName string                    `json:"exercise_name,omitempty"`
	OriginalKey  string                    `json:"original_key,omitempty"`
	ExerciseKey  *string                   `json:"exercise_key,omitempty"`
	Status       string                    `json:"status"`
	Candidates   []ExerciseCandidateOutput `json:"candidates"`
}
type normalizationReport struct {
	CatalogRevision         int                 `json:"catalog_revision"`
	Explicit                int                 `json:"explicit"`
	ResolvedFromName        int                 `json:"resolved_from_name"`
	DeprecatedKeysRewritten int                 `json:"deprecated_keys_rewritten"`
	Unknown                 int                 `json:"unknown"`
	Items                   []normalizationItem `json:"items"`
}
type upsertWorkoutOutput struct {
	Workout             strengthWorkoutOutput `json:"workout"`
	Created             bool                  `json:"created"`
	Deleted             bool                  `json:"deleted"`
	SubmittedVersionWon bool                  `json:"submitted_version_won"`
	ServerSyncedAt      int64                 `json:"server_synced_at"`
	Normalization       normalizationReport   `json:"normalization"`
}

// The repository deliberately keeps JSON columns as json.RawMessage for
// storage and strict input validation. MCP output types must describe their
// wire shapes directly: the SDK reflects json.RawMessage as an array of
// bytes, which is not the object returned by these tools.
type planResultOutput struct {
	Plan     *trainingPlanOutput    `json:"plan"`
	Sessions []plannedSessionOutput `json:"sessions"`
}
type trainingPlanOutput struct {
	ID             string                 `json:"plan_id"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	AthleteID      string                 `json:"athlete_id,omitempty"`
	Title          string                 `json:"title"`
	Description    *string                `json:"description,omitempty"`
	Goal           *map[string]any        `json:"goal"`
	Status         string                 `json:"status"`
	StartDate      *string                `json:"start_date,omitempty"`
	EndDate        *string                `json:"end_date,omitempty"`
	Source         string                 `json:"source"`
	SourceRef      *string                `json:"source_ref,omitempty"`
	CreatedAt      int64                  `json:"created_at,omitempty"`
	SyncSeq        int64                  `json:"sync_seq"`
	Sessions       []plannedSessionOutput `json:"sessions,omitempty"`
}
type plannedSessionOutput struct {
	ID                  string                          `json:"session_id"`
	IdempotencyKey      string                          `json:"idempotency_key,omitempty"`
	PlanID              string                          `json:"plan_id"`
	AthleteID           string                          `json:"athlete_id,omitempty"`
	Date                string                          `json:"date"`
	Title               string                          `json:"title"`
	Description         *string                         `json:"description,omitempty"`
	Sport               string                          `json:"sport"`
	SessionType         string                          `json:"session_type"`
	Priority            string                          `json:"priority"`
	Targets             fitnessrepo.PlannedTargets      `json:"targets"`
	Notes               *string                         `json:"notes,omitempty"`
	Status              string                          `json:"status"`
	CompletedActivityID *string                         `json:"completed_activity_id,omitempty"`
	CompletedWorkoutID  *string                         `json:"completed_workout_id,omitempty"`
	Completion          *fitnessrepo.ActivityCompletion `json:"completion,omitempty"`
	Steps               []plannedStepOutput             `json:"steps,omitempty"`
	Groups              []plannedStepGroupOutput        `json:"groups"`
}
type plannedStepGroupOutput struct {
	ID               string  `json:"group_id"`
	IdempotencyKey   string  `json:"idempotency_key,omitempty"`
	PlannedSessionID string  `json:"planned_session_id,omitempty"`
	Label            string  `json:"label,omitempty"`
	Mode             string  `json:"mode"`
	Rounds           int     `json:"rounds"`
	StepSequenceNos  []int   `json:"step_sequence_nos"`
	Notes            *string `json:"notes,omitempty"`
}
type plannedStepOutput struct {
	ID               string                    `json:"step_id"`
	IdempotencyKey   string                    `json:"idempotency_key,omitempty"`
	PlannedSessionID string                    `json:"planned_session_id,omitempty"`
	AthleteID        string                    `json:"athlete_id,omitempty"`
	SequenceNo       int                       `json:"sequence_no"`
	StepType         string                    `json:"step_type"`
	ExerciseKey      *string                   `json:"exercise_key,omitempty"`
	ExerciseName     *string                   `json:"exercise_name,omitempty"`
	Prescription     *fitnessrepo.Prescription `json:"prescription"`
	Notes            *string                   `json:"notes,omitempty"`
}
type planPublishOutput struct {
	Plan           planResultOutput `json:"plan"`
	ArchivedPlanID *string          `json:"archived_plan_id,omitempty"`
}

// These output types deliberately do not expose the repository's JSON storage
// types. The MCP SDK derives output schemas from Go types, and json.RawMessage
// is reflected as []byte even though it marshals as the stored JSON value.
type profileOutput struct {
	Timezone               string                                      `json:"timezone"`
	Units                  string                                      `json:"units"`
	BirthYear              *int                                        `json:"birth_year,omitempty"`
	Goals                  []fitnessrepo.ProfileGoal                   `json:"goals,omitempty"`
	TargetEvents           []fitnessrepo.TargetEvent                   `json:"target_events,omitempty"`
	WeeklyAvailability     map[string][]fitnessrepo.AvailabilityWindow `json:"weekly_availability,omitempty"`
	Preferences            fitnessrepo.Preferences                     `json:"preferences"`
	HeartRateZones         []fitnessrepo.Zone                          `json:"heart_rate_zones,omitempty"`
	PaceZones              []fitnessrepo.Zone                          `json:"pace_zones,omitempty"`
	PowerZones             []fitnessrepo.Zone                          `json:"power_zones,omitempty"`
	Constraints            map[string]any                              `json:"constraints,omitempty"`
	ConditionsAndInjuries  []fitnessrepo.Condition                     `json:"conditions_and_injuries,omitempty"`
	UpdatedAt              int64                                       `json:"updated_at"`
	SensitiveFieldsPresent bool                                        `json:"sensitive_fields_present"`
}
type profileWinnerOutput struct {
	Profile             profileOutput `json:"profile"`
	SubmittedVersionWon bool          `json:"submitted_version_won"`
}
type trainingContextOutput struct {
	AthleteID          string                        `json:"athlete_id"`
	AsOf               string                        `json:"as_of"`
	Timezone           string                        `json:"timezone"`
	GeneratedAt        string                        `json:"generated_at"`
	DataStatus         fitnessrepo.DataStatus        `json:"data_status"`
	Profile            *profileOutput                `json:"profile,omitempty"`
	NearestTargetEvent *fitnessrepo.TargetEvent      `json:"nearest_target_event,omitempty"`
	CurrentStatus      fitnessrepo.ContextStatus     `json:"current_status"`
	RecentTraining     fitnessrepo.RecentTraining    `json:"recent_training"`
	RecentActivities   []fitnessdata.ActivitySummary `json:"recent_activities,omitempty"`
	LatestCheckin      *fitnessrepo.Checkin          `json:"latest_checkin,omitempty"`
	PlannedSessions    []plannedSessionOutput        `json:"planned_sessions,omitempty"`
	Flags              []string                      `json:"flags"`
}
type timelineOutput struct {
	Days       []timelineDayOutput    `json:"days"`
	NextCursor *string                `json:"next_cursor,omitempty"`
	DataStatus fitnessrepo.DataStatus `json:"data_status"`
	Warnings   []fitnessrepo.Warning  `json:"warnings,omitempty"`
	Method     fitnessrepo.Method     `json:"method"`
}
type timelineDayOutput struct {
	Date      string                          `json:"date"`
	Planned   []plannedSessionOutput          `json:"planned"`
	Completed []fitnessrepo.TimelineCompleted `json:"completed"`
	Links     []fitnessrepo.TimelineLink      `json:"links"`
	Status    string                          `json:"status"`
	Totals    fitnessrepo.TimelineTotals      `json:"totals"`
}
type readinessOutput struct {
	Recommendation         string                          `json:"recommendation"`
	Confidence             string                          `json:"confidence"`
	Reasons                []fitnessrepo.ReadinessReason   `json:"reasons"`
	Counterpoints          []fitnessrepo.ReadinessReason   `json:"counterpoints"`
	RulesEvaluated         []string                        `json:"rules_evaluated"`
	RulesUnavailable       []string                        `json:"rules_unavailable"`
	RelevantRecentSessions []fitnessrepo.TimelineCompleted `json:"relevant_recent_sessions"`
	RecoverySignals        *fitnessrepo.RecoveryStatus     `json:"recovery_signals"`
	LoadContext            *fitnessrepo.TrainingLoadStatus `json:"load_context"`
	NearbyPlan             []plannedSessionOutput          `json:"nearby_plan"`
	Method                 fitnessrepo.Method              `json:"method"`
}
type plannedSessionWinnerOutput struct {
	Session             plannedSessionOutput `json:"session"`
	Created             bool                 `json:"created"`
	SubmittedVersionWon bool                 `json:"submitted_version_won"`
	ServerSyncedAt      int64                `json:"server_synced_at"`
}
type strengthWorkoutOutput struct {
	WorkoutID        string                         `json:"workout_id"`
	AthleteID        string                         `json:"athlete_id,omitempty"`
	PlannedSessionID *string                        `json:"planned_session_id,omitempty"`
	Date             string                         `json:"date"`
	Title            string                         `json:"title"`
	Source           string                         `json:"source"`
	WorkoutType      string                         `json:"workout_type,omitempty"`
	Status           string                         `json:"status,omitempty"`
	StartedAt        *string                        `json:"started_at,omitempty"`
	CompletedAt      *string                        `json:"completed_at,omitempty"`
	DurationMinutes  *float64                       `json:"duration_minutes,omitempty"`
	PerceivedEffort  *float64                       `json:"perceived_effort,omitempty"`
	Notes            *string                        `json:"notes,omitempty"`
	LinkedActivityID *string                        `json:"linked_activity_id,omitempty"`
	Entries          []fitnessrepo.StrengthEntry    `json:"entries,omitempty"`
	Summary          fitnessrepo.StrengthSummary    `json:"summary"`
	PlannedSession   *plannedSessionOutput          `json:"planned_session,omitempty"`
	Comparison       *fitnessrepo.WorkoutComparison `json:"comparison,omitempty"`
	SyncSeq          int64                          `json:"sync_seq,omitempty"`
}
type strengthPageOutput struct {
	Items      []strengthWorkoutOutput `json:"items"`
	NextCursor *string                 `json:"next_cursor,omitempty"`
	DataStatus fitnessrepo.DataStatus  `json:"data_status"`
}
type workoutWinnerOutput struct {
	Workout             strengthWorkoutOutput `json:"workout"`
	Created             bool                  `json:"created"`
	Deleted             bool                  `json:"deleted"`
	SubmittedVersionWon bool                  `json:"submitted_version_won"`
	ServerSyncedAt      int64                 `json:"server_synced_at"`
}
type patchWorkoutOutput struct {
	Workout  workoutWinnerOutput  `json:"workout"`
	Conflict *workoutWinnerOutput `json:"conflict,omitempty"`
}
type PublishPlanInput struct {
	TargetAthleteID string `json:"target_athlete_id,omitempty"`
	PlanID          string `json:"plan_id"`
}
type WorkoutInput struct {
	TargetAthleteID string `json:"target_athlete_id,omitempty"`
	WorkoutID       string `json:"workout_id"`
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
	s.mcp = mcp.NewServer(&mcp.Implementation{Name: "fitnessd", Version: "2.0.0", Description: "Household fitness data"}, &mcp.ServerOptions{PageSize: maxToolResults, Instructions: agentInstructions})
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_training_context", Description: "Get a bounded decision-ready snapshot for fitness guidance, not diagnosis or emergency assessment.", Annotations: readAnnotations(), InputSchema: trainingContextSchema()}, s.trainingContext)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_daily_health", Description: "Read normalized daily sleep, recovery, and wellness metrics.", Annotations: readAnnotations(), InputSchema: dailyHealthSchema()}, s.dailyHealth)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_activities", Description: "Read normalized, bounded activity summaries for training analysis.", Annotations: readAnnotations(), InputSchema: activitySchema()}, s.activities)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_activity", Description: "Read one activity with bounded splits, zones, and GPS-free metrics.", Annotations: readAnnotations(), InputSchema: activityDetailSchema()}, s.activity)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_activity_intervals", Description: "Read bounded GPS-free activity laps or structured interval steps without inferring work from pace oscillation.", Annotations: readAnnotations(), InputSchema: activityIntervalsSchema()}, s.activityIntervals)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_training_timeline", Description: "Reconcile bounded planned sessions, Garmin activities, and structured workouts into date-grouped training evidence without persisting inferred links.", Annotations: readAnnotations(), InputSchema: timelineSchema()}, s.timeline)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_readiness_for_session", Description: "Return a transparent deterministic training recommendation from bounded recovery, load, recent-session, and plan evidence; proceed is not medical safety advice.", Annotations: readAnnotations(), InputSchema: readinessSchema()}, s.readiness)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_strength_workouts", Description: "Read structured strength sessions, entries, and muscle-group summaries.", Annotations: readAnnotations(), InputSchema: strengthSchema()}, s.strength)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_exercise_progress", Description: "Read bounded progress for one reviewed exercise alias without making claims from ambiguous loads.", Annotations: readAnnotations(), InputSchema: exerciseProgressSchema()}, s.exerciseProgress)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "resolve_exercises", Description: "Resolve a batch of free-form exercise names to FitSync canonical keys before a plan or workout write. Exact aliases resolve automatically; non-exact matches are suggestions only.", Annotations: readAnnotations(), InputSchema: resolveExercisesSchema()}, s.resolveExercises)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "upsert_workout", Description: "Atomically create or replace one entire structured workout and its ordered entries. Omitted fields are cleared and omitted entries are removed. Read get_workout first for edits; prefer patch_workout for supported partial changes. Omit uncertain exercise_key values; never invent keys. Free-form exercise_name values are preserved and exact aliases are normalized. Omit new IDs to have the server generate UUIDv7 values; use idempotency_key to make retries address the same record.", Annotations: writeAnnotations(true), InputSchema: upsertWorkoutSchema()}, s.upsertWorkout)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "patch_workout", Description: "Apply a bounded reviewed operation list to one workout atomically with optimistic concurrency; omit uncertain exercise keys and never invent them. Untouched entries remain unchanged.", Annotations: writeAnnotations(true), InputSchema: patchWorkoutSchema()}, s.patchWorkout)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "delete_workout", Description: "Tombstone one workout and its entries using last-write-wins.", Annotations: writeAnnotations(true), InputSchema: deleteWorkoutSchema()}, s.deleteWorkout)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_checkins", Description: "Read subjective readiness check-ins in a bounded date range.", Annotations: readAnnotations(), InputSchema: checkinsSchema()}, s.checkins)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "record_checkin", Description: "Create or replace one subjective readiness check-in. Omit a new checkin_id to have the server generate a UUIDv7; use idempotency_key to make retries address the same record.", Annotations: writeAnnotations(true), InputSchema: recordCheckinSchema()}, s.recordCheckin)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "record_quick_readiness", Description: "Record a bounded quick readiness check-in with separate leg-freshness and pain-presence signals.", Annotations: writeAnnotations(true), InputSchema: quickReadinessSchema()}, s.recordQuickReadiness)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_athlete_profile", Description: "Read the editable athlete goals, zones, preferences, and constraints.", Annotations: readAnnotations(), InputSchema: targetOnlySchema()}, s.profile)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "update_athlete_profile", Description: "Replace the entire editable athlete profile atomically. Read it first and carry forward every field to retain; omitted fields are cleared.", Annotations: writeAnnotations(true), InputSchema: profileSchema()}, s.updateProfile)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_training_plan", Description: "Read the current training plan in an inclusive date range (maximum 366 days), or a specific plan_id including drafts and archives. With plan_id, dates are optional; when supplied they filter sessions. include_steps and include_completed default to true in both modes.", Annotations: readAnnotations(), InputSchema: planSchema()}, s.plan)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "upsert_training_plan_draft", Description: "Atomically create or replace one complete draft training plan. Omitted sessions, steps, and groups are removed; read get_training_plan by plan_id before editing and carry forward everything to retain. Omit uncertain exercise_key values; never invent keys. Free-form exercise_name values are preserved and exact aliases are normalized. Use groups for all execution-critical supersets and circuits; do not encode them only in notes. A two-member rounds group is a superset and a larger group is a circuit. Each group member is performed in declared order for every round, and each member's rest_seconds is recovery after that member (normally 0 for non-final members and the intended recovery after the final member). Group member set counts must equal rounds. Omit plan_id, session_id, step_id, and group_id to have the server generate UUIDv7 values; use each record's idempotency_key to make retries address the same record.", Annotations: writeAnnotations(true), InputSchema: planDraftSchema()}, s.upsertPlanDraft)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "publish_training_plan", Description: "Publish a draft training plan and archive the previous active plan.", Annotations: writeAnnotations(true), InputSchema: publishPlanSchema()}, s.publishPlan)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_workout", Description: "Read one authorized workout with prescribed-versus-actual details.", Annotations: readAnnotations(), InputSchema: workoutSchema()}, s.workout)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "upsert_planned_session", Description: "Create or replace one planned training session. Supply the existing plan_id when adding to a plan; omitting it creates a separate active manual plan. For complete structured plans prefer upsert_training_plan_draft and publish_training_plan. Omit new plan_id or session_id values to have the server generate UUIDv7 values; use idempotency keys for retries.", Annotations: writeAnnotations(true), InputSchema: plannedSessionSchema()}, s.upsertPlannedSession)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "update_planned_session_status", Description: "Update planned-session status and owned completion links.", Annotations: writeAnnotations(true), InputSchema: plannedStatusSchema()}, s.updatePlannedStatus)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "get_training_analysis", Description: "Calculate bounded deterministic period and comparison aggregates.", Annotations: readAnnotations(), InputSchema: analysisSchema()}, s.analysis)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "update_workout", Description: "Deprecated low-level sync mutation; use upsert_workout or delete_workout.", Annotations: writeAnnotations(true), InputSchema: legacyUpdateSchema()}, s.updateWorkout)
	s.mcp.AddReceivingMiddleware(logToolCalls)
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

func (s *Server) trainingContext(ctx context.Context, _ *mcp.CallToolRequest, in TrainingContextInput) (*mcp.CallToolResult, trainingContextOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, trainingContextOutput{}, e
	}
	lookback := in.LookbackDays
	if lookback == 0 {
		lookback = 28
	}
	value, err := s.repo.TrainingContext(ctx, id, in.AsOf, lookback, in.Include)
	if err != nil {
		return nil, trainingContextOutput{}, err
	}
	output, err := toTrainingContextOutput(value)
	return nil, output, err
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
func (s *Server) activityIntervals(ctx context.Context, _ *mcp.CallToolRequest, in ActivityIntervalsInput) (*mcp.CallToolResult, fitnessrepo.ActivityIntervalsResult, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.ActivityIntervalsResult{}, e
	}
	value, err := s.repo.ActivityIntervals(ctx, id, fitnessrepo.ActivityIntervalsQuery{ActivityID: in.ActivityID, IncludeLaps: in.IncludeLaps, IncludeRecoveries: in.IncludeRecoveries, MaxSegments: in.MaxSegments})
	return nil, value, err
}
func (s *Server) timeline(ctx context.Context, _ *mcp.CallToolRequest, in TimelineInput) (*mcp.CallToolResult, timelineOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, timelineOutput{}, e
	}
	value, err := s.repo.TrainingTimeline(ctx, id, fitnessrepo.TimelineQuery{From: in.From, To: in.To, IncludeUnplanned: in.IncludeUnplanned, IncludeEntries: in.IncludeEntries, Order: in.Order, Limit: in.Limit, Cursor: in.Cursor})
	if err != nil {
		return nil, timelineOutput{}, err
	}
	output, err := toTimelineOutput(value)
	return nil, output, err
}
func (s *Server) readiness(ctx context.Context, _ *mcp.CallToolRequest, in ReadinessInput) (*mcp.CallToolResult, readinessOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, readinessOutput{}, e
	}
	if in.AsOf == "" {
		in.AsOf = s.clock().Format(time.RFC3339)
	}
	asOf, parseErr := time.Parse(time.RFC3339, in.AsOf)
	if parseErr != nil || asOf.After(s.clock().Add(5*time.Minute)) {
		return nil, readinessOutput{}, errors.New("invalid_input: as_of must be RFC3339 and no more than five minutes in the future")
	}
	value, err := s.repo.ReadinessForSession(ctx, id, fitnessrepo.ReadinessQuery{AsOf: in.AsOf, SessionDate: in.SessionDate, ProposedSessionType: in.ProposedSessionType, ProposedDurationMinutes: in.ProposedDurationMinutes, ProposedIntensity: in.ProposedIntensity})
	if err != nil {
		return nil, readinessOutput{}, err
	}
	output, err := toReadinessOutput(value)
	return nil, output, err
}
func (s *Server) strength(ctx context.Context, _ *mcp.CallToolRequest, in StrengthInput) (*mcp.CallToolResult, strengthPageOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, strengthPageOutput{}, e
	}
	value, err := s.repo.StrengthWorkouts(ctx, id, fitnessrepo.StrengthQuery{From: in.From, To: in.To, ExerciseKeys: in.ExerciseKeys, MuscleGroups: in.MuscleGroups, IncludeEntries: in.IncludeEntries, Order: in.Order, Limit: in.Limit, Cursor: in.Cursor})
	if err != nil {
		return nil, strengthPageOutput{}, err
	}
	output, err := toStrengthPageOutput(value)
	return nil, output, err
}

func (s *Server) exerciseProgress(ctx context.Context, _ *mcp.CallToolRequest, in ExerciseProgressInput) (*mcp.CallToolResult, fitnessrepo.ExerciseProgress, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, fitnessrepo.ExerciseProgress{}, e
	}
	if in.ExerciseKey == "" || len(in.ExerciseKey) > 128 {
		return nil, fitnessrepo.ExerciseProgress{}, errors.New("invalid_input: exercise_key")
	}
	value, err := s.repo.ExerciseProgress(ctx, id, fitnessrepo.ExerciseProgressQuery{ExerciseKey: in.ExerciseKey, From: in.From, To: in.To, IncludeWarmups: in.IncludeWarmups, Limit: in.Limit, Cursor: in.Cursor})
	return nil, value, err
}

func (s *Server) resolveExercises(ctx context.Context, _ *mcp.CallToolRequest, in ResolveExercisesInput) (*mcp.CallToolResult, ResolveExercisesOutput, error) {
	if _, _, err := s.principal(ctx, in.TargetAthleteID, "fitness:read"); err != nil {
		return nil, ResolveExercisesOutput{}, err
	}
	if len(in.Exercises) < 1 || len(in.Exercises) > 100 {
		return nil, ResolveExercisesOutput{}, errors.New("invalid_input: exercises must contain 1 to 100 items")
	}
	limit := in.MaxCandidates
	if limit == 0 {
		limit = 3
	}
	if limit < 1 || limit > 10 {
		return nil, ResolveExercisesOutput{}, errors.New("invalid_input: max_candidates must be between 1 and 10")
	}
	seen := map[string]bool{}
	output := ResolveExercisesOutput{CatalogRevision: exercisecatalog.CatalogRevision, Results: make([]ResolveExerciseResult, 0, len(in.Exercises))}
	for _, input := range in.Exercises {
		if strings.TrimSpace(input.Ref) == "" {
			return nil, ResolveExercisesOutput{}, errors.New("invalid_input: exercise refs must be non-empty")
		}
		if seen[input.Ref] {
			return nil, ResolveExercisesOutput{}, errors.New("invalid_input: exercise refs must be unique")
		}
		seen[input.Ref] = true
		if strings.TrimSpace(input.Name) == "" {
			return nil, ResolveExercisesOutput{}, errors.New("invalid_input: exercise names must be non-empty")
		}
		resolution, err := s.repo.ResolveExerciseIdentity(nil, &input.Name)
		if err != nil {
			return nil, ResolveExercisesOutput{}, err
		}
		result := ResolveExerciseResult{Ref: input.Ref, Name: input.Name, Status: "unknown", ExerciseKey: resolution.Key, CanonicalName: resolution.CanonicalName, MatchedAlias: resolution.MatchedAlias, Candidates: []ExerciseCandidateOutput{}}
		if resolution.Key != nil {
			result.Status = "exact_alias"
		} else if candidates := exerciseCandidates(input.Name, limit); len(candidates) > 0 {
			result.Status = "suggestions"
			result.Candidates = candidates
		}
		output.Results = append(output.Results, result)
	}
	return nil, output, nil
}

func exerciseCandidates(name string, limit int) []ExerciseCandidateOutput {
	results := exercisecatalog.Search(name, limit)
	output := make([]ExerciseCandidateOutput, 0, len(results))
	for _, result := range results {
		output = append(output, ExerciseCandidateOutput{ExerciseKey: result.Key, CanonicalName: result.CanonicalName, Description: result.Description, MatchedAlias: result.MatchedAlias, Score: result.Score})
	}
	return output
}

type normalizationInput struct {
	Ref  string
	Key  *string
	Name *string
}

func (s *Server) normalizationReport(inputs []normalizationInput) (normalizationReport, error) {
	report := normalizationReport{CatalogRevision: exercisecatalog.CatalogRevision, Items: []normalizationItem{}}
	for _, input := range inputs {
		if input.Key == nil && input.Name == nil {
			continue
		}
		resolution, err := s.repo.ResolveExerciseIdentity(input.Key, input.Name)
		if err != nil {
			return normalizationReport{}, err
		}
		switch resolution.Status {
		case exercisecatalog.StatusExplicit:
			report.Explicit++
		case exercisecatalog.StatusExactAlias:
			report.ResolvedFromName++
		case exercisecatalog.StatusDeprecatedKey:
			report.DeprecatedKeysRewritten++
		case exercisecatalog.StatusUnknown:
			report.Unknown++
		}
		candidates := []ExerciseCandidateOutput{}
		if resolution.Status == exercisecatalog.StatusUnknown && input.Name != nil {
			candidates = exerciseCandidates(*input.Name, 3)
		}
		if resolution.Status != exercisecatalog.StatusDeprecatedKey && resolution.Status != exercisecatalog.StatusUnknown && len(candidates) == 0 {
			continue
		}
		item := normalizationItem{Ref: input.Ref, Status: string(resolution.Status), ExerciseKey: resolution.Key, Candidates: candidates}
		if input.Name != nil {
			item.ExerciseName = *input.Name
		}
		item.OriginalKey = resolution.OriginalKey
		report.Items = append(report.Items, item)
	}
	return report, nil
}

func (s *Server) upsertWorkout(ctx context.Context, _ *mcp.CallToolRequest, in UpsertWorkoutInput) (*mcp.CallToolResult, upsertWorkoutOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, upsertWorkoutOutput{}, e
	}
	in.WorkoutID, e = s.resolveCreateID(ctx, id, in.WorkoutID, in.IdempotencyKey, s.repo.WorkoutIDForIdempotencyKey)
	if e != nil {
		return nil, upsertWorkoutOutput{}, e
	}
	if in.ClientUpdatedAt < 0 || in.Date == "" || in.Title == "" || in.WorkoutType == "" || in.Source == "" || len(in.Entries) > 200 {
		return nil, upsertWorkoutOutput{}, errors.New("invalid_input: workout fields or entry bounds are invalid")
	}
	var started *time.Time
	if in.StartedAt != "" {
		v, err := time.Parse(time.RFC3339, in.StartedAt)
		if err != nil {
			return nil, upsertWorkoutOutput{}, errors.New("invalid_input: started_at must be RFC3339")
		}
		started = &v
	}
	entries := make([]fitnessrepo.StrengthEntry, 0, len(in.Entries))
	seen := map[int]bool{}
	for _, x := range in.Entries {
		entryID, resolveErr := s.resolveCreateID(ctx, id, x.EntryID, x.IdempotencyKey, s.repo.WorkoutEntryIDForIdempotencyKey)
		if resolveErr != nil {
			return nil, upsertWorkoutOutput{}, resolveErr
		}
		if x.SequenceNo < 1 || seen[x.SequenceNo] || (x.EntryType != "set" && x.EntryType != "rest" && x.EntryType != "note") {
			return nil, upsertWorkoutOutput{}, errors.New("invalid_input: entries are invalid")
		}
		seen[x.SequenceNo] = true
		entries = append(entries, fitnessrepo.StrengthEntry{EntryID: entryID, IdempotencyKey: x.IdempotencyKey, SequenceNo: x.SequenceNo, EntryType: x.EntryType, ExerciseKey: x.ExerciseKey, ExerciseName: x.ExerciseName, UserLabel: x.UserLabel, SetNumber: x.SetNumber, Reps: x.Reps, DurationSeconds: x.DurationSeconds, LoadKG: x.LoadKG, LoadDescription: x.LoadDescription, IsBodyweight: x.IsBodyweight, ToFailure: x.ToFailure, RIR: x.RIR, RIRMin: x.RIRMin, RIRMax: x.RIRMax, Laterality: x.Laterality, RepsPerSide: x.RepsPerSide, LoadBasis: x.LoadBasis, LoadPerLimbKG: x.LoadPerLimbKG, AddedLoadKG: x.AddedLoadKG, TotalExternalLoadKG: x.TotalExternalLoadKG, MuscleGroups: x.MuscleGroups, DirectMuscleGroups: x.DirectMuscleGroups, IndirectMuscleGroups: x.IndirectMuscleGroups, Notes: x.Notes, Completed: x.Completed, IsWarmup: x.IsWarmup})
	}
	if in.LinkedActivityID != nil && !s.repo.OwnedActivity(ctx, id, *in.LinkedActivityID) {
		return nil, upsertWorkoutOutput{}, errors.New("not_found: linked activity")
	}
	normalizationInputs := make([]normalizationInput, 0, len(entries))
	for _, entry := range entries {
		normalizationInputs = append(normalizationInputs, normalizationInput{Ref: entry.EntryID, Key: entry.ExerciseKey, Name: entry.ExerciseName})
	}
	normalization, reportErr := s.normalizationReport(normalizationInputs)
	if reportErr != nil {
		return nil, upsertWorkoutOutput{}, reportErr
	}
	value, err := s.repo.UpsertWorkout(ctx, id, fitnessrepo.WorkoutWrite{ID: in.WorkoutID, IdempotencyKey: in.IdempotencyKey, ClientUpdatedAt: in.ClientUpdatedAt, Date: in.Date, Title: in.Title, WorkoutType: in.WorkoutType, Source: in.Source, StartedAt: started, DurationMinutes: in.DurationMinutes, PerceivedEffort: in.PerceivedEffort, Notes: in.Notes, LinkedActivityID: in.LinkedActivityID, Entries: entries})
	if err != nil {
		return nil, upsertWorkoutOutput{}, err
	}
	workout, err := toWorkoutWinnerOutput(value)
	if err != nil {
		return nil, upsertWorkoutOutput{}, err
	}
	return nil, upsertWorkoutOutput{Workout: workout.Workout, Created: workout.Created, Deleted: workout.Deleted, SubmittedVersionWon: workout.SubmittedVersionWon, ServerSyncedAt: workout.ServerSyncedAt, Normalization: normalization}, nil
}
func (s *Server) deleteWorkout(ctx context.Context, _ *mcp.CallToolRequest, in DeleteWorkoutInput) (*mcp.CallToolResult, workoutWinnerOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, workoutWinnerOutput{}, e
	}
	value, err := s.repo.DeleteWorkout(ctx, id, in.WorkoutID, in.ClientUpdatedAt)
	if err != nil {
		return nil, workoutWinnerOutput{}, err
	}
	output, err := toWorkoutWinnerOutput(value)
	return nil, output, err
}
func (s *Server) patchWorkout(ctx context.Context, _ *mcp.CallToolRequest, in PatchWorkoutInput) (*mcp.CallToolResult, patchWorkoutOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, patchWorkoutOutput{}, e
	}
	if in.WorkoutID == "" || in.ExpectedSyncSeq < 0 || in.ClientUpdatedAt < 0 || len(in.Operations) < 1 || len(in.Operations) > 50 {
		return nil, patchWorkoutOutput{}, errors.New("invalid_input: patch workout bounds")
	}
	operations := make([]fitnessrepo.WorkoutPatchOperation, 0, len(in.Operations))
	for _, operation := range in.Operations {
		operations = append(operations, fitnessrepo.WorkoutPatchOperation{Op: operation.Op, ActivityID: operation.ActivityID, EntryID: operation.EntryID, RIR: operation.RIR, ExerciseKey: operation.ExerciseKey, Laterality: operation.Laterality, RepsPerSide: operation.RepsPerSide, LoadBasis: operation.LoadBasis, LoadPerLimbKG: operation.LoadPerLimbKG, TotalExternalLoadKG: operation.TotalExternalLoadKG, AddedLoadKG: operation.AddedLoadKG, DirectMuscleGroups: operation.DirectMuscleGroups, IndirectMuscleGroups: operation.IndirectMuscleGroups, Text: operation.Text})
	}
	value, err := s.repo.PatchWorkout(ctx, id, in.WorkoutID, in.ExpectedSyncSeq, in.ClientUpdatedAt, operations)
	if err != nil {
		return nil, patchWorkoutOutput{}, err
	}
	output, err := toPatchWorkoutOutput(value)
	return nil, output, err
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
	in.CheckinID, e = s.resolveCreateID(ctx, id, in.CheckinID, in.IdempotencyKey, s.repo.CheckinIDForIdempotencyKey)
	if e != nil {
		return nil, fitnessrepo.CheckinWinner{}, e
	}
	at, e := time.Parse(time.RFC3339, in.RecordedAt)
	if e != nil || at.After(s.clock().Add(24*time.Hour)) {
		return nil, fitnessrepo.CheckinWinner{}, errors.New("invalid_input: recorded_at must be RFC3339 and not more than 24 hours ahead")
	}
	if in.ClientUpdatedAt < 0 {
		return nil, fitnessrepo.CheckinWinner{}, errors.New("invalid_input: check-in identity or clock is invalid")
	}
	if in.Energy == nil && in.Fatigue == nil && in.Motivation == nil && in.SleepQuality == nil && in.Stress == nil && len(in.Soreness) == 0 && len(in.Pain) == 0 && !in.Illness.Present && len(in.Illness.Symptoms) == 0 && in.Notes == nil {
		return nil, fitnessrepo.CheckinWinner{}, errors.New("invalid_input: at least one check-in field is required")
	}
	value, err := s.repo.UpsertCheckin(ctx, id, fitnessrepo.CheckinWrite{ID: in.CheckinID, IdempotencyKey: in.IdempotencyKey, ClientUpdatedAt: in.ClientUpdatedAt, RecordedAt: at, Energy: in.Energy, Fatigue: in.Fatigue, Motivation: in.Motivation, SleepQuality: in.SleepQuality, Stress: in.Stress, Soreness: in.Soreness, Pain: in.Pain, Illness: in.Illness, Notes: in.Notes})
	return nil, value, err
}
func (s *Server) recordQuickReadiness(ctx context.Context, _ *mcp.CallToolRequest, in QuickReadinessInput) (*mcp.CallToolResult, fitnessrepo.CheckinWinner, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, fitnessrepo.CheckinWinner{}, e
	}
	in.CheckinID, e = s.resolveCreateID(ctx, id, in.CheckinID, in.IdempotencyKey, s.repo.CheckinIDForIdempotencyKey)
	if e != nil {
		return nil, fitnessrepo.CheckinWinner{}, e
	}
	at, e := time.Parse(time.RFC3339, in.RecordedAt)
	if e != nil || at.After(s.clock().Add(24*time.Hour)) {
		return nil, fitnessrepo.CheckinWinner{}, errors.New("invalid_input: recorded_at must be RFC3339 and not more than 24 hours ahead")
	}
	painPresent := in.PainPresent
	value, err := s.repo.RecordQuickReadiness(ctx, id, fitnessrepo.CheckinWrite{ID: in.CheckinID, IdempotencyKey: in.IdempotencyKey, ClientUpdatedAt: in.ClientUpdatedAt, RecordedAt: at, Energy: &in.Energy, LegFreshness: &in.LegFreshness, SleepQuality: &in.SleepQuality, Pain: in.Pain, PainPresent: &painPresent})
	return nil, value, err
}
func (s *Server) profile(ctx context.Context, _ *mcp.CallToolRequest, in targetInput) (*mcp.CallToolResult, profileOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, profileOutput{}, e
	}
	value, err := s.repo.Profile(ctx, id)
	if err != nil {
		return nil, profileOutput{}, err
	}
	output, err := toProfileOutput(value)
	return nil, output, err
}
func (s *Server) updateProfile(ctx context.Context, _ *mcp.CallToolRequest, in ProfileInput) (*mcp.CallToolResult, profileWinnerOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, profileWinnerOutput{}, e
	}
	value, err := s.repo.UpsertProfile(ctx, id, fitnessrepo.ProfileWrite{ClientUpdatedAt: in.ClientUpdatedAt, Profile: fitnessrepo.Profile{Timezone: in.Timezone, Units: in.Units, BirthYear: in.BirthYear, Goals: in.Goals, TargetEvents: in.TargetEvents, WeeklyAvailability: in.WeeklyAvailability, Preferences: in.Preferences, HeartRateZones: in.HeartRateZones, PaceZones: in.PaceZones, PowerZones: in.PowerZones, Constraints: in.Constraints, ConditionsAndInjuries: in.ConditionsAndInjuries}})
	if err != nil {
		return nil, profileWinnerOutput{}, err
	}
	output, err := toProfileWinnerOutput(value)
	return nil, output, err
}
func (s *Server) plan(ctx context.Context, _ *mcp.CallToolRequest, in PlanInput) (*mcp.CallToolResult, planResultOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if e != nil {
		return nil, planResultOutput{}, e
	}
	var value fitnessrepo.PlanResult
	var err error
	if in.PlanID != "" {
		value, err = s.repo.PlanByID(ctx, id, in.PlanID, true)
	} else {
		value, err = s.repo.Plan(ctx, id, in.From, in.To, true)
	}
	if err != nil {
		return nil, planResultOutput{}, err
	}
	value, err = filterPlanResult(value, in)
	if err != nil {
		return nil, planResultOutput{}, err
	}
	output, err := toPlanResultOutput(value)
	return nil, output, err
}

func (s *Server) upsertPlanDraft(ctx context.Context, _ *mcp.CallToolRequest, in PlanDraftInput) (*mcp.CallToolResult, PlanDraftOutput, error) {
	_, id, err := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if err != nil {
		return nil, PlanDraftOutput{}, err
	}
	in.PlanID, err = s.resolveCreateID(ctx, id, in.PlanID, in.IdempotencyKey, s.repo.PlanIDForIdempotencyKey)
	if err != nil {
		return nil, PlanDraftOutput{}, err
	}
	sessions := make([]fitnessrepo.PlannedSessionWrite, 0, len(in.Sessions))
	normalizationInputs := make([]normalizationInput, 0)
	steps := 0
	groups := 0
	for _, input := range in.Sessions {
		sessionID, resolveErr := s.resolveCreateID(ctx, id, input.SessionID, input.IdempotencyKey, s.repo.PlannedSessionIDForIdempotencyKey)
		if resolveErr != nil {
			return nil, PlanDraftOutput{}, resolveErr
		}
		inputSteps := make([]fitnessrepo.PlannedStep, 0, len(input.Steps))
		for _, step := range input.Steps {
			stepID, resolveErr := s.resolveCreateID(ctx, id, step.ID, step.IdempotencyKey, s.repo.PlannedStepIDForIdempotencyKey)
			if resolveErr != nil {
				return nil, PlanDraftOutput{}, resolveErr
			}
			step.ID = stepID
			normalizationInputs = append(normalizationInputs, normalizationInput{Ref: sessionID + ":" + stepID, Key: step.ExerciseKey, Name: step.ExerciseName})
			inputSteps = append(inputSteps, step)
		}
		inputGroups := make([]fitnessrepo.PlannedStepGroup, 0, len(input.Groups))
		for _, group := range input.Groups {
			groupID, resolveErr := s.resolveCreateID(ctx, id, group.ID, group.IdempotencyKey, s.repo.PlannedStepGroupIDForIdempotencyKey)
			if resolveErr != nil {
				return nil, PlanDraftOutput{}, resolveErr
			}
			inputGroups = append(inputGroups, fitnessrepo.PlannedStepGroup{ID: groupID, IdempotencyKey: group.IdempotencyKey, PlannedSessionID: sessionID, Label: group.Label, Mode: group.Mode, Rounds: group.Rounds, StepSequenceNos: group.StepSequenceNos, Notes: group.Notes})
		}
		sessions = append(sessions, fitnessrepo.PlannedSessionWrite{ID: sessionID, IdempotencyKey: input.IdempotencyKey, PlanID: in.PlanID, Date: input.Date, Title: input.Title, Description: derefString(input.Description), Sport: input.Sport, SessionType: input.SessionType, Priority: input.Priority, Targets: input.Targets, Notes: input.Notes, Steps: inputSteps, Groups: inputGroups})
		steps += len(input.Steps)
		groups += len(input.Groups)
	}
	normalization, reportErr := s.normalizationReport(normalizationInputs)
	if reportErr != nil {
		return nil, PlanDraftOutput{}, reportErr
	}
	value, err := s.repo.UpsertPlanDraft(ctx, id, fitnessrepo.PlanWrite{Plan: fitnessrepo.TrainingPlan{ID: in.PlanID, IdempotencyKey: in.IdempotencyKey, Title: in.Title, Description: in.Description, Goal: in.Goal, StartDate: in.StartDate, EndDate: in.EndDate, SourceRef: in.SourceRef}, Sessions: sessions})
	if err != nil {
		return nil, PlanDraftOutput{}, err
	}
	seq := int64(0)
	if value.Plan != nil {
		seq = value.Plan.SyncSeq
	}
	output, err := toPlanResultOutput(value)
	if err != nil {
		return nil, PlanDraftOutput{}, err
	}
	return nil, PlanDraftOutput{Plan: output, SessionCount: len(value.Sessions), StepCount: steps, GroupCount: groups, SyncSeq: seq, Normalization: normalization}, nil
}

func (s *Server) publishPlan(ctx context.Context, _ *mcp.CallToolRequest, in PublishPlanInput) (*mcp.CallToolResult, planPublishOutput, error) {
	_, id, err := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if err != nil {
		return nil, planPublishOutput{}, err
	}
	value, err := s.repo.PublishPlan(ctx, id, in.PlanID)
	if err != nil {
		return nil, planPublishOutput{}, err
	}
	output, err := toPlanResultOutput(value.Plan)
	if err != nil {
		return nil, planPublishOutput{}, err
	}
	return nil, planPublishOutput{Plan: output, ArchivedPlanID: value.ArchivedPlanID}, nil
}

func (s *Server) workout(ctx context.Context, _ *mcp.CallToolRequest, in WorkoutInput) (*mcp.CallToolResult, strengthWorkoutOutput, error) {
	_, id, err := s.principal(ctx, in.TargetAthleteID, "fitness:read")
	if err != nil {
		return nil, strengthWorkoutOutput{}, err
	}
	value, err := s.repo.Workout(ctx, id, in.WorkoutID)
	if err != nil {
		return nil, strengthWorkoutOutput{}, err
	}
	output, err := toStrengthWorkoutOutput(value)
	return nil, output, err
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type idempotencyLookup func(context.Context, string, string) (string, error)

func newUUIDv7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", errors.New("internal_error: could not generate UUIDv7")
	}
	return id.String(), nil
}

func (s *Server) resolveCreateID(ctx context.Context, athleteID, requestedID, key string, lookup idempotencyLookup) (string, error) {
	if requestedID != "" {
		return requestedID, nil
	}
	if key != "" {
		if existing, err := lookup(ctx, athleteID, key); err != nil {
			return "", err
		} else if existing != "" {
			return existing, nil
		}
	}
	return newUUIDv7()
}

func toProfileOutput(value fitnessrepo.Profile) (profileOutput, error) {
	constraints, err := jsonObjectMap(value.Constraints, "profile constraints")
	if err != nil {
		return profileOutput{}, err
	}
	return profileOutput{
		Timezone: value.Timezone, Units: value.Units, BirthYear: value.BirthYear,
		Goals: value.Goals, TargetEvents: value.TargetEvents, WeeklyAvailability: value.WeeklyAvailability,
		Preferences: value.Preferences, HeartRateZones: value.HeartRateZones, PaceZones: value.PaceZones,
		PowerZones: value.PowerZones, Constraints: constraints, ConditionsAndInjuries: value.ConditionsAndInjuries,
		UpdatedAt: value.UpdatedAt, SensitiveFieldsPresent: value.SensitiveFieldsPresent,
	}, nil
}

func toProfileWinnerOutput(value fitnessrepo.ProfileWinner) (profileWinnerOutput, error) {
	profile, err := toProfileOutput(value.Profile)
	if err != nil {
		return profileWinnerOutput{}, err
	}
	return profileWinnerOutput{Profile: profile, SubmittedVersionWon: value.SubmittedVersionWon}, nil
}

func toTrainingContextOutput(value fitnessrepo.TrainingContext) (trainingContextOutput, error) {
	output := trainingContextOutput{
		AthleteID: value.AthleteID, AsOf: value.AsOf, Timezone: value.Timezone, GeneratedAt: value.GeneratedAt,
		DataStatus: value.DataStatus, NearestTargetEvent: value.NearestTargetEvent, CurrentStatus: value.CurrentStatus,
		RecentTraining: value.RecentTraining, RecentActivities: value.RecentActivities, LatestCheckin: value.LatestCheckin,
		Flags: value.Flags, PlannedSessions: make([]plannedSessionOutput, 0, len(value.PlannedSessions)),
	}
	if value.Profile != nil {
		profile, err := toProfileOutput(*value.Profile)
		if err != nil {
			return trainingContextOutput{}, err
		}
		output.Profile = &profile
	}
	for _, session := range value.PlannedSessions {
		converted, err := toPlannedSessionOutput(session)
		if err != nil {
			return trainingContextOutput{}, err
		}
		output.PlannedSessions = append(output.PlannedSessions, converted)
	}
	return output, nil
}

func toTimelineOutput(value fitnessrepo.TimelineResult) (timelineOutput, error) {
	output := timelineOutput{
		Days: make([]timelineDayOutput, 0, len(value.Days)), NextCursor: value.NextCursor,
		DataStatus: value.DataStatus, Warnings: value.Warnings, Method: value.Method,
	}
	for _, day := range value.Days {
		converted := timelineDayOutput{
			Date: day.Date, Completed: day.Completed, Links: day.Links, Status: day.Status, Totals: day.Totals,
			Planned: make([]plannedSessionOutput, 0, len(day.Planned)),
		}
		for _, session := range day.Planned {
			planned, err := toPlannedSessionOutput(session)
			if err != nil {
				return timelineOutput{}, err
			}
			converted.Planned = append(converted.Planned, planned)
		}
		output.Days = append(output.Days, converted)
	}
	return output, nil
}

func toReadinessOutput(value fitnessrepo.ReadinessResult) (readinessOutput, error) {
	output := readinessOutput{
		Recommendation: value.Recommendation, Confidence: value.Confidence, Reasons: value.Reasons,
		Counterpoints: value.Counterpoints, RulesEvaluated: value.RulesEvaluated, RulesUnavailable: value.RulesUnavailable,
		RelevantRecentSessions: value.RelevantRecentSessions, RecoverySignals: value.RecoverySignals, LoadContext: value.LoadContext,
		Method: value.Method, NearbyPlan: make([]plannedSessionOutput, 0, len(value.NearbyPlan)),
	}
	for _, session := range value.NearbyPlan {
		converted, err := toPlannedSessionOutput(session)
		if err != nil {
			return readinessOutput{}, err
		}
		output.NearbyPlan = append(output.NearbyPlan, converted)
	}
	return output, nil
}

func toPlannedSessionWinnerOutput(value fitnessrepo.PlannedSessionWinner) (plannedSessionWinnerOutput, error) {
	session, err := toPlannedSessionOutput(value.Session)
	if err != nil {
		return plannedSessionWinnerOutput{}, err
	}
	return plannedSessionWinnerOutput{Session: session, Created: value.Created, SubmittedVersionWon: value.SubmittedVersionWon, ServerSyncedAt: value.ServerSyncedAt}, nil
}

func toStrengthWorkoutOutput(value fitnessrepo.StrengthWorkout) (strengthWorkoutOutput, error) {
	output := strengthWorkoutOutput{
		WorkoutID: value.WorkoutID, AthleteID: value.AthleteID, PlannedSessionID: value.PlannedSessionID,
		Date: value.Date, Title: value.Title, Source: value.Source, WorkoutType: value.WorkoutType, Status: value.Status,
		StartedAt: value.StartedAt, CompletedAt: value.CompletedAt, DurationMinutes: value.DurationMinutes,
		PerceivedEffort: value.PerceivedEffort, Notes: value.Notes, LinkedActivityID: value.LinkedActivityID,
		Entries: value.Entries, Summary: value.Summary, Comparison: value.Comparison, SyncSeq: value.SyncSeq,
	}
	if value.PlannedSession != nil {
		planned, err := toPlannedSessionOutput(*value.PlannedSession)
		if err != nil {
			return strengthWorkoutOutput{}, err
		}
		output.PlannedSession = &planned
	}
	return output, nil
}

func toStrengthPageOutput(value fitnessrepo.Page[fitnessrepo.StrengthWorkout]) (strengthPageOutput, error) {
	output := strengthPageOutput{Items: make([]strengthWorkoutOutput, 0, len(value.Items)), NextCursor: value.NextCursor, DataStatus: value.DataStatus}
	for _, workout := range value.Items {
		converted, err := toStrengthWorkoutOutput(workout)
		if err != nil {
			return strengthPageOutput{}, err
		}
		output.Items = append(output.Items, converted)
	}
	return output, nil
}

func toWorkoutWinnerOutput(value fitnessrepo.WorkoutWinner) (workoutWinnerOutput, error) {
	workout, err := toStrengthWorkoutOutput(value.Workout)
	if err != nil {
		return workoutWinnerOutput{}, err
	}
	return workoutWinnerOutput{Workout: workout, Created: value.Created, Deleted: value.Deleted, SubmittedVersionWon: value.SubmittedVersionWon, ServerSyncedAt: value.ServerSyncedAt}, nil
}

func toPatchWorkoutOutput(value fitnessrepo.PatchWorkoutResult) (patchWorkoutOutput, error) {
	workout, err := toWorkoutWinnerOutput(value.Workout)
	if err != nil {
		return patchWorkoutOutput{}, err
	}
	output := patchWorkoutOutput{Workout: workout}
	if value.Conflict != nil {
		conflict, err := toWorkoutWinnerOutput(*value.Conflict)
		if err != nil {
			return patchWorkoutOutput{}, err
		}
		output.Conflict = &conflict
	}
	return output, nil
}

func jsonObjectMap(raw map[string]json.RawMessage, label string) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	value := make(map[string]any, len(raw))
	for key, item := range raw {
		var decoded any
		if err := json.Unmarshal(item, &decoded); err != nil {
			return nil, fmt.Errorf("internal_error: %s contains invalid JSON", label)
		}
		value[key] = decoded
	}
	return value, nil
}

func toPlanResultOutput(value fitnessrepo.PlanResult) (planResultOutput, error) {
	output := planResultOutput{Sessions: make([]plannedSessionOutput, 0, len(value.Sessions))}
	for _, session := range value.Sessions {
		converted, err := toPlannedSessionOutput(session)
		if err != nil {
			return planResultOutput{}, err
		}
		output.Sessions = append(output.Sessions, converted)
	}
	if value.Plan == nil {
		return output, nil
	}
	goal, err := jsonObject(value.Plan.Goal, "plan goal")
	if err != nil {
		return planResultOutput{}, err
	}
	plan := &trainingPlanOutput{ID: value.Plan.ID, IdempotencyKey: value.Plan.IdempotencyKey, AthleteID: value.Plan.AthleteID, Title: value.Plan.Title, Description: value.Plan.Description, Goal: &goal, Status: value.Plan.Status, StartDate: value.Plan.StartDate, EndDate: value.Plan.EndDate, Source: value.Plan.Source, SourceRef: value.Plan.SourceRef, CreatedAt: value.Plan.CreatedAt, SyncSeq: value.Plan.SyncSeq}
	if value.Plan.Sessions != nil {
		plan.Sessions = output.Sessions
	}
	output.Plan = plan
	return output, nil
}

func toPlannedSessionOutput(value fitnessrepo.PlannedSession) (plannedSessionOutput, error) {
	output := plannedSessionOutput{ID: value.ID, IdempotencyKey: value.IdempotencyKey, PlanID: value.PlanID, AthleteID: value.AthleteID, Date: value.Date, Title: value.Title, Description: value.Description, Sport: value.Sport, SessionType: value.SessionType, Priority: value.Priority, Targets: value.Targets, Notes: value.Notes, Status: value.Status, CompletedActivityID: value.CompletedActivityID, CompletedWorkoutID: value.CompletedWorkoutID, Completion: value.Completion, Steps: make([]plannedStepOutput, 0, len(value.Steps))}
	for _, step := range value.Steps {
		prescription, err := jsonPrescription(step.Prescription)
		if err != nil {
			return plannedSessionOutput{}, err
		}
		output.Steps = append(output.Steps, plannedStepOutput{ID: step.ID, IdempotencyKey: step.IdempotencyKey, PlannedSessionID: step.PlannedSessionID, AthleteID: step.AthleteID, SequenceNo: step.SequenceNo, StepType: step.StepType, ExerciseKey: step.ExerciseKey, ExerciseName: step.ExerciseName, Prescription: &prescription, Notes: step.Notes})
	}
	storedGroups := value.Groups
	if storedGroups == nil {
		storedGroups = []fitnessrepo.PlannedStepGroup{}
	}
	output.Groups = make([]plannedStepGroupOutput, 0, len(storedGroups))
	for _, group := range storedGroups {
		output.Groups = append(output.Groups, plannedStepGroupOutput{ID: group.ID, IdempotencyKey: group.IdempotencyKey, PlannedSessionID: group.PlannedSessionID, Label: group.Label, Mode: group.Mode, Rounds: group.Rounds, StepSequenceNos: append([]int(nil), group.StepSequenceNos...), Notes: group.Notes})
	}
	return output, nil
}

func jsonObject(raw json.RawMessage, label string) (map[string]any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 {
		return map[string]any{}, nil
	}
	if trimmed == "null" {
		return nil, nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, fmt.Errorf("internal_error: %s is not a JSON object", label)
	}
	return value, nil
}

func jsonPrescription(raw json.RawMessage) (fitnessrepo.Prescription, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return fitnessrepo.Prescription{}, nil
	}
	var value fitnessrepo.Prescription
	if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(string(raw))[0] != '{' {
		return fitnessrepo.Prescription{}, errors.New("internal_error: planned step prescription is not a JSON object")
	}
	return value, nil
}

func (s *Server) upsertPlannedSession(ctx context.Context, _ *mcp.CallToolRequest, in PlannedSessionInput) (*mcp.CallToolResult, plannedSessionWinnerOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, plannedSessionWinnerOutput{}, e
	}
	in.PlanID, e = s.resolveCreateID(ctx, id, in.PlanID, in.PlanIdempotencyKey, s.repo.PlanIDForIdempotencyKey)
	if e != nil {
		return nil, plannedSessionWinnerOutput{}, e
	}
	in.SessionID, e = s.resolveCreateID(ctx, id, in.SessionID, in.IdempotencyKey, s.repo.PlannedSessionIDForIdempotencyKey)
	if e != nil {
		return nil, plannedSessionWinnerOutput{}, e
	}
	value, err := s.repo.UpsertPlannedSession(ctx, id, fitnessrepo.PlannedSessionWrite{ID: in.SessionID, PlanID: in.PlanID, IdempotencyKey: in.IdempotencyKey, PlanIdempotencyKey: in.PlanIdempotencyKey, ClientUpdatedAt: in.ClientUpdatedAt, Date: in.Date, Sport: in.Sport, SessionType: in.SessionType, Priority: in.Priority, Targets: in.Targets, Notes: in.Notes})
	if err != nil {
		return nil, plannedSessionWinnerOutput{}, err
	}
	output, err := toPlannedSessionWinnerOutput(value)
	return nil, output, err
}
func (s *Server) updatePlannedStatus(ctx context.Context, _ *mcp.CallToolRequest, in PlannedStatusInput) (*mcp.CallToolResult, plannedSessionWinnerOutput, error) {
	_, id, e := s.principal(ctx, in.TargetAthleteID, "fitness:write")
	if e != nil {
		return nil, plannedSessionWinnerOutput{}, e
	}
	value, err := s.repo.UpdatePlannedSessionStatus(ctx, id, fitnessrepo.PlannedSessionStatusWrite{ID: in.SessionID, ClientUpdatedAt: in.ClientUpdatedAt, Status: in.Status, ActivityID: in.CompletedActivityID, WorkoutID: in.CompletedWorkoutID})
	if err != nil {
		return nil, plannedSessionWinnerOutput{}, err
	}
	output, err := toPlannedSessionWinnerOutput(value)
	return nil, output, err
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
