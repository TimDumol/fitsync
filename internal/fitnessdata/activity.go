package fitnessdata

import (
	"encoding/json"
	"fmt"
	"strings"
)

var activityCategories = map[string]string{
	"running": "running", "trail_running": "running", "treadmill_running": "running", "track_running": "running",
	"walking": "walking", "hiking": "hiking", "cycling": "cycling", "indoor_cycling": "cycling", "mountain_biking": "cycling",
	"swimming": "swimming", "pool_swimming": "swimming", "open_water_swimming": "swimming",
	"strength_training": "strength", "strength": "strength", "cardio": "cardio", "elliptical": "cardio",
	"mobility": "mobility", "yoga": "mobility", "multisport": "multisport",
}

func ActivityCategory(source string) string {
	key := strings.ToLower(strings.TrimSpace(source))
	if category, ok := activityCategories[key]; ok {
		return category
	}
	return "other"
}

func DecodeActivity(_ string, payload json.RawMessage) (DecoderResult[ActivitySummary], error) {
	object, err := decodeObject(payload)
	if err != nil {
		return DecoderResult[ActivitySummary]{}, err
	}
	activity := ActivitySummary{
		ActivityID:      stringValue(nested(object, "activityId", "activity_id", "id")),
		StartLocal:      text(object, "startTimeLocal", "start_time_local"),
		StartGMT:        text(object, "startTimeGMT", "start_time_gmt"),
		Name:            text(object, "activityName", "activity_name", "name"),
		SourceType:      stringValue(nested(object, "activityType.typeKey", "activityType.typeName", "activity_type", "activity_type_key", "type")),
		ParentType:      text(object, "activityType.parentType.typeKey", "parent_type", "parent_activity_type"),
		DistanceKM:      firstNumber(object, "distanceKm", "distance_km"),
		DurationMinutes: durationMinutes(object, []string{"duration", "durationSeconds", "duration_seconds"}, []string{"duration_minutes"}),
		MovingMinutes:   durationMinutes(object, []string{"movingDuration", "moving_duration_seconds"}, []string{"moving_minutes"}),
		ElapsedMinutes:  durationMinutes(object, []string{"elapsedDuration", "elapsed_duration_seconds"}, []string{"elapsed_minutes"}),
		PaceMinPerKM:    firstNumber(object, "averagePace", "pace_min_per_km", "average_pace_min_per_km"),
		SpeedKPH:        firstNumber(object, "averageSpeedKPH", "average_speed_kmh", "average_speed_kph"),
		ElevationGainM:  firstNumber(object, "elevationGain", "elevation_gain_m"),
		ElevationLossM:  firstNumber(object, "elevationLoss", "elevation_loss_m"),
		AverageHR:       firstNumber(object, "averageHR", "average_hr", "average_heart_rate"), MaxHR: firstNumber(object, "maxHR", "max_hr", "max_heart_rate"),
		Calories:            firstNumber(object, "calories", "totalCalories"),
		AerobicEffect:       firstNumber(object, "aerobicTrainingEffect", "aerobic_training_effect"),
		AnaerobicEffect:     firstNumber(object, "anaerobicTrainingEffect", "anaerobic_training_effect"),
		TrainingEffectLabel: text(object, "trainingEffectLabel", "training_effect_label"),
		TrainingLoad:        firstNumber(object, "activityTrainingLoad", "activity_training_load", "training_load"),
		VO2Max:              firstNumber(object, "vO2MaxValue", "vo2Max", "vo2_max"),
		Cadence:             firstNumber(object, "averageRunningCadenceInStepsPerMinute", "average_cadence", "cadence", "average_running_cadence_spm"),
		Power:               firstNumber(object, "averagePower", "average_power", "power", "average_power_w"),
		GradeAdjustedPace:   firstNumber(object, "avgGradeAdjustedPace", "grade_adjusted_pace", "grade_adjusted_pace_min_per_km"),
		Respiration:         firstNumber(object, "averageRespirationRate", "respiration", "average_respiration_rate"),
		Temperature:         firstNumber(object, "avgTemperature", "temperature", "min_temperature_c", "max_temperature_c"),
		WaterLoss:           firstNumber(object, "waterEstimatedLossVolume", "water_loss", "estimated_water_loss_ml"),
		BodyBatteryChange:   firstNumber(object, "differenceBodyBattery", "body_battery_change"),
		Steps:               firstNumber(object, "steps", "totalSteps"), IntensityMinutes: firstNumber(object, "vigorousIntensityMinutes", "intensity_minutes", "vigorous_intensity_minutes"),
		Fastest1K: firstNumber(object, "fastest1Km", "fastest_1k", "fastest_1k_seconds"), FastestMile: firstNumber(object, "fastestMile", "fastest_mile", "fastest_mile_seconds"),
		PerceivedEffort: firstNumber(object, "perceivedEffort", "perceived_effort"), Notes: text(object, "notes"),
		PersonalRecord:  boolean(object, "isPR", "is_pr", "personal_record", "is_personal_record"),
		Manufacturer:    text(object, "manufacturer"),
		LinkedWorkoutID: text(object, "linkedWorkoutId", "linked_workout_id"),
	}
	if value := firstNumber(object, "lapCount", "lap_count"); value != nil {
		lapCount := int(*value)
		activity.LapCount = &lapCount
	}
	if activity.DistanceKM == nil {
		if distanceMeters := firstNumber(object, "distance"); distanceMeters != nil {
			*distanceMeters /= 1000
			activity.DistanceKM = distanceMeters
		}
	}
	if activity.SpeedKPH == nil {
		activity.SpeedKPH = firstNumber(object, "averageSpeedKPH", "average_speed_kmh", "average_speed_kph")
	}
	if activity.DurationMinutes != nil && activity.DistanceKM != nil && *activity.DistanceKM > 0 {
		pace := *activity.DurationMinutes / *activity.DistanceKM
		activity.PaceMinPerKM = &pace
		speed := *activity.DistanceKM / (*activity.DurationMinutes / 60)
		activity.SpeedKPH = &speed
	}
	if activity.DurationMinutes != nil {
		activity.DurationDisplay = FormatDuration(*activity.DurationMinutes * 60)
	}
	if activity.PaceMinPerKM != nil {
		activity.PaceDisplay = FormatPace(*activity.PaceMinPerKM)
	}
	activity.SourceType = strings.ToLower(activity.SourceType)
	activity.Category = ActivityCategory(activity.SourceType)
	activity.HRZoneMinutes = make([]*float64, 5)
	for i := range activity.HRZoneMinutes {
		zone := i + 1
		activity.HRZoneMinutes[i] = firstNumber(object, fmt.Sprintf("hrZone%dMinutes", zone), fmt.Sprintf("hr_zone_%d_minutes", zone))
		if activity.HRZoneMinutes[i] == nil {
			activity.HRZoneMinutes[i] = firstNumber(object, fmt.Sprintf("hrZone%dTimeInSeconds", zone))
			if activity.HRZoneMinutes[i] != nil {
				*activity.HRZoneMinutes[i] /= 60
			}
		}
	}
	if activity.ActivityID == "" {
		activity.ActivityID = stringValue(nested(object, "activityId", "activity_id", "id"))
	}
	return DecoderResult[ActivitySummary]{Value: activity, Completeness: Availability{"activity": activity.ActivityID != ""}}, nil
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func secondsToMinutes(value *float64) *float64 {
	if value == nil {
		return nil
	}
	// Legacy curated payloads already use minutes; explicit *_seconds paths are
	// selected before this helper only when the key is present. Garmin's raw
	// duration fields are seconds, while a value named duration_minutes is not.
	v := *value / 60
	return &v
}

func durationMinutes(object map[string]any, secondsPaths, minutePaths []string) *float64 {
	if value := firstNumber(object, secondsPaths...); value != nil {
		return secondsToMinutes(value)
	}
	return firstNumber(object, minutePaths...)
}
