// Package fitnessdata contains the bounded, normalized values exposed by the
// Fitnessd service. Raw Garmin JSON never crosses this package's boundary.
package fitnessdata

import "encoding/json"

type Availability map[string]bool

// Provenance identifies the bounded source and deterministic method behind a
// value returned by the coaching API. It deliberately contains no raw Garmin
// payload or athlete identifiers.
type Provenance struct {
	Source    string `json:"source"`
	Dataset   string `json:"dataset"`
	Method    string `json:"method"`
	UpdatedAt string `json:"updated_at"`
}

type SignalStatus struct {
	Available        bool       `json:"available"`
	ObservedDate     *string    `json:"observed_date"`
	LastObservedDate *string    `json:"last_observed_date"`
	AgeDays          *int       `json:"age_days"`
	Stale            bool       `json:"stale"`
	SourceStatus     string     `json:"source_status"`
	Provenance       Provenance `json:"provenance"`
}

type NormalizationWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type SleepSummary struct {
	SignalStatus
	MainSleepHours *float64     `json:"main_sleep_hours"`
	NapSleepHours  *float64     `json:"nap_sleep_hours"`
	TotalSleep24H  *float64     `json:"total_sleep_24h"`
	NapCount       int          `json:"nap_count"`
	Naps           []NapSummary `json:"naps,omitempty"`
	NapSourceKnown bool         `json:"-"`

	// Deprecated compatibility aliases. total_hours is intentionally the
	// corrected 24-hour total, not main sleep alone.
	MainHours   *float64 `json:"main_hours"`
	NapHours    *float64 `json:"nap_hours"`
	TotalHours  *float64 `json:"total_hours"`
	DeepHours   *float64 `json:"deep_hours"`
	LightHours  *float64 `json:"light_hours"`
	REMHours    *float64 `json:"rem_hours"`
	AwakeHours  *float64 `json:"awake_hours"`
	Score       *float64 `json:"score"`
	Qualifier   *string  `json:"qualifier"`
	SleepStress *float64 `json:"sleep_stress"`
}

type NapSummary struct {
	StartLocal      *string  `json:"start_local,omitempty"`
	EndLocal        *string  `json:"end_local,omitempty"`
	DurationSeconds float64  `json:"duration_seconds"`
	DurationDisplay string   `json:"duration_display"`
	Score           *float64 `json:"score,omitempty"`
	Source          string   `json:"source"`
}

type HRVSummary struct {
	SignalStatus
	OvernightAverage *float64 `json:"overnight_average"`
	WeeklyAverage    *float64 `json:"weekly_average"`
	Status           *string  `json:"status"`
}

type HeartRateSummary struct {
	SignalStatus
	Resting *float64 `json:"resting"`
	Minimum *float64 `json:"minimum"`
	Maximum *float64 `json:"maximum"`
}

type StressSummary struct {
	SignalStatus
	Average       *float64 `json:"average"`
	Maximum       *float64 `json:"maximum"`
	Qualifier     *string  `json:"qualifier"`
	HighMinutes   *float64 `json:"high_minutes"`
	MediumMinutes *float64 `json:"medium_minutes"`
	LowMinutes    *float64 `json:"low_minutes"`
	RestMinutes   *float64 `json:"rest_minutes"`
}

type BodyBatterySummary struct {
	SignalStatus
	AtWake     *float64 `json:"at_wake"`
	Highest    *float64 `json:"highest"`
	Lowest     *float64 `json:"lowest"`
	MostRecent *float64 `json:"most_recent"`
	Charged    *float64 `json:"charged"`
	Drained    *float64 `json:"drained"`
}

type SpO2Summary struct {
	SignalStatus
	SleepAverage *float64 `json:"sleep_average,omitempty"`
	SleepLowest  *float64 `json:"sleep_lowest,omitempty"`
	DailyAverage *float64 `json:"daily_average,omitempty"`
	DailyLowest  *float64 `json:"daily_lowest,omitempty"`
}

type DailyActivitySummary struct {
	SignalStatus
	Steps             *float64 `json:"steps,omitempty"`
	StepsGoal         *float64 `json:"steps_goal,omitempty"`
	DistanceKM        *float64 `json:"distance_km,omitempty"`
	ActiveMinutes     *float64 `json:"active_minutes,omitempty"`
	HighlyActiveMin   *float64 `json:"highly_active_minutes,omitempty"`
	SedentaryMinutes  *float64 `json:"sedentary_minutes,omitempty"`
	Calories          *float64 `json:"calories,omitempty"`
	IntensityMinutes  *float64 `json:"intensity_minutes,omitempty"`
	Floors            *float64 `json:"floors,omitempty"`
	WakingRespiration *float64 `json:"waking_respiration,omitempty"`
}

type DailyHealth struct {
	Date          string                `json:"date"`
	Sleep         *SleepSummary         `json:"sleep"`
	HRV           *HRVSummary           `json:"hrv"`
	HeartRate     *HeartRateSummary     `json:"heart_rate"`
	Stress        *StressSummary        `json:"stress"`
	BodyBattery   *BodyBatterySummary   `json:"body_battery"`
	SpO2          *SpO2Summary          `json:"spo2,omitempty"`
	DailyActivity *DailyActivitySummary `json:"daily_activity,omitempty"`
	Completeness  map[string]bool       `json:"completeness"`
}

type ActivitySummary struct {
	// Compatibility identifiers are additive; normalized consumers should use
	// ActivityID and Date.
	ID         string  `json:"id,omitempty"`
	Dataset    string  `json:"dataset,omitempty"`
	RecordKey  string  `json:"record_key,omitempty"`
	IngestedAt *int64  `json:"ingested_at,omitempty"`
	ActivityID string  `json:"activity_id"`
	Date       string  `json:"date"`
	StartLocal *string `json:"start_local,omitempty"`
	StartGMT   *string `json:"start_gmt,omitempty"`
	Name       *string `json:"name,omitempty"`
	Category   string  `json:"category"`
	SourceType string  `json:"source_type"`
	ParentType *string `json:"parent_type,omitempty"`

	DistanceKM          *float64   `json:"distance_km,omitempty"`
	DurationMinutes     *float64   `json:"duration_minutes,omitempty"`
	MovingMinutes       *float64   `json:"moving_minutes,omitempty"`
	ElapsedMinutes      *float64   `json:"elapsed_minutes,omitempty"`
	PaceMinPerKM        *float64   `json:"pace_min_per_km,omitempty"`
	PaceDisplay         string     `json:"pace_display,omitempty"`
	DurationDisplay     string     `json:"duration_display,omitempty"`
	SpeedKPH            *float64   `json:"speed_kph,omitempty"`
	ElevationGainM      *float64   `json:"elevation_gain_m,omitempty"`
	ElevationLossM      *float64   `json:"elevation_loss_m,omitempty"`
	AverageHR           *float64   `json:"average_hr,omitempty"`
	MaxHR               *float64   `json:"max_hr,omitempty"`
	HRZoneMinutes       []*float64 `json:"hr_zone_minutes,omitempty"`
	Calories            *float64   `json:"calories,omitempty"`
	AerobicEffect       *float64   `json:"aerobic_training_effect,omitempty"`
	AnaerobicEffect     *float64   `json:"anaerobic_training_effect,omitempty"`
	TrainingEffectLabel *string    `json:"training_effect_label,omitempty"`
	TrainingLoad        *float64   `json:"training_load,omitempty"`
	VO2Max              *float64   `json:"vo2_max,omitempty"`
	Cadence             *float64   `json:"cadence,omitempty"`
	Power               *float64   `json:"power,omitempty"`
	GradeAdjustedPace   *float64   `json:"grade_adjusted_pace,omitempty"`
	Respiration         *float64   `json:"respiration,omitempty"`
	Temperature         *float64   `json:"temperature,omitempty"`
	WaterLoss           *float64   `json:"water_loss,omitempty"`
	BodyBatteryChange   *float64   `json:"body_battery_change,omitempty"`
	Steps               *float64   `json:"steps,omitempty"`
	IntensityMinutes    *float64   `json:"intensity_minutes,omitempty"`
	Fastest1K           *float64   `json:"fastest_1k,omitempty"`
	FastestMile         *float64   `json:"fastest_mile,omitempty"`
	PersonalRecord      *bool      `json:"personal_record,omitempty"`
	LapCount            *int       `json:"lap_count,omitempty"`
	Manufacturer        *string    `json:"manufacturer,omitempty"`
	PerceivedEffort     *float64   `json:"perceived_effort,omitempty"`
	Notes               *string    `json:"notes,omitempty"`
	LinkedWorkoutID     *string    `json:"linked_workout_id,omitempty"`
}

type ActivityDetail struct {
	Summary             ActivitySummary `json:"summary"`
	Splits              []Split         `json:"splits,omitempty"`
	HRZones             []Zone          `json:"hr_zones,omitempty"`
	PowerZones          []Zone          `json:"power_zones,omitempty"`
	HRZoneProvenance    *ZoneProvenance `json:"hr_zone_provenance,omitempty"`
	PowerZoneProvenance *ZoneProvenance `json:"power_zone_provenance,omitempty"`
	Metrics             []MetricPoint   `json:"metrics,omitempty"`
	SourcePoints        int             `json:"source_point_count"`
	ReturnedPoints      int             `json:"returned_point_count"`
	ResolutionSeconds   int             `json:"resolution_seconds"`
	Truncated           bool            `json:"truncated"`
}

type Split struct {
	Number          int      `json:"number"`
	SourceID        string   `json:"source_id,omitempty"`
	StartElapsed    float64  `json:"start_elapsed_seconds,omitempty"`
	DistanceKM      *float64 `json:"distance_km,omitempty"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
	PaceMinPerKM    *float64 `json:"pace_min_per_km,omitempty"`
	AverageHR       *float64 `json:"average_hr,omitempty"`
	EndHR           *float64 `json:"end_hr,omitempty"`
	MaxHR           *float64 `json:"max_hr,omitempty"`
	AveragePower    *float64 `json:"average_power,omitempty"`
	MaxPower        *float64 `json:"max_power,omitempty"`
	Cadence         *float64 `json:"cadence,omitempty"`
	StepType        string   `json:"step_type,omitempty"`
	DurationDisplay string   `json:"duration_display,omitempty"`
	PaceDisplay     string   `json:"pace_display,omitempty"`
}

type PlannedIntervalStep struct {
	StepType string `json:"step_type"`
}

type Zone struct {
	Zone    int      `json:"zone"`
	Low     *float64 `json:"low,omitempty"`
	High    *float64 `json:"high,omitempty"`
	Minutes *float64 `json:"minutes,omitempty"`
}

type ZoneProvenance struct {
	ZoneModel     string `json:"zone_model"`
	ZoneSource    string `json:"zone_source"`
	EffectiveDate string `json:"effective_date"`
	Version       string `json:"version"`
}

type MetricPoint struct {
	ElapsedSeconds float64  `json:"elapsed_seconds"`
	DistanceKM     *float64 `json:"distance_km,omitempty"`
	HeartRate      *float64 `json:"heart_rate,omitempty"`
	PaceMinPerKM   *float64 `json:"pace_min_per_km,omitempty"`
	SpeedKPH       *float64 `json:"speed_kph,omitempty"`
	Cadence        *float64 `json:"cadence,omitempty"`
	Power          *float64 `json:"power,omitempty"`
	ElevationM     *float64 `json:"elevation_m,omitempty"`
	Respiration    *float64 `json:"respiration,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
}

type DecoderResult[T any] struct {
	Value        T
	Completeness Availability
	Warnings     []NormalizationWarning
}

func Raw(value any) json.RawMessage {
	b, _ := json.Marshal(value)
	return b
}
