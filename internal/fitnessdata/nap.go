package fitnessdata

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// NapEvents is the bounded, source-specific nap projection used to reconcile
// Garmin's detailed Body Battery events with the sleep DTO aggregate.
type NapEvents struct {
	Naps      []NapSummary `json:"naps"`
	Available bool         `json:"available"`
}

// DecodeNapEvents accepts the recovery event shapes seen across Garmin
// Connect versions. Only explicitly nap-labelled events are considered; a
// generic sleep event is never guessed to be a nap.
func DecodeNapEvents(dataset string, payload json.RawMessage) (DecoderResult[NapEvents], error) {
	object, err := decodeObject(payload)
	if err != nil {
		return DecoderResult[NapEvents]{}, err
	}
	result := NapEvents{}
	for _, key := range []string{"events", "bodyBatteryEvents", "body_battery_events", "napEvents", "nap_events", "naps"} {
		value := nested(object, key)
		if value == nil {
			continue
		}
		result.Available = true
		walkNapEvents(value, dataset, &result.Naps)
	}
	if len(result.Naps) > 1 {
		unique := make([]NapSummary, 0, len(result.Naps))
		seen := map[string]bool{}
		for _, nap := range result.Naps {
			key := fmt.Sprintf("%s|%s|%.3f", stringValuePointer(nap.StartLocal), stringValuePointer(nap.EndLocal), nap.DurationSeconds)
			if seen[key] {
				continue
			}
			seen[key] = true
			unique = append(unique, nap)
			if len(unique) == 8 {
				break
			}
		}
		result.Naps = unique
	} else if len(result.Naps) > 8 {
		result.Naps = result.Naps[:8]
	}
	return DecoderResult[NapEvents]{Value: result, Completeness: Availability{"naps": result.Available}}, nil
}

func stringValuePointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func walkNapEvents(value any, dataset string, result *[]NapSummary) {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if len(*result) >= 8 {
				return
			}
			walkNapEvents(item, dataset, result)
		}
	case map[string]any:
		if isNapEvent(value) {
			if nap, ok := decodeNapEvent(value, dataset); ok {
				*result = append(*result, nap)
				return
			}
		}
		for _, child := range value {
			if len(*result) >= 8 {
				return
			}
			if _, ok := child.([]any); ok {
				walkNapEvents(child, dataset, result)
			}
		}
	}
}

func isNapEvent(event map[string]any) bool {
	for _, key := range []string{"eventType", "event_type", "type", "napType", "nap_type", "activityType"} {
		if value := strings.ToLower(stringValue(event[key])); strings.Contains(value, "nap") {
			return true
		}
	}
	for key := range event {
		if strings.Contains(strings.ToLower(key), "nap") {
			return true
		}
	}
	return false
}

func decodeNapEvent(event map[string]any, dataset string) (NapSummary, bool) {
	start := stringValue(nested(event, "eventStartTimeLocal", "event_start_time_local", "startTimeLocal", "start_local", "start"))
	end := stringValue(nested(event, "eventEndTimeLocal", "event_end_time_local", "endTimeLocal", "end_local", "end"))
	duration := firstNumber(event, "durationInSeconds", "duration_seconds", "durationSeconds", "napDurationSeconds", "nap_duration_seconds")
	if duration == nil && start != "" && end != "" {
		if startTime, ok := parseGarminTime(start); ok {
			if endTime, endOK := parseGarminTime(end); endOK && endTime.After(startTime) {
				seconds := endTime.Sub(startTime).Seconds()
				duration = &seconds
			}
		}
	}
	if duration == nil || *duration <= 0 {
		return NapSummary{}, false
	}
	return NapSummary{
		StartLocal:      optionalText(start),
		EndLocal:        optionalText(end),
		DurationSeconds: *duration,
		DurationDisplay: FormatDuration(*duration),
		Score:           firstNumber(event, "sleepScore", "sleep_score", "score"),
		Source:          dataset,
	}, true
}

func parseGarminTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func napDate(nap NapSummary, location *time.Location) string {
	if nap.StartLocal == nil {
		return ""
	}
	if parsed, ok := parseGarminTime(*nap.StartLocal); ok {
		return parsed.In(location).Format("2006-01-02")
	}
	if len(*nap.StartLocal) >= 10 {
		return (*nap.StartLocal)[:10]
	}
	return ""
}

// NapLocalDate returns the athlete-local calendar date represented by a nap
// start. It is exported for repository composition; parsing remains owned by
// fitnessdata.
func NapLocalDate(nap NapSummary, location *time.Location) string {
	return napDate(nap, location)
}
