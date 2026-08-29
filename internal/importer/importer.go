// Package importer imports the deliberately narrow, curated Neon export.
package importer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const legacyPayloadFormat = "legacy_curated_v1"

// Counts is safe to print in dry-run output: it contains no source values.
type Counts struct {
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
}

type Dump struct {
	Records         []Record
	WorkoutSessions []WorkoutSession
	WorkoutEntries  []WorkoutEntry
	Counts          Counts
}

type Record struct {
	Dataset       string          `json:"dataset"`
	RecordKey     string          `json:"record_key"`
	RecordDate    *string         `json:"record_date"`
	SyncedAt      string          `json:"synced_at"`
	PayloadSHA256 string          `json:"payload_sha256"`
	Payload       json.RawMessage `json:"payload"`
	ID            string          `json:"-"`
}

type WorkoutSession struct {
	WorkoutID   string          `json:"workout_id"`
	WorkoutDate string          `json:"workout_date"`
	Title       string          `json:"title"`
	Source      string          `json:"source"`
	SourceRef   string          `json:"source_ref"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	Raw         json.RawMessage `json:"-"`
}

type WorkoutEntry struct {
	EntryID    int64           `json:"entry_id"`
	WorkoutID  string          `json:"workout_id"`
	SequenceNo int             `json:"sequence_no"`
	EntryType  string          `json:"entry_type"`
	Raw        json.RawMessage `json:"-"`
}

// Load reads exactly the three permitted fitness export files and validates
// all records before any database transaction is opened.
func Load(dir string) (Dump, error) {
	var dump Dump
	recordRows, err := readArray(filepath.Join(dir, "fitness", "records.json"))
	if err != nil {
		return Dump{}, err
	}
	dump.Records = make([]Record, 0, len(recordRows))
	seen := make(map[string]struct{}, len(recordRows))
	activityDates := make(map[string]string)
	for _, raw := range recordRows {
		var record Record
		if err := json.Unmarshal(raw, &record); err != nil {
			return Dump{}, fmt.Errorf("decode fitness record: %w", err)
		}
		if record.Dataset == "" || record.RecordKey == "" || len(record.Payload) == 0 || !json.Valid(record.Payload) {
			return Dump{}, errors.New("fitness record has an invalid required field")
		}
		key := record.Dataset + "\x00" + record.RecordKey
		if _, exists := seen[key]; exists {
			return Dump{}, errors.New("duplicate fitness record key")
		}
		seen[key] = struct{}{}
		if len(record.PayloadSHA256) != sha256.Size*2 || !isHex(record.PayloadSHA256) {
			return Dump{}, errors.New("fitness record has an invalid payload hash")
		}
		if _, err := parseMillis(record.SyncedAt); err != nil {
			return Dump{}, fmt.Errorf("fitness record has invalid sync timestamp: %w", err)
		}
		if record.RecordDate != nil {
			if err := validateDate(*record.RecordDate); err != nil {
				return Dump{}, err
			}
		}
		if record.Dataset == "activities" {
			if record.RecordDate == nil {
				return Dump{}, errors.New("activity record is missing a date")
			}
			activityDates[record.RecordKey] = *record.RecordDate
		}
		record.ID = legacyID(record.Dataset, record.RecordKey)
		dump.Records = append(dump.Records, record)
	}
	for i := range dump.Records {
		record := &dump.Records[i]
		if record.Dataset == "activity_details" && record.RecordDate == nil {
			date, ok := activityDates[record.RecordKey]
			if !ok {
				activityID, hasActivityID := payloadString(record.Payload, "activity_id")
				if !hasActivityID {
					return Dump{}, errors.New("activity_details record has no matching activity")
				}
				date, ok = activityDates[activityID]
			}
			if !ok {
				return Dump{}, errors.New("activity_details record has no matching activity")
			}
			record.RecordDate = &date
		}
	}

	sessionRows, err := readArray(filepath.Join(dir, "fitness", "workout_sessions.json"))
	if err != nil {
		return Dump{}, err
	}
	seenSessions := make(map[string]struct{}, len(sessionRows))
	for _, raw := range sessionRows {
		var session WorkoutSession
		if err := json.Unmarshal(raw, &session); err != nil {
			return Dump{}, fmt.Errorf("decode workout session: %w", err)
		}
		if session.WorkoutID == "" || session.WorkoutDate == "" || session.Title == "" || session.Source == "" {
			return Dump{}, errors.New("workout session has an invalid required field")
		}
		if err := validateDate(session.WorkoutDate); err != nil {
			return Dump{}, err
		}
		if _, ok := seenSessions[session.WorkoutID]; ok {
			return Dump{}, errors.New("duplicate workout session ID")
		}
		seenSessions[session.WorkoutID] = struct{}{}
		if _, err := parseMillis(session.CreatedAt); err != nil {
			return Dump{}, fmt.Errorf("workout session has invalid created timestamp: %w", err)
		}
		if _, err := parseMillis(session.UpdatedAt); err != nil {
			return Dump{}, fmt.Errorf("workout session has invalid updated timestamp: %w", err)
		}
		session.Raw = append(json.RawMessage(nil), raw...)
		dump.WorkoutSessions = append(dump.WorkoutSessions, session)
	}

	entryRows, err := readArray(filepath.Join(dir, "fitness", "workout_entries.json"))
	if err != nil {
		return Dump{}, err
	}
	seenEntries := make(map[string]struct{}, len(entryRows))
	seenOrdering := make(map[string]struct{}, len(entryRows))
	for _, raw := range entryRows {
		var entry WorkoutEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return Dump{}, fmt.Errorf("decode workout entry: %w", err)
		}
		if entry.EntryID < 0 || entry.WorkoutID == "" || entry.SequenceNo <= 0 || entry.EntryType == "" {
			return Dump{}, errors.New("workout entry has an invalid required field")
		}
		if _, exists := seenSessions[entry.WorkoutID]; !exists {
			return Dump{}, errors.New("workout entry references an unknown session")
		}
		entryID := strconv.FormatInt(entry.EntryID, 10)
		if _, exists := seenEntries[entryID]; exists {
			return Dump{}, errors.New("duplicate workout entry ID")
		}
		ordering := entry.WorkoutID + "\x00" + strconv.Itoa(entry.SequenceNo)
		if _, exists := seenOrdering[ordering]; exists {
			return Dump{}, errors.New("duplicate workout entry ordering")
		}
		seenEntries[entryID] = struct{}{}
		seenOrdering[ordering] = struct{}{}
		entry.Raw = append(json.RawMessage(nil), raw...)
		dump.WorkoutEntries = append(dump.WorkoutEntries, entry)
	}

	dump.Counts.GarminRecords = len(dump.Records)
	for _, record := range dump.Records {
		switch record.Dataset {
		case "activities":
			dump.Counts.Activities++
		case "activity_details":
			dump.Counts.ActivityDetails++
		case "body_battery_events":
			dump.Counts.BodyBattery++
		case "body_measurements":
			dump.Counts.BodyMeasurements++
		case "daily_recovery":
			dump.Counts.DailyRecovery++
		case "sleep":
			dump.Counts.Sleep++
		case "training_snapshot":
			dump.Counts.TrainingSnapshot++
		}
	}
	dump.Counts.WorkoutSessions = len(dump.WorkoutSessions)
	dump.Counts.WorkoutEntries = len(dump.WorkoutEntries)
	return dump, nil
}

// Insert writes a validated dump in one transaction. Existing rows are
// replaced by the same stable IDs, making a repeated import idempotent.
func Insert(ctx context.Context, database *sql.DB, dump Dump, athleteHandle string) (Counts, error) {
	if athleteHandle != "tim" {
		return Counts{}, errors.New("Neon dump imports are restricted to athlete tim")
	}
	var athleteID string
	if err := database.QueryRowContext(ctx, "SELECT id FROM athletes WHERE handle = ?", athleteHandle).Scan(&athleteID); err != nil {
		return Counts{}, fmt.Errorf("find athlete: %w", err)
	}
	now := time.Now().UnixMilli()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return Counts{}, err
	}
	rollback := func(cause error) (Counts, error) {
		_ = tx.Rollback()
		return Counts{}, cause
	}
	for _, record := range dump.Records {
		date := ""
		if record.RecordDate != nil {
			date = *record.RecordDate
		}
		ingestedAt, _ := parseMillis(record.SyncedAt)
		_, err := tx.ExecContext(ctx, `INSERT INTO garmin_records
			(id, athlete_id, dataset, record_key, record_date, payload_format, payload_raw, payload_sha256, ingested_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(athlete_id, dataset, record_key) DO UPDATE SET
				record_date = excluded.record_date,
				payload_format = excluded.payload_format,
				payload_raw = excluded.payload_raw,
				payload_sha256 = excluded.payload_sha256,
				ingested_at = excluded.ingested_at`,
			record.ID, athleteID, record.Dataset, record.RecordKey, date, legacyPayloadFormat, string(record.Payload), record.PayloadSHA256, ingestedAt)
		if err != nil {
			return rollback(fmt.Errorf("insert Garmin record: %w", err))
		}
	}
	for _, session := range dump.WorkoutSessions {
		createdAt, _ := parseMillis(session.CreatedAt)
		updatedAt, _ := parseMillis(session.UpdatedAt)
		_, err := tx.ExecContext(ctx, `INSERT INTO workout_sessions
			(id, athlete_id, workout_date, title, source, source_ref, metadata_raw, client_updated_at, server_synced_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				athlete_id = excluded.athlete_id, workout_date = excluded.workout_date,
				title = excluded.title, source = excluded.source, source_ref = excluded.source_ref,
				metadata_raw = excluded.metadata_raw, client_updated_at = excluded.client_updated_at,
				server_synced_at = excluded.server_synced_at, created_at = excluded.created_at`,
			session.WorkoutID, athleteID, session.WorkoutDate, session.Title, session.Source, session.SourceRef, string(session.Raw), updatedAt, now, createdAt)
		if err != nil {
			return rollback(fmt.Errorf("insert workout session: %w", err))
		}
	}
	for _, entry := range dump.WorkoutEntries {
		entryID := strconv.FormatInt(entry.EntryID, 10)
		_, err := tx.ExecContext(ctx, `INSERT INTO workout_entries
			(id, session_id, athlete_id, sequence_no, entry_type, payload_raw, client_updated_at, server_synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				session_id = excluded.session_id, athlete_id = excluded.athlete_id,
				sequence_no = excluded.sequence_no, entry_type = excluded.entry_type,
				payload_raw = excluded.payload_raw, client_updated_at = excluded.client_updated_at,
				server_synced_at = excluded.server_synced_at`,
			entryID, entry.WorkoutID, athleteID, entry.SequenceNo, entry.EntryType, string(entry.Raw), now, now)
		if err != nil {
			return rollback(fmt.Errorf("insert workout entry: %w", err))
		}
	}
	if err := tx.Commit(); err != nil {
		return Counts{}, fmt.Errorf("commit import: %w", err)
	}
	return dump.Counts, nil
}

func readArray(path string) ([]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dump file: %w", err)
	}
	defer file.Close()
	var rows []json.RawMessage
	decoder := json.NewDecoder(file)
	var values []json.RawMessage
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("decode dump file: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode dump file: %w", err)
	}
	rows = append(rows, values...)
	return rows, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func legacyID(dataset, key string) string {
	sum := sha256.Sum256([]byte(dataset + "\x00" + key))
	return "legacy-" + hex.EncodeToString(sum[:])
}

func parseMillis(value string) (int64, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999-07:00", "2006-01-02 15:04:05.999999"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UnixMilli(), nil
		}
	}
	return 0, errors.New("timestamp is not RFC3339")
}

func validateDate(value string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return errors.New("date is not YYYY-MM-DD")
	}
	return nil
}

func isHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func payloadString(raw json.RawMessage, key string) (string, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return "", false
	}
	var value string
	if json.Unmarshal(object[key], &value) == nil {
		return value, value != ""
	}
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(object[key])))
	decoder.UseNumber()
	if decoder.Decode(&number) == nil {
		return number.String(), true
	}
	return "", false
}
