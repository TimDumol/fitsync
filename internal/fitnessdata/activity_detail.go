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
	result.HRZones = decodeZones(cleanObject, "hrZones", "hr_zones")
	result.PowerZones = decodeZones(cleanObject, "powerZones", "power_zones")
	result.Metrics = decodeMetrics(cleanObject)
	result.SourcePoints = len(result.Metrics)
	result.ReturnedPoints = len(result.Metrics)
	return DecoderResult[ActivityDetail]{Value: result, Completeness: Availability{
		"splits": result.Splits != nil, "zones": result.HRZones != nil || result.PowerZones != nil, "metrics": result.Metrics != nil,
	}}, nil
}

func stripLocation(value any) any {
	locationKeys := map[string]bool{"latitude": true, "longitude": true, "lat": true, "lon": true, "map": true, "route": true, "polyline": true, "startCoordinate": true, "endCoordinate": true, "start_coordinate": true, "end_coordinate": true, "location": true, "coordinates": true}
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			lower := strings.ToLower(key)
			if !locationKeys[key] && !strings.Contains(lower, "latitude") && !strings.Contains(lower, "longitude") && !strings.Contains(lower, "polyline") && !strings.Contains(lower, "coordinate") {
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
	value := nested(object, "splits", "laps", "activityLaps")
	items, ok := value.([]any)
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
		result = append(result, Split{Number: n, DistanceKM: firstNumber(m, "distanceKm", "distance_km"), DurationSeconds: firstNumber(m, "duration", "duration_seconds"), PaceMinPerKM: firstNumber(m, "averagePace", "pace_min_per_km"), AverageHR: firstNumber(m, "averageHR", "average_hr")})
	}
	return result
}

func decodeZones(object map[string]any, paths ...string) []Zone {
	items, ok := nested(object, paths...).([]any)
	if !ok {
		return nil
	}
	result := make([]Zone, 0, min(len(items), 20))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok || i >= 20 {
			break
		}
		result = append(result, Zone{Zone: i + 1, Low: firstNumber(m, "zoneLow", "low"), High: firstNumber(m, "zoneHigh", "high"), Minutes: firstNumber(m, "minutes", "duration_minutes")})
	}
	return result
}

func decodeMetrics(object map[string]any) []MetricPoint {
	value := nested(object, "metricTimeSeries", "metric_time_series", "metrics", "metricsData")
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]MetricPoint, 0, min(len(items), 5000))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		elapsed := firstNumber(m, "elapsedSeconds", "elapsed_seconds", "timeOffset")
		if elapsed == nil {
			elapsed = new(float64)
		}
		result = append(result, MetricPoint{ElapsedSeconds: *elapsed, DistanceKM: firstNumber(m, "distanceKm", "distance_km"), HeartRate: firstNumber(m, "heartRate", "heart_rate"), PaceMinPerKM: firstNumber(m, "pace", "pace_min_per_km"), SpeedKPH: firstNumber(m, "speed", "speed_kph"), Cadence: firstNumber(m, "cadence"), Power: firstNumber(m, "power"), ElevationM: firstNumber(m, "elevation", "elevation_m"), Respiration: firstNumber(m, "respiration"), Temperature: firstNumber(m, "temperature")})
	}
	return result
}

func min(a, b int) int { return int(math.Min(float64(a), float64(b))) }
