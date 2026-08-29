package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/timdumol/fitsync/internal/db"
)

const timID = "00000000-0000-0000-0000-000000000002"

func TestSyncPushLWWDeleteAndPull(t *testing.T) {
	database := testAPIDB(t)
	server := Handler(NewAPI(database, FakeHouseholdAuthorizer{}))

	sessionID := newV7(t)
	entryID := newV7(t)
	documentID := newV7(t)
	push := func(mutations ...map[string]any) SyncPushResponse {
		t.Helper()
		body := postJSON(t, server, "/api/v1/sync/push", map[string]any{"mutations": mutations})
		var response SyncPushResponse
		decodeJSON(t, body, &response)
		return response
	}
	baseSession := map[string]any{
		"kind": "session", "id": sessionID, "athlete_id": timID, "client_updated_at": 10,
		"deleted": false, "workout_date": "2026-01-02", "title": "first", "source": "mobile",
		"payload": map[string]any{"note": "synthetic"},
	}
	baseEntry := map[string]any{
		"kind": "entry", "id": entryID, "athlete_id": timID, "client_updated_at": 10,
		"deleted": false, "session_id": sessionID, "sequence_no": 1, "entry_type": "set",
		"payload": map[string]any{"reps": 5},
	}
	baseDocument := map[string]any{
		"kind": "document", "id": documentID, "athlete_id": timID, "client_updated_at": 10,
		"deleted": false, "document_key": "synthetic", "payload": map[string]any{"v": 1},
	}
	if got := len(push(baseSession, baseEntry, baseDocument).Winners); got != 3 {
		t.Fatalf("winners = %d, want 3", got)
	}

	stale := clone(baseSession)
	stale["client_updated_at"] = 9
	stale["title"] = "stale"
	if got := push(stale).Winners[0].Title; got == nil || *got != "first" {
		t.Fatalf("stale winner title = %v, want first", got)
	}
	equal := clone(baseSession)
	equal["title"] = "equal"
	if got := push(equal).Winners[0].Title; got == nil || *got != "first" {
		t.Fatalf("equal-time winner title = %v, want first", got)
	}

	fresh := clone(baseSession)
	fresh["client_updated_at"] = 11
	fresh["title"] = "newer"
	if got := push(fresh).Winners[0].Title; got == nil || *got != "newer" {
		t.Fatalf("newer winner title = %v, want newer", got)
	}
	deleted := map[string]any{"kind": "session", "id": sessionID, "athlete_id": timID, "client_updated_at": 12, "deleted": true}
	if !push(deleted).Winners[0].Deleted {
		t.Fatal("delete did not return a tombstone")
	}

	pullBody := get(t, server, "/api/v1/sync/pull?since=0")
	var pulled SyncPullResponse
	decodeJSON(t, pullBody, &pulled)
	if pulled.NextSince < 1 || len(pulled.Changes) != 3 {
		t.Fatalf("pull next_since/changes = %d/%d, want nonzero/3", pulled.NextSince, len(pulled.Changes))
	}
	var foundDeleted bool
	for _, change := range pulled.Changes {
		if change.Id == sessionID && change.Deleted {
			foundDeleted = true
		}
	}
	if !foundDeleted {
		t.Fatal("pull omitted session tombstone")
	}
}

func TestSyncPushRollbackPaginationAndBounds(t *testing.T) {
	database := testAPIDB(t)
	server := Handler(NewAPI(database, FakeHouseholdAuthorizer{}))

	sessionID := newV7(t)
	entryA, entryB := newV7(t), newV7(t)
	body := map[string]any{"mutations": []map[string]any{
		{"kind": "session", "id": sessionID, "athlete_id": timID, "client_updated_at": 1, "deleted": false, "workout_date": "2026-01-02", "title": "rollback", "source": "test"},
		{"kind": "entry", "id": entryA, "athlete_id": timID, "client_updated_at": 1, "deleted": false, "session_id": sessionID, "sequence_no": 1, "entry_type": "set"},
		{"kind": "entry", "id": entryB, "athlete_id": timID, "client_updated_at": 1, "deleted": false, "session_id": sessionID, "sequence_no": 1, "entry_type": "set"},
	}}
	response := postJSONStatus(t, server, "/api/v1/sync/push", body)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("rollback status = %d, want 422", response.StatusCode)
	}
	var count int
	if err := database.QueryRow("SELECT count(*) FROM workout_sessions WHERE id = ?", sessionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed batch left its session behind")
	}

	for i, date := range []string{"2026-01-01", "2026-01-02"} {
		_, err := database.Exec(`INSERT INTO garmin_records
			(id, athlete_id, dataset, record_key, record_date, payload_format, payload_raw, payload_sha256, ingested_at)
			VALUES (?, ?, 'activities', ?, ?, 'test', '{}', ?, ?)`,
			"page-"+string(rune('a'+i)), timID, "page-key-"+string(rune('a'+i)), date, strings.Repeat("a", 64), i+1)
		if err != nil {
			t.Fatal(err)
		}
	}
	page := get(t, server, "/api/v1/athletes/"+timID+"/activities?from=2026-01-01&to=2026-01-02&limit=1")
	var first ActivityPage
	decodeJSON(t, page, &first)
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("first page items/cursor = %d/%v", len(first.Items), first.NextCursor)
	}
	second := get(t, server, "/api/v1/athletes/"+timID+"/activities?from=2026-01-01&to=2026-01-02&limit=1&cursor="+*first.NextCursor)
	var next ActivityPage
	decodeJSON(t, second, &next)
	if len(next.Items) != 1 || next.Items[0].RecordKey != "page-key-b" {
		t.Fatalf("second page = %#v", next.Items)
	}

	invalid := getStatus(t, server, "/api/v1/athletes/not-a-uuid/activities?from=2026-01-01&to=2026-01-02")
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid UUID status = %d, want 400", invalid.StatusCode)
	}
	mutations := make([]map[string]any, maxSyncBatch+1)
	for i := range mutations {
		mutations[i] = map[string]any{"kind": "document", "id": newV7(t), "athlete_id": timID, "client_updated_at": i, "deleted": true, "document_key": "bound"}
	}
	tooLarge := postJSONStatus(t, server, "/api/v1/sync/push", map[string]any{"mutations": mutations})
	if tooLarge.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized batch status = %d, want 413", tooLarge.StatusCode)
	}
}

func testAPIDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newV7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func clone(value map[string]any) map[string]any {
	copy := make(map[string]any, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}

func postJSON(t *testing.T, handler http.Handler, path string, value any) []byte {
	t.Helper()
	response := postJSONStatus(t, handler, path, value)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status = %d, body = %s", path, response.StatusCode, response.Body)
	}
	return response.Body
}

type responseBody struct {
	StatusCode int
	Body       []byte
}

func postJSONStatus(t *testing.T, handler http.Handler, path string, value any) responseBody {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return responseBody{StatusCode: recorder.Code, Body: recorder.Body.Bytes()}
}

func get(t *testing.T, handler http.Handler, path string) []byte {
	t.Helper()
	response := getStatus(t, handler, path)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, body = %s", path, response.StatusCode, response.Body)
	}
	return response.Body
}

func getStatus(t *testing.T, handler http.Handler, path string) responseBody {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return responseBody{StatusCode: recorder.Code, Body: recorder.Body.Bytes()}
}

func decodeJSON(t *testing.T, body []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(body, value); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
}
