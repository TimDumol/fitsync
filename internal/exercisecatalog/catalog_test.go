package exercisecatalog

import "testing"

func TestReviewedAliasesNormalizeToOneKey(t *testing.T) {
	for _, alias := range []string{"one-arm dumbbell row", "DB row", "OADBR", "single arm row"} {
		if key, ok := NormalizeExercise(alias); !ok || key != "one_arm_dumbbell_row" {
			t.Fatalf("alias %q -> %q, %v", alias, key, ok)
		}
	}
	if _, ok := NormalizeExercise("invented row variation"); ok {
		t.Fatal("unknown free text was normalized")
	}
}

func TestCatalogKeysAndProductionAliases(t *testing.T) {
	for _, definition := range Definitions() {
		key, ok := NormalizeExercise(definition.Key)
		if !ok || key != definition.Key {
			t.Errorf("canonical key %q -> %q, %v", definition.Key, key, ok)
		}
	}
	aliases := map[string]string{
		"B-stance DB RDL":                             "b_stance_romanian_deadlift",
		"Deficit push-up on 17 cm parallettes":        "deficit_push_up",
		"1-arm DB overhead press":                     "one_arm_overhead_press",
		"Single-leg loaded calf raise on slant board": "single_leg_calf_raise",
		"Tibialis raise (slant board)":                "tibialis_raise_slant_board",
	}
	for alias, want := range aliases {
		if key, ok := NormalizeExercise(alias); !ok || key != want {
			t.Errorf("alias %q -> %q, %v; want %q", alias, key, ok, want)
		}
	}
	for _, alternative := range []string{"Pull-up or chin-up", "Hanging knee raise or leg raise", "Side plank or long-lever plank", "Sliding hamstring curl or hamstring walkout"} {
		if _, ok := NormalizeExercise(alternative); ok {
			t.Errorf("alternative %q unexpectedly resolved", alternative)
		}
	}
}

func TestResolveStatusesAndConflicts(t *testing.T) {
	name := "Bulgarian split squat"
	resolved, err := Resolve(nil, &name)
	if err != nil || resolved.Status != StatusExactAlias || resolved.Key == nil || *resolved.Key != "bulgarian_split_squat" {
		t.Fatalf("exact resolution = %#v, %v", resolved, err)
	}
	deprecated := "dumbbell_row"
	resolved, err = Resolve(&deprecated, nil)
	if err != nil || resolved.Status != StatusDeprecatedKey || resolved.Key == nil || *resolved.Key != "one_arm_dumbbell_row" || resolved.OriginalKey != deprecated {
		t.Fatalf("deprecated resolution = %#v, %v", resolved, err)
	}
	conflicting := "bench_press"
	if _, err := Resolve(&conflicting, &name); err == nil || err.Error() != "invalid_input: exercise_key conflicts with exercise_name" {
		t.Fatalf("conflict error = %v", err)
	}
	unknown := "Pull-up or chin-up"
	resolved, err = Resolve(nil, &unknown)
	if err != nil || resolved.Status != StatusUnknown || resolved.Key != nil {
		t.Fatalf("unknown resolution = %#v, %v", resolved, err)
	}
}

func TestSearchIsBoundedAndTokenAware(t *testing.T) {
	results := Search("one arm db row — upper-back biased", 10)
	if len(results) == 0 || results[0].Key != "one_arm_dumbbell_row" || results[0].Score != 90 {
		t.Fatalf("search results = %#v", results)
	}
	if results := Search("diplomatic", 10); len(results) != 0 {
		t.Fatalf("token boundary search = %#v", results)
	}
	if results := Search("row", 100); len(results) > 10 {
		t.Fatalf("search limit = %d", len(results))
	}
	if results := Search("", 3); results == nil || len(results) != 0 {
		t.Fatalf("empty search = %#v", results)
	}
}

func TestCatalogConstructionRejectsDuplicateClaims(t *testing.T) {
	base := []ExerciseDefinition{{Key: "one", CanonicalName: "One", Description: "one"}}
	assertPanics := func(t *testing.T, definitions []ExerciseDefinition) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatal("newCatalog did not panic")
			}
		}()
		_ = newCatalog(definitions)
	}
	assertPanics(t, append(append([]ExerciseDefinition{}, base...), ExerciseDefinition{Key: "one", CanonicalName: "Duplicate", Description: "duplicate"}))
	assertPanics(t, []ExerciseDefinition{{Key: "one", CanonicalName: "One", Description: "one", Aliases: []string{"shared"}}, {Key: "two", CanonicalName: "Two", Description: "two", Aliases: []string{"SHARED"}}})
	assertPanics(t, []ExerciseDefinition{{Key: "one", CanonicalName: "One", Description: "one", DeprecatedKeys: []string{"old"}}, {Key: "two", CanonicalName: "Two", Description: "two", Aliases: []string{"old"}}})
}

func TestDefinitionsAreDefensiveCopies(t *testing.T) {
	definitions := Definitions()
	definitions[0].Aliases[0] = "changed"
	definitions[0].DirectMuscles[0] = "changed"
	again, ok := Definition(definitions[0].Key)
	if !ok || again.Aliases[0] == "changed" || again.DirectMuscles[0] == "changed" {
		t.Fatal("catalog returned mutable slices")
	}
}
