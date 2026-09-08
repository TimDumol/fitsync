package fitnessdata

import (
	"bytes"
	"encoding/json"
	"errors"
)

func decodeObject(payload json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(payload)) == 0 || !json.Valid(payload) {
		return nil, errors.New("payload is invalid JSON")
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil || object == nil {
		return nil, errors.New("payload must be a JSON object")
	}
	return object, nil
}

func nested(object map[string]any, paths ...string) any {
	for _, path := range paths {
		var current any = object
		ok := true
		for _, part := range splitPath(path) {
			m, isMap := current.(map[string]any)
			if !isMap {
				ok = false
				break
			}
			current, ok = m[part]
			if !ok {
				break
			}
		}
		if ok && current != nil {
			return current
		}
	}
	return nil
}

func splitPath(path string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			result = append(result, path[start:i])
			start = i + 1
		}
	}
	return result
}

func number(value any) (*float64, bool) {
	switch value := value.(type) {
	case float64:
		return &value, true
	case float32:
		v := float64(value)
		return &v, true
	case int:
		v := float64(value)
		return &v, true
	case int64:
		v := float64(value)
		return &v, true
	case json.Number:
		v, err := value.Float64()
		return &v, err == nil
	default:
		return nil, false
	}
}

func firstNumber(object map[string]any, paths ...string) *float64 {
	v, _ := number(nested(object, paths...))
	return v
}

func text(object map[string]any, paths ...string) *string {
	v := nested(object, paths...)
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

func boolean(object map[string]any, paths ...string) *bool {
	v, ok := nested(object, paths...).(bool)
	if !ok {
		return nil
	}
	return &v
}

// DecodeSleep accepts both the direct Garmin envelope and the legacy flat
// curated shape. Empty strings are treated as missing; numeric strings are
// intentionally not coerced.
func DecodeSleep(_ string, payload json.RawMessage) (DecoderResult[SleepSummary], error) {
	object, err := decodeObject(payload)
	if err != nil {
		return DecoderResult[SleepSummary]{}, err
	}
	mainSeconds := firstNumber(object, "sleep.dailySleepDTO.sleepTimeSeconds", "sleep_seconds", "main_sleep_seconds")
	napSeconds := firstNumber(object, "sleep.dailySleepDTO.napTimeSeconds", "nap_seconds", "nap_duration_seconds")
	mainHours := firstNumber(object, "main_sleep_hours")
	s := SleepSummary{
		MainHours:   mainSeconds,
		NapHours:    napSeconds,
		DeepHours:   firstNumber(object, "sleep.dailySleepDTO.deepSleepSeconds", "deep_sleep_seconds"),
		LightHours:  firstNumber(object, "sleep.dailySleepDTO.lightSleepSeconds", "light_sleep_seconds"),
		REMHours:    firstNumber(object, "sleep.dailySleepDTO.remSleepSeconds", "rem_sleep_seconds"),
		AwakeHours:  firstNumber(object, "sleep.dailySleepDTO.awakeSleepSeconds", "awake_sleep_seconds"),
		Score:       firstNumber(object, "sleep.dailySleepDTO.sleepScores.overall.value", "sleep.dailySleepDTO.sleepScore", "sleep_score"),
		Qualifier:   text(object, "sleep.dailySleepDTO.sleepScores.overall.qualifier", "sleep_qualifier"),
		SleepStress: firstNumber(object, "sleep.dailySleepDTO.sleepStress", "sleep_stress"),
	}
	if s.MainHours == nil {
		s.MainHours = mainHours
	}
	if mainSeconds != nil {
		*s.MainHours /= 3600
	}
	if s.DeepHours != nil {
		*s.DeepHours /= 3600
	}
	if s.LightHours != nil {
		*s.LightHours /= 3600
	}
	if s.REMHours != nil {
		*s.REMHours /= 3600
	}
	if s.AwakeHours != nil {
		*s.AwakeHours /= 3600
	}
	if s.NapHours != nil && napSeconds != nil {
		*s.NapHours /= 3600
	}
	if napHours := firstNumber(object, "nap_hours"); napHours != nil && napSeconds == nil {
		s.NapHours = napHours
	}
	s.NapSourceKnown = hasNested(object, "sleep.dailySleepDTO.napTimeSeconds", "nap_seconds", "nap_duration_seconds", "nap_hours")
	s.MainSleepHours = s.MainHours
	s.NapSleepHours = s.NapHours
	if total := firstNumber(object, "total_sleep_including_naps_hours"); total != nil {
		s.TotalSleep24H = total
	} else if s.MainHours != nil && s.NapSourceKnown && s.NapHours != nil {
		total := *s.MainHours + *s.NapHours
		s.TotalSleep24H = &total
	}
	s.TotalHours = s.TotalSleep24H
	return DecoderResult[SleepSummary]{Value: s, Completeness: Availability{
		"sleep": s.MainHours != nil || s.Score != nil,
	}}, nil
}

func hasNested(object map[string]any, paths ...string) bool {
	for _, path := range paths {
		current := any(object)
		found := true
		for _, part := range splitPath(path) {
			m, ok := current.(map[string]any)
			if !ok {
				found = false
				break
			}
			current, ok = m[part]
			if !ok {
				found = false
				break
			}
		}
		if found && current != nil {
			return true
		}
	}
	return false
}
