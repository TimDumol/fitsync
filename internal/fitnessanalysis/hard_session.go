// Package fitnessanalysis contains deterministic, SQL-free coaching analysis.
package fitnessanalysis

import (
	"strings"

	"github.com/timdumol/fitsync/internal/fitnessdata"
)

type HardReason struct {
	Rule      string  `json:"rule"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
}

type HardClassification struct {
	Hard    bool         `json:"hard"`
	Reasons []HardReason `json:"reasons"`
	Method  string       `json:"method"`
}

var sessionTypeAliases = map[string]string{
	"vo2": "vo2", "vo2max": "vo2", "vo2_max": "vo2",
	"interval": "intervals", "intervals": "intervals",
	"threshold": "threshold", "tempo": "tempo", "race": "race",
	"long": "long", "easy": "easy", "recovery": "recovery",
}

func NormalizeSessionType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	if normalized, ok := sessionTypeAliases[value]; ok {
		return normalized
	}
	return value
}

// ClassifyHardActivity evaluates every documented rule. Planned type is an
// explicit link-derived input and is intentionally separate from the raw
// Garmin activity type.
func ClassifyHardActivity(activity fitnessdata.ActivitySummary, plannedType string) HardClassification {
	result := HardClassification{Method: "hard_session_v2", Reasons: []HardReason{}}
	addType := func(value, rule string) {
		if normalized := NormalizeSessionType(value); normalized == "intervals" || normalized == "tempo" || normalized == "threshold" || normalized == "vo2" || normalized == "race" {
			result.Reasons = append(result.Reasons, HardReason{Rule: rule, Value: 1, Threshold: 1})
		}
	}
	addType(activity.SourceType, "normalized_session_type_hard")
	if normalized := NormalizeSessionType(plannedType); normalized == "intervals" || normalized == "tempo" || normalized == "threshold" || normalized == "vo2" || normalized == "race" {
		result.Reasons = append(result.Reasons, HardReason{Rule: "linked_planned_session_type_hard", Value: 1, Threshold: 1})
	}
	if activity.AerobicEffect != nil && *activity.AerobicEffect >= 3.5 {
		result.Reasons = append(result.Reasons, HardReason{Rule: "aerobic_effect_gte_3_5", Value: *activity.AerobicEffect, Threshold: 3.5})
	}
	if activity.AnaerobicEffect != nil && *activity.AnaerobicEffect >= 3.5 {
		result.Reasons = append(result.Reasons, HardReason{Rule: "anaerobic_effect_gte_3_5", Value: *activity.AnaerobicEffect, Threshold: 3.5})
	}
	zoneMinutes := 0.0
	zoneKnown := false
	for index := 3; index < len(activity.HRZoneMinutes) && index < 5; index++ {
		if activity.HRZoneMinutes[index] != nil {
			zoneMinutes += *activity.HRZoneMinutes[index]
			zoneKnown = true
		}
	}
	if zoneKnown && zoneMinutes >= 20 {
		result.Reasons = append(result.Reasons, HardReason{Rule: "z4_z5_minutes_gte_20", Value: zoneMinutes, Threshold: 20})
	}
	if activity.PerceivedEffort != nil && *activity.PerceivedEffort >= 8 {
		result.Reasons = append(result.Reasons, HardReason{Rule: "perceived_effort_gte_8", Value: *activity.PerceivedEffort, Threshold: 8})
	}
	result.Hard = len(result.Reasons) > 0
	return result
}
