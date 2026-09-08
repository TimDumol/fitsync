// Package fitnessrepo is the SQL-backed application repository used by MCP
// and future REST revisions. It owns athlete-scoped queries and write clocks.
package fitnessrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/timdumol/fitsync/internal/fitnessdata"
)

type Clock func() time.Time

type Repository struct {
	db    DBTX
	clock Clock
}

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type DataStatus struct {
	LastSyncAt      *string   `json:"last_sync_at,omitempty"`
	CompleteThrough *string   `json:"complete_through,omitempty"`
	MissingDates    []string  `json:"missing_dates,omitempty"`
	Warnings        []Warning `json:"warnings,omitempty"`
}

type Warning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type Page[T any] struct {
	Items      []T        `json:"items"`
	NextCursor *string    `json:"next_cursor,omitempty"`
	DataStatus DataStatus `json:"data_status"`
}

type DailyHealthItem struct {
	Date          string                            `json:"date"`
	Sleep         *fitnessdata.SleepSummary         `json:"sleep"`
	HRV           *fitnessdata.HRVSummary           `json:"hrv"`
	HeartRate     *fitnessdata.HeartRateSummary     `json:"heart_rate"`
	Stress        *fitnessdata.StressSummary        `json:"stress"`
	BodyBattery   *fitnessdata.BodyBatterySummary   `json:"body_battery"`
	SpO2          *fitnessdata.SpO2Summary          `json:"spo2,omitempty"`
	DailyActivity *fitnessdata.DailyActivitySummary `json:"daily_activity,omitempty"`
	Completeness  map[string]bool                   `json:"completeness"`
}

type ActivityQuery struct {
	From, To                                           string
	Categories, SourceTypes                            []string
	Detail                                             string
	MinDurationMinutes, MinDistanceKM, MinTrainingLoad *float64
	Order                                              string
	Limit                                              int
	Cursor                                             string
}

type HealthQuery struct {
	From, To string
	Metrics  []string
	Order    string
	Limit    int
	Cursor   string
}

type ActivityResult struct {
	Summary  fitnessdata.ActivitySummary `json:"summary"`
	Warnings []Warning                   `json:"warnings,omitempty"`
	Detail   *fitnessdata.ActivityDetail `json:"detail,omitempty"`
}

type StrengthQuery struct {
	From, To                   string
	ExerciseKeys, MuscleGroups []string
	IncludeEntries             bool
	Order                      string
	Limit                      int
	Cursor                     string
}

type StrengthWorkout struct {
	WorkoutID        string             `json:"workout_id"`
	AthleteID        string             `json:"athlete_id,omitempty"`
	PlannedSessionID *string            `json:"planned_session_id,omitempty"`
	Date             string             `json:"date"`
	Title            string             `json:"title"`
	Source           string             `json:"source"`
	WorkoutType      string             `json:"workout_type,omitempty"`
	Status           string             `json:"status,omitempty"`
	StartedAt        *string            `json:"started_at,omitempty"`
	CompletedAt      *string            `json:"completed_at,omitempty"`
	DurationMinutes  *float64           `json:"duration_minutes,omitempty"`
	PerceivedEffort  *float64           `json:"perceived_effort,omitempty"`
	Notes            *string            `json:"notes,omitempty"`
	LinkedActivityID *string            `json:"linked_activity_id,omitempty"`
	Entries          []StrengthEntry    `json:"entries,omitempty"`
	Summary          StrengthSummary    `json:"summary"`
	PlannedSession   *PlannedSession    `json:"planned_session,omitempty"`
	Comparison       *WorkoutComparison `json:"comparison,omitempty"`
	SyncSeq          int64              `json:"sync_seq,omitempty"`
}

type StrengthEntry struct {
	EntryID                string   `json:"entry_id"`
	IdempotencyKey         string   `json:"-"`
	PlannedStepID          *string  `json:"planned_step_id,omitempty"`
	SequenceNo             int      `json:"sequence_no"`
	EntryType              string   `json:"entry_type"`
	ExerciseKey            *string  `json:"exercise_key,omitempty"`
	ExerciseName           *string  `json:"exercise_name,omitempty"`
	UserLabel              *string  `json:"user_label,omitempty"`
	SetNumber              *int     `json:"set_number,omitempty"`
	Reps                   *float64 `json:"reps,omitempty"`
	DurationSeconds        *float64 `json:"duration_seconds,omitempty"`
	LoadKG                 *float64 `json:"load_kg,omitempty"`
	LoadDescription        *string  `json:"load_description,omitempty"`
	IsBodyweight           *bool    `json:"is_bodyweight,omitempty"`
	ToFailure              *bool    `json:"to_failure,omitempty"`
	RIR                    *float64 `json:"rir,omitempty"`
	RIRMin                 *float64 `json:"rir_min,omitempty"`
	RIRMax                 *float64 `json:"rir_max,omitempty"`
	Laterality             *string  `json:"laterality,omitempty"`
	RepsPerSide            *bool    `json:"reps_per_side,omitempty"`
	LoadBasis              *string  `json:"load_basis,omitempty"`
	LoadPerLimbKG          *float64 `json:"load_per_limb_kg,omitempty"`
	TotalExternalLoadKG    *float64 `json:"total_external_load_kg,omitempty"`
	AddedLoadKG            *float64 `json:"added_load_kg,omitempty"`
	LoadSemantics          string   `json:"load_semantics"`
	NormalizedReps         *float64 `json:"normalized_reps,omitempty"`
	NormalizedVolumeLoadKG *float64 `json:"normalized_volume_load_kg,omitempty"`
	MuscleGroups           []string `json:"muscle_groups,omitempty"`
	DirectMuscleGroups     []string `json:"direct_muscle_groups,omitempty"`
	IndirectMuscleGroups   []string `json:"indirect_muscle_groups,omitempty"`
	Notes                  *string  `json:"notes,omitempty"`
	IsWarmup               bool     `json:"is_warmup,omitempty"`
}

type StrengthSummary struct {
	WorkingSets            int                            `json:"working_sets"`
	VolumeLoadKG           *float64                       `json:"volume_load_kg,omitempty"`
	MuscleGroupSets        map[string]int                 `json:"muscle_group_sets"`
	HighEffortSets         int                            `json:"high_effort_sets"`
	NormalizedVolumeLoadKG *float64                       `json:"normalized_volume_load_kg,omitempty"`
	LoadMethod             string                         `json:"load_method"`
	MuscleVolume           map[string]MuscleVolumeSummary `json:"muscle_volume"`
}

type MuscleVolumeSummary struct {
	DirectSets   float64 `json:"direct_sets"`
	IndirectSets float64 `json:"indirect_sets"`
	WeightedSets float64 `json:"weighted_sets"`
}

type WorkoutWrite struct {
	ID                               string
	IdempotencyKey                   string
	ClientUpdatedAt                  int64
	Date, Title, WorkoutType, Source string
	PlannedSessionID                 *string
	Status                           string
	StartedAt                        *time.Time
	CompletedAt                      *time.Time
	DurationMinutes                  *float64
	PerceivedEffort                  *float64
	Notes                            *string
	LinkedActivityID                 *string
	Entries                          []StrengthEntry
}

type WorkoutWinner struct {
	Workout             StrengthWorkout `json:"workout"`
	Created             bool            `json:"created"`
	Deleted             bool            `json:"deleted"`
	SubmittedVersionWon bool            `json:"submitted_version_won"`
	ServerSyncedAt      int64           `json:"server_synced_at"`
}

type CheckinQuery struct {
	From, To, Order, Cursor string
	Limit                   int
}

type PainArea struct {
	Area     string `json:"area"`
	Severity int    `json:"severity"`
}
type Illness struct {
	Present  bool     `json:"present"`
	Symptoms []string `json:"symptoms,omitempty"`
}
type Checkin struct {
	ID            string     `json:"checkin_id"`
	RecordedAt    string     `json:"recorded_at"`
	LocalDate     string     `json:"local_date"`
	Timezone      string     `json:"timezone"`
	Energy        *int       `json:"energy,omitempty"`
	Fatigue       *int       `json:"fatigue,omitempty"`
	Motivation    *int       `json:"motivation,omitempty"`
	SleepQuality  *int       `json:"sleep_quality,omitempty"`
	Stress        *int       `json:"stress,omitempty"`
	Soreness      []PainArea `json:"soreness,omitempty"`
	Pain          []PainArea `json:"pain,omitempty"`
	Illness       Illness    `json:"illness"`
	Notes         *string    `json:"notes,omitempty"`
	SchemaVersion int        `json:"schema_version,omitempty"`
	LegFreshness  *int       `json:"leg_freshness,omitempty"`
	PainPresent   *bool      `json:"pain_present,omitempty"`
	NeedsDetail   []string   `json:"needs_detail,omitempty"`
}
type CheckinWrite struct {
	ID                                                string
	IdempotencyKey                                    string
	ClientUpdatedAt                                   int64
	RecordedAt                                        time.Time
	Energy, Fatigue, Motivation, SleepQuality, Stress *int
	Soreness, Pain                                    []PainArea
	Illness                                           Illness
	Notes                                             *string
	SchemaVersion                                     int
	LegFreshness                                      *int
	PainPresent                                       *bool
}
type CheckinWinner struct {
	Checkin             Checkin `json:"checkin"`
	Created             bool    `json:"created"`
	SubmittedVersionWon bool    `json:"submitted_version_won"`
	ServerSyncedAt      int64   `json:"server_synced_at"`
}

type Profile struct {
	Timezone               string                          `json:"timezone"`
	Units                  string                          `json:"units"`
	BirthYear              *int                            `json:"birth_year,omitempty"`
	Goals                  []ProfileGoal                   `json:"goals,omitempty"`
	TargetEvents           []TargetEvent                   `json:"target_events,omitempty"`
	WeeklyAvailability     map[string][]AvailabilityWindow `json:"weekly_availability,omitempty"`
	Preferences            Preferences                     `json:"preferences"`
	HeartRateZones         []Zone                          `json:"heart_rate_zones,omitempty"`
	PaceZones              []Zone                          `json:"pace_zones,omitempty"`
	PowerZones             []Zone                          `json:"power_zones,omitempty"`
	Constraints            map[string]json.RawMessage      `json:"constraints,omitempty"`
	ConditionsAndInjuries  []Condition                     `json:"conditions_and_injuries,omitempty"`
	UpdatedAt              int64                           `json:"updated_at"`
	SensitiveFieldsPresent bool                            `json:"sensitive_fields_present"`
}
type ProfileGoal struct {
	Name        string   `json:"name"`
	TargetValue *float64 `json:"target_value,omitempty"`
	Unit        *string  `json:"unit,omitempty"`
	TargetDate  *string  `json:"target_date,omitempty"`
}
type TargetEvent struct {
	Name       string   `json:"name"`
	Sport      string   `json:"sport"`
	Date       string   `json:"date"`
	DistanceKM *float64 `json:"distance_km,omitempty"`
}
type AvailabilityWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
type Preferences struct {
	RunDays            int      `json:"run_days,omitempty"`
	StrengthSessions   int      `json:"strength_sessions,omitempty"`
	PreferredWeekdays  []string `json:"preferred_weekdays,omitempty"`
	BackToBackHardDays bool     `json:"back_to_back_hard_days,omitempty"`
}
type Zone struct {
	Name string   `json:"name,omitempty"`
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
}
type Condition struct {
	Label  string  `json:"label"`
	Status string  `json:"status"`
	Notes  *string `json:"notes,omitempty"`
}

type ProfileWrite struct {
	Profile         Profile
	ClientUpdatedAt int64
}
type ProfileWinner struct {
	Profile             Profile `json:"profile"`
	SubmittedVersionWon bool    `json:"submitted_version_won"`
}

type PlanResult struct {
	Plan     *TrainingPlan    `json:"plan"`
	Sessions []PlannedSession `json:"sessions"`
}
type TrainingPlan struct {
	ID             string           `json:"plan_id"`
	IdempotencyKey string           `json:"-"`
	AthleteID      string           `json:"athlete_id,omitempty"`
	Title          string           `json:"title"`
	Description    *string          `json:"description,omitempty"`
	Goal           json.RawMessage  `json:"goal"`
	Status         string           `json:"status"`
	StartDate      *string          `json:"start_date,omitempty"`
	EndDate        *string          `json:"end_date,omitempty"`
	Source         string           `json:"source"`
	SourceRef      *string          `json:"source_ref,omitempty"`
	CreatedAt      int64            `json:"created_at,omitempty"`
	SyncSeq        int64            `json:"sync_seq"`
	Sessions       []PlannedSession `json:"sessions,omitempty"`
}
type PlannedSession struct {
	ID                  string              `json:"session_id"`
	IdempotencyKey      string              `json:"-"`
	PlanID              string              `json:"plan_id"`
	AthleteID           string              `json:"athlete_id,omitempty"`
	Date                string              `json:"date"`
	Title               string              `json:"title"`
	Description         *string             `json:"description,omitempty"`
	Sport               string              `json:"sport"`
	SessionType         string              `json:"session_type"`
	Priority            string              `json:"priority"`
	Targets             PlannedTargets      `json:"targets"`
	Notes               *string             `json:"notes,omitempty"`
	Status              string              `json:"status"`
	CompletedActivityID *string             `json:"completed_activity_id,omitempty"`
	CompletedWorkoutID  *string             `json:"completed_workout_id,omitempty"`
	Completion          *ActivityCompletion `json:"completion,omitempty"`
	Steps               []PlannedStep       `json:"steps,omitempty"`
	Groups              []PlannedStepGroup  `json:"groups,omitempty"`
}

// PlannedStepGroup describes a non-nested set of exercise steps executed
// member-by-member for each round. The repository validates all references
// against the containing session before persisting the group.
type PlannedStepGroup struct {
	ID               string  `json:"group_id"`
	IdempotencyKey   string  `json:"idempotency_key,omitempty"`
	PlannedSessionID string  `json:"planned_session_id,omitempty"`
	AthleteID        string  `json:"athlete_id,omitempty"`
	Label            string  `json:"label,omitempty"`
	Mode             string  `json:"mode"`
	Rounds           int     `json:"rounds"`
	StepSequenceNos  []int   `json:"step_sequence_nos"`
	Notes            *string `json:"notes,omitempty"`
}

type PlannedStep struct {
	ID               string          `json:"step_id"`
	IdempotencyKey   string          `json:"idempotency_key,omitempty"`
	PlannedSessionID string          `json:"planned_session_id,omitempty"`
	AthleteID        string          `json:"athlete_id,omitempty"`
	SequenceNo       int             `json:"sequence_no"`
	StepType         string          `json:"step_type"`
	ExerciseKey      *string         `json:"exercise_key,omitempty"`
	ExerciseName     *string         `json:"exercise_name,omitempty"`
	Prescription     json.RawMessage `json:"prescription"`
	Notes            *string         `json:"notes,omitempty"`
}

type PrescriptionSet struct {
	RepsMin         *int     `json:"reps_min,omitempty"`
	RepsMax         *int     `json:"reps_max,omitempty"`
	LoadKG          *float64 `json:"load_kg,omitempty"`
	RIR             *float64 `json:"rir,omitempty"`
	RPE             *float64 `json:"rpe,omitempty"`
	DurationSeconds *int     `json:"duration_seconds,omitempty"`
	RestSeconds     *int     `json:"rest_seconds,omitempty"`
	Tempo           *string  `json:"tempo,omitempty"`
}

type Prescription struct {
	Sets            []PrescriptionSet `json:"sets,omitempty"`
	Rounds          *int              `json:"rounds,omitempty"`
	DistanceMeters  *float64          `json:"distance_meters,omitempty"`
	DurationSeconds *int              `json:"duration_seconds,omitempty"`
	PaceMinPerKMMin *float64          `json:"pace_min_per_km_min,omitempty"`
	PaceMinPerKMMax *float64          `json:"pace_min_per_km_max,omitempty"`
	HeartRateZone   *string           `json:"heart_rate_zone,omitempty"`
}

type WorkoutComparison struct {
	Steps []StepComparison `json:"steps"`
}

type StepComparison struct {
	PlannedStepID string    `json:"planned_step_id"`
	ExerciseKey   *string   `json:"exercise_key,omitempty"`
	ExerciseName  *string   `json:"exercise_name,omitempty"`
	PlannedSets   int       `json:"planned_sets,omitempty"`
	ActualSets    int       `json:"actual_sets,omitempty"`
	PlannedReps   []int     `json:"planned_reps,omitempty"`
	ActualReps    []float64 `json:"actual_reps,omitempty"`
	PlannedLoadKG []float64 `json:"planned_load_kg,omitempty"`
	ActualLoadKG  []float64 `json:"actual_load_kg,omitempty"`
}
type PlannedTargets struct {
	DurationMinutes *float64 `json:"duration_minutes,omitempty"`
	DistanceKM      *float64 `json:"distance_km,omitempty"`
	HeartRateZone   *string  `json:"heart_rate_zone,omitempty"`
	PaceMinPerKMMin *float64 `json:"pace_min_per_km_min,omitempty"`
	PaceMinPerKMMax *float64 `json:"pace_min_per_km_max,omitempty"`
	Sets            *int     `json:"sets,omitempty"`
	Reps            *int     `json:"reps,omitempty"`
	LoadKG          *float64 `json:"load_kg,omitempty"`
}
type ActivityCompletion struct {
	ActivityID      string   `json:"activity_id"`
	Date            string   `json:"date"`
	DurationMinutes *float64 `json:"duration_minutes,omitempty"`
	DistanceKM      *float64 `json:"distance_km,omitempty"`
}

type TimelineQuery struct {
	From, To         string
	IncludeUnplanned bool
	IncludeEntries   bool
	Order            string
	Limit            int
	Cursor           string
}

type TimelineResult struct {
	Days       []TimelineDay `json:"days"`
	NextCursor *string       `json:"next_cursor,omitempty"`
	DataStatus DataStatus    `json:"data_status"`
	Warnings   []Warning     `json:"warnings,omitempty"`
	Method     Method        `json:"method"`
}

type TimelineDay struct {
	Date      string              `json:"date"`
	Planned   []PlannedSession    `json:"planned"`
	Completed []TimelineCompleted `json:"completed"`
	Links     []TimelineLink      `json:"links"`
	Status    string              `json:"status"`
	Totals    TimelineTotals      `json:"totals"`
}

type TimelineCompleted struct {
	Kind             string          `json:"kind"`
	ID               string          `json:"id"`
	Date             string          `json:"date"`
	Title            string          `json:"title"`
	Category         string          `json:"category,omitempty"`
	SessionType      string          `json:"session_type,omitempty"`
	DurationMinutes  *float64        `json:"duration_minutes,omitempty"`
	DistanceKM       *float64        `json:"distance_km,omitempty"`
	GarminLoad       *float64        `json:"garmin_load,omitempty"`
	ActivityID       *string         `json:"activity_id,omitempty"`
	WorkoutID        *string         `json:"workout_id,omitempty"`
	PlannedSessionID *string         `json:"planned_session_id,omitempty"`
	Entries          []StrengthEntry `json:"entries,omitempty"`
}

type TimelineLink struct {
	PlannedSessionID *string  `json:"planned_session_id,omitempty"`
	ActivityID       *string  `json:"activity_id,omitempty"`
	WorkoutID        *string  `json:"workout_id,omitempty"`
	Status           string   `json:"status"`
	Method           string   `json:"method"`
	Score            *float64 `json:"score,omitempty"`
	Margin           *float64 `json:"margin,omitempty"`
	CandidateIDs     []string `json:"candidate_ids,omitempty"`
}

type TimelineTotals struct {
	DurationMinutes *float64 `json:"duration_minutes,omitempty"`
	DistanceKM      *float64 `json:"distance_km,omitempty"`
	GarminLoad      *float64 `json:"garmin_load,omitempty"`
}

type ActivityIntervalsQuery struct {
	ActivityID        string
	IncludeLaps       bool
	IncludeRecoveries bool
	MaxSegments       int
}

type ActivityIntervalsResult struct {
	ActivityID string            `json:"activity_id"`
	Segments   []IntervalSegment `json:"segments"`
	Warnings   []Warning         `json:"warnings,omitempty"`
	Method     Method            `json:"method"`
}

type IntervalSegment struct {
	Sequence             int      `json:"sequence"`
	SourceID             string   `json:"source_id"`
	Classification       string   `json:"classification"`
	StartElapsed         float64  `json:"start_elapsed_seconds"`
	DurationSeconds      *float64 `json:"duration_seconds,omitempty"`
	DurationDisplay      string   `json:"duration_display,omitempty"`
	DistanceKM           *float64 `json:"distance_km,omitempty"`
	PaceMinPerKM         *float64 `json:"pace_min_per_km,omitempty"`
	PaceDisplay          string   `json:"pace_display,omitempty"`
	AverageHR            *float64 `json:"average_hr,omitempty"`
	EndHR                *float64 `json:"end_hr,omitempty"`
	MaxHR                *float64 `json:"max_hr,omitempty"`
	AveragePower         *float64 `json:"average_power,omitempty"`
	MaxPower             *float64 `json:"max_power,omitempty"`
	Cadence              *float64 `json:"cadence,omitempty"`
	RecoveryHRDrop       *float64 `json:"recovery_hr_drop,omitempty"`
	ClassificationSource string   `json:"classification_source"`
	Confidence           string   `json:"confidence"`
}

type ExerciseProgressQuery struct {
	ExerciseKey    string
	From, To       string
	IncludeWarmups bool
	Limit          int
	Cursor         string
}

type ExerciseDefinitionOutput struct {
	Key                 string   `json:"key"`
	Aliases             []string `json:"aliases"`
	DirectMuscles       []string `json:"direct_muscles"`
	IndirectMuscles     []string `json:"indirect_muscles"`
	SupportsE1RM        bool     `json:"supports_e1rm"`
	AliasUsed           string   `json:"alias_used,omitempty"`
	NormalizationStatus string   `json:"normalization_status"`
}

type ExerciseProgress struct {
	Exercise   ExerciseDefinitionOutput  `json:"exercise"`
	Sessions   []ExerciseProgressSession `json:"sessions"`
	RepPRs     []ExerciseRepPR           `json:"rep_prs"`
	Trend      ExerciseTrendResult       `json:"trend"`
	NextCursor *string                   `json:"next_cursor,omitempty"`
	Method     Method                    `json:"method"`
}

type ExerciseProgressSession struct {
	WorkoutID              string                `json:"workout_id"`
	Date                   string                `json:"date"`
	Title                  string                `json:"title"`
	Sets                   []ExerciseProgressSet `json:"sets"`
	BestSet                *ExerciseProgressSet  `json:"best_set,omitempty"`
	VolumeLoadKG           *float64              `json:"volume_load_kg,omitempty"`
	NormalizedVolumeLoadKG *float64              `json:"normalized_volume_load_kg,omitempty"`
}

type ExerciseProgressSet struct {
	EntryID         string   `json:"entry_id"`
	Reps            *float64 `json:"reps,omitempty"`
	NormalizedReps  *float64 `json:"normalized_reps,omitempty"`
	LoadKG          *float64 `json:"load_kg,omitempty"`
	LoadBasis       *string  `json:"load_basis,omitempty"`
	LoadSemantics   string   `json:"load_semantics"`
	RIR             *float64 `json:"rir,omitempty"`
	Estimated1RMKG  *float64 `json:"estimated_1rm_kg,omitempty"`
	DurationDisplay string   `json:"duration_display,omitempty"`
}

type ExerciseRepPR struct {
	Reps    int      `json:"reps"`
	LoadKG  float64  `json:"load_kg"`
	RIRMin  *float64 `json:"rir_min,omitempty"`
	RIRMax  *float64 `json:"rir_max,omitempty"`
	EntryID string   `json:"entry_id"`
	Date    string   `json:"date"`
}

type ExerciseTrendResult struct {
	Label          string   `json:"label"`
	LatestLoadKG   *float64 `json:"latest_load_kg,omitempty"`
	PreviousLoadKG *float64 `json:"previous_load_kg,omitempty"`
	ChangeKG       *float64 `json:"change_kg,omitempty"`
	Samples        int      `json:"samples"`
	ComparableReps *int     `json:"comparable_reps,omitempty"`
	Method         string   `json:"method"`
}

type ReadinessQuery struct {
	AsOf                    string
	SessionDate             string
	ProposedSessionType     string
	ProposedDurationMinutes int
	ProposedIntensity       string
}

type ReadinessResult struct {
	Recommendation         string              `json:"recommendation"`
	Confidence             string              `json:"confidence"`
	Reasons                []ReadinessReason   `json:"reasons"`
	Counterpoints          []ReadinessReason   `json:"counterpoints"`
	RulesEvaluated         []string            `json:"rules_evaluated"`
	RulesUnavailable       []string            `json:"rules_unavailable"`
	RelevantRecentSessions []TimelineCompleted `json:"relevant_recent_sessions"`
	RecoverySignals        *RecoveryStatus     `json:"recovery_signals"`
	LoadContext            *TrainingLoadStatus `json:"load_context"`
	NearbyPlan             []PlannedSession    `json:"nearby_plan"`
	Method                 Method              `json:"method"`
}

type ReadinessReason struct {
	Rule        string   `json:"rule"`
	Actual      any      `json:"actual"`
	Threshold   any      `json:"threshold"`
	SourceIDs   []string `json:"source_entity_ids"`
	Date        string   `json:"date"`
	Explanation string   `json:"explanation"`
}

type WorkoutPatchOperation struct {
	Op                   string
	ActivityID           *string
	EntryID              *string
	RIR                  *float64
	ExerciseKey          *string
	Laterality           *string
	RepsPerSide          *bool
	LoadBasis            *string
	LoadPerLimbKG        *float64
	TotalExternalLoadKG  *float64
	AddedLoadKG          *float64
	DirectMuscleGroups   []string
	IndirectMuscleGroups []string
	Text                 *string
}

type PatchWorkoutResult struct {
	Workout  WorkoutWinner  `json:"workout"`
	Conflict *WorkoutWinner `json:"conflict,omitempty"`
}
type PlannedSessionWrite struct {
	ID, PlanID                         string
	IdempotencyKey, PlanIdempotencyKey string
	ClientUpdatedAt                    int64
	Date, Sport, SessionType, Priority string
	Targets                            PlannedTargets
	Notes                              *string
	Title, Description                 string
	Steps                              []PlannedStep
	Groups                             []PlannedStepGroup
}

type PlanWrite struct {
	Plan     TrainingPlan
	Sessions []PlannedSessionWrite
	Steps    map[string][]PlannedStep
}

type SyncChange struct {
	SyncSeq   int64  `json:"sync_seq"`
	Kind      string `json:"kind"`
	Operation string `json:"operation"`
	EntityID  string `json:"entity_id"`
	Entity    any    `json:"entity,omitempty"`
}

type SyncPage struct {
	Changes   []SyncChange `json:"changes"`
	NextAfter int64        `json:"next_after"`
	HasMore   bool         `json:"has_more"`
}
type PlannedSessionStatusWrite struct {
	ID                    string
	ClientUpdatedAt       int64
	Status                string
	ActivityID, WorkoutID *string
}
type PlannedSessionWinner struct {
	Session             PlannedSession `json:"session"`
	Created             bool           `json:"created"`
	SubmittedVersionWon bool           `json:"submitted_version_won"`
	ServerSyncedAt      int64          `json:"server_synced_at"`
}

type AnalysisQuery struct {
	From, To, GroupBy string
	Compare           bool
	Categories        []string
}
type TrainingAnalysis struct {
	From                 string              `json:"from"`
	To                   string              `json:"to"`
	Comparison           *AnalysisPeriod     `json:"comparison,omitempty"`
	Buckets              []AnalysisBucket    `json:"buckets"`
	Current              AnalysisAggregates  `json:"current"`
	ComparisonAggregates *AnalysisAggregates `json:"comparison_aggregates,omitempty"`
	Changes              map[string]Change   `json:"changes,omitempty"`
	Methods              map[string]Method   `json:"methods"`
	Warnings             []Warning           `json:"warnings,omitempty"`
}
type AnalysisPeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type AnalysisBucket struct {
	Start           string   `json:"start"`
	End             string   `json:"end"`
	Activities      int      `json:"activities"`
	ActiveDays      int      `json:"active_days"`
	DurationMinutes *float64 `json:"duration_minutes,omitempty"`
	DistanceKM      *float64 `json:"distance_km,omitempty"`
	GarminLoad      *float64 `json:"garmin_load,omitempty"`
}
type AnalysisAggregates struct {
	ActivityCount   int               `json:"activity_count"`
	ActiveDays      int               `json:"active_days"`
	DurationMinutes *float64          `json:"duration_minutes,omitempty"`
	DistanceKM      *float64          `json:"distance_km,omitempty"`
	GarminLoad      *float64          `json:"garmin_load,omitempty"`
	Running         RunningAggregate  `json:"running"`
	Strength        StrengthAggregate `json:"strength"`
	Recovery        RecoveryAggregate `json:"recovery"`
	PlanConsistency PlanConsistency   `json:"plan_consistency"`
}
type RunningAggregate struct {
	DistanceKM            *float64           `json:"distance_km,omitempty"`
	DurationMinutes       *float64           `json:"duration_minutes,omitempty"`
	Frequency             int                `json:"frequency"`
	LongestRunKM          *float64           `json:"longest_run_km,omitempty"`
	IntensityDistribution map[string]float64 `json:"intensity_distribution,omitempty"`
	ComparableHRSlope     *Trend             `json:"comparable_hr_pace_trend,omitempty"`
}
type StrengthAggregate struct {
	Sessions        int             `json:"sessions"`
	WorkingSets     int             `json:"working_sets"`
	VolumeLoadKG    *float64        `json:"volume_load_kg,omitempty"`
	MuscleGroupSets map[string]int  `json:"muscle_group_sets,omitempty"`
	ExerciseTrends  []ExerciseTrend `json:"exercise_trends,omitempty"`
	HighEffortSets  int             `json:"high_effort_sets"`
	LatestDate      *string         `json:"latest_date,omitempty"`
}
type ExerciseTrend struct {
	ExerciseKey       string   `json:"exercise_key"`
	RecentSets        int      `json:"recent_sets"`
	FirstEstimated1RM *float64 `json:"first_estimated_1rm,omitempty"`
	LastEstimated1RM  *float64 `json:"last_estimated_1rm,omitempty"`
}
type RecoveryAggregate struct {
	SleepHours   *float64 `json:"sleep_hours,omitempty"`
	SleepScore   *float64 `json:"sleep_score,omitempty"`
	SleepSamples int      `json:"sleep_samples"`
	HRVTrend     *Trend   `json:"hrv_trend,omitempty"`
	RHRTrend     *Trend   `json:"rhr_trend,omitempty"`
}
type PlanConsistency struct {
	Planned        int      `json:"planned"`
	Completed      int      `json:"completed"`
	Skipped        int      `json:"skipped"`
	CompletionRate *float64 `json:"completion_rate,omitempty"`
}
type Trend struct {
	Label        string   `json:"label"`
	Percent      *float64 `json:"percent,omitempty"`
	SamplesFirst int      `json:"samples_first"`
	SamplesLast  int      `json:"samples_last"`
	Method       string   `json:"method"`
}
type Change struct {
	Absolute *float64 `json:"absolute,omitempty"`
	Percent  *float64 `json:"percent,omitempty"`
}
type Method struct {
	Name            string `json:"name"`
	Version         int    `json:"version,omitempty"`
	ValidSamples    int    `json:"valid_samples,omitempty"`
	RequiredSamples int    `json:"required_samples,omitempty"`
	InputsUsed      int    `json:"inputs_used,omitempty"`
	InputsAvailable int    `json:"inputs_available,omitempty"`
	Confidence      string `json:"confidence,omitempty"`
}

type TrainingContext struct {
	AthleteID          string                        `json:"athlete_id"`
	AsOf               string                        `json:"as_of"`
	Timezone           string                        `json:"timezone"`
	GeneratedAt        string                        `json:"generated_at"`
	DataStatus         DataStatus                    `json:"data_status"`
	Profile            *Profile                      `json:"profile,omitempty"`
	NearestTargetEvent *TargetEvent                  `json:"nearest_target_event,omitempty"`
	CurrentStatus      ContextStatus                 `json:"current_status"`
	RecentTraining     RecentTraining                `json:"recent_training"`
	RecentActivities   []fitnessdata.ActivitySummary `json:"recent_activities,omitempty"`
	LatestCheckin      *Checkin                      `json:"latest_checkin,omitempty"`
	PlannedSessions    []PlannedSession              `json:"planned_sessions,omitempty"`
	Flags              []string                      `json:"flags"`
}
type ContextStatus struct {
	Recovery     *RecoveryStatus     `json:"recovery,omitempty"`
	TrainingLoad *TrainingLoadStatus `json:"training_load,omitempty"`
}
type RecoveryStatus struct {
	Classification         string                 `json:"classification"`
	Confidence             string                 `json:"confidence"`
	Sleep                  RecoverySleepEvidence  `json:"sleep"`
	HRV                    RecoverySignalEvidence `json:"hrv"`
	RestingHeartRateSignal RecoverySignalEvidence `json:"resting_heart_rate"`
	SleepScore             *float64               `json:"sleep_score,omitempty"`
	SleepHours             *float64               `json:"sleep_hours,omitempty"` // Deprecated alias for sleep.total_24h.
	OvernightHRV           *float64               `json:"overnight_hrv,omitempty"`
	HRVBaseline            *float64               `json:"hrv_baseline,omitempty"`
	HRVRatio               *float64               `json:"hrv_ratio,omitempty"`
	RestingHeartRate       *float64               `json:"resting_heart_rate_value,omitempty"` // Deprecated flattened alias.
	RHRBaseline            *float64               `json:"rhr_baseline,omitempty"`
	RHRDelta               *float64               `json:"rhr_delta,omitempty"`
	BodyBatteryAtWake      *float64               `json:"body_battery_at_wake,omitempty"`
	AverageStressYesterday *float64               `json:"average_stress_yesterday,omitempty"`
	SubjectiveFatigue      *int                   `json:"subjective_fatigue,omitempty"`
	Soreness               []PainArea             `json:"soreness,omitempty"`
	Method                 RecoveryMethod         `json:"method"`
}

type RecoverySleepEvidence struct {
	MainHours        *float64 `json:"main_hours"`
	NapHours         *float64 `json:"nap_hours"`
	Total24H         *float64 `json:"total_24h"`
	Score            *float64 `json:"score"`
	Available        bool     `json:"available"`
	ObservedDate     *string  `json:"observed_date"`
	LastObservedDate *string  `json:"last_observed_date"`
	AgeDays          *int     `json:"age_days"`
	Stale            bool     `json:"stale"`
	SourceStatus     string   `json:"source_status"`
}

type RecoverySignalEvidence struct {
	Value            *float64 `json:"value"`
	Baseline         *float64 `json:"baseline"`
	Ratio            *float64 `json:"ratio,omitempty"`
	Delta            *float64 `json:"delta,omitempty"`
	Available        bool     `json:"available"`
	ObservedDate     *string  `json:"observed_date"`
	LastObservedDate *string  `json:"last_observed_date"`
	AgeDays          *int     `json:"age_days"`
	Stale            bool     `json:"stale"`
	SourceStatus     string   `json:"source_status"`
}

type RecoveryEvaluation struct {
	Rule      string `json:"rule"`
	Value     any    `json:"value"`
	Operator  string `json:"operator"`
	Threshold any    `json:"threshold"`
	Matched   bool   `json:"matched"`
}
type RecoveryMethod struct {
	Name                string               `json:"name"`
	Version             int                  `json:"version"`
	ValidPrimarySignals int                  `json:"valid_primary_signals"`
	InputsUsed          int                  `json:"inputs_used"`
	InputsAvailable     int                  `json:"inputs_available"`
	Confidence          string               `json:"confidence"`
	NegativeSignals     []string             `json:"negative_signals"`
	Evaluations         []RecoveryEvaluation `json:"evaluations"`
	UnavailableRules    []string             `json:"unavailable_rules"`
}
type TrainingLoadStatus struct {
	Last7Days                    *float64              `json:"last_7_days,omitempty"`
	Previous7Days                *float64              `json:"previous_7_days,omitempty"`
	Last28Days                   *float64              `json:"last_28_days,omitempty"`
	ChronicWeeklyAverage         *float64              `json:"chronic_weekly_average,omitempty"`
	AcuteChronicRatio            *float64              `json:"acute_chronic_ratio,omitempty"`
	HardSessionsLast7Days        int                   `json:"hard_sessions_last_7_days"`
	ConsecutiveTrainingDays      int                   `json:"consecutive_training_days"`
	Classification               string                `json:"classification"`
	RunningDistance7Days         *float64              `json:"running_distance_7_days,omitempty"`
	RunningDistance14Days        *float64              `json:"running_distance_14_days,omitempty"`
	RunningDistance28Days        *float64              `json:"running_distance_28_days,omitempty"`
	RunningDuration7Days         *float64              `json:"running_duration_7_days,omitempty"`
	RunningDuration14Days        *float64              `json:"running_duration_14_days,omitempty"`
	RunningDuration28Days        *float64              `json:"running_duration_28_days,omitempty"`
	RunningDistanceChangePercent *float64              `json:"running_distance_change_percent,omitempty"`
	RunningGarminLoad            *float64              `json:"running_garmin_load,omitempty"`
	TotalGarminLoad              *float64              `json:"total_garmin_load,omitempty"`
	HardSessions                 []HardSessionEvidence `json:"hard_sessions,omitempty"`
	DoubleSessionDays7           int                   `json:"double_session_days_7"`
	DoubleSessionDays28          int                   `json:"double_session_days_28"`
	DoubleSessionEvidence        []DoubleSessionDay    `json:"double_session_evidence,omitempty"`
	DaysSinceRestDay             *int                  `json:"days_since_rest_day,omitempty"`
	StrengthContext              StrengthContext       `json:"strength_context"`
	Method                       LoadMethod            `json:"method"`
}

type HardSessionEvidence struct {
	ActivityID string              `json:"activity_id,omitempty"`
	WorkoutID  string              `json:"workout_id,omitempty"`
	Date       string              `json:"date"`
	Hard       bool                `json:"hard"`
	Reasons    []HardSessionReason `json:"reasons"`
	Method     string              `json:"method"`
}

type HardSessionReason struct {
	Rule      string  `json:"rule"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
}

type DoubleSessionDay struct {
	Date       string   `json:"date"`
	SessionIDs []string `json:"session_ids"`
	Categories []string `json:"categories"`
}
type LoadMethod struct {
	Name               string `json:"name"`
	ActivitiesWithLoad int    `json:"activities_with_load"`
	TotalActivities    int    `json:"total_activities"`
	InputsUsed         int    `json:"inputs_used"`
	InputsAvailable    int    `json:"inputs_available"`
	Confidence         string `json:"confidence"`
}
type StrengthContext struct {
	Sessions       int    `json:"sessions"`
	WorkingSets    int    `json:"working_sets"`
	HighEffortSets int    `json:"high_effort_sets"`
	Method         string `json:"method"`
}
type RecentTraining struct {
	Runs     TrainingAggregate `json:"runs"`
	Strength StrengthAggregate `json:"strength"`
}
type TrainingAggregate struct {
	Sessions        int            `json:"sessions"`
	DurationMinutes *float64       `json:"duration_minutes,omitempty"`
	DistanceKM      *float64       `json:"distance_km,omitempty"`
	Load            *float64       `json:"load,omitempty"`
	LatestDate      *string        `json:"latest_date,omitempty"`
	MuscleGroupSets map[string]int `json:"muscle_group_sets,omitempty"`
	HighEffortSets  int            `json:"high_effort_sets,omitempty"`
}
