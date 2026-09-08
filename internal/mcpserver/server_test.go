package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	fitnessauth "github.com/timdumol/fitsync/internal/auth"
	"github.com/timdumol/fitsync/internal/fitnessrepo"
	"github.com/timdumol/fitsync/internal/httpapi"
)

func TestNewRegistersTools(t *testing.T) {
	if _, err := New(&httpapi.API{}, &fitnessauth.Authenticator{}); err != nil {
		t.Fatalf("New returned error: %v", err)
	}
}

func TestCreateSchemasGenerateIDsAndDocumentIdempotency(t *testing.T) {
	for name, schema := range map[string]map[string]any{
		"plan":    planDraftSchema(),
		"session": plannedSessionSchema(),
	} {
		t.Run(name, func(t *testing.T) {
			required, ok := schema["required"].([]string)
			if !ok {
				t.Fatalf("required = %#v", schema["required"])
			}
			for _, field := range required {
				if field == "plan_id" || field == "session_id" {
					t.Errorf("%s must be optional when the server generates it", field)
				}
			}
			properties := schema["properties"].(map[string]any)
			if properties["idempotency_key"] == nil {
				t.Error("idempotency_key is missing from the input schema")
			}
		})
	}
	group := plannedStepGroupSchema()
	if group["additionalProperties"] != false || group["description"] == nil {
		t.Fatalf("group schema = %#v", group)
	}
	groupProperties := group["properties"].(map[string]any)
	if groupProperties["step_sequence_nos"].(map[string]any)["uniqueItems"] != true {
		t.Fatalf("group members are not unique: %#v", groupProperties["step_sequence_nos"])
	}

	id, err := newUUIDv7()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 {
		t.Fatalf("generated ID = %q, want UUIDv7", id)
	}
}

func TestPlanOutputSchemaAcceptsJSONObjects(t *testing.T) {
	planID, sessionID, stepID := "018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002", "018f0000-0000-7000-8000-000000000003"
	exercise := "Bulgarian split squat"
	value, err := toPlanResultOutput(fitnessrepo.PlanResult{
		Plan:     &fitnessrepo.TrainingPlan{ID: planID, Title: "Strength", Goal: json.RawMessage(`{"primary":"hypertrophy"}`), Status: "draft", Source: "mcp"},
		Sessions: []fitnessrepo.PlannedSession{{ID: sessionID, PlanID: planID, Date: "2026-09-04", Title: "Strength", Sport: "strength", SessionType: "full_body", Priority: "normal", Steps: []fitnessrepo.PlannedStep{{ID: stepID, SequenceNo: 1, StepType: "exercise", ExerciseName: &exercise, Prescription: json.RawMessage(`{"sets":[{"reps_min":8,"reps_max":12,"rir":2,"rest_seconds":120}]}`)}}, Groups: []fitnessrepo.PlannedStepGroup{{ID: "018f0000-0000-7000-8000-000000000004", Mode: "rounds", Rounds: 1, StepSequenceNos: []int{1, 2}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "plan"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, planResultOutput, error) {
		return nil, value, nil
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("MCP rejected object output: %v", result.Content)
	}
	var decoded map[string]any
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	plan := decoded["plan"].(map[string]any)
	if _, ok := plan["goal"].(map[string]any); !ok {
		t.Fatalf("goal = %#v, want object", plan["goal"])
	}
	step := decoded["sessions"].([]any)[0].(map[string]any)["steps"].([]any)[0].(map[string]any)
	if _, ok := step["prescription"].(map[string]any); !ok {
		t.Fatalf("prescription = %#v, want object", step["prescription"])
	}
}

func TestTrainingContextOutputSchemaAcceptsPlannedJSONObjects(t *testing.T) {
	planID, sessionID, stepID := "018f0000-0000-7000-8000-000000000011", "018f0000-0000-7000-8000-000000000012", "018f0000-0000-7000-8000-000000000013"
	exercise := "Bulgarian split squat"
	value, err := toTrainingContextOutput(fitnessrepo.TrainingContext{
		AthleteID:   "018f0000-0000-7000-8000-000000000010",
		AsOf:        "2026-09-07T00:00:00+02:00",
		Timezone:    "Europe/Paris",
		GeneratedAt: "2026-09-07T00:00:00+02:00",
		PlannedSessions: []fitnessrepo.PlannedSession{{
			ID: sessionID, PlanID: planID, Date: "2026-09-07", Title: "Strength", Sport: "strength",
			SessionType: "full_body", Priority: "normal",
			Steps: []fitnessrepo.PlannedStep{{
				ID: stepID, SequenceNo: 1, StepType: "exercise", ExerciseName: &exercise,
				Prescription: json.RawMessage(`{"sets":[{"reps_min":8,"reps_max":12}]}`),
			}},
		}},
		Flags: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "context"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, trainingContextOutput, error) {
		return nil, value, nil
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "context"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("MCP rejected object output: %v", result.Content)
	}
	var decoded map[string]any
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	planned := decoded["planned_sessions"].([]any)[0].(map[string]any)
	step := planned["steps"].([]any)[0].(map[string]any)
	if _, ok := step["prescription"].(map[string]any); !ok {
		t.Fatalf("prescription = %#v, want object", step["prescription"])
	}
}

func TestStrengthOutputSchemaAcceptsStructuredWorkout(t *testing.T) {
	workoutID := "018f0000-0000-7000-8000-000000000021"
	athleteID := "018f0000-0000-7000-8000-000000000022"
	plannedSessionID := "018f0000-0000-7000-8000-000000000023"
	stepID := "018f0000-0000-7000-8000-000000000024"
	entryID := "018f0000-0000-7000-8000-000000000025"
	exerciseKey := "back_squat"
	exerciseName := "Back squat"
	laterality := "bilateral"
	loadBasis := "total_external"
	reps := 8.0
	load := 100.0
	rir := 2.0
	value, err := toStrengthWorkoutOutput(fitnessrepo.StrengthWorkout{
		WorkoutID: workoutID, AthleteID: athleteID, PlannedSessionID: &plannedSessionID,
		Date: "2026-09-08", Title: "Today's structured strength", Source: "mcp",
		WorkoutType: "strength", Status: "completed", DurationMinutes: &load,
		Entries: []fitnessrepo.StrengthEntry{{
			EntryID: entryID, PlannedStepID: &stepID, SequenceNo: 1, EntryType: "set",
			ExerciseKey: &exerciseKey, ExerciseName: &exerciseName, Reps: &reps, LoadKG: &load,
			RIR: &rir, Laterality: &laterality, LoadBasis: &loadBasis, LoadSemantics: "explicit",
			MuscleGroups: []string{"quadriceps"}, DirectMuscleGroups: []string{"quadriceps"},
		}},
		Summary: fitnessrepo.StrengthSummary{
			WorkingSets: 1, MuscleGroupSets: map[string]int{"quadriceps": 1},
			HighEffortSets: 0, LoadMethod: "explicit_strength_load_v1",
			MuscleVolume: map[string]fitnessrepo.MuscleVolumeSummary{"quadriceps": {DirectSets: 1, WeightedSets: 1}},
		},
		PlannedSession: &fitnessrepo.PlannedSession{
			ID: plannedSessionID, PlanID: "018f0000-0000-7000-8000-000000000026", AthleteID: athleteID,
			Date: "2026-09-08", Title: "Today's structured strength", Sport: "strength",
			SessionType: "full_body", Priority: "normal", Status: "completed",
			Steps: []fitnessrepo.PlannedStep{{
				ID: stepID, SequenceNo: 1, StepType: "exercise", ExerciseKey: &exerciseKey,
				ExerciseName: &exerciseName, Prescription: json.RawMessage(`{"sets":[{"reps_min":8,"reps_max":10}]}`),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "strength"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, strengthWorkoutOutput, error) {
		return nil, value, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "strength-page"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, strengthPageOutput, error) {
		return nil, strengthPageOutput{Items: []strengthWorkoutOutput{value}, DataStatus: fitnessrepo.DataStatus{}}, nil
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, name := range []string{"strength", "strength-page"} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("MCP rejected structured strength output for %s: %v", name, result.Content)
		}
	}
}
