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
	Sleep         *fitnessdata.SleepSummary         `json:"sleep,omitempty"`
	HRV           *fitnessdata.HRVSummary           `json:"hrv,omitempty"`
	HeartRate     *fitnessdata.HeartRateSummary     `json:"heart_rate,omitempty"`
	Stress        *fitnessdata.StressSummary        `json:"stress,omitempty"`
	BodyBattery   *fitnessdata.BodyBatterySummary   `json:"body_battery,omitempty"`
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
	EntryID         string   `json:"entry_id"`
	PlannedStepID   *string  `json:"planned_step_id,omitempty"`
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

type StrengthSummary struct {
	WorkingSets     int            `json:"working_sets"`
	VolumeLoadKG    *float64       `json:"volume_load_kg,omitempty"`
	MuscleGroupSets map[string]int `json:"muscle_group_sets"`
	HighEffortSets  int            `json:"high_effort_sets"`
}

type WorkoutWrite struct {
	ID                               string
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
	ID           string     `json:"checkin_id"`
	RecordedAt   string     `json:"recorded_at"`
	LocalDate    string     `json:"local_date"`
	Timezone     string     `json:"timezone"`
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
type CheckinWrite struct {
	ID                                                string
	ClientUpdatedAt                                   int64
	RecordedAt                                        time.Time
	Energy, Fatigue, Motivation, SleepQuality, Stress *int
	Soreness, Pain                                    []PainArea
	Illness                                           Illness
	Notes                                             *string
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
	ID          string           `json:"plan_id"`
	AthleteID   string           `json:"athlete_id,omitempty"`
	Title       string           `json:"title"`
	Description *string          `json:"description,omitempty"`
	Goal        json.RawMessage  `json:"goal"`
	Status      string           `json:"status"`
	StartDate   *string          `json:"start_date,omitempty"`
	EndDate     *string          `json:"end_date,omitempty"`
	Source      string           `json:"source"`
	SourceRef   *string          `json:"source_ref,omitempty"`
	CreatedAt   int64            `json:"created_at,omitempty"`
	SyncSeq     int64            `json:"sync_seq"`
	Sessions    []PlannedSession `json:"sessions,omitempty"`
}
type PlannedSession struct {
	ID                  string              `json:"session_id"`
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
}

type PlannedStep struct {
	ID               string          `json:"step_id"`
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
type PlannedSessionWrite struct {
	ID, PlanID                         string
	ClientUpdatedAt                    int64
	Date, Sport, SessionType, Priority string
	Targets                            PlannedTargets
	Notes                              *string
	Title, Description                 string
	Steps                              []PlannedStep
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
	ValidSamples    int    `json:"valid_samples,omitempty"`
	RequiredSamples int    `json:"required_samples,omitempty"`
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
	Classification         string         `json:"classification"`
	Confidence             string         `json:"confidence"`
	SleepScore             *float64       `json:"sleep_score,omitempty"`
	SleepHours             *float64       `json:"sleep_hours,omitempty"`
	OvernightHRV           *float64       `json:"overnight_hrv,omitempty"`
	HRVBaseline            *float64       `json:"hrv_baseline,omitempty"`
	HRVRatio               *float64       `json:"hrv_ratio,omitempty"`
	RestingHeartRate       *float64       `json:"resting_heart_rate,omitempty"`
	RHRBaseline            *float64       `json:"rhr_baseline,omitempty"`
	RHRDelta               *float64       `json:"rhr_delta,omitempty"`
	BodyBatteryAtWake      *float64       `json:"body_battery_at_wake,omitempty"`
	AverageStressYesterday *float64       `json:"average_stress_yesterday,omitempty"`
	SubjectiveFatigue      *int           `json:"subjective_fatigue,omitempty"`
	Soreness               []PainArea     `json:"soreness,omitempty"`
	Method                 RecoveryMethod `json:"method"`
}
type RecoveryMethod struct {
	Name                string   `json:"name"`
	ValidPrimarySignals int      `json:"valid_primary_signals"`
	NegativeSignals     []string `json:"negative_signals"`
}
type TrainingLoadStatus struct {
	Last7Days               *float64   `json:"last_7_days,omitempty"`
	Previous7Days           *float64   `json:"previous_7_days,omitempty"`
	Last28Days              *float64   `json:"last_28_days,omitempty"`
	ChronicWeeklyAverage    *float64   `json:"chronic_weekly_average,omitempty"`
	AcuteChronicRatio       *float64   `json:"acute_chronic_ratio,omitempty"`
	HardSessionsLast7Days   int        `json:"hard_sessions_last_7_days"`
	ConsecutiveTrainingDays int        `json:"consecutive_training_days"`
	Classification          string     `json:"classification"`
	Method                  LoadMethod `json:"method"`
}
type LoadMethod struct {
	Name               string `json:"name"`
	ActivitiesWithLoad int    `json:"activities_with_load"`
	TotalActivities    int    `json:"total_activities"`
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
