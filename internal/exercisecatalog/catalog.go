// Package exercisecatalog contains only explicit exercise-key mappings. It
// deliberately does not infer muscles from display names.
package exercisecatalog

var groups = map[string][]string{
	"back_squat":        {"quads", "glutes", "hamstrings"},
	"front_squat":       {"quads", "glutes"},
	"deadlift":          {"hamstrings", "glutes", "back"},
	"bench_press":       {"chest", "triceps", "shoulders"},
	"overhead_press":    {"shoulders", "triceps"},
	"pull_up":           {"back", "biceps"},
	"barbell_row":       {"back", "biceps"},
	"romanian_deadlift": {"hamstrings", "glutes"},
}

func Groups(exerciseKey string) []string {
	value := groups[exerciseKey]
	return append([]string(nil), value...)
}
