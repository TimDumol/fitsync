// Package ingest accepts authenticated raw Garmin batches and manages archives.
package ingest

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	fitnessauth "github.com/timdumol/fitsync/internal/auth"
	"github.com/timdumol/fitsync/internal/config"
)

const (
	maxBatchRecords = 100
	maxRequestSize  = 100 << 20
	maxMetadataSize = 10 << 20
	maxArchiveSize  = 50 << 20
	archiveLifetime = 90 * 24 * time.Hour
)

// Handler is the authenticated Garmin ingestion endpoint.
type Handler struct {
	DB          *sql.DB
	ArchiveRoot string
	TokenHashes map[string][sha256.Size]byte
	OIDC        *fitnessauth.Authenticator
}

func NewHandler(database *sql.DB, cfg config.Config) *Handler {
	return &Handler{DB: database, ArchiveRoot: cfg.ArchiveRoot, TokenHashes: cfg.IngestTokenHashes}
}

// SetAuthenticator installs the OIDC boundary for the mobile routes. The
// legacy handler remains independently protected by its static token hashes.
func (h *Handler) SetAuthenticator(authenticator *fitnessauth.Authenticator) { h.OIDC = authenticator }

type batch struct {
	Records  []recordMetadata  `json:"records"`
	Archives []archiveMetadata `json:"archives"`
}

type recordMetadata struct {
	Dataset           string          `json:"dataset"`
	RecordKey         string          `json:"record_key"`
	RecordDate        string          `json:"record_date"`
	OccurredAtUTCMS   *int64          `json:"occurred_at_utc_ms"`
	TZOffsetSeconds   *int64          `json:"tz_offset_seconds"`
	PayloadRaw        json.RawMessage `json:"payload_raw"`
	ActivityRecordKey string          `json:"activity_record_key"`
}

type archiveMetadata struct {
	UploadID          string `json:"upload_id"`
	OriginalFilename  string `json:"original_filename"`
	ContentType       string `json:"content_type"`
	ActivityRecordKey string `json:"activity_record_key"`
}

type archiveUpload struct {
	metadata    archiveMetadata
	tempPath    string
	finalPath   string
	archiveID   string
	filename    string
	contentType string
	size        int64
	hash        string
}

// ServeHTTP authenticates the request, parses bounded multipart data, and
// commits records and archive metadata as one logical operation.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	athleteHandle, athleteID, err := h.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid ingest credentials")
		return
	}
	h.serveAuthorized(w, r, athleteHandle, athleteID)
}

// serveAuthorized is the one parsing and commit path shared by legacy and
// OIDC-authenticated ingestion. The caller has already resolved the athlete.
func (h *Handler) serveAuthorized(w http.ResponseWriter, r *http.Request, athleteHandle, athleteID string) {
	if r.ContentLength > maxRequestSize {
		writeError(w, http.StatusRequestEntityTooLarge, "request is too large")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	b, uploads, err := parseMultipart(r, h.ArchiveRoot)
	if err != nil {
		cleanupUploads(uploads)
		status := http.StatusBadRequest
		if errors.Is(err, errTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, err.Error())
		return
	}
	if err := validateBatch(b, uploads); err != nil {
		cleanupUploads(uploads)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	counts, err := h.commit(r.Context(), athleteID, b, uploads)
	if err != nil {
		cleanupUploads(uploads)
		writeError(w, http.StatusInternalServerError, "ingest transaction failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"athlete":  athleteHandle,
		"records":  counts.records,
		"archives": counts.archives,
	})
}

var errTooLarge = errors.New("request part exceeds its size limit")

func parseMultipart(r *http.Request, archiveRoot string) (batch, map[string]*archiveUpload, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return batch{}, nil, errors.New("request must be multipart/form-data")
	}
	// MultipartReader consumes the bounded request body without buffering the
	// full upload. The temporary archive files are placed outside the final
	// archive directory until their transaction is ready to commit.
	if err := os.MkdirAll(archiveRoot, 0750); err != nil {
		return batch{}, nil, fmt.Errorf("prepare archive staging: %w", err)
	}
	var b batch
	uploads := make(map[string]*archiveUpload)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return batch{}, uploads, fmt.Errorf("read multipart body: %w", err)
		}
		name := part.FormName()
		switch {
		case name == "records":
			data, err := io.ReadAll(io.LimitReader(part, maxMetadataSize+1))
			if err != nil {
				return batch{}, uploads, err
			}
			if int64(len(data)) > maxMetadataSize {
				return batch{}, uploads, errTooLarge
			}
			if err := json.Unmarshal(data, &b); err != nil {
				return batch{}, uploads, errors.New("records part is invalid JSON")
			}
		case strings.HasPrefix(name, "archive_"):
			uploadID := strings.TrimPrefix(name, "archive_")
			rawDisposition := part.Header.Get("Content-Disposition")
			if !validUploadID(uploadID) || strings.ContainsAny(part.FileName(), `/\`) || strings.Contains(rawDisposition, "../") || strings.Contains(rawDisposition, `..\`) {
				return batch{}, uploads, errors.New("archive upload ID or filename is invalid")
			}
			if _, exists := uploads[uploadID]; exists {
				return batch{}, uploads, errors.New("duplicate archive upload ID")
			}
			temp, err := os.CreateTemp(archiveRoot, ".fitnessd-upload-*")
			if err != nil {
				return batch{}, uploads, fmt.Errorf("create archive staging file: %w", err)
			}
			hasher := sha256.New()
			written, copyErr := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(part, maxArchiveSize+1))
			if copyErr == nil && written > maxArchiveSize {
				copyErr = errTooLarge
			}
			if copyErr == nil {
				copyErr = temp.Sync()
			}
			if closeErr := temp.Close(); copyErr == nil {
				copyErr = closeErr
			}
			if copyErr != nil {
				_ = os.Remove(temp.Name())
				return batch{}, uploads, copyErr
			}
			uploads[uploadID] = &archiveUpload{
				tempPath: temp.Name(), filename: part.FileName(), contentType: part.Header.Get("Content-Type"),
				size: written, hash: hex.EncodeToString(hasher.Sum(nil)),
			}
		}
	}
	if len(b.Records) == 0 {
		return batch{}, uploads, errors.New("records part is required")
	}
	return b, uploads, nil
}

func validUploadID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func validateBatch(b batch, uploads map[string]*archiveUpload) error {
	if len(b.Records) == 0 || len(b.Records) > maxBatchRecords {
		return errors.New("records batch must contain 1 to 100 records")
	}
	seen := make(map[string]struct{}, len(b.Records))
	for _, record := range b.Records {
		if record.Dataset == "" || record.RecordKey == "" || record.RecordDate == "" {
			return errors.New("record metadata has a missing required field")
		}
		if err := validDate(record.RecordDate); err != nil {
			return err
		}
		if len(record.PayloadRaw) == 0 || !json.Valid(record.PayloadRaw) {
			return errors.New("record payload_raw is invalid JSON")
		}
		if record.TZOffsetSeconds != nil && (*record.TZOffsetSeconds < -64800 || *record.TZOffsetSeconds > 64800) {
			return errors.New("record timezone offset is out of range")
		}
		key := record.Dataset + "\x00" + record.RecordKey
		if _, exists := seen[key]; exists {
			return errors.New("duplicate record in batch")
		}
		seen[key] = struct{}{}
	}
	seenArchives := make(map[string]struct{}, len(b.Archives))
	for _, archive := range b.Archives {
		if !validUploadID(archive.UploadID) {
			return errors.New("archive upload ID is invalid")
		}
		if _, exists := seenArchives[archive.UploadID]; exists {
			return errors.New("duplicate archive metadata")
		}
		seenArchives[archive.UploadID] = struct{}{}
		upload, exists := uploads[archive.UploadID]
		if !exists {
			return errors.New("archive metadata references a missing upload")
		}
		upload.metadata = archive
		if archive.OriginalFilename == "" {
			upload.metadata.OriginalFilename = upload.filename
		}
		if archive.ContentType == "" {
			upload.metadata.ContentType = upload.contentType
		}
	}
	if len(seenArchives) != len(uploads) {
		return errors.New("archive upload is not referenced by metadata")
	}
	return nil
}

func (h *Handler) authenticate(r *http.Request) (string, string, error) {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return "", "", errors.New("bearer credentials required")
	}
	credential := strings.TrimPrefix(value, "Bearer ")
	if credential == "" {
		return "", "", errors.New("empty credentials")
	}
	hash := sha256.Sum256([]byte(credential))
	matched := ""
	for athlete, expected := range h.TokenHashes {
		if subtle.ConstantTimeCompare(hash[:], expected[:]) == 1 {
			if matched != "" {
				return "", "", errors.New("credential maps to multiple athletes")
			}
			matched = athlete
		}
	}
	if matched == "" {
		return "", "", errors.New("credential not recognized")
	}
	var athleteID string
	if err := h.DB.QueryRow("SELECT id FROM athletes WHERE handle = ?", matched).Scan(&athleteID); err != nil {
		return "", "", err
	}
	return matched, athleteID, nil
}

type commitCounts struct {
	records  int
	archives int
}

func (h *Handler) commit(ctx context.Context, athleteID string, b batch, uploads map[string]*archiveUpload) (commitCounts, error) {
	archiveDir := filepath.Join(h.ArchiveRoot, "archives")
	if err := os.MkdirAll(archiveDir, 0750); err != nil {
		return commitCounts{}, err
	}
	newFiles := make([]string, 0, len(uploads))
	for _, upload := range uploads {
		upload.archiveID = uuid.NewString()
		upload.finalPath = filepath.Join(archiveDir, upload.archiveID+".bin")
		if err := os.Rename(upload.tempPath, upload.finalPath); err != nil {
			return commitCounts{}, err
		}
		upload.tempPath = ""
		newFiles = append(newFiles, upload.finalPath)
	}

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		removeFiles(newFiles)
		return commitCounts{}, err
	}
	rollback := func(cause error) (commitCounts, error) {
		_ = tx.Rollback()
		removeFiles(newFiles)
		return commitCounts{}, cause
	}
	for _, record := range b.Records {
		hash := sha256.Sum256(record.PayloadRaw)
		_, err := tx.ExecContext(ctx, `INSERT INTO garmin_records
			(id, athlete_id, dataset, record_key, record_date, occurred_at_utc_ms,
			tz_offset_seconds, payload_format, payload_raw, payload_sha256, ingested_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'raw_garmin_v1', ?, ?, ?)
			ON CONFLICT(athlete_id, dataset, record_key) DO UPDATE SET
				record_date=excluded.record_date, occurred_at_utc_ms=excluded.occurred_at_utc_ms,
				tz_offset_seconds=excluded.tz_offset_seconds, payload_format=excluded.payload_format,
				payload_raw=excluded.payload_raw, payload_sha256=excluded.payload_sha256,
				ingested_at=excluded.ingested_at
			WHERE garmin_records.payload_sha256 <> excluded.payload_sha256`,
			uuid.NewString(), athleteID, record.Dataset, record.RecordKey, record.RecordDate,
			nullableInt(record.OccurredAtUTCMS), nullableInt(record.TZOffsetSeconds), string(record.PayloadRaw), hex.EncodeToString(hash[:]), time.Now().UnixMilli())
		if err != nil {
			return rollback(err)
		}
	}
	archiveCount := 0
	for _, upload := range uploads {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM garmin_archives WHERE athlete_id=? AND content_sha256=? LIMIT 1`, athleteID, upload.hash).Scan(&existingID)
		if err == nil {
			if removeErr := os.Remove(upload.finalPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return rollback(removeErr)
			}
			newFiles = removeString(newFiles, upload.finalPath)
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return rollback(err)
		}
		var activityRecordID any
		if upload.metadata.ActivityRecordKey != "" {
			var id string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM garmin_records WHERE athlete_id=? AND dataset='activities' AND record_key=?`, athleteID, upload.metadata.ActivityRecordKey).Scan(&id); err != nil {
				return rollback(errors.New("archive references an unknown activity record"))
			}
			activityRecordID = id
		}
		contentType := upload.metadata.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		filename := displayFilename(upload.metadata.OriginalFilename)
		_, err = tx.ExecContext(ctx, `INSERT INTO garmin_archives
			(id, athlete_id, activity_record_id, original_filename, size_bytes, content_sha256,
			content_type, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			upload.archiveID, athleteID, activityRecordID, filename, upload.size, upload.hash,
			contentType, time.Now().UnixMilli(), time.Now().Add(archiveLifetime).UnixMilli())
		if err != nil {
			return rollback(err)
		}
		archiveCount++
	}
	if err := tx.Commit(); err != nil {
		removeFiles(newFiles)
		return commitCounts{}, err
	}
	return commitCounts{records: len(b.Records), archives: archiveCount}, nil
}

func nullableInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func displayFilename(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	return filepath.Base(value)
}

func cleanupUploads(uploads map[string]*archiveUpload) {
	for _, upload := range uploads {
		if upload.tempPath != "" {
			_ = os.Remove(upload.tempPath)
		}
		if upload.finalPath != "" {
			_ = os.Remove(upload.finalPath)
		}
	}
}

func removeFiles(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func removeString(values []string, target string) []string {
	for i, value := range values {
		if value == target {
			return append(values[:i], values[i+1:]...)
		}
	}
	return values
}

func validDate(value string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return errors.New("record_date must be YYYY-MM-DD")
	}
	return nil
}

// Reconcile removes archive metadata whose file is missing, removes expired
// metadata and files, and removes files that have no metadata row.
func (h *Handler) Reconcile(ctx context.Context) error {
	archiveDir := filepath.Join(h.ArchiveRoot, "archives")
	if err := os.MkdirAll(archiveDir, 0750); err != nil {
		return err
	}
	type metadata struct {
		id, storagePath      string
		expiresAt, createdAt int64
	}
	rows, err := h.DB.QueryContext(ctx, "SELECT id, storage_path, expires_at, created_at FROM garmin_archives")
	if err != nil {
		return err
	}
	defer rows.Close()
	all := make([]metadata, 0)
	for rows.Next() {
		var item metadata
		if err := rows.Scan(&item.id, &item.storagePath, &item.expiresAt, &item.createdAt); err != nil {
			return err
		}
		all = append(all, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	oldest := time.Now().Add(-archiveLifetime).UnixMilli()
	expected := make(map[string]struct{}, len(all))
	for _, item := range all {
		if filepath.IsAbs(item.storagePath) || filepath.Clean(item.storagePath) != item.storagePath || strings.HasPrefix(item.storagePath, "..") {
			return errors.New("archive metadata contains an unsafe storage path")
		}
		fullPath := filepath.Join(h.ArchiveRoot, item.storagePath)
		info, statErr := os.Stat(fullPath)
		expired := item.expiresAt <= now || item.createdAt <= oldest
		if expired {
			if statErr == nil {
				if err := os.Remove(fullPath); err != nil {
					return err
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
			if _, err := h.DB.ExecContext(ctx, "DELETE FROM garmin_archives WHERE id = ?", item.id); err != nil {
				return err
			}
			continue
		}
		if errors.Is(statErr, os.ErrNotExist) {
			if _, err := h.DB.ExecContext(ctx, "DELETE FROM garmin_archives WHERE id = ?", item.id); err != nil {
				return err
			}
			continue
		}
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return errors.New("archive storage path is not a regular file")
		}
		expected[filepath.Clean(item.storagePath)] = struct{}{}
	}
	return filepath.WalkDir(archiveDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(h.ArchiveRoot, path)
		if err != nil {
			return err
		}
		if _, ok := expected[filepath.Clean(relative)]; !ok {
			return os.Remove(path)
		}
		return nil
	})
}

// StartReconciler performs startup reconciliation and repeats it daily until
// ctx is cancelled.
func (h *Handler) StartReconciler(ctx context.Context) error {
	if err := h.Reconcile(ctx); err != nil {
		return err
	}
	return h.RunDaily(ctx)
}

// RunDaily repeats reconciliation until ctx is cancelled. Call Reconcile once
// before serving traffic when startup failures must prevent serving.
func (h *Handler) RunDaily(ctx context.Context) error {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := h.Reconcile(ctx); err != nil {
				return err
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
