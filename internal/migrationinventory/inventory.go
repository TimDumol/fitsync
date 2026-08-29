// Package migrationinventory records the expected synthetic migration counts.
package migrationinventory

// ExpectedNeonDumpInventory is the committed, non-sensitive inventory used by
// migration checks. It describes counts only; it intentionally contains no
// personal payloads or identity values.
var ExpectedNeonDumpInventory = struct {
	GarminRecords    int
	Activities       int
	ActivityDetails  int
	BodyBattery      int
	BodyMeasurements int
	DailyRecovery    int
	Sleep            int
	TrainingSnapshot int
	WorkoutSessions  int
	WorkoutEntries   int
}{
	GarminRecords:    205,
	Activities:       34,
	ActivityDetails:  34,
	BodyBattery:      39,
	BodyMeasurements: 10,
	DailyRecovery:    39,
	Sleep:            39,
	TrainingSnapshot: 10,
	WorkoutSessions:  1,
	WorkoutEntries:   16,
}
