package fitnessdata

import (
	"encoding/json"
	"math"
	"testing"
)

func TestDecodeSleepFormatsAndZeros(t *testing.T) {
	raw, err := DecodeSleep("sleep", json.RawMessage(`{"sleep":{"dailySleepDTO":{"sleepTimeSeconds":0,"sleepScores":{"overall":{"value":0}}}}}`))
	if err != nil || raw.Value.MainHours == nil || *raw.Value.MainHours != 0 || raw.Value.Score == nil || *raw.Value.Score != 0 {
		t.Fatalf("raw decode = %#v, %v", raw, err)
	}
	legacy, err := DecodeSleep("sleep", json.RawMessage(`{"sleep_seconds":25200,"sleep_score":82}`))
	if err != nil || legacy.Value.MainHours == nil || *legacy.Value.MainHours != 7 {
		t.Fatalf("legacy decode = %#v, %v", legacy, err)
	}
}

func TestDecodeSleepCanonicalNapSemantics(t *testing.T) {
	value, err := DecodeSleep("sleep", json.RawMessage(`{"sleep":{"dailySleepDTO":{"sleepTimeSeconds":22608,"napTimeSeconds":1800,"sleepScores":{"overall":{"value":59}}}}}`))
	if err != nil || value.Value.MainSleepHours == nil || value.Value.NapSleepHours == nil || value.Value.TotalSleep24H == nil {
		t.Fatalf("canonical sleep = %#v, %v", value, err)
	}
	if *value.Value.MainSleepHours != 6.28 || *value.Value.NapSleepHours != 0.5 || *value.Value.TotalSleep24H != 6.78 || *value.Value.TotalHours != 6.78 {
		t.Fatalf("canonical hours = %#v", value.Value)
	}

	missing, err := DecodeSleep("sleep", json.RawMessage(`{"sleep":{"dailySleepDTO":{"sleepTimeSeconds":21600}}}`))
	if err != nil || missing.Value.NapSleepHours != nil || missing.Value.TotalSleep24H != nil {
		t.Fatalf("missing nap source = %#v, %v", missing, err)
	}
	noNaps, err := DecodeNapEvents("body_battery_events", json.RawMessage(`{"events":[]}`))
	if err != nil || !noNaps.Value.Available || len(noNaps.Value.Naps) != 0 {
		t.Fatalf("explicit no-nap source = %#v, %v", noNaps, err)
	}
}

func TestDecodeNapEventsDeduplicatesDetailedEventsAndUsesLocalTimes(t *testing.T) {
	raw := json.RawMessage(`{"events":[{"eventType":"nap","eventStartTimeLocal":"2026-09-04T13:00:00+02:00","eventEndTimeLocal":"2026-09-04T13:30:00+02:00"}],"bodyBatteryEvents":[{"event_type":"nap","start_local":"2026-09-04T13:00:00+02:00","end_local":"2026-09-04T13:30:00+02:00"}]}`)
	value, err := DecodeNapEvents("body_battery_events", raw)
	if err != nil || len(value.Value.Naps) != 1 {
		t.Fatalf("nap events = %#v, %v", value, err)
	}
	if value.Value.Naps[0].DurationSeconds != 1800 || value.Value.Naps[0].DurationDisplay != "30:00" {
		t.Fatalf("nap duration = %#v", value.Value.Naps[0])
	}
}

func TestDecodeActivityUnknownAndMalformed(t *testing.T) {
	value, err := DecodeActivity("activities", json.RawMessage(`{"activity_id":"a","activity_type":"dragon_boat","duration_seconds":600,"distance_km":1}`))
	if err != nil || value.Value.Category != "other" || value.Value.SourceType != "dragon_boat" {
		t.Fatalf("activity = %#v, %v", value, err)
	}
	if _, err := DecodeActivity("activities", json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed payload accepted")
	}
}

func TestDecodeDetailStripsCoordinates(t *testing.T) {
	value, err := DecodeActivityDetail("activity_details", json.RawMessage(`{"metric_time_series":[{"latitude":1,"longitude":2,"heart_rate":140}],"polyline":"secret"}`))
	if err != nil || len(value.Value.Metrics) != 1 || value.Value.Metrics[0].HeartRate == nil {
		t.Fatalf("detail = %#v, %v", value, err)
	}
}

func TestDecodeDetailMetricEnvelopeAndAvailability(t *testing.T) {
	value, err := DecodeActivityDetail("activity_details", json.RawMessage(`{
		"splits":[{"splitNumber":1,"distanceKm":1,"duration":300,"averageHR":150,"heartRateAtEnd":152}],
		"heart_rate_zones":[{"zoneNumber":1,"secsInZone":60}],
		"metric_time_series":{
			"metricDescriptors":[
				{"metricsIndex":0,"key":"directTimestamp","unit":"millisecond"},
				{"metricsIndex":1,"key":"directHeartRate"},
				{"metricsIndex":2,"key":"directDistance","unit":"meter"},
				{"metricsIndex":3,"key":"directSpeed","unit":"meterPerSecond"}
			],
			"activityDetailMetrics":[{"metrics":[1000000,140,1000,2.5]},{"metrics":[1030000,145,1500,2.6]}]
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !value.Completeness["splits"] || !value.Completeness["zones"] || !value.Completeness["metrics"] {
		t.Fatalf("availability = %#v", value.Completeness)
	}
	if len(value.Value.Metrics) != 2 || value.Value.Metrics[1].ElapsedSeconds != 30 || value.Value.Metrics[0].DistanceKM == nil || *value.Value.Metrics[0].DistanceKM != 1 || value.Value.Metrics[0].SpeedKPH == nil || *value.Value.Metrics[0].SpeedKPH != 9 {
		t.Fatalf("metrics = %#v", value.Value.Metrics)
	}
	if value.Value.Splits[0].EndHR == nil || *value.Value.Splits[0].EndHR != 152 {
		t.Fatalf("split end HR = %#v", value.Value.Splits[0])
	}

	empty, err := DecodeActivityDetail("activity_details", json.RawMessage(`{"activity_id":"a","metric_time_series":[]}`))
	if err != nil || empty.Completeness["metrics"] {
		t.Fatalf("empty detail = %#v, %v", empty, err)
	}
}

func TestDecodeDetailMetricUnitsWarningsAndSparseIndices(t *testing.T) {
	value, err := DecodeActivityDetail("activity_details", json.RawMessage(`{
		"distanceKm":5.25,
		"metric_time_series":{"metricDescriptors":[
			{"metricsIndex":7,"key":"directDistance","unit":"meter"},
			{"metricsIndex":2,"key":"directHeartRate","unit":"bpm"}],
			"activityDetailMetrics":[{"metrics":[{"metricsIndex":2,"value":140},{"metricsIndex":7,"value":24.58}]}]}
	}`))
	if err != nil || len(value.Value.Metrics) != 1 || value.Value.Metrics[0].HeartRate == nil || *value.Value.Metrics[0].HeartRate != 140 || value.Value.Metrics[0].DistanceKM == nil || math.Abs(*value.Value.Metrics[0].DistanceKM-0.02458) > 1e-9 {
		t.Fatalf("sparse descriptor decode = metrics=%#v, distance=%v, hr=%v, %v", value.Value.Metrics, *value.Value.Metrics[0].DistanceKM, *value.Value.Metrics[0].HeartRate, err)
	}
	// A 24.58 m value is 0.02458 km before validation; the synthetic payload
	// has no activity summary distance, so unit conversion remains observable.

	unknown, err := DecodeActivityDetail("activity_details", json.RawMessage(`{"metric_time_series":{"metricDescriptors":[{"metricsIndex":0,"key":"directDistance"}],"activityDetailMetrics":[{"metrics":[10]}]}}`))
	if err != nil || len(unknown.Warnings) == 0 || unknown.Warnings[0].Code != "UNKNOWN_METRIC_DISTANCE_UNIT" || len(unknown.Value.Metrics) != 0 {
		t.Fatalf("unknown distance unit = %#v, %v", unknown, err)
	}
}
