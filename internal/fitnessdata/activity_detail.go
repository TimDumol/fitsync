package fitnessdata

import (
	"encoding/json"
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
	result.Metrics = decodeMetrics(cleanObject)
	result.SourcePoints = len(result.Metrics)
	result.ReturnedPoints = len(result.Metrics)
	return DecoderResult[ActivityDetail]{Value: result, Completeness: Availability{
		"splits": len(result.Splits) > 0, "zones": len(result.HRZones) > 0 || len(result.PowerZones) > 0, "metrics": len(result.Metrics) > 0,
	}}, nil
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
		result = append(result, Split{Number: n, DistanceKM: firstNumber(m, "distanceKm", "distance_km"), DurationSeconds: firstNumber(m, "duration", "duration_seconds"), PaceMinPerKM: firstNumber(m, "averagePace", "pace_min_per_km", "average_pace_min_per_km"), AverageHR: firstNumber(m, "averageHR", "average_hr", "average_heart_rate")})
	}
	if len(result) == 0 {
		return nil
	}
	return result
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

func decodeMetrics(object map[string]any) []MetricPoint {
	value := nested(object, "metricTimeSeries", "metric_time_series", "metrics", "metricsData")
	if items, ok := value.([]any); ok {
		return decodeMetricItems(items)
	}
	series, ok := value.(map[string]any)
	if !ok {
		return nil
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

func decodeMetricItems(items []any) []MetricPoint {
	result := make([]MetricPoint, 0, min(len(items), 5000))
	for index, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		point, recognized := decodeMetricItem(m)
		if !recognized {
			continue
		}
		if point.ElapsedSeconds == 0 && firstNumber(m, "elapsedSeconds", "elapsed_seconds", "timeOffset") == nil {
			point.ElapsedSeconds = float64(index)
		}
		result = append(result, point)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

type metricDescriptor struct {
	Key   string
	Index int
	Unit  string
}

func decodeMetricSeries(series map[string]any) []MetricPoint {
	descriptorItems, ok := detailArray(nested(series, "metricDescriptors", "metric_descriptors"))
	if !ok {
		return nil
	}
	rows, ok := detailArray(nested(series, "activityDetailMetrics", "activity_detail_metrics", "rows"), "items")
	if !ok {
		return nil
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
	var timestampOrigin *float64
	for rowIndex, item := range rows {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		point := MetricPoint{}
		recognized := false
		hasElapsed := false
		if direct, ok := decodeMetricItem(m); ok {
			point = direct
			recognized = true
			hasElapsed = hasDirectElapsed(m)
		}
		values, ok := detailArray(nested(m, "metrics", "values"))
		if ok {
			for position, raw := range values {
				descriptor, exists := descriptors[position]
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
				if assignMetric(&point, descriptor.Key, descriptor.Unit, raw) {
					recognized = true
					if strings.Contains(normalizedMetricKey(descriptor.Key), "duration") || strings.Contains(normalizedMetricKey(descriptor.Key), "elapsed") {
						hasElapsed = true
					}
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
		return nil
	}
	return result
}

func decodeMetricItem(m map[string]any) (MetricPoint, bool) {
	point := MetricPoint{}
	recognized := false
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
	return point, recognized
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

func assignMetric(point *MetricPoint, key, unit string, raw any) bool {
	value := metricNumber(raw)
	if value == nil {
		return false
	}
	normalized := normalizedMetricKey(key)
	switch {
	case strings.Contains(normalized, "heartrate"):
		point.HeartRate = value
	case strings.Contains(normalized, "distance"):
		if strings.Contains(normalizedMetricKey(unit), "meter") && !strings.Contains(normalizedMetricKey(unit), "kilometer") {
			*value /= 1000
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
		return false
	}
	return true
}

func min(a, b int) int { return int(math.Min(float64(a), float64(b))) }
