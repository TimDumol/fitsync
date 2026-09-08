package ingest

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	fitnessauth "github.com/timdumol/fitsync/internal/auth"
)

var mobileDatasetNames = map[string]struct{}{
	"activities": {}, "sleep": {}, "daily_recovery": {},
	"body_battery_events": {}, "activity_details": {},
	"training_snapshot": {}, "body_measurements": {},
}

// MobileBatchHandler authenticates the Pocket ID access token and then hands
// the request to the same bounded multipart/transaction path as legacy sync.
func (h *Handler) MobileBatchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if h.OIDC == nil {
			writeError(w, http.StatusUnauthorized, "invalid access token")
			return
		}
		principal, err := h.OIDC.Authenticate(r, "fitness:write")
		if err != nil {
			writeMobileAuthError(w, err)
			return
		}
		athleteID := pathAthleteID(r)
		if _, err := uuid.Parse(athleteID); err != nil {
			writeError(w, http.StatusBadRequest, "athlete_id must be a UUID")
			return
		}
		if err := h.OIDC.AuthorizeTarget(r.Context(), principal, athleteID); err != nil {
			writeError(w, http.StatusForbidden, "athlete is not available")
			return
		}
		var athleteHandle string
		if err := h.DB.QueryRowContext(r.Context(), "SELECT handle FROM athletes WHERE id=?", athleteID).Scan(&athleteHandle); err != nil {
			writeError(w, http.StatusForbidden, "athlete is not available")
			return
		}
		h.serveAuthorized(w, r, athleteHandle, athleteID)
	})
}

// MobileStatusHandler returns only values derived from committed Garmin rows.
func (h *Handler) MobileStatusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if h.OIDC == nil {
			writeError(w, http.StatusUnauthorized, "invalid access token")
			return
		}
		principal, err := h.OIDC.Authenticate(r, "fitness:read")
		if err != nil {
			writeMobileAuthError(w, err)
			return
		}
		athleteID := pathAthleteID(r)
		if _, err := uuid.Parse(athleteID); err != nil {
			writeError(w, http.StatusBadRequest, "athlete_id must be a UUID")
			return
		}
		if err := h.OIDC.AuthorizeTarget(r.Context(), principal, athleteID); err != nil {
			writeError(w, http.StatusForbidden, "athlete is not available")
			return
		}
		var ingested sql.NullInt64
		if err := h.DB.QueryRowContext(r.Context(), "SELECT MAX(ingested_at) FROM garmin_records WHERE athlete_id=?", athleteID).Scan(&ingested); err != nil {
			writeError(w, http.StatusInternalServerError, "sync status unavailable")
			return
		}
		var latest sql.NullString
		if err := h.DB.QueryRowContext(r.Context(), "SELECT MAX(record_date) FROM garmin_records WHERE athlete_id=?", athleteID).Scan(&latest); err != nil {
			writeError(w, http.StatusInternalServerError, "sync status unavailable")
			return
		}
		rows, err := h.DB.QueryContext(r.Context(), "SELECT dataset, MAX(record_date) FROM garmin_records WHERE athlete_id=? GROUP BY dataset", athleteID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "sync status unavailable")
			return
		}
		defer rows.Close()
		datasets := map[string]string{}
		for rows.Next() {
			var dataset, recordDate string
			if err := rows.Scan(&dataset, &recordDate); err != nil {
				writeError(w, http.StatusInternalServerError, "sync status unavailable")
				return
			}
			if _, known := mobileDatasetNames[dataset]; known {
				datasets[dataset] = recordDate
			}
		}
		if err := rows.Err(); err != nil {
			writeError(w, http.StatusInternalServerError, "sync status unavailable")
			return
		}
		var last any
		if ingested.Valid {
			last = time.UnixMilli(ingested.Int64).UTC().Format(time.RFC3339)
		}
		var latestValue any
		if latest.Valid {
			latestValue = latest.String
		}
		writeJSON(w, http.StatusOK, map[string]any{"last_ingested_at": last, "latest_record_date": latestValue, "datasets": datasets})
	})
}

func pathAthleteID(r *http.Request) string {
	if value := r.PathValue("athlete_id"); value != "" {
		return value
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for index, part := range parts {
		if part == "athletes" && index+1 < len(parts) {
			return parts[index+1]
		}
	}
	return ""
}

func writeMobileAuthError(w http.ResponseWriter, err error) {
	var authErr *fitnessauth.Error
	if errors.As(err, &authErr) {
		writeError(w, authErr.Status, authErr.Code)
		return
	}
	writeError(w, http.StatusUnauthorized, "invalid access token")
}
