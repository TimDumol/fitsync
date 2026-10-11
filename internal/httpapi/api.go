package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/timdumol/fitsync/internal/exercisecatalog"
	"github.com/timdumol/fitsync/internal/fitnessrepo"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
	maxSyncBatch    = 100
	maxRequestBody  = 1 << 20
)

// API implements the generated OpenAPI server interface.
type API struct {
	db           *sql.DB
	auth         Authorizer
	repo         *fitnessrepo.Repository
	garminBatch  http.Handler
	garminStatus http.Handler
}

// AuthMiddleware lets the production transport authenticate once per HTTP
// request. This matters for a 100-record sync push: each target is still
// checked below, while JWT verification and rate limiting happen once.
func (a *API) AuthMiddleware(next http.Handler) http.Handler {
	if middleware, ok := a.auth.(interface {
		Middleware(http.Handler) http.Handler
	}); ok {
		return middleware.Middleware(next)
	}
	return next
}

func NewAPI(database *sql.DB, authorizer Authorizer) *API {
	if authorizer == nil {
		authorizer = denyAuthorizer{}
	}
	return &API{db: database, auth: authorizer, repo: fitnessrepo.New(database, time.Now)}
}

// SetGarminRoutes lets the generated OpenAPI adapter and the small explicit
// server mux share the ingestion implementation without duplicating parsing
// or transaction logic in the general API layer.
func (a *API) SetGarminRoutes(provider interface {
	MobileBatchHandler() http.Handler
	MobileStatusHandler() http.Handler
}) {
	a.garminBatch = provider.MobileBatchHandler()
	a.garminStatus = provider.MobileStatusHandler()
}

// Database exposes the shared store to application services such as MCP.
// Callers must still apply transport authorization before using it.
func (a *API) Database() *sql.DB {
	if a == nil {
		return nil
	}
	return a.db
}

func (a *API) GetDailyHealth(w http.ResponseWriter, r *http.Request, id AthleteID, params GetDailyHealthParams) {
	if err := a.auth.Authorize(r, id.String()); err != nil {
		writeAuthorizationError(w, err)
		return
	}
	a.getGarminPage(w, r, id.String(), params.From.String(), params.To.String(), params.Limit, params.Cursor, false)
}

func (a *API) GetActivities(w http.ResponseWriter, r *http.Request, id AthleteID, params GetActivitiesParams) {
	if err := a.auth.Authorize(r, id.String()); err != nil {
		writeAuthorizationError(w, err)
		return
	}
	a.getGarminPage(w, r, id.String(), params.From.String(), params.To.String(), params.Limit, params.Cursor, true)
}

func (a *API) getGarminPage(w http.ResponseWriter, r *http.Request, athleteID, from, to string, requestedLimit *Limit, requestedCursor *Cursor, activitiesOnly bool) {
	limit, err := pageSize(requestedLimit)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	if err := validDateRange(from, to); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursor *recordCursor
	if requestedCursor != nil && *requestedCursor != "" {
		cursor, err = decodeRecordCursor(*requestedCursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
	}
	if !a.athleteExists(r.Context(), athleteID) {
		writeError(w, http.StatusNotFound, "athlete not found")
		return
	}

	query := `SELECT id, dataset, record_key, record_date, payload_raw, ingested_at
		FROM garmin_records WHERE athlete_id = ? AND record_date BETWEEN ? AND ?`
	args := []any{athleteID, from, to}
	if activitiesOnly {
		query += " AND dataset = 'activities'"
	} else {
		query += " AND dataset NOT IN ('activities', 'activity_details')"
	}
	if cursor != nil {
		query += " AND (record_date > ? OR (record_date = ? AND record_key > ?))"
		args = append(args, cursor.Date, cursor.Date, cursor.Key)
	}
	query += " ORDER BY record_date, record_key LIMIT ?"
	args = append(args, limit+1)
	rows, err := a.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	defer rows.Close()
	items := make([]HealthRecord, 0, limit)
	for rows.Next() {
		var item HealthRecord
		var raw, recordDate string
		if err := rows.Scan(&item.Id, &item.Dataset, &item.RecordKey, &recordDate, &raw, &item.IngestedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "database row failed")
			return
		}
		item.RecordDate = dateValue(recordDate)
		if err := json.Unmarshal([]byte(raw), &item.Payload); err != nil {
			writeError(w, http.StatusInternalServerError, "stored payload is invalid")
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "database rows failed")
		return
	}
	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	next := ""
	if hasNext {
		last := items[len(items)-1]
		next = encodeRecordCursor(recordCursor{Date: last.RecordDate.String(), Key: last.RecordKey})
	}
	if activitiesOnly {
		response := ActivityPage{Items: items}
		if next != "" {
			response.NextCursor = &next
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	response := HealthPage{Items: items}
	if next != "" {
		response.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) PullSync(w http.ResponseWriter, r *http.Request, params PullSyncParams) {
	if err := a.auth.Authorize(r, ""); err != nil {
		writeAuthorizationError(w, err)
		return
	}
	householdID := ""
	if scoped, ok := a.auth.(HouseholdAuthorizer); ok {
		var err error
		householdID, err = scoped.HouseholdID(r)
		if err != nil {
			writeAuthorizationError(w, err)
			return
		}
	}
	if params.Since < 0 {
		writeError(w, http.StatusBadRequest, "since must be non-negative")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database transaction failed")
		return
	}
	changes := make([]SyncChange, 0, maxSyncBatch)
	if err := appendSessionChanges(r.Context(), tx, params.Since, householdID, &changes); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	if err := appendEntryChanges(r.Context(), tx, params.Since, householdID, &changes); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	if err := appendDocumentChanges(r.Context(), tx, params.Since, householdID, &changes); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "database query failed")
		return
	}
	if len(changes) > maxSyncBatch {
		_ = tx.Rollback()
		writeError(w, http.StatusRequestEntityTooLarge, "sync result exceeds batch limit")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "database transaction failed")
		return
	}
	next := time.Now().UnixMilli()
	if next < params.Since {
		next = params.Since
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].ServerSyncedAt != changes[j].ServerSyncedAt {
			return changes[i].ServerSyncedAt < changes[j].ServerSyncedAt
		}
		return string(changes[i].Kind)+":"+changes[i].Id < string(changes[j].Kind)+":"+changes[j].Id
	})
	writeJSON(w, http.StatusOK, SyncPullResponse{NextSince: next, Changes: changes})
}

func (a *API) PushSync(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxRequestBody {
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		return
	}
	var request SyncPushRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request must contain one JSON value")
		return
	}
	if len(request.Mutations) == 0 || len(request.Mutations) > maxSyncBatch {
		writeError(w, http.StatusRequestEntityTooLarge, "mutation batch must contain 1 to 100 records")
		return
	}
	for _, mutation := range request.Mutations {
		if mutation.AthleteId == uuid.Nil || mutation.Id == "" || mutation.ClientUpdatedAt < 0 {
			writeError(w, http.StatusUnprocessableEntity, "mutation has an invalid identity or timestamp")
			return
		}
		if mutation.Kind != SyncMutationKindSession && mutation.Kind != SyncMutationKindEntry && mutation.Kind != SyncMutationKindDocument {
			writeError(w, http.StatusUnprocessableEntity, "mutation kind is invalid")
			return
		}
		if err := a.auth.Authorize(r, mutation.AthleteId.String()); err != nil {
			writeAuthorizationError(w, err)
			return
		}
	}
	winners, err := a.ApplySyncMutations(r.Context(), request.Mutations)
	if err != nil {
		if errors.Is(err, errSyncDatabase) {
			writeError(w, http.StatusInternalServerError, "database transaction failed")
		} else {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, SyncPushResponse{Winners: winners})
}

var errSyncDatabase = errors.New("sync database failure")

// ApplySyncMutations is the shared transactional LWW service used by REST
// and MCP. Authorization is intentionally performed by each transport before
// calling this method.
func (a *API) ApplySyncMutations(ctx context.Context, mutations []SyncMutation) ([]SyncChange, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: begin transaction", errSyncDatabase)
	}
	rollback := func(cause error) ([]SyncChange, error) { _ = tx.Rollback(); return nil, cause }
	serverClock, err := syncClock(ctx, tx)
	if err != nil {
		return rollback(fmt.Errorf("%w: read clock", errSyncDatabase))
	}
	winners := make([]SyncChange, len(mutations))
	indexes := make([]int, len(mutations))
	for i := range indexes {
		indexes[i] = i
	}
	for _, kind := range []SyncMutationKind{SyncMutationKindSession, SyncMutationKindEntry, SyncMutationKindDocument} {
		for _, index := range indexes {
			mutation := mutations[index]
			if mutation.Kind != kind {
				continue
			}
			clock := nextSyncClock(&serverClock)
			var winner SyncChange
			switch kind {
			case SyncMutationKindSession:
				winner, err = applySession(ctx, tx, mutation, clock)
			case SyncMutationKindEntry:
				winner, err = applyEntry(ctx, tx, mutation, clock)
			case SyncMutationKindDocument:
				winner, err = applyDocument(ctx, tx, mutation, clock)
			}
			if err != nil {
				return rollback(err)
			}
			winners[index] = winner
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("%w: commit transaction", errSyncDatabase)
	}
	return winners, nil
}

func (a *API) athleteExists(ctx context.Context, id string) bool {
	var exists int
	return a.db.QueryRowContext(ctx, "SELECT 1 FROM athletes WHERE id = ?", id).Scan(&exists) == nil
}

// GarminSummary is the intentionally small representation exposed to MCP;
// raw Garmin payloads remain available through the bounded REST endpoints.
type GarminSummary struct {
	ID         string   `json:"id"`
	Dataset    string   `json:"dataset"`
	RecordKey  string   `json:"record_key"`
	RecordDate string   `json:"record_date"`
	IngestedAt int64    `json:"ingested_at"`
	SleepScore *float64 `json:"sleep_score,omitempty"`
	ActivityID *string  `json:"activity_id,omitempty"`
}

func (a *API) ListGarminSummaries(ctx context.Context, athleteID, from, to string, limit int, activitiesOnly bool) ([]GarminSummary, error) {
	if err := validDateRange(from, to); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxPageSize {
		return nil, errors.New("limit must be between 1 and 100")
	}
	query := `SELECT id, dataset, record_key, record_date, ingested_at, sleep_score, activity_id
		FROM garmin_records WHERE athlete_id=? AND record_date BETWEEN ? AND ?`
	if activitiesOnly {
		query += " AND dataset='activities'"
	} else {
		query += " AND dataset NOT IN ('activities','activity_details')"
	}
	query += " ORDER BY record_date, record_key LIMIT ?"
	rows, err := a.db.QueryContext(ctx, query, athleteID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []GarminSummary
	for rows.Next() {
		var item GarminSummary
		var score sql.NullFloat64
		var activity sql.NullString
		if err := rows.Scan(&item.ID, &item.Dataset, &item.RecordKey, &item.RecordDate, &item.IngestedAt, &score, &activity); err != nil {
			return nil, err
		}
		if score.Valid {
			item.SleepScore = &score.Float64
		}
		if activity.Valid {
			item.ActivityID = &activity.String
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type recordCursor struct {
	Date string
	Key  string
}

func encodeRecordCursor(cursor recordCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursor.Date + "\x00" + cursor.Key))
}

func decodeRecordCursor(value string) (*recordCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 2 || validDate(parts[0]) != nil || parts[1] == "" {
		return nil, errors.New("invalid cursor")
	}
	return &recordCursor{Date: parts[0], Key: parts[1]}, nil
}

func pageSize(value *Limit) (int, error) {
	if value == nil {
		return defaultPageSize, nil
	}
	if *value < 1 || *value > maxPageSize {
		return 0, errors.New("page size must be between 1 and 100")
	}
	return *value, nil
}

func validDate(value string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return errors.New("date must be YYYY-MM-DD")
	}
	return nil
}

func validDateRange(from, to string) error {
	if err := validDate(from); err != nil {
		return err
	}
	if err := validDate(to); err != nil {
		return err
	}
	if from > to {
		return errors.New("from must not be after to")
	}
	return nil
}

func dateValue(value string) openapi_types.Date {
	parsed, _ := time.Parse("2006-01-02", value)
	return openapi_types.Date{Time: parsed}
}

func stringPtr(value string) *string { return &value }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, Error{Error: message})
}

func syncClock(ctx context.Context, tx *sql.Tx) (int64, error) {
	var maximum sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT MAX(server_synced_at) FROM (
		SELECT server_synced_at FROM workout_sessions
		UNION ALL SELECT server_synced_at FROM workout_entries
		UNION ALL SELECT server_synced_at FROM app_documents
	)`).Scan(&maximum)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	if maximum.Valid && now < maximum.Int64 {
		now = maximum.Int64
	}
	return now, nil
}

func nextSyncClock(clock *int64) int64 {
	now := time.Now().UnixMilli()
	if now <= *clock {
		now = *clock + 1
	}
	*clock = now
	return now
}

func requireUUIDv7(id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 {
		return errors.New("new sync IDs must be UUIDv7")
	}
	return nil
}

func payloadJSON(value any, emptyObject bool) (string, error) {
	if value == nil && emptyObject {
		return "{}", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil || !json.Valid(encoded) {
		return "", errors.New("payload must be valid JSON")
	}
	return string(encoded), nil
}

func normalizeRawEntryPayload(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload == nil {
		return "", errors.New("invalid_input: entry payload must be an object")
	}
	key, keyPresent, err := rawOptionalString(payload, "exercise_key")
	if err != nil {
		return "", errors.New("invalid_input: exercise_key")
	}
	name, namePresent, err := rawOptionalString(payload, "exercise_name")
	if err != nil {
		return "", errors.New("invalid_input: exercise_name")
	}
	if !keyPresent && !namePresent {
		return raw, nil
	}
	resolution, err := exercisecatalog.Resolve(key, name)
	if err != nil {
		return "", err
	}
	if resolution.Key == nil {
		payload["exercise_key"] = []byte("null")
	} else {
		payload["exercise_key"], _ = json.Marshal(*resolution.Key)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", errors.New("internal_error: entry payload")
	}
	return string(encoded), nil
}

func rawOptionalString(payload map[string]json.RawMessage, field string) (*string, bool, error) {
	raw, present := payload[field]
	if !present || string(raw) == "null" {
		return nil, present, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, err
	}
	return &value, true, nil
}

type storedSession struct {
	id, athleteID, workoutDate, title, source string
	sourceRef, metadataRaw                    sql.NullString
	clientUpdatedAt, serverSyncedAt           int64
	deletedAt                                 sql.NullInt64
}

func loadSession(ctx context.Context, tx *sql.Tx, id string) (storedSession, error) {
	var session storedSession
	err := tx.QueryRowContext(ctx, `SELECT id, athlete_id, workout_date, title, source,
		source_ref, metadata_raw, client_updated_at, server_synced_at, deleted_at
		FROM workout_sessions WHERE id = ?`, id).Scan(
		&session.id, &session.athleteID, &session.workoutDate, &session.title, &session.source,
		&session.sourceRef, &session.metadataRaw, &session.clientUpdatedAt, &session.serverSyncedAt, &session.deletedAt)
	return session, err
}

func applySession(ctx context.Context, tx *sql.Tx, mutation SyncMutation, syncedAt int64) (SyncChange, error) {
	athleteID := mutation.AthleteId.String()
	current, err := loadSession(ctx, tx, mutation.Id)
	if errors.Is(err, sql.ErrNoRows) {
		if err := requireUUIDv7(mutation.Id); err != nil {
			return SyncChange{}, err
		}
		if mutation.WorkoutDate == nil || mutation.Title == nil || mutation.Source == nil {
			return SyncChange{}, errors.New("new session requires workout_date, title, and source")
		}
		if err := validDate(mutation.WorkoutDate.String()); err != nil {
			return SyncChange{}, err
		}
		metadata, err := payloadJSON(mutation.Payload, true)
		if err != nil {
			return SyncChange{}, err
		}
		var sourceRef any
		if mutation.SourceRef != nil {
			sourceRef = *mutation.SourceRef
		}
		var deletedAt any
		if mutation.Deleted {
			deletedAt = syncedAt
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO workout_sessions
			(id, athlete_id, workout_date, title, source, source_ref, metadata_raw,
			 client_updated_at, server_synced_at, deleted_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, mutation.Id, athleteID,
			mutation.WorkoutDate.String(), *mutation.Title, *mutation.Source, sourceRef, metadata,
			mutation.ClientUpdatedAt, syncedAt, deletedAt, syncedAt)
		if err != nil {
			return SyncChange{}, err
		}
		current, err = loadSession(ctx, tx, mutation.Id)
		if err != nil {
			return SyncChange{}, err
		}
	} else if err != nil {
		return SyncChange{}, err
	} else {
		if current.athleteID != athleteID {
			return SyncChange{}, errors.New("session belongs to another athlete")
		}
		if mutation.ClientUpdatedAt > current.clientUpdatedAt {
			workoutDate := current.workoutDate
			if mutation.WorkoutDate != nil {
				workoutDate = mutation.WorkoutDate.String()
			}
			if err := validDate(workoutDate); err != nil {
				return SyncChange{}, err
			}
			title, source, sourceRef := current.title, current.source, current.sourceRef
			if mutation.Title != nil {
				title = *mutation.Title
			}
			if mutation.Source != nil {
				source = *mutation.Source
			}
			if mutation.SourceRef != nil {
				sourceRef = sql.NullString{String: *mutation.SourceRef, Valid: true}
			}
			metadata := current.metadataRaw.String
			if mutation.Payload != nil {
				metadata, err = payloadJSON(mutation.Payload, false)
				if err != nil {
					return SyncChange{}, err
				}
			}
			var deletedAt any
			if mutation.Deleted {
				deletedAt = syncedAt
			}
			_, err = tx.ExecContext(ctx, `UPDATE workout_sessions SET workout_date=?, title=?, source=?,
				source_ref=?, metadata_raw=?, client_updated_at=?, server_synced_at=?, deleted_at=? WHERE id=?`,
				workoutDate, title, source, nullableString(sourceRef), metadata, mutation.ClientUpdatedAt, syncedAt, deletedAt, mutation.Id)
			if err != nil {
				return SyncChange{}, err
			}
			current, err = loadSession(ctx, tx, mutation.Id)
			if err != nil {
				return SyncChange{}, err
			}
		}
	}
	return sessionChange(current), nil
}

type storedEntry struct {
	id, sessionID, athleteID, entryType, payloadRaw string
	sequenceNo                                      int
	clientUpdatedAt, serverSyncedAt                 int64
	deletedAt                                       sql.NullInt64
}

func loadEntry(ctx context.Context, tx *sql.Tx, id string) (storedEntry, error) {
	var entry storedEntry
	err := tx.QueryRowContext(ctx, `SELECT id, session_id, athlete_id, sequence_no, entry_type,
		payload_raw, client_updated_at, server_synced_at, deleted_at FROM workout_entries WHERE id = ?`, id).Scan(
		&entry.id, &entry.sessionID, &entry.athleteID, &entry.sequenceNo, &entry.entryType,
		&entry.payloadRaw, &entry.clientUpdatedAt, &entry.serverSyncedAt, &entry.deletedAt)
	return entry, err
}

func applyEntry(ctx context.Context, tx *sql.Tx, mutation SyncMutation, syncedAt int64) (SyncChange, error) {
	athleteID := mutation.AthleteId.String()
	current, err := loadEntry(ctx, tx, mutation.Id)
	if errors.Is(err, sql.ErrNoRows) {
		if err := requireUUIDv7(mutation.Id); err != nil {
			return SyncChange{}, err
		}
		if mutation.SessionId == nil || mutation.SequenceNo == nil || mutation.EntryType == nil {
			return SyncChange{}, errors.New("new entry requires session_id, sequence_no, and entry_type")
		}
		if *mutation.SequenceNo < 1 {
			return SyncChange{}, errors.New("sequence_no must be positive")
		}
		if err := sessionBelongsTo(ctx, tx, *mutation.SessionId, athleteID); err != nil {
			return SyncChange{}, err
		}
		payload, err := payloadJSON(mutation.Payload, true)
		if err != nil {
			return SyncChange{}, err
		}
		payload, err = normalizeRawEntryPayload(payload)
		if err != nil {
			return SyncChange{}, err
		}
		var deletedAt any
		if mutation.Deleted {
			deletedAt = syncedAt
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO workout_entries
			(id, session_id, athlete_id, sequence_no, entry_type, payload_raw,
			 client_updated_at, server_synced_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, mutation.Id, *mutation.SessionId, athleteID,
			*mutation.SequenceNo, *mutation.EntryType, payload, mutation.ClientUpdatedAt, syncedAt, deletedAt)
		if err != nil {
			return SyncChange{}, err
		}
		current, err = loadEntry(ctx, tx, mutation.Id)
		if err != nil {
			return SyncChange{}, err
		}
	} else if err != nil {
		return SyncChange{}, err
	} else {
		if current.athleteID != athleteID {
			return SyncChange{}, errors.New("entry belongs to another athlete")
		}
		if mutation.ClientUpdatedAt > current.clientUpdatedAt {
			sessionID, sequenceNo, entryType := current.sessionID, current.sequenceNo, current.entryType
			if mutation.SessionId != nil {
				sessionID = *mutation.SessionId
			}
			if mutation.SequenceNo != nil {
				sequenceNo = *mutation.SequenceNo
			}
			if mutation.EntryType != nil {
				entryType = *mutation.EntryType
			}
			if sequenceNo < 1 {
				return SyncChange{}, errors.New("sequence_no must be positive")
			}
			if err := sessionBelongsTo(ctx, tx, sessionID, athleteID); err != nil {
				return SyncChange{}, err
			}
			payload := current.payloadRaw
			if mutation.Payload != nil {
				payload, err = payloadJSON(mutation.Payload, false)
				if err != nil {
					return SyncChange{}, err
				}
				payload, err = normalizeRawEntryPayload(payload)
				if err != nil {
					return SyncChange{}, err
				}
			}
			var deletedAt any
			if mutation.Deleted {
				deletedAt = syncedAt
			}
			_, err = tx.ExecContext(ctx, `UPDATE workout_entries SET session_id=?, sequence_no=?, entry_type=?,
				payload_raw=?, client_updated_at=?, server_synced_at=?, deleted_at=? WHERE id=?`,
				sessionID, sequenceNo, entryType, payload, mutation.ClientUpdatedAt, syncedAt, deletedAt, mutation.Id)
			if err != nil {
				return SyncChange{}, err
			}
			current, err = loadEntry(ctx, tx, mutation.Id)
			if err != nil {
				return SyncChange{}, err
			}
		}
	}
	return entryChange(current), nil
}

func sessionBelongsTo(ctx context.Context, tx *sql.Tx, sessionID, athleteID string) error {
	var found int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM workout_sessions WHERE id=? AND athlete_id=?", sessionID, athleteID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("entry references an unknown session or athlete")
		}
		return err
	}
	return nil
}

type storedDocument struct {
	id, athleteID, documentKey, contentRaw string
	clientUpdatedAt, serverSyncedAt        int64
	deletedAt                              sql.NullInt64
}

func loadDocument(ctx context.Context, tx *sql.Tx, id string) (storedDocument, error) {
	var document storedDocument
	err := tx.QueryRowContext(ctx, `SELECT id, athlete_id, document_key, content_raw,
		client_updated_at, server_synced_at, deleted_at FROM app_documents WHERE id = ?`, id).Scan(
		&document.id, &document.athleteID, &document.documentKey, &document.contentRaw,
		&document.clientUpdatedAt, &document.serverSyncedAt, &document.deletedAt)
	return document, err
}

func applyDocument(ctx context.Context, tx *sql.Tx, mutation SyncMutation, syncedAt int64) (SyncChange, error) {
	athleteID := mutation.AthleteId.String()
	current, err := loadDocument(ctx, tx, mutation.Id)
	if errors.Is(err, sql.ErrNoRows) {
		if err := requireUUIDv7(mutation.Id); err != nil {
			return SyncChange{}, err
		}
		if mutation.DocumentKey == nil || *mutation.DocumentKey == "" {
			return SyncChange{}, errors.New("new document requires document_key")
		}
		content, err := payloadJSON(mutation.Payload, true)
		if err != nil {
			return SyncChange{}, err
		}
		var deletedAt any
		if mutation.Deleted {
			deletedAt = syncedAt
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO app_documents
			(id, athlete_id, document_key, content_raw, client_updated_at, server_synced_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, mutation.Id, athleteID, *mutation.DocumentKey, content,
			mutation.ClientUpdatedAt, syncedAt, deletedAt)
		if err != nil {
			return SyncChange{}, err
		}
		current, err = loadDocument(ctx, tx, mutation.Id)
		if err != nil {
			return SyncChange{}, err
		}
	} else if err != nil {
		return SyncChange{}, err
	} else {
		if current.athleteID != athleteID {
			return SyncChange{}, errors.New("document belongs to another athlete")
		}
		if mutation.ClientUpdatedAt > current.clientUpdatedAt {
			documentKey := current.documentKey
			if mutation.DocumentKey != nil {
				documentKey = *mutation.DocumentKey
			}
			content := current.contentRaw
			if mutation.Payload != nil {
				content, err = payloadJSON(mutation.Payload, false)
				if err != nil {
					return SyncChange{}, err
				}
			}
			var deletedAt any
			if mutation.Deleted {
				deletedAt = syncedAt
			}
			_, err = tx.ExecContext(ctx, `UPDATE app_documents SET document_key=?, content_raw=?,
				client_updated_at=?, server_synced_at=?, deleted_at=? WHERE id=?`,
				documentKey, content, mutation.ClientUpdatedAt, syncedAt, deletedAt, mutation.Id)
			if err != nil {
				return SyncChange{}, err
			}
			current, err = loadDocument(ctx, tx, mutation.Id)
			if err != nil {
				return SyncChange{}, err
			}
		}
	}
	return documentChange(current), nil
}

func nullableString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func sessionChange(session storedSession) SyncChange {
	change := SyncChange{
		AthleteId: uuid.MustParse(session.athleteID), ClientUpdatedAt: session.clientUpdatedAt,
		Deleted: session.deletedAt.Valid, Id: session.id, Kind: SyncChangeKindSession,
		Payload: decodePayload(session.metadataRaw.String), ServerSyncedAt: session.serverSyncedAt,
		Source: stringPtr(session.source), Title: stringPtr(session.title), WorkoutDate: datePtr(session.workoutDate),
	}
	if session.sourceRef.Valid {
		change.SourceRef = stringPtr(session.sourceRef.String)
	}
	return change
}

func entryChange(entry storedEntry) SyncChange {
	sequenceNo := entry.sequenceNo
	return SyncChange{
		AthleteId: uuid.MustParse(entry.athleteID), ClientUpdatedAt: entry.clientUpdatedAt,
		Deleted: entry.deletedAt.Valid, Id: entry.id, Kind: SyncChangeKindEntry,
		Payload: decodePayload(entry.payloadRaw), ServerSyncedAt: entry.serverSyncedAt,
		EntryType: stringPtr(entry.entryType), SequenceNo: &sequenceNo, SessionId: stringPtr(entry.sessionID),
	}
}

func documentChange(document storedDocument) SyncChange {
	return SyncChange{
		AthleteId: uuid.MustParse(document.athleteID), ClientUpdatedAt: document.clientUpdatedAt,
		Deleted: document.deletedAt.Valid, Id: document.id, Kind: SyncChangeKindDocument,
		DocumentKey: stringPtr(document.documentKey), Payload: decodePayload(document.contentRaw),
		ServerSyncedAt: document.serverSyncedAt,
	}
}

func datePtr(value string) *openapi_types.Date {
	date := dateValue(value)
	return &date
}

func decodePayload(raw string) any {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return nil
	}
	return value
}

func appendSessionChanges(ctx context.Context, tx *sql.Tx, since int64, householdID string, changes *[]SyncChange) error {
	query := `SELECT id, athlete_id, workout_date, title, source, source_ref,
		metadata_raw, client_updated_at, server_synced_at, deleted_at FROM workout_sessions
		WHERE server_synced_at > ?`
	args := []any{since}
	if householdID != "" {
		query += " AND athlete_id IN (SELECT id FROM athletes WHERE household_id=?)"
		args = append(args, householdID)
	}
	query += " ORDER BY server_synced_at, id LIMIT ?"
	args = append(args, maxSyncBatch+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var session storedSession
		if err := rows.Scan(&session.id, &session.athleteID, &session.workoutDate, &session.title, &session.source,
			&session.sourceRef, &session.metadataRaw, &session.clientUpdatedAt, &session.serverSyncedAt, &session.deletedAt); err != nil {
			return err
		}
		*changes = append(*changes, sessionChange(session))
	}
	return rows.Err()
}

func appendEntryChanges(ctx context.Context, tx *sql.Tx, since int64, householdID string, changes *[]SyncChange) error {
	query := `SELECT id, session_id, athlete_id, sequence_no, entry_type,
		payload_raw, client_updated_at, server_synced_at, deleted_at FROM workout_entries
		WHERE server_synced_at > ?`
	args := []any{since}
	if householdID != "" {
		query += " AND athlete_id IN (SELECT id FROM athletes WHERE household_id=?)"
		args = append(args, householdID)
	}
	query += " ORDER BY server_synced_at, id LIMIT ?"
	args = append(args, maxSyncBatch+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var entry storedEntry
		if err := rows.Scan(&entry.id, &entry.sessionID, &entry.athleteID, &entry.sequenceNo, &entry.entryType,
			&entry.payloadRaw, &entry.clientUpdatedAt, &entry.serverSyncedAt, &entry.deletedAt); err != nil {
			return err
		}
		*changes = append(*changes, entryChange(entry))
	}
	return rows.Err()
}

func appendDocumentChanges(ctx context.Context, tx *sql.Tx, since int64, householdID string, changes *[]SyncChange) error {
	query := `SELECT id, athlete_id, document_key, content_raw,
		client_updated_at, server_synced_at, deleted_at FROM app_documents
		WHERE server_synced_at > ?`
	args := []any{since}
	if householdID != "" {
		query += " AND athlete_id IN (SELECT id FROM athletes WHERE household_id=?)"
		args = append(args, householdID)
	}
	query += " ORDER BY server_synced_at, id LIMIT ?"
	args = append(args, maxSyncBatch+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var document storedDocument
		if err := rows.Scan(&document.id, &document.athleteID, &document.documentKey, &document.contentRaw,
			&document.clientUpdatedAt, &document.serverSyncedAt, &document.deletedAt); err != nil {
			return err
		}
		*changes = append(*changes, documentChange(document))
	}
	return rows.Err()
}
