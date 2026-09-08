package fitnessanalysis

import (
	"testing"

	"github.com/timdumol/fitsync/internal/fitnessdata"
)

func TestClassifyHardActivityBoundariesAndAllReasons(t *testing.T) {
	value := 3.5
	zone19 := 19.99
	zone20 := 20.0
	effort := 8.0
	cases := []struct {
		name    string
		value   fitnessdata.ActivitySummary
		planned string
		rule    string
	}{
		{name: "aerobic boundary", value: fitnessdata.ActivitySummary{AerobicEffect: &value}, rule: "aerobic_effect_gte_3_5"},
		{name: "anaerobic boundary", value: fitnessdata.ActivitySummary{AnaerobicEffect: &value}, rule: "anaerobic_effect_gte_3_5"},
		{name: "zone below boundary", value: fitnessdata.ActivitySummary{HRZoneMinutes: []*float64{nil, nil, nil, &zone19}}, rule: "not_hard"},
		{name: "zone boundary", value: fitnessdata.ActivitySummary{HRZoneMinutes: []*float64{nil, nil, nil, &zone20}}, rule: "z4_z5_minutes_gte_20"},
		{name: "perceived effort boundary", value: fitnessdata.ActivitySummary{PerceivedEffort: &effort}, rule: "perceived_effort_gte_8"},
		{name: "planned type", value: fitnessdata.ActivitySummary{}, planned: "vo2max", rule: "linked_planned_session_type_hard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ClassifyHardActivity(tc.value, tc.planned)
			if tc.rule == "not_hard" {
				if result.Hard {
					t.Fatalf("19.99 minutes classified hard: %#v", result)
				}
				return
			}
			if !result.Hard || len(result.Reasons) == 0 || result.Method != "hard_session_v2" {
				t.Fatalf("classification = %#v", result)
			}
			found := false
			for _, reason := range result.Reasons {
				if reason.Rule == tc.rule {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing reason %q in %#v", tc.rule, result.Reasons)
			}
		})
	}
}

func TestNormalizeSessionTypeDoesNotGuessNames(t *testing.T) {
	if got := NormalizeSessionType("VO2-Max"); got != "vo2" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeSessionType("my interval-ish workout"); got == "intervals" {
		t.Fatalf("guessed free text as interval")
	}
}
