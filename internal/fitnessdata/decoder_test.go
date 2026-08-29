package fitnessdata

import (
	"encoding/json"
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
