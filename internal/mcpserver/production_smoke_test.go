package mcpserver

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	fitnessauth "github.com/timdumol/fitsync/internal/auth"
	"github.com/timdumol/fitsync/internal/db"
	"github.com/timdumol/fitsync/internal/httpapi"
)

func TestAllMCPActionsExposeInputAndOutputSchemas(t *testing.T) {
	server, err := New(&httpapi.API{}, &fitnessauth.Authenticator{})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRegisteredTools(context.Background(), server.mcp); err != nil {
		t.Fatal(err)
	}
	all := make(map[string]bool, len(mcpActionNames))
	for _, name := range mcpActionNames {
		all[name] = true
	}
	for _, name := range mcpReadActionNames {
		if !all[name] {
			t.Fatalf("read action %q is absent from the complete MCP action inventory", name)
		}
	}
	for _, name := range mcpWriteActionNames {
		if !all[name] {
			t.Fatalf("write action %q is absent from the complete MCP action inventory", name)
		}
	}
	if len(mcpReadActionNames)+len(mcpWriteActionNames) != len(mcpActionNames) {
		t.Fatalf("read/write MCP coverage lists do not partition the action inventory")
	}
}

func TestProductionReadSmokeOnEmptyDatabase(t *testing.T) {
	database, err := db.OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	athletes, err := RunProductionReadSmoke(context.Background(), database, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if athletes != 2 {
		t.Fatalf("smoke athletes = %d, want seeded household athletes", athletes)
	}
}

func TestAllWriteMCPActionsValidateBeforeMutation(t *testing.T) {
	database, err := db.OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := New(httpapi.NewAPI(database, httpapi.FakeHouseholdAuthorizer{}), &fitnessauth.Authenticator{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := fitnessauth.WithPrincipal(context.Background(), fitnessauth.Principal{
		AthleteID: "00000000-0000-0000-0000-000000000002",
		Scopes:    map[string]bool{"fitness:read": true, "fitness:write": true},
	})

	before := countWriteRows(t, database)
	checks := map[string]func() error{
		"upsert_workout": func() error {
			_, _, err := server.upsertWorkout(ctx, nil, UpsertWorkoutInput{ClientUpdatedAt: -1})
			return err
		},
		"patch_workout": func() error {
			_, _, err := server.patchWorkout(ctx, nil, PatchWorkoutInput{ExpectedSyncSeq: -1})
			return err
		},
		"delete_workout": func() error {
			_, _, err := server.deleteWorkout(ctx, nil, DeleteWorkoutInput{WorkoutID: "not-a-uuid"})
			return err
		},
		"record_checkin": func() error {
			_, _, err := server.recordCheckin(ctx, nil, CheckinInput{})
			return err
		},
		"record_quick_readiness": func() error {
			_, _, err := server.recordQuickReadiness(ctx, nil, QuickReadinessInput{})
			return err
		},
		"update_athlete_profile": func() error {
			_, _, err := server.updateProfile(ctx, nil, ProfileInput{Timezone: "not/a/timezone", Units: "metric"})
			return err
		},
		"upsert_training_plan_draft": func() error {
			_, _, err := server.upsertPlanDraft(ctx, nil, PlanDraftInput{PlanID: "not-a-uuid", Title: "invalid"})
			return err
		},
		"publish_training_plan": func() error {
			_, _, err := server.publishPlan(ctx, nil, PublishPlanInput{PlanID: "not-a-uuid"})
			return err
		},
		"upsert_planned_session": func() error {
			_, _, err := server.upsertPlannedSession(ctx, nil, PlannedSessionInput{PlanID: "not-a-uuid", SessionID: "not-a-uuid"})
			return err
		},
		"update_planned_session_status": func() error {
			_, _, err := server.updatePlannedStatus(ctx, nil, PlannedStatusInput{SessionID: "not-a-uuid"})
			return err
		},
		"update_workout": func() error {
			_, _, err := server.updateWorkout(ctx, nil, UpdateWorkoutInput{Kind: "invalid"})
			return err
		},
	}
	for _, name := range mcpWriteActionNames {
		action := checks[name]
		if action == nil {
			t.Fatalf("write MCP action %q has no validation case", name)
		}
		if err := action(); err == nil {
			t.Errorf("%s accepted an invalid no-op fixture", name)
		}
	}
	if after := countWriteRows(t, database); !reflect.DeepEqual(after, before) {
		t.Fatalf("write validation changed row counts: before=%v after=%v", before, after)
	}
}

func countWriteRows(t *testing.T, database interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range []string{"training_plans", "planned_sessions", "workout_sessions", "checkins", "athlete_profiles"} {
		var count int
		if err := database.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}
