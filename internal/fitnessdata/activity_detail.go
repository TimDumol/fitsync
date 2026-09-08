package fitnessdata

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// DecodeActivityDetail normalizes bounded chart data. Location-bearing keys
// are ignored recursively before any value is returned.
func DecodeActivityDetail(dataset string, payload json.RawMessage) (DecoderResult[ActivityDetail], error) {
	object, err := decodeObject(payload)
	if err != nil {
		return DecoderResult[ActivityDetail]{}, err
	}
	clean := stripLocation(object)
	cleanObject, _ := clean.(map[string]any)
	cleanPayload, _ := json.Marshal(clean)
	activity, _ := DecodeActivity(dataset, cleanPayload)
	result := ActivityDetail{Summary: activity.Value, ResolutionSeconds: 30}
	result.Splits = decodeSplits(cleanObject)
	result.HRZones = decodeZones(cleanObject, "heartRateZones", "heart_rate_zones", "hrZones", "hr_zones")
	result.PowerZones = decodeZones(cleanObject, "powerZones", "power_zones", "powerInTimezones", "power_in_timezones")
	if len(result.HRZones) > 0 {
		result.HRZoneProvenance = zoneProvenance("heart_rate", result.Summary.Date, result.HRZones)
	}
	if len(result.PowerZones) > 0 {
		result.PowerZoneProvenance = zoneProvenance("power", result.Summary.Date, result.PowerZones)
	}
	var metricWarnings []NormalizationWarning
	result.Metrics, metricWarnings = decodeMetrics(cleanObject)
	result.SourcePoints = len(result.Metrics)
	result.ReturnedPoints = len(result.Metrics)
	warnings := append([]NormalizationWarning(nil), metricWarnings...)
	validateMetricDistances(result.Metrics, result.Summary.DistanceKM, &warnings)
	return DecoderResult[ActivityDetail]{Value: result, Completeness: Availability{
		"splits": len(result.Splits) > 0, "zones": len(result.HRZones) > 0 || len(result.PowerZones) > 0, "metrics": len(result.Metrics) > 0,
	}, Warnings: warnings}, nil
}

func stripLocation(value any) any {
	locationKeys := map[string]bool{"latitude": true, "longitude": true, "lat": true, "lon": true, "map": true, "route": true, "polyline": true, "startCoordinate": true, "endCoordinate": true, "start_coordinate": true, "end_coordinate": true, "location": true, "coordinates": true}
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			lower := strings.ToLower(key)
			if !locationKeys[lower] && !strings.Contains(lower, "latitude") && !strings.Contains(lower, "longitude") && !strings.Contains(lower, "polyline") && !strings.Contains(lower, "coordinate") {
				result[key] = stripLocation(item)
			}
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = stripLocation(item)
		}
		return result
	default:
		return value
	}
}

func decodeSplits(object map[string]any) []Split {
	items, ok := firstDetailArray(object, "splits", "splitSummaries", "split_summaries", "laps", "activityLaps")
	if !ok {
		return nil
	}
	result := make([]Split, 0, min(len(items), 200))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok || i >= 200 {
			break
		}
		n := i + 1
		if v, ok := number(m["splitNumber"]); ok {
			n = int(*v)
		}
		if v, ok := number(m["split_number"]); ok {
			n = int(*v)
		}
		split := Split{Number: n, SourceID: stringValue(nested(m, "lapId", "lap_id", "splitId", "split_id", "id")), StartElapsed: derefNumber(firstNumber(m, "startElapsedSeconds", "start_elapsed_seconds", "startTimeOffset")), DistanceKM: firstNumber(m, "distanceKm", "distance_km"), DurationSeconds: firstNumber(m, "duration", "duration_seconds"), PaceMinPerKM: firstNumber(m, "averagePace", "pace_min_per_km", "average_pace_min_per_km"), AverageHR: firstNumber(m, "averageHR", "average_hr", "average_heart_rate"), EndHR: firstNumber(m, "heartRateAtEnd", "heart_rate_at_end", "endHeartRate", "end_heart_rate", "end_hr"), MaxHR: firstNumber(m, "maxHR", "max_hr", "max_heart_rate"), AveragePower: firstNumber(m, "averagePower", "average_power"), MaxPower: firstNumber(m, "maxPower", "max_power"), Cadence: firstNumber(m, "averageCadence", "average_cadence", "cadence"), StepType: stringValue(nested(m, "stepType", "step_type", "intervalType", "interval_type"))}
		if split.DurationSeconds != nil {
			split.DurationDisplay = FormatDuration(*split.DurationSeconds)
		}
		if split.PaceMinPerKM != nil {
			split.PaceDisplay = FormatPace(*split.PaceMinPerKM)
		}
		result = append(result, split)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func derefNumber(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func decodeZones(object map[string]any, paths ...string) []Zone {
	items, ok := firstDetailArray(object, paths...)
	if !ok {
		return nil
	}
	result := make([]Zone, 0, min(len(items), 20))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok || i >= 20 {
			break
		}
		n := i + 1
		if v := firstNumber(m, "zoneNumber", "zone_number", "zone"); v != nil {
			n = int(*v)
		}
		minutes := firstNumber(m, "minutes", "duration_minutes")
		if minutes == nil {
			minutes = firstNumber(m, "secsInZone", "seconds", "durationSeconds", "duration_seconds")
			if minutes != nil {
				*minutes /= 60
			}
		}
		result = append(result, Zone{Zone: n, Low: firstNumber(m, "zoneLowBoundary", "zoneLow", "low"), High: firstNumber(m, "zoneHighBoundary", "zoneHigh", "high"), Minutes: minutes})
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func zoneProvenance(metric, effectiveDate string, zones []Zone) *ZoneProvenance {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s|", metric)
	for _, zone := range zones {
		_, _ = fmt.Fprintf(hash, "%d|", zone.Zone)
		if zone.Low == nil {
			_, _ = fmt.Fprint(hash, "null|")
		} else {
			_, _ = fmt.Fprintf(hash, "%.9g|", *zone.Low)
		}
		if zone.High == nil {
			_, _ = fmt.Fprint(hash, "null|")
		} else {
			_, _ = fmt.Fprintf(hash, "%.9g|", *zone.High)
		}
	}
	return &ZoneProvenance{ZoneModel: "unknown", ZoneSource: "garmin_activity_snapshot", EffectiveDate: effectiveDate, Version: fmt.Sprintf("sha256:%x", hash.Sum(nil))}
}

func decodeMetrics(object map[string]any) ([]MetricPoint, []NormalizationWarning) {
	value := nested(object, "metricTimeSeries", "metric_time_series", "metrics", "metricsData")
	if items, ok := value.([]any); ok {
		return decodeMetricItems(items)
	}
	series, ok := value.(map[string]any)
	if !ok {
		return nil, nil
	}
	return decodeMetricSeries(series)
}

func detailArray(value any, paths ...string) ([]any, bool) {
	if items, ok := value.([]any); ok {
		return items, true
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	items, ok := nested(object, paths...).([]any)
	return items, ok
}

func firstDetailArray(object map[string]any, paths ...string) ([]any, bool) {
	var empty []any
	found := false
	for _, path := range paths {
		items, ok := detailArray(nested(object, path), "items", "splits", "splitSummaries", "split_summaries", "laps", "zones", "heartRateZones", "heart_rate_zones", "powerZones", "power_zones")
		if !ok {
			continue
		}
		found = true
		if len(items) > 0 {
			return items, true
		}
		if empty == nil {
			empty = items
		}
	}
	return empty, found
}

func decodeMetricItems(items []any) ([]MetricPoint, []NormalizationWarning) {
	result := make([]MetricPoint, 0, min(len(items), 5000))
	warnings := []NormalizationWarning{}
	for index, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		point, recognized, itemWarnings := decodeMetricItem(m)
		warnings = appendBoundedWarnings(warnings, itemWarnings...)
		if !recognized {
			continue
		}
		if point.ElapsedSeconds == 0 && firstNumber(m, "elapsedSeconds", "elapsed_seconds", "timeOffset") == nil {
			point.ElapsedSeconds = float64(index)
		}
		result = append(result, point)
	}
	if len(result) == 0 {
		return nil, warnings
	}
	return result, warnings
}

type metricDescriptor struct {
	Key   string
	Index int
	Unit  string
}

func decodeMetricSeries(series map[string]any) ([]MetricPoint, []NormalizationWarning) {
	descriptorItems, ok := detailArray(nested(series, "metricDescriptors", "metric_descriptors"))
	if !ok {
		return nil, nil
	}
	rows, ok := detailArray(nested(series, "activityDetailMetrics", "activity_detail_metrics", "rows"), "items")
	if !ok {
		return nil, nil
	}
	descriptors := make(map[int]metricDescriptor, len(descriptorItems))
	for position, item := range descriptorItems {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		index := position
		if value, ok := number(m["metricsIndex"]); ok {
			index = int(*value)
		} else if value, ok := number(m["metrics_index"]); ok {
			index = int(*value)
		}
		descriptors[index] = metricDescriptor{Key: stringValue(nested(m, "key", "name")), Index: index, Unit: stringValue(nested(m, "unit", "units"))}
	}
	result := make([]MetricPoint, 0, min(len(rows), 5000))
	warnings := []NormalizationWarning{}
	var timestampOrigin *float64
	for rowIndex, item := range rows {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		point := MetricPoint{}
		recognized := false
		hasElapsed := false
		if direct, ok, itemWarnings := decodeMetricItem(m); ok {
			point = direct
			recognized = true
			hasElapsed = hasDirectElapsed(m)
			warnings = appendBoundedWarnings(warnings, itemWarnings...)
		} else {
			warnings = appendBoundedWarnings(warnings, itemWarnings...)
		}
		values, ok := detailArray(nested(m, "metrics", "values"))
		if ok {
			for position, raw := range values {
				metricsIndex := position
				if rawObject, isObject := raw.(map[string]any); isObject {
					if value, valid := number(nested(rawObject, "metricsIndex", "metrics_index", "index")); valid {
						metricsIndex = int(*value)
					}
				}
				descriptor, exists := descriptors[metricsIndex]
				if !exists {
					continue
				}
				if isMetricTimestamp(descriptor.Key) {
					if value := metricNumber(raw); value != nil {
						seconds := metricSeconds(*value, descriptor.Unit)
						if timestampOrigin == nil {
							origin := seconds
							timestampOrigin = &origin
						}
						point.ElapsedSeconds = seconds - *timestampOrigin
						recognized = true
						hasElapsed = true
					}
					continue
				}
				if assigned, warning := assignMetric(&point, descriptor.Key, descriptor.Unit, raw); assigned {
					recognized = true
					if strings.Contains(normalizedMetricKey(descriptor.Key), "duration") || strings.Contains(normalizedMetricKey(descriptor.Key), "elapsed") {
						hasElapsed = true
					}
				} else if warning != nil {
					warnings = appendBoundedWarnings(warnings, *warning)
				}
			}
		}
		if !recognized {
			continue
		}
		if !hasElapsed {
			point.ElapsedSeconds = float64(rowIndex)
		}
		result = append(result, point)
	}
	if len(result) == 0 {
		return nil, warnings
	}
	return result, warnings
}

func decodeMetricItem(m map[string]any) (MetricPoint, bool, []NormalizationWarning) {
	point := MetricPoint{}
	recognized := false
	warnings := []NormalizationWarning{}
	for _, path := range []string{"elapsedSeconds", "elapsed_seconds", "timeOffset"} {
		if value := firstNumber(m, path); value != nil {
			point.ElapsedSeconds = *value
			recognized = true
			break
		}
	}
	if value := metricFirstNumber(m, "distanceKm", "distance_km"); value != nil {
		point.DistanceKM, recognized = value, true
	}
	for _, key := range []string{"distance_m", "distance_meters"} {
		if value := metricFirstNumber(m, key); value != nil {
			*value /= 1000
			point.DistanceKM, recognized = value, true
			break
		}
	}
	if value := metricFirstNumber(m, "distance"); value != nil && point.DistanceKM == nil {
		unit := stringValue(nested(m, "distance_unit", "distanceUnit", "unit", "units"))
		if normalizedDistanceUnit(unit) == "meter" {
			*value /= 1000
			point.DistanceKM, recognized = value, true
		} else if normalizedDistanceUnit(unit) == "kilometer" {
			point.DistanceKM, recognized = value, true
		} else {
			warnings = append(warnings, unknownDistanceUnitWarning(unit))
		}
	}
	if value := metricFirstNumber(m, "heartRate", "heart_rate"); value != nil {
		point.HeartRate, recognized = value, true
	}
	if value := metricFirstNumber(m, "pace", "pace_min_per_km"); value != nil {
		point.PaceMinPerKM, recognized = value, true
	}
	if value := metricFirstNumber(m, "speed", "speed_kph"); value != nil {
		point.SpeedKPH, recognized = value, true
	}
	if value := metricFirstNumber(m, "cadence"); value != nil {
		point.Cadence, recognized = value, true
	}
	if value := metricFirstNumber(m, "power"); value != nil {
		point.Power, recognized = value, true
	}
	if value := metricFirstNumber(m, "elevation", "elevation_m"); value != nil {
		point.ElevationM, recognized = value, true
	}
	if value := metricFirstNumber(m, "respiration"); value != nil {
		point.Respiration, recognized = value, true
	}
	if value := metricFirstNumber(m, "temperature"); value != nil {
		point.Temperature, recognized = value, true
	}
	return point, recognized, warnings
}

func hasDirectElapsed(m map[string]any) bool {
	return firstNumber(m, "elapsedSeconds", "elapsed_seconds", "timeOffset") != nil
}

func metricNumber(value any) *float64 {
	if object, ok := value.(map[string]any); ok {
		return metricNumber(nested(object, "value", "numericValue", "numeric_value"))
	}
	result, _ := number(value)
	return result
}

func metricFirstNumber(object map[string]any, paths ...string) *float64 {
	for _, path := range paths {
		if value := metricNumber(nested(object, path)); value != nil {
			return value
		}
	}
	return nil
}

func normalizedMetricKey(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

func isMetricTimestamp(key string) bool {
	key = normalizedMetricKey(key)
	return strings.Contains(key, "timestamp")
}

func metricSeconds(value float64, unit string) float64 {
	unit = normalizedMetricKey(unit)
	if strings.Contains(unit, "millisecond") || unit == "ms" {
		return value / 1000
	}
	if strings.Contains(unit, "microsecond") || unit == "us" {
		return value / 1000000
	}
	if strings.Contains(unit, "minute") {
		return value * 60
	}
	return value
}

func assignMetric(point *MetricPoint, key, unit string, raw any) (bool, *NormalizationWarning) {
	value := metricNumber(raw)
	if value == nil {
		return false, nil
	}
	normalized := normalizedMetricKey(key)
	switch {
	case strings.Contains(normalized, "heartrate"):
		point.HeartRate = value
	case strings.Contains(normalized, "distance"):
		switch normalizedDistanceUnit(unit) {
		case "meter":
			*value /= 1000
		case "kilometer":
		default:
			return false, ptrNormalizationWarning(unknownDistanceUnitWarning(unit))
		}
		point.DistanceKM = value
	case strings.Contains(normalized, "pace"):
		if strings.Contains(normalizedMetricKey(unit), "second") {
			*value /= 60
		}
		point.PaceMinPerKM = value
	case strings.Contains(normalized, "speed"):
		normalizedUnit := normalizedMetricKey(unit)
		if strings.Contains(normalizedUnit, "meterpersecond") || strings.Contains(normalizedUnit, "meterspersecond") || normalizedUnit == "mps" || normalizedUnit == "m/s" {
			*value *= 3.6
		}
		point.SpeedKPH = value
	case strings.Contains(normalized, "cadence"):
		point.Cadence = value
	case strings.Contains(normalized, "power"):
		point.Power = value
	case strings.Contains(normalized, "elevation"):
		point.ElevationM = value
	case strings.Contains(normalized, "respiration"):
		point.Respiration = value
	case strings.Contains(normalized, "temperature"):
		point.Temperature = value
	case strings.Contains(normalized, "duration") || strings.Contains(normalized, "elapsed"):
		point.ElapsedSeconds = metricSeconds(*value, unit)
	default:
		return false, nil
	}
	return true, nil
}

func normalizedDistanceUnit(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	switch normalized {
	case "m", "meter", "meters":
		return "meter"
	case "km", "kilometer", "kilometers":
		return "kilometer"
	default:
		return ""
	}
}

func unknownDistanceUnitWarning(unit string) NormalizationWarning {
	message := "Distance metric was not returned because its unit is missing or unknown."
	if unit != "" {
		message = "Distance metric was not returned because its unit is unknown."
	}
	return NormalizationWarning{Code: "UNKNOWN_METRIC_DISTANCE_UNIT", Severity: "warning", Message: message}
}

func ptrNormalizationWarning(value NormalizationWarning) *NormalizationWarning { return &value }

func appendBoundedWarnings(target []NormalizationWarning, values ...NormalizationWarning) []NormalizationWarning {
	for _, value := range values {
		if len(target) >= 20 {
			break
		}
		target = append(target, value)
	}
	return target
}

func validateMetricDistances(points []MetricPoint, summary *float64, warnings *[]NormalizationWarning) {
	var previous float64
	previousKnown := false
	for index := range points {
		value := points[index].DistanceKM
		if value == nil {
			continue
		}
		invalid := *value < 0
		if previousKnown && *value+0.01 < previous {
			invalid = true
		}
		if summary != nil {
			limit := *summary * 1.02
			if limit < *summary+0.10 {
				limit = *summary + 0.10
			}
			if *value > limit {
				invalid = true
			}
		}
		if invalid {
			points[index].DistanceKM = nil
			*warnings = appendBoundedWarnings(*warnings, NormalizationWarning{Code: "IMPLAUSIBLE_METRIC_DISTANCE", Severity: "warning", Message: "A cumulative distance point was excluded as implausible."})
			continue
		}
		previous = *value
		previousKnown = true
	}
}

func min(a, b int) int { return int(math.Min(float64(a), float64(b))) }
