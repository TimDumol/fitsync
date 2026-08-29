package fitnessdata

import "encoding/json"

func DecodeRecovery(_ string, payload json.RawMessage) (DecoderResult[DailyHealth], error) {
	object, err := decodeObject(payload)
	if err != nil {
		return DecoderResult[DailyHealth]{}, err
	}
	recovery := DecoderResult[DailyHealth]{Value: DailyHealth{Completeness: Availability{}}}
	recovery.Value.HeartRate = &HeartRateSummary{
		Resting: firstNumber(object, "summary.restingHeartRate", "heart_rate.resting", "resting_heart_rate.restingHeartRate", "resting_heart_rate"),
		Minimum: firstNumber(object, "heart_rate.minHeartRate", "heart_rate.minimum", "minimum_heart_rate"),
		Maximum: firstNumber(object, "heart_rate.maxHeartRate", "heart_rate.maximum", "maximum_heart_rate"),
	}
	recovery.Value.Stress = &StressSummary{
		Average:       firstNumber(object, "stress.averageStressLevel", "stress.average", "average_stress"),
		Maximum:       firstNumber(object, "stress.maxStressLevel", "stress.maximum", "maximum_stress"),
		Qualifier:     text(object, "stress.stressQualifier", "stress.qualifier"),
		HighMinutes:   firstNumber(object, "stress.highStressDuration", "stress.high_minutes"),
		MediumMinutes: firstNumber(object, "stress.mediumStressDuration", "stress.medium_minutes"),
		LowMinutes:    firstNumber(object, "stress.lowStressDuration", "stress.low_minutes"),
		RestMinutes:   firstNumber(object, "stress.restStressDuration", "stress.rest_minutes"),
	}
	recovery.Value.BodyBattery = &BodyBatterySummary{
		AtWake:     firstNumber(object, "summary.bodyBatteryAtWakeTime", "body_battery.at_wake", "body_battery_at_wake"),
		Highest:    firstNumber(object, "summary.bodyBatteryHighestValue", "body_battery.highest", "body_battery_highest"),
		Lowest:     firstNumber(object, "summary.bodyBatteryLowestValue", "body_battery.lowest", "body_battery_lowest"),
		MostRecent: firstNumber(object, "summary.bodyBatteryMostRecentValue", "body_battery.most_recent", "body_battery_most_recent"),
		Charged:    firstNumber(object, "summary.bodyBatteryChargedValue", "body_battery.charged", "body_battery_charged"),
		Drained:    firstNumber(object, "summary.bodyBatteryDrainedValue", "body_battery.drained", "body_battery_drained"),
	}
	recovery.Value.SpO2 = &SpO2Summary{
		SleepAverage: firstNumber(object, "spo2.sleepAverage", "spo2_sleep_average"),
		SleepLowest:  firstNumber(object, "spo2.sleepLowest", "spo2_sleep_lowest"),
		DailyAverage: firstNumber(object, "spo2.average", "spo2_daily_average"),
		DailyLowest:  firstNumber(object, "spo2.lowest", "spo2_daily_lowest"),
	}
	recovery.Value.DailyActivity = &DailyActivitySummary{
		Steps:             firstNumber(object, "summary.steps", "daily_activity.steps", "steps"),
		StepsGoal:         firstNumber(object, "summary.stepsGoal", "daily_activity.steps_goal", "steps_goal"),
		DistanceKM:        firstNumber(object, "summary.distanceKm", "daily_activity.distance_km", "distance_km"),
		ActiveMinutes:     firstNumber(object, "intensity.activeMinutes", "daily_activity.active_minutes"),
		HighlyActiveMin:   firstNumber(object, "intensity.highlyActiveMinutes", "daily_activity.highly_active_minutes"),
		SedentaryMinutes:  firstNumber(object, "intensity.sedentaryMinutes", "daily_activity.sedentary_minutes"),
		Calories:          firstNumber(object, "summary.calories", "daily_activity.calories"),
		IntensityMinutes:  firstNumber(object, "intensity.intensityMinutes", "daily_activity.intensity_minutes"),
		Floors:            firstNumber(object, "summary.floors", "daily_activity.floors"),
		WakingRespiration: firstNumber(object, "summary.wakingRespiration", "daily_activity.waking_respiration"),
	}
	for name, value := range map[string]any{"heart_rate": recovery.Value.HeartRate, "stress": recovery.Value.Stress, "body_battery": recovery.Value.BodyBattery, "spo2": recovery.Value.SpO2, "daily_activity": recovery.Value.DailyActivity} {
		recovery.Value.Completeness[name] = value != nil
	}
	return recovery, nil
}

func DecodeHRV(_ string, payload json.RawMessage) (DecoderResult[HRVSummary], error) {
	object, err := decodeObject(payload)
	if err != nil {
		return DecoderResult[HRVSummary]{}, err
	}
	value := HRVSummary{
		OvernightAverage: firstNumber(object, "hrv.hrvSummary.overnightAverage", "hrv_summary.overnight_average", "overnight_hrv"),
		WeeklyAverage:    firstNumber(object, "hrv.hrvSummary.weeklyAverage", "hrv_summary.weekly_average", "weekly_hrv"),
		Status:           text(object, "hrv.hrvSummary.status", "hrv_status"),
	}
	return DecoderResult[HRVSummary]{Value: value, Completeness: Availability{"hrv": value.OvernightAverage != nil || value.WeeklyAverage != nil}}, nil
}
