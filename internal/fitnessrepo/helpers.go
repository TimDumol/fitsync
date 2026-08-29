package fitnessrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type cursorData struct {
	Version int    `json:"v"`
	Date    string `json:"d"`
	ID      string `json:"i"`
	Order   string `json:"o"`
	Filters string `json:"f"`
}

func filterHash(values ...string) string {
	h := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return fmt.Sprintf("%x", h[:])
}
func encodeCursor(value cursorData) string {
	raw, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeCursor(value, order, filters string) (*cursorData, error) {
	if value == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("invalid_input: cursor is invalid")
	}
	var c cursorData
	if json.Unmarshal(raw, &c) != nil || c.Version != 1 || c.Order != order || c.Filters != filters || c.Date == "" || c.ID == "" {
		return nil, errors.New("invalid_input: cursor does not match filters or order")
	}
	return &c, nil
}
func afterCursor(date, id, cursorDate, cursorID, order string) bool {
	if order == "asc" {
		return date > cursorDate || (date == cursorDate && id > cursorID)
	}
	return date < cursorDate || (date == cursorDate && id < cursorID)
}
func reverseStrings(values []string) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}
func boundedLimit(value int) (int, error) {
	if value == 0 {
		return 50, nil
	}
	if value < 1 || value > 100 {
		return 0, errors.New("invalid_input: limit must be between 1 and 100")
	}
	return value, nil
}
func validRange(from, to string, max int) error {
	f, err := time.Parse("2006-01-02", from)
	if err != nil || f.Format("2006-01-02") != from {
		return errors.New("invalid_input: from must be YYYY-MM-DD")
	}
	t, err := time.Parse("2006-01-02", to)
	if err != nil || t.Format("2006-01-02") != to {
		return errors.New("invalid_input: to must be YYYY-MM-DD")
	}
	if from > to {
		return errors.New("invalid_input: from must not be after to")
	}
	if int(t.Sub(f).Hours()/24)+1 > max {
		return fmt.Errorf("invalid_input: date range exceeds %d days", max)
	}
	return nil
}
func containsOrEmpty(values []string, needle string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
func anyIn(values, wanted []string) bool {
	for _, value := range values {
		if containsOrEmpty(wanted, value) {
			return true
		}
	}
	return false
}
func has(values []string, want string) bool {
	if len(values) == 0 {
		return want == "summary"
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func normalizeWriteError(err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "constraint") {
		return errors.New("conflict: write conflicts with an existing record")
	}
	return errors.New("internal_error: write failed")
}

func requireUUIDv7(id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 {
		return errors.New("invalid_input: new IDs must be UUIDv7")
	}
	return nil
}

func (r *Repository) dataStatus(ctx context.Context, athleteID, from, to string) DataStatus {
	status := DataStatus{}
	var last sql.NullInt64
	if r.db.QueryRowContext(ctx, "SELECT MAX(ingested_at) FROM garmin_records WHERE athlete_id=? AND record_date BETWEEN ? AND ?", athleteID, from, to).Scan(&last) == nil && last.Valid {
		value := time.UnixMilli(last.Int64).UTC().Format(time.RFC3339)
		status.LastSyncAt = &value
	}
	rows, err := r.db.QueryContext(ctx, "SELECT record_date FROM garmin_records WHERE athlete_id=? AND record_date BETWEEN ? AND ? GROUP BY record_date HAVING SUM(CASE WHEN dataset='sleep' THEN 1 ELSE 0 END)>0 AND SUM(CASE WHEN dataset='daily_recovery' THEN 1 ELSE 0 END)>0 ORDER BY record_date DESC LIMIT 1", athleteID, from, to)
	if err == nil {
		if rows.Next() {
			var date string
			if rows.Scan(&date) == nil {
				status.CompleteThrough = &date
			}
		}
		rows.Close()
	}
	fromDate, err := time.Parse("2006-01-02", from)
	if err != nil {
		return status
	}
	toDate, _ := time.Parse("2006-01-02", to)
	missingCount := 0
	for date := fromDate; !date.After(toDate); date = date.AddDate(0, 0, 1) {
		d := date.Format("2006-01-02")
		var count int
		if r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM garmin_records WHERE athlete_id=? AND record_date=?", athleteID, d).Scan(&count) == nil && count == 0 {
			missingCount++
			if len(status.MissingDates) < 31 {
				status.MissingDates = append(status.MissingDates, d)
			}
		}
	}
	if missingCount > 31 {
		status.Warnings = append(status.Warnings, Warning{Code: "MISSING_DATES_TRUNCATED", Severity: "info", Message: "Missing dates are limited to 31 entries."})
	}
	return status
}
