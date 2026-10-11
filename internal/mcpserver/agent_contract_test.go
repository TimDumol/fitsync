package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/timdumol/fitsync/internal/fitnessrepo"
)

func TestPlanFiltersApplyToBothReadModes(t *testing.T) {
	for _, planID := range []string{"", "specific-plan"} {
		t.Run(planID, func(t *testing.T) {
			value := fitnessrepo.PlanResult{Plan: &fitnessrepo.TrainingPlan{}, Sessions: []fitnessrepo.PlannedSession{
				{Date: "2026-10-01", Status: "planned"},
				{Date: "2026-10-02", Status: "completed"},
				{Date: "2026-10-03", Status: "planned", Steps: []fitnessrepo.PlannedStep{{ID: "step"}}, Groups: []fitnessrepo.PlannedStepGroup{{ID: "group"}}},
			}}
			result, err := filterPlanResult(value, PlanInput{PlanID: planID, From: "2026-10-02", To: "2026-10-03", IncludeCompleted: boolPtr(false), IncludeSteps: boolPtr(false)})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Sessions) != 1 || result.Sessions[0].Date != "2026-10-03" || len(result.Sessions[0].Steps) != 0 || len(result.Sessions[0].Groups) != 0 || len(result.Plan.Sessions) != 1 {
				t.Fatalf("incorrect filtered result: %+v", result)
			}
		})
	}
	for _, in := range []PlanInput{
		{PlanID: "id", From: "2026-10-01"},
		{PlanID: "id", From: "2026-02-30", To: "2026-03-01"},
		{PlanID: "id", From: "2026-10-03", To: "2026-10-01"},
		{PlanID: "id", From: "2025-01-01", To: "2026-01-02"},
		{},
	} {
		if _, err := filterPlanResult(fitnessrepo.PlanResult{}, in); err == nil {
			t.Errorf("accepted invalid range: %+v", in)
		}
	}
	if _, err := filterPlanResult(fitnessrepo.PlanResult{}, PlanInput{PlanID: "id"}); err != nil {
		t.Fatalf("ID-only read rejected: %v", err)
	}
}

func TestAgentSchemasValidateThroughMCP(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	calls := 0
	mcp.AddTool(server, &mcp.Tool{Name: "plan", InputSchema: planSchema()}, func(context.Context, *mcp.CallToolRequest, PlanInput) (*mcp.CallToolResult, map[string]any, error) {
		calls++
		return nil, map[string]any{}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "patch", InputSchema: patchWorkoutSchema()}, func(context.Context, *mcp.CallToolRequest, PatchWorkoutInput) (*mcp.CallToolResult, map[string]any, error) {
		calls++
		return nil, map[string]any{}, nil
	})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	cases := []struct {
		name      string
		arguments map[string]any
		valid     bool
	}{
		{"plan", map[string]any{"plan_id": "id"}, true},
		{"plan", map[string]any{"from": "2026-10-01", "to": "2026-10-03"}, true},
		{"plan", map[string]any{}, false},
		{"plan", map[string]any{"plan_id": "id", "from": "2026-10-01"}, false},
		{"patch", patchArguments(map[string]any{"op": "append_note", "text": "hello"}), true},
		{"patch", patchArguments(map[string]any{"op": "set_entry_rir", "entry_id": "id"}), false},
		{"patch", patchArguments(map[string]any{"op": "append_note", "text": "hello", "rir": 2}), false},
		{"patch", patchArguments(map[string]any{"op": "set_entry_muscle_roles", "entry_id": "id"}), false},
	}
	for _, c := range cases {
		before := calls
		result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: c.name, Arguments: c.arguments})
		if c.valid {
			if err != nil || result.IsError || calls != before+1 {
				t.Fatalf("valid %s rejected: %v %+v", c.name, err, result)
			}
		} else if calls != before || (err == nil && !result.IsError) {
			t.Fatalf("invalid %s reached handler: %+v", c.name, c.arguments)
		}
	}
}

func patchArguments(operation map[string]any) map[string]any {
	return map[string]any{"workout_id": "id", "expected_sync_seq": 1, "client_updated_at": 2, "operations": []any{operation}}
}

func TestToolLoggingIncludesPayloadsAndDistinguishesErrors(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "get_workout", Arguments: json.RawMessage(`{"workout_id":"test-id"}`)}}
	for _, c := range []struct {
		result  mcp.Result
		err     error
		outcome string
	}{
		{&mcp.CallToolResult{StructuredContent: map[string]any{"value": 42}}, nil, "success"},
		{&mcp.CallToolResult{IsError: true}, nil, "tool_error"},
		{nil, errors.New("bad input"), "protocol_error"},
	} {
		output.Reset()
		result, err := logToolCalls(func(context.Context, string, mcp.Request) (mcp.Result, error) { return c.result, c.err })(context.Background(), "tools/call", req)
		if result != c.result || err != c.err {
			t.Fatal("logging changed handler result")
		}
		var entry map[string]any
		if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		if entry["tool"] != "get_workout" || entry["outcome"] != c.outcome || !strings.Contains(output.String(), "test-id") {
			t.Fatalf("incomplete log: %s", output.String())
		}
		if c.outcome == "success" && !strings.Contains(output.String(), "42") {
			t.Fatalf("result payload missing: %s", output.String())
		}
	}
	output.Reset()
	_, _ = logToolCalls(func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, nil })(context.Background(), "tools/list", req)
	if output.Len() != 0 {
		t.Fatal("logged non-tool call")
	}
}
