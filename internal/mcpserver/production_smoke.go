package mcpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	fitnessauth "github.com/timdumol/fitsync/internal/auth"
	"github.com/timdumol/fitsync/internal/httpapi"
)

const missingSmokeWorkoutID = "00000000-0000-0000-0000-000000000099"

type smokeAthlete struct {
	id         string
	timezone   *time.Location
	activityID string
	workoutID  string
}

// RunProductionReadSmoke executes every read-only MCP action against the
// supplied database and validates each returned value through the MCP SDK.
// It deliberately does not invoke write actions: the release check must be
// safe to run against the live production database.
func RunProductionReadSmoke(ctx context.Context, database *sql.DB, now time.Time) (int, error) {
	if database == nil {
		return 0, errors.New("MCP production smoke requires a database")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if now.IsZero() {
		now = time.Now()
	}

	api := httpapi.NewAPI(database, httpapi.FakeHouseholdAuthorizer{})
	server, err := New(api, &fitnessauth.Authenticator{})
	if err != nil {
		return 0, fmt.Errorf("create MCP server: %w", err)
	}
	if err := validateRegisteredTools(ctx, server.mcp); err != nil {
		return 0, err
	}

	athletes, err := loadSmokeAthletes(ctx, database)
	if err != nil {
		return 0, err
	}
	if len(athletes) == 0 {
		return 0, errors.New("MCP production smoke found no athletes")
	}
	for _, athlete := range athletes {
		if err := server.smokeAthlete(ctx, athlete, now); err != nil {
			return 0, err
		}
	}
	return len(athletes), nil
}

func validateRegisteredTools(ctx context.Context, server *mcp.Server) error {
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return fmt.Errorf("connect MCP inventory probe: %w", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fitnessd-release-smoke", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("connect MCP inventory client: %w", err)
	}
	defer clientSession.Close()
	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list MCP tools: %w", err)
	}
	expected := make(map[string]bool, len(mcpActionNames))
	for _, name := range mcpActionNames {
		if expected[name] {
			return fmt.Errorf("MCP action inventory contains duplicate %q", name)
		}
		expected[name] = true
	}
	seen := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		if seen[tool.Name] {
			return fmt.Errorf("MCP tool inventory contains duplicate %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			return fmt.Errorf("MCP tool %q is missing an input or output schema", tool.Name)
		}
		if !expected[tool.Name] {
			return fmt.Errorf("MCP exposed unexpected tool %q", tool.Name)
		}
	}
	for _, name := range mcpActionNames {
		if !seen[name] {
			return fmt.Errorf("MCP action %q is not exposed", name)
		}
	}
	if len(result.Tools) != len(mcpActionNames) {
		return fmt.Errorf("MCP exposed %d tools, want %d", len(result.Tools), len(mcpActionNames))
	}
	return nil
}

func loadSmokeAthletes(ctx context.Context, database *sql.DB) ([]smokeAthlete, error) {
	rows, err := database.QueryContext(ctx, "SELECT id,timezone FROM athletes ORDER BY handle")
	if err != nil {
		return nil, fmt.Errorf("load MCP smoke athletes: %w", err)
	}
	defer rows.Close()
	var athletes []smokeAthlete
	for rows.Next() {
		var athlete smokeAthlete
		var timezone string
		if err := rows.Scan(&athlete.id, &timezone); err != nil {
			return nil, fmt.Errorf("scan MCP smoke athlete: %w", err)
		}
		athlete.timezone, _ = time.LoadLocation(timezone)
		if athlete.timezone == nil {
			athlete.timezone = time.UTC
		}
		athletes = append(athletes, athlete)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read MCP smoke athletes: %w", err)
	}
	for index := range athletes {
		athletes[index].activityID = optionalSmokeID(ctx, database, "SELECT record_key FROM garmin_records WHERE athlete_id=? AND dataset='activities' ORDER BY record_date DESC,record_key DESC LIMIT 1", athletes[index].id)
		athletes[index].workoutID = optionalSmokeID(ctx, database, "SELECT id FROM workout_sessions WHERE athlete_id=? AND deleted_at IS NULL ORDER BY workout_date DESC,id DESC LIMIT 1", athletes[index].id)
	}
	return athletes, nil
}

func optionalSmokeID(ctx context.Context, database *sql.DB, query, athleteID string) string {
	var value string
	if err := database.QueryRowContext(ctx, query, athleteID).Scan(&value); err != nil {
		return ""
	}
	return value
}

func (s *Server) smokeAthlete(ctx context.Context, athlete smokeAthlete, now time.Time) error {
	localNow := now.In(athlete.timezone)
	to := localNow.Format("2006-01-02")
	from366 := localNow.AddDate(0, 0, -365).Format("2006-01-02")
	from730 := localNow.AddDate(0, 0, -729).Format("2006-01-02")
	principalCtx := fitnessauth.WithPrincipal(ctx, fitnessauth.Principal{
		AthleteID: athlete.id,
		Scopes:    map[string]bool{"fitness:read": true, "fitness:write": true},
	})
	called := make(map[string]bool, len(mcpReadActionNames))
	run := func(name string, action func() error) error {
		called[name] = true
		if err := action(); err != nil {
			return fmt.Errorf("MCP production smoke %s for athlete %s: %w", name, athlete.id, err)
		}
		return nil
	}

	if err := run("get_training_context", func() error {
		_, output, err := s.trainingContext(principalCtx, nil, TrainingContextInput{AsOf: to, LookbackDays: 28})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_training_context", output)
	}); err != nil {
		return err
	}
	if err := run("get_daily_health", func() error {
		_, output, err := s.dailyHealth(principalCtx, nil, DailyHealthInput{From: from366, To: to, Limit: 100, Order: "desc"})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_daily_health", output)
	}); err != nil {
		return err
	}
	if err := run("get_activities", func() error {
		_, output, err := s.activities(principalCtx, nil, ActivityInput{From: from366, To: to, Detail: "summary", Limit: 100, Order: "desc"})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_activities", output)
	}); err != nil {
		return err
	}
	if err := run("get_activity", func() error {
		if athlete.activityID == "" {
			return expectSmokeNotFound(func() error {
				_, _, err := s.activity(principalCtx, nil, ActivityDetailInput{ActivityID: "missing-activity"})
				return err
			})
		}
		_, output, err := s.activity(principalCtx, nil, ActivityDetailInput{ActivityID: athlete.activityID, Include: []string{"splits", "zones", "metrics"}, MetricResolutionSeconds: 30, MaxMetricPoints: 100})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_activity", output)
	}); err != nil {
		return err
	}
	if err := run("get_activity_intervals", func() error {
		if athlete.activityID == "" {
			return expectSmokeNotFound(func() error {
				_, _, err := s.activityIntervals(principalCtx, nil, ActivityIntervalsInput{ActivityID: "missing-activity"})
				return err
			})
		}
		_, output, err := s.activityIntervals(principalCtx, nil, ActivityIntervalsInput{ActivityID: athlete.activityID, IncludeLaps: true, IncludeRecoveries: true, MaxSegments: 50})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_activity_intervals", output)
	}); err != nil {
		return err
	}
	if err := run("get_training_timeline", func() error {
		_, output, err := s.timeline(principalCtx, nil, TimelineInput{From: from366, To: to, IncludeUnplanned: true, IncludeEntries: true, Order: "asc", Limit: 100})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_training_timeline", output)
	}); err != nil {
		return err
	}
	if err := run("get_readiness_for_session", func() error {
		_, output, err := s.readiness(principalCtx, nil, ReadinessInput{AsOf: now.UTC().Format(time.RFC3339), SessionDate: to, ProposedSessionType: "strength", ProposedDurationMinutes: 45, ProposedIntensity: "moderate"})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_readiness_for_session", output)
	}); err != nil {
		return err
	}
	if err := run("get_strength_workouts", func() error {
		_, output, err := s.strength(principalCtx, nil, StrengthInput{From: from730, To: to, IncludeEntries: true, Limit: 100, Order: "desc"})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_strength_workouts", output)
	}); err != nil {
		return err
	}
	if err := run("get_exercise_progress", func() error {
		_, output, err := s.exerciseProgress(principalCtx, nil, ExerciseProgressInput{ExerciseKey: "back_squat", From: from730, To: to, Limit: 100})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_exercise_progress", output)
	}); err != nil {
		return err
	}
	if err := run("resolve_exercises", func() error {
		_, output, err := s.resolveExercises(principalCtx, nil, ResolveExercisesInput{Exercises: []ResolveExerciseInput{{Ref: "exact", Name: "back squat"}, {Ref: "unknown", Name: "unusual movement"}}, MaxCandidates: 3})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "resolve_exercises", output)
	}); err != nil {
		return err
	}
	if err := run("get_checkins", func() error {
		_, output, err := s.checkins(principalCtx, nil, CheckinsInput{From: from366, To: to, Limit: 100, Order: "desc"})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_checkins", output)
	}); err != nil {
		return err
	}
	if err := run("get_athlete_profile", func() error {
		_, output, err := s.profile(principalCtx, nil, targetInput{})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_athlete_profile", output)
	}); err != nil {
		return err
	}
	if err := run("get_training_plan", func() error {
		_, output, err := s.plan(principalCtx, nil, PlanInput{From: from366, To: to, IncludeSteps: boolPointer(true), IncludeCompleted: boolPointer(true)})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_training_plan", output)
	}); err != nil {
		return err
	}
	if err := run("get_workout", func() error {
		if athlete.workoutID == "" {
			return expectSmokeNotFound(func() error {
				_, _, err := s.workout(principalCtx, nil, WorkoutInput{WorkoutID: missingSmokeWorkoutID})
				return err
			})
		}
		_, output, err := s.workout(principalCtx, nil, WorkoutInput{WorkoutID: athlete.workoutID})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_workout", output)
	}); err != nil {
		return err
	}
	if err := run("get_training_analysis", func() error {
		_, output, err := s.analysis(principalCtx, nil, AnalysisInput{From: from366, To: to, CompareWithPreviousPeriod: true, GroupBy: "week"})
		if err != nil {
			return err
		}
		return validateMCPOutput(principalCtx, "get_training_analysis", output)
	}); err != nil {
		return err
	}
	for _, name := range mcpReadActionNames {
		if !called[name] {
			return fmt.Errorf("MCP production smoke did not execute read action %q", name)
		}
	}
	return nil
}

func expectSmokeNotFound(action func() error) error {
	err := action()
	if err == nil || !strings.HasPrefix(err.Error(), "not_found:") {
		return fmt.Errorf("expected not_found for missing production fixture, got %v", err)
	}
	return nil
}

func boolPointer(value bool) *bool { return &value }

func validateMCPOutput[T any](ctx context.Context, name string, output T) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "fitnessd-output-smoke", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: name}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, T, error) {
		return nil, output, nil
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return fmt.Errorf("connect output validator for %s: %w", name, err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fitnessd-output-smoke-client", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("connect output validator client for %s: %w", name, err)
	}
	defer clientSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: name})
	if err != nil {
		return fmt.Errorf("call output validator for %s: %w", name, err)
	}
	if result.IsError {
		return fmt.Errorf("MCP output validation failed for %s: %v", name, result.Content)
	}
	return nil
}
