package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timdumol/fitsync/internal/db"
)

func TestRawIngestTokenBindingRevisionAndArchiveDeduplication(t *testing.T) {
	handler, database, archiveRoot := testHandler(t)
	payload := json.RawMessage(`{"activityId":42,"latitude":40.1,"polyline":"gps"}`)
	body, contentType := multipartBatch(t, []recordFixture{{dataset: "activities", key: "activity-42", payload: payload}}, nil, "")
	response := serveBatch(t, handler, body, contentType, "tim-secret")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"athlete":"tim"`) {
		t.Fatalf("initial ingest = %d %s", response.Code, response.Body.String())
	}
	var stored, storedHash string
	if err := database.QueryRow("SELECT payload_raw, payload_sha256 FROM garmin_records WHERE record_key='activity-42'").Scan(&stored, &storedHash); err != nil {
		t.Fatal(err)
	}
	if stored != string(payload) {
		t.Fatalf("raw payload was changed: %q", stored)
	}
	expected := sha256.Sum256(payload)
	if storedHash != hexString(expected[:]) {
		t.Fatalf("server hash = %q, want calculated hash", storedHash)
	}
	var archiveCount int
	if err := database.QueryRow("SELECT count(*) FROM garmin_archives").Scan(&archiveCount); err != nil {
		t.Fatal(err)
	}
	if archiveCount != 0 {
		t.Fatalf("archive count without archive part = %d", archiveCount)
	}

	// The same bytes and record key are a no-op for the record and archive.
	body, contentType = multipartBatch(t, []recordFixture{{dataset: "activities", key: "activity-42", payload: payload}}, []byte("fit-data"), "safe.fit")
	if response := serveBatch(t, handler, body, contentType, "tim-secret"); response.Code != http.StatusOK {
		t.Fatalf("archive ingest = %d %s", response.Code, response.Body.String())
	}
	body, contentType = multipartBatch(t, []recordFixture{{dataset: "activities", key: "activity-42", payload: payload}}, []byte("fit-data"), "safe.fit")
	if response := serveBatch(t, handler, body, contentType, "tim-secret"); response.Code != http.StatusOK {
		t.Fatalf("duplicate archive ingest = %d %s", response.Code, response.Body.String())
	}
	if err := database.QueryRow("SELECT count(*) FROM garmin_archives").Scan(&archiveCount); err != nil {
		t.Fatal(err)
	}
	if archiveCount != 1 {
		t.Fatalf("deduplicated archive count = %d, want 1", archiveCount)
	}
	entries, err := os.ReadDir(filepath.Join(archiveRoot, "archives"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("stored archive files = %d, want 1", len(entries))
	}

	revision := json.RawMessage(`{"activityId":42,"latitude":41.2,"polyline":"revised-gps"}`)
	body, contentType = multipartBatch(t, []recordFixture{{dataset: "activities", key: "activity-42", payload: revision}}, nil, "")
	if response := serveBatch(t, handler, body, contentType, "tim-secret"); response.Code != http.StatusOK {
		t.Fatalf("revision ingest = %d %s", response.Code, response.Body.String())
	}
	if err := database.QueryRow("SELECT payload_raw FROM garmin_records WHERE record_key='activity-42'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != string(revision) {
		t.Fatalf("revision payload = %q", stored)
	}
	if response := serveBatch(t, handler, body, contentType, "wrong-secret"); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", response.Code)
	}
}

func TestIngestRejectsTraversalAndRollsBackArchiveFailure(t *testing.T) {
	handler, database, archiveRoot := testHandler(t)
	payload := json.RawMessage(`{"activityId":7}`)
	body, contentType := multipartBatch(t, []recordFixture{{dataset: "activities", key: "activity-7", payload: payload}}, []byte("fit-data"), "../../escape.fit")
	if response := serveBatch(t, handler, body, contentType, "tim-secret"); response.Code != http.StatusBadRequest {
		t.Fatalf("traversal status = %d, want 400", response.Code)
	}
	var count int
	if err := database.QueryRow("SELECT count(*) FROM garmin_records").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("traversal request inserted a record")
	}

	body, contentType = multipartBatch(t, []recordFixture{{dataset: "activities", key: "activity-7", payload: payload}}, []byte("fit-data"), "safe.fit")
	// Rebuild metadata with an activity association that does not exist. The
	// record insert and renamed archive must both roll back/clean up.
	body = replaceActivityKey(t, body, contentType, "missing-activity", &contentType)
	if response := serveBatch(t, handler, body, contentType, "tim-secret"); response.Code != http.StatusInternalServerError {
		t.Fatalf("atomic failure status = %d, want 500", response.Code)
	}
	if err := database.QueryRow("SELECT count(*) FROM garmin_records").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed archive transaction inserted a record")
	}
	entries, err := os.ReadDir(filepath.Join(archiveRoot, "archives"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed archive transaction left %d files", len(entries))
	}
}

func TestIngestBoundsAndReconcile(t *testing.T) {
	handler, database, archiveRoot := testHandler(t)
	body, contentType := multipartBatch(t, []recordFixture{{dataset: "activities", key: "activity-8", payload: json.RawMessage(`{"activityId":8}`)}}, nil, "")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/garmin/batch", bytes.NewReader(body))
	request.ContentLength = maxRequestSize + 1
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Authorization", "Bearer tim-secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request status = %d, want 413", recorder.Code)
	}

	body, contentType = multipartBatch(t, []recordFixture{{dataset: "activities", key: "activity-8", payload: json.RawMessage(`{"activityId":8}`)}}, []byte("fit-data"), "safe.fit")
	if response := serveBatch(t, handler, body, contentType, "tim-secret"); response.Code != http.StatusOK {
		t.Fatalf("reconcile fixture ingest = %d", response.Code)
	}
	var storagePath, archiveID string
	if err := database.QueryRow("SELECT id, storage_path FROM garmin_archives").Scan(&archiveID, &storagePath); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(archiveRoot, "archives", "orphan.bin")
	if err := os.WriteFile(orphan, []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE garmin_archives SET expires_at=?, created_at=? WHERE id=?", 1, 1, archiveID); err != nil {
		t.Fatal(err)
	}
	if err := handler.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(archiveRoot, storagePath)); !os.IsNotExist(err) {
		t.Fatalf("expired archive still exists: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan archive still exists: %v", err)
	}
	var remaining int
	if err := database.QueryRow("SELECT count(*) FROM garmin_archives").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("expired archive metadata remaining = %d", remaining)
	}
}

type recordFixture struct {
	dataset string
	key     string
	payload json.RawMessage
}

func testHandler(t *testing.T) (*Handler, *sql.DB, string) {
	t.Helper()
	database, err := db.OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	archiveRoot := t.TempDir()
	t.Cleanup(func() { database.Close() })
	hash := sha256.Sum256([]byte("tim-secret"))
	return &Handler{DB: database, ArchiveRoot: archiveRoot, TokenHashes: map[string][sha256.Size]byte{"tim": hash}}, database, archiveRoot
}

func multipartBatch(t *testing.T, records []recordFixture, archive []byte, filename string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata := make([]map[string]any, 0, len(records))
	for _, record := range records {
		metadata = append(metadata, map[string]any{
			"dataset": record.dataset, "record_key": record.key, "record_date": "2026-01-02", "payload_raw": record.payload,
		})
	}
	archives := []map[string]any{}
	if archive != nil {
		uploadID := "upload-1"
		archives = append(archives, map[string]any{"upload_id": uploadID, "original_filename": filename, "content_type": "application/octet-stream"})
		part, err := writer.CreateFormFile("archive_"+uploadID, filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(archive); err != nil {
			t.Fatal(err)
		}
	}
	metadataPart, err := writer.CreateFormField("records")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(metadataPart).Encode(map[string]any{"records": metadata, "archives": archives}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func replaceActivityKey(t *testing.T, body []byte, contentType, activityKey string, updatedContentType *string) []byte {
	t.Helper()
	// This helper intentionally rebuilds a tiny equivalent request, retaining
	// synthetic payload bytes while changing only server-side association metadata.
	var rebuilt bytes.Buffer
	writer := multipart.NewWriter(&rebuilt)
	part, err := writer.CreateFormFile("archive_upload-1", "safe.fit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("fit-data")); err != nil {
		t.Fatal(err)
	}
	metadataPart, err := writer.CreateFormField("records")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(metadataPart).Encode(map[string]any{
		"records":  []map[string]any{{"dataset": "activities", "record_key": "activity-7", "record_date": "2026-01-02", "payload_raw": json.RawMessage(`{"activityId":7}`)}},
		"archives": []map[string]any{{"upload_id": "upload-1", "original_filename": "safe.fit", "content_type": "application/octet-stream", "activity_record_key": activityKey}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	*updatedContentType = writer.FormDataContentType()
	return rebuilt.Bytes()
}

func serveBatch(t *testing.T, handler http.Handler, body []byte, contentType, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/garmin/batch", bytes.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func hexString(value []byte) string {
	const alphabet = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for i, byteValue := range value {
		result[i*2] = alphabet[byteValue>>4]
		result[i*2+1] = alphabet[byteValue&15]
	}
	return string(result)
}
