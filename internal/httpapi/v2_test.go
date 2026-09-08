package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/timdumol/fitsync/internal/fitnessrepo"
)

func TestV2PlanAndWorkoutLifecycle(t *testing.T) {
	database := testAPIDB(t)
	repository := fitnessrepo.New(database, nil)
	planID, sessionID, stepID := newV7(t), newV7(t), newV7(t)
	name := "Deadlift"
	if _, err := repository.UpsertPlanDraft(context.Background(), timID, fitnessrepo.PlanWrite{
		Plan:     fitnessrepo.TrainingPlan{ID: planID, Title: "Test plan", Goal: json.RawMessage(`{"summary":"test"}`)},
		Sessions: []fitnessrepo.PlannedSessionWrite{{ID: sessionID, PlanID: planID, Date: "2026-08-29", Title: "Pull", Sport: "strength", SessionType: "full_body", Priority: "normal", Steps: []fitnessrepo.PlannedStep{{ID: stepID, SequenceNo: 1, StepType: "exercise", ExerciseName: &name}}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PublishPlan(context.Background(), timID, planID); err != nil {
		t.Fatal(err)
	}

	handler := Handler(NewAPI(database, FakeHouseholdAuthorizer{}))
	response := getStatus(t, handler, "/api/v2/athletes/"+timID+"/training-plans/active")
	if response.StatusCode != http.StatusOK || !strings.Contains(string(response.Body), "Test plan") || !strings.Contains(string(response.Body), "Deadlift") {
		t.Fatalf("active plan = %d %s", response.StatusCode, response.Body)
	}

	workoutID, entryID := newV7(t), newV7(t)
	body := map[string]any{"workout_id": workoutID, "athlete_id": timID, "date": "2026-08-29", "title": "Pull", "source": "mobile", "status": "in_progress", "planned_session_id": sessionID, "entries": []map[string]any{{"entry_id": entryID, "planned_step_id": stepID, "sequence_no": 1, "entry_type": "set", "reps": 5, "load_kg": 100}}}
	path := "/api/v2/athletes/" + timID + "/workouts/" + workoutID
	saved := postOrPutJSON(t, handler, http.MethodPut, path, body)
	if saved.StatusCode != http.StatusOK {
		t.Fatalf("save status = %d body=%s", saved.StatusCode, saved.Body)
	}
	body["status"] = "completed"
	completed := postOrPutJSON(t, handler, http.MethodPost, path+"/complete", body)
	if completed.StatusCode != http.StatusOK || !strings.Contains(string(completed.Body), "completed") {
		t.Fatalf("complete = %d %s", completed.StatusCode, completed.Body)
	}

	pull := getStatus(t, handler, "/api/v2/sync/pull?athlete_id="+timID+"&after=0&limit=50")
	if pull.StatusCode != http.StatusOK || !strings.Contains(string(pull.Body), "training_plan") || !strings.Contains(string(pull.Body), "workout") {
		t.Fatalf("pull = %d %s", pull.StatusCode, pull.Body)
	}
	var page struct {
		Changes []struct {
			Kind   string          `json:"kind"`
			Entity json.RawMessage `json:"entity"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(pull.Body, &page); err != nil {
		t.Fatalf("decode pull = %v", err)
	}
	for _, change := range page.Changes {
		if change.Kind != "workout" {
			continue
		}
		var entity map[string]json.RawMessage
		if err := json.Unmarshal(change.Entity, &entity); err != nil {
			t.Fatalf("decode workout entity = %v", err)
		}
		if _, ok := entity["summary"]; ok {
			t.Fatal("sync workout leaked the repository-only summary field")
		}
		if _, ok := entity["entries"]; !ok {
			t.Fatal("sync workout omitted entries")
		}
	}

	deleted := postOrPutJSON(t, handler, http.MethodDelete, path, nil)
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", deleted.StatusCode, deleted.Body)
	}
	if gone := getStatus(t, handler, path); gone.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted workout status = %d body=%s", gone.StatusCode, gone.Body)
	}
	if repeated := postOrPutJSON(t, handler, http.MethodDelete, path, nil); repeated.StatusCode != http.StatusNoContent {
		t.Fatalf("repeated delete status = %d body=%s", repeated.StatusCode, repeated.Body)
	}
}

func postOrPutJSON(t *testing.T, handler http.Handler, method, path string, value any) responseBody {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return responseBody{StatusCode: recorder.Code, Body: recorder.Body.Bytes()}
}
