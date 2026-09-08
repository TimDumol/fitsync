package fitnessdata

import "fmt"

// FormatDuration is the single display formatter for normalized durations.
// Calculations continue to use seconds and callers may safely ignore this
// convenience field.
func FormatDuration(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	whole := int(seconds + 0.5)
	return fmt.Sprintf("%d:%02d", whole/60, whole%60)
}

func FormatPace(minutesPerKM float64) string {
	if minutesPerKM < 0 {
		minutesPerKM = 0
	}
	whole := int(minutesPerKM*60 + 0.5)
	return fmt.Sprintf("%d:%02d/km", whole/60, whole%60)
}
