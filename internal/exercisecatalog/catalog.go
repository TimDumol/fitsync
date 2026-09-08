// Package exercisecatalog contains the reviewed exercise vocabulary used for
// identity normalization. It deliberately does not infer muscles from free
// text.
package exercisecatalog

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

const CatalogRevision = 1

type ExerciseDefinition struct {
	Key             string
	CanonicalName   string
	Description     string
	Aliases         []string
	DeprecatedKeys  []string
	DirectMuscles   []string
	IndirectMuscles []string
	SupportsE1RM    bool
}

type ResolutionStatus string

const (
	StatusExplicit      ResolutionStatus = "explicit_key"
	StatusExactAlias    ResolutionStatus = "exact_alias"
	StatusDeprecatedKey ResolutionStatus = "deprecated_key"
	StatusUnknown       ResolutionStatus = "unknown"
)

type Resolution struct {
	Key           *string
	CanonicalName string
	Status        ResolutionStatus
	MatchedAlias  string
	OriginalKey   string
}

type SearchResult struct {
	Key           string
	CanonicalName string
	Description   string
	MatchedAlias  string
	Score         int
}

type catalog struct {
	definitions []ExerciseDefinition
	byKey       map[string]ExerciseDefinition
	byAlias     map[string]string
	search      map[string][]searchAlias
}

type searchAlias struct {
	normalized string
	original   string
}

// Canonical keys are added automatically as aliases. The other aliases below
// are reviewed identity aliases, not fuzzy matching rules.
var definitions = []ExerciseDefinition{
	{Key: "back_squat", CanonicalName: "Back squat", Description: "Barbell squat with the load supported across the upper back.", Aliases: []string{"back squat", "barbell back squat"}, DirectMuscles: []string{"quads", "glutes"}, IndirectMuscles: []string{"hamstrings"}, SupportsE1RM: true},
	{Key: "front_squat", CanonicalName: "Front squat", Description: "Barbell squat with the load supported at the front of the shoulders.", Aliases: []string{"front squat"}, DirectMuscles: []string{"quads", "glutes"}, IndirectMuscles: []string{"hamstrings"}, SupportsE1RM: true},
	{Key: "goblet_squat", CanonicalName: "Goblet squat", Description: "Squat holding one dumbbell or kettlebell at the chest.", Aliases: []string{"goblet squat"}, DirectMuscles: []string{"quads", "glutes"}, IndirectMuscles: []string{"hamstrings"}, SupportsE1RM: true},
	{Key: "deadlift", CanonicalName: "Deadlift", Description: "Hip-hinge lift from the floor with the load held in front of the body.", Aliases: []string{"deadlift", "barbell deadlift"}, DirectMuscles: []string{"hamstrings", "glutes", "back"}, SupportsE1RM: true},
	{Key: "bench_press", CanonicalName: "Bench press", Description: "Horizontal press performed while lying on a bench.", Aliases: []string{"bench press", "barbell bench press"}, DirectMuscles: []string{"chest"}, IndirectMuscles: []string{"triceps", "shoulders"}, SupportsE1RM: true},
	{Key: "overhead_press", CanonicalName: "Overhead press", Description: "Press a weight vertically from shoulder height to overhead.", Aliases: []string{"overhead press", "shoulder press"}, DirectMuscles: []string{"shoulders"}, IndirectMuscles: []string{"triceps"}, SupportsE1RM: true},
	{Key: "pull_up", CanonicalName: "Pull-up", Description: "Vertical bodyweight pull with the hands pulling the body over a bar.", Aliases: []string{"pull up", "pull-up", "pullup"}, DirectMuscles: []string{"back"}, IndirectMuscles: []string{"biceps"}, SupportsE1RM: true},
	{Key: "barbell_row", CanonicalName: "Barbell row", Description: "Horizontal pull using a barbell while hinged at the hips.", Aliases: []string{"barbell row"}, DirectMuscles: []string{"back"}, IndirectMuscles: []string{"biceps"}, SupportsE1RM: true},
	{Key: "one_arm_dumbbell_row", CanonicalName: "One-arm dumbbell row", Description: "Unilateral horizontal pull using a dumbbell, with one arm at a time.", Aliases: []string{"one arm dumbbell row", "one-arm dumbbell row", "one arm db row", "db row", "dumbbell row", "oadbr", "single arm row", "single-arm row", "1-arm DB row — upper-back biased"}, DeprecatedKeys: []string{"dumbbell_row"}, DirectMuscles: []string{"back"}, IndirectMuscles: []string{"biceps"}, SupportsE1RM: true},
	{Key: "romanian_deadlift", CanonicalName: "Romanian deadlift", Description: "Controlled hip hinge from standing with the weight lowered along the legs.", Aliases: []string{"romanian deadlift", "rdl"}, DirectMuscles: []string{"hamstrings", "glutes"}, SupportsE1RM: true},
	{Key: "biceps_curl", CanonicalName: "Biceps curl", Description: "Elbow-flexion curl that trains the elbow flexors.", Aliases: []string{"biceps curl", "bicep curl", "dumbbell curl"}, DirectMuscles: []string{"biceps"}},
	{Key: "triceps_extension", CanonicalName: "Triceps extension", Description: "Elbow-extension exercise that trains the triceps.", Aliases: []string{"triceps extension", "tricep extension"}, DeprecatedKeys: []string{"standing_overhead_triceps_extension"}, DirectMuscles: []string{"triceps"}},

	{Key: "b_stance_romanian_deadlift", CanonicalName: "B-stance Romanian deadlift", Description: "Romanian deadlift with one leg assisting in a staggered B-stance.", Aliases: []string{"B-stance DB RDL"}, DirectMuscles: []string{"hamstrings", "glutes"}, SupportsE1RM: true},
	{Key: "bicycle_crunch", CanonicalName: "Bicycle crunch", Description: "Alternating trunk flexion and rotation performed in a supine bicycle pattern.", Aliases: []string{"Bicycle crunch"}, DirectMuscles: []string{"abs"}},
	{Key: "bulgarian_split_squat", CanonicalName: "Bulgarian split squat", Description: "Rear-foot-elevated unilateral squat.", Aliases: []string{"Bulgarian split squat"}, DirectMuscles: []string{"quads", "glutes"}, IndirectMuscles: []string{"hamstrings"}, SupportsE1RM: true},
	{Key: "calf_raise", CanonicalName: "Calf raise", Description: "Ankle plantar-flexion raise with both legs unless another variation is specified.", Aliases: []string{"Calf raise"}, DirectMuscles: []string{"calves"}},
	{Key: "deficit_push_up", CanonicalName: "Deficit push-up", Description: "Push-up performed with the hands elevated on parallettes for extra range of motion.", Aliases: []string{"Deficit push-up on parallettes", "Deficit push-up on 17 cm parallettes"}, DirectMuscles: []string{"chest"}, IndirectMuscles: []string{"triceps", "shoulders"}, SupportsE1RM: true},
	{Key: "dip", CanonicalName: "Dip", Description: "Closed-chain pressing movement with the body lowered between supports.", Aliases: []string{"Dip"}, DirectMuscles: []string{"triceps", "chest"}, IndirectMuscles: []string{"shoulders"}, SupportsE1RM: true},
	{Key: "dumbbell_lateral_raise", CanonicalName: "Dumbbell lateral raise", Description: "Shoulder abduction using dumbbells, typically with one dumbbell in each hand.", Aliases: []string{"DB lateral raise", "Dumbbell lateral raise"}, DirectMuscles: []string{"shoulders"}},
	{Key: "hammer_curl", CanonicalName: "Hammer curl", Description: "Curl performed with a neutral grip to emphasize the brachialis and brachioradialis.", Aliases: []string{"Hammer curl"}, DirectMuscles: []string{"biceps"}},
	{Key: "hanging_knee_raise", CanonicalName: "Hanging knee raise", Description: "Raise bent knees while suspended from a bar or other overhead support.", Aliases: []string{"Hanging knee raise"}, DirectMuscles: []string{"abs"}},
	{Key: "lateral_raise", CanonicalName: "Lateral raise", Description: "Shoulder abduction movement without specifying a particular implement.", Aliases: []string{"Lateral raise"}, DirectMuscles: []string{"shoulders"}},
	{Key: "lying_leg_raise", CanonicalName: "Lying leg raise", Description: "Raise straight legs from a supine position.", Aliases: []string{"Leg raise"}, DirectMuscles: []string{"abs"}},
	{Key: "one_arm_dumbbell_press", CanonicalName: "One-arm dumbbell press", Description: "Unilateral dumbbell pressing movement without specifying a floor or overhead position.", Aliases: []string{"One-arm dumbbell press"}, DirectMuscles: []string{"chest"}, IndirectMuscles: []string{"triceps", "shoulders"}, SupportsE1RM: true},
	{Key: "one_arm_floor_press", CanonicalName: "One-arm floor press", Description: "Unilateral dumbbell press performed while lying on the floor.", Aliases: []string{"One-arm floor press"}, DirectMuscles: []string{"chest"}, IndirectMuscles: []string{"triceps", "shoulders"}, SupportsE1RM: true},
	{Key: "one_arm_overhead_press", CanonicalName: "One-arm overhead press", Description: "Unilateral press from the shoulder to an overhead lockout.", Aliases: []string{"1-arm DB overhead press"}, DirectMuscles: []string{"shoulders"}, IndirectMuscles: []string{"triceps"}, SupportsE1RM: true},
	{Key: "push_up", CanonicalName: "Push-up", Description: "Bodyweight horizontal press performed from a plank position.", Aliases: []string{"Push-up"}, DirectMuscles: []string{"chest"}, IndirectMuscles: []string{"triceps", "shoulders"}, SupportsE1RM: true},
	{Key: "reverse_crunch", CanonicalName: "Reverse crunch", Description: "Curl the pelvis and knees toward the torso from a supine position.", Aliases: []string{"Reverse crunch"}, DirectMuscles: []string{"abs"}},
	{Key: "reverse_wrist_curl", CanonicalName: "Reverse wrist curl", Description: "Wrist extension exercise performed with the palms facing down.", Aliases: []string{"Reverse wrist curl"}, DirectMuscles: []string{"forearms"}},
	{Key: "side_plank", CanonicalName: "Side plank", Description: "Lateral isometric trunk hold supported on one forearm or hand.", Aliases: []string{"Side plank"}, DirectMuscles: []string{"obliques"}},
	{Key: "single_arm_dumbbell_curl", CanonicalName: "Single-arm dumbbell curl", Description: "Unilateral dumbbell elbow-flexion curl performed one arm at a time.", Aliases: []string{"Single-arm dumbbell curl", "1-arm DB curl"}, DirectMuscles: []string{"biceps"}},
	{Key: "single_leg_calf_raise", CanonicalName: "Single-leg calf raise", Description: "Unilateral ankle plantar-flexion raise performed on one leg.", Aliases: []string{"Single-leg calf raise", "Single-leg loaded calf raise", "Single-leg loaded calf raise on slant board"}, DirectMuscles: []string{"calves"}},
	{Key: "sliding_hamstring_curl", CanonicalName: "Sliding hamstring curl", Description: "Hamstring curl using sliders or towels while bridging the hips.", Aliases: []string{"Sliding hamstring curl"}, DirectMuscles: []string{"hamstrings"}},
	{Key: "superman_dynamic", CanonicalName: "Dynamic superman", Description: "Dynamic prone back-extension movement lifting the arms and legs.", Aliases: []string{"Superman"}, DirectMuscles: []string{"back"}},
	{Key: "tibialis_raise", CanonicalName: "Tibialis raise", Description: "Dorsiflexion raise without specifying a slant-board variation.", Aliases: []string{"Tibialis raise"}, DirectMuscles: []string{"shins"}},
	{Key: "tibialis_raise_slant_board", CanonicalName: "Tibialis raise on slant board", Description: "Dorsiflexion raise performed with the heels elevated on a slant board.", Aliases: []string{"Tibialis raise (slant board)"}, DirectMuscles: []string{"shins"}},
}

var global = newCatalog(definitions)

func newCatalog(input []ExerciseDefinition) *catalog {
	c := &catalog{definitions: make([]ExerciseDefinition, 0, len(input)), byKey: map[string]ExerciseDefinition{}, byAlias: map[string]string{}, search: map[string][]searchAlias{}}
	for _, original := range input {
		definition := cloneDefinition(original)
		if !validKey(definition.Key) {
			panic(fmt.Sprintf("invalid exercise key %q", definition.Key))
		}
		if definition.CanonicalName == "" || len([]rune(definition.CanonicalName)) > 256 {
			panic(fmt.Sprintf("invalid canonical name for %s", definition.Key))
		}
		if len([]rune(definition.Description)) >= 300 {
			panic(fmt.Sprintf("exercise description too long for %s", definition.Key))
		}
		if _, exists := c.byKey[definition.Key]; exists {
			panic(fmt.Sprintf("duplicate exercise key %q", definition.Key))
		}
		c.byKey[definition.Key] = definition
		c.definitions = append(c.definitions, definition)
	}
	for _, definition := range c.definitions {
		aliases := append([]string{}, definition.Aliases...)
		aliases = append(aliases, definition.Key)
		for _, alias := range aliases {
			c.claimAlias(alias, definition.Key)
		}
		for _, deprecated := range definition.DeprecatedKeys {
			if !validKey(deprecated) {
				panic(fmt.Sprintf("invalid deprecated exercise key %q", deprecated))
			}
			c.claimAlias(deprecated, definition.Key)
		}
	}
	for _, definition := range c.definitions {
		aliases := append([]string{}, definition.Aliases...)
		aliases = append(aliases, definition.Key)
		for _, deprecated := range definition.DeprecatedKeys {
			aliases = append(aliases, deprecated)
		}
		seen := map[string]bool{}
		for _, alias := range aliases {
			normalized := NormalizeAlias(alias)
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			c.search[definition.Key] = append(c.search[definition.Key], searchAlias{normalized: normalized, original: alias})
		}
	}
	return c
}

func (c *catalog) claimAlias(alias, key string) {
	normalized := NormalizeAlias(alias)
	if normalized == "" {
		panic(fmt.Sprintf("empty exercise alias for %s", key))
	}
	if old, exists := c.byAlias[normalized]; exists && old != key {
		panic(fmt.Sprintf("exercise alias %q claimed by %s and %s", alias, old, key))
	}
	c.byAlias[normalized] = key
}

func cloneDefinition(value ExerciseDefinition) ExerciseDefinition {
	value.Aliases = append([]string(nil), value.Aliases...)
	value.DeprecatedKeys = append([]string(nil), value.DeprecatedKeys...)
	value.DirectMuscles = append([]string(nil), value.DirectMuscles...)
	value.IndirectMuscles = append([]string(nil), value.IndirectMuscles...)
	return value
}

func validKey(value string) bool {
	if len(value) < 1 || len(value) > 128 || value[0] == '_' || value[len(value)-1] == '_' || strings.Contains(value, "__") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func NormalizeAlias(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	space := false
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			space = builder.Len() > 0
			continue
		}
		if space {
			builder.WriteByte(' ')
			space = false
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}

func IsCanonicalKey(key string) bool {
	_, ok := global.byKey[strings.TrimSpace(key)]
	return ok
}

func DefinitionForKeyOrDeprecated(key string) (ExerciseDefinition, string, bool) {
	canonical := strings.TrimSpace(key)
	if definition, ok := global.byKey[canonical]; ok {
		return cloneDefinition(definition), canonical, true
	}
	resolved, ok := global.byAlias[NormalizeAlias(canonical)]
	if !ok {
		return ExerciseDefinition{}, "", false
	}
	definition, ok := global.byKey[resolved]
	if !ok {
		return ExerciseDefinition{}, "", false
	}
	for _, candidate := range definition.DeprecatedKeys {
		if candidate == canonical {
			return cloneDefinition(definition), resolved, true
		}
	}
	return ExerciseDefinition{}, "", false
}

func NormalizeExercise(value string) (string, bool) {
	key, ok := global.byAlias[NormalizeAlias(value)]
	return key, ok
}

func Resolve(exerciseKey, exerciseName *string) (Resolution, error) {
	var result Resolution
	key := ""
	if exerciseKey != nil {
		key = strings.TrimSpace(*exerciseKey)
		result.OriginalKey = *exerciseKey
	}
	name := ""
	if exerciseName != nil {
		name = strings.TrimSpace(*exerciseName)
	}
	if key != "" {
		definition, canonical, ok := DefinitionForKeyOrDeprecated(key)
		if !ok {
			return Resolution{}, errors.New("invalid_input: unknown exercise_key")
		}
		result.Key = stringPointer(canonical)
		result.CanonicalName = definition.CanonicalName
		result.Status = StatusExplicit
		if canonical != key {
			result.Status = StatusDeprecatedKey
		}
		if name != "" {
			if named, exact := NormalizeExercise(name); exact && named != canonical {
				return Resolution{}, errors.New("invalid_input: exercise_key conflicts with exercise_name")
			}
			if _, exact := NormalizeExercise(name); exact {
				result.MatchedAlias = name
			}
		}
		return result, nil
	}
	if name != "" {
		if canonical, ok := NormalizeExercise(name); ok {
			definition := global.byKey[canonical]
			result.Key = stringPointer(canonical)
			result.CanonicalName = definition.CanonicalName
			result.Status = StatusExactAlias
			result.MatchedAlias = name
			return result, nil
		}
	}
	result.Status = StatusUnknown
	return result, nil
}

func stringPointer(value string) *string { return &value }

func Definition(key string) (ExerciseDefinition, bool) {
	value, ok := global.byKey[key]
	if !ok {
		return ExerciseDefinition{}, false
	}
	return cloneDefinition(value), true
}

func Definitions() []ExerciseDefinition {
	result := make([]ExerciseDefinition, 0, len(global.definitions))
	for _, definition := range global.definitions {
		result = append(result, cloneDefinition(definition))
	}
	return result
}

func DirectGroups(exerciseKey string) []string {
	value, ok := global.byKey[exerciseKey]
	if !ok {
		return nil
	}
	return append([]string(nil), value.DirectMuscles...)
}

func IndirectGroups(exerciseKey string) []string {
	value, ok := global.byKey[exerciseKey]
	if !ok {
		return nil
	}
	return append([]string(nil), value.IndirectMuscles...)
}

// Groups preserves the old undifferentiated read projection.
func Groups(exerciseKey string) []string {
	value, ok := global.byKey[exerciseKey]
	if !ok {
		return nil
	}
	return append(append([]string(nil), value.DirectMuscles...), value.IndirectMuscles...)
}

func Search(query string, limit int) []SearchResult {
	normalizedQuery := NormalizeAlias(query)
	if normalizedQuery == "" {
		return []SearchResult{}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 10 {
		limit = 10
	}
	queryTokens := strings.Fields(normalizedQuery)
	results := make([]SearchResult, 0)
	for _, definition := range global.definitions {
		best := SearchResult{Key: definition.Key, CanonicalName: definition.CanonicalName, Description: definition.Description}
		for _, alias := range global.search[definition.Key] {
			score := searchScore(queryTokens, strings.Fields(alias.normalized))
			if score > best.Score {
				best.Score = score
				best.MatchedAlias = alias.original
			}
		}
		if best.Score >= 50 {
			results = append(results, best)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Key < results[j].Key
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func searchScore(query, alias []string) int {
	if equalTokens(query, alias) {
		return 100
	}
	if containsTokens(query, alias) {
		return 90
	}
	if containsTokens(alias, query) {
		return 80
	}
	querySet := map[string]bool{}
	aliasSet := map[string]bool{}
	for _, token := range query {
		querySet[token] = true
	}
	for _, token := range alias {
		aliasSet[token] = true
	}
	intersection := 0
	for token := range querySet {
		if aliasSet[token] {
			intersection++
		}
	}
	union := len(querySet)
	for token := range aliasSet {
		if !querySet[token] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return int(math.Round(100 * float64(intersection) / float64(union)))
}

func equalTokens(left, right []string) bool {
	return len(left) == len(right) && containsTokens(left, right) && containsTokens(right, left)
}

func containsTokens(container, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(container) {
		return false
	}
	for start := 0; start <= len(container)-len(needle); start++ {
		matched := true
		for offset := range needle {
			if container[start+offset] != needle[offset] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
