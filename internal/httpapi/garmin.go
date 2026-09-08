package httpapi

import "net/http"

// These methods satisfy the regenerated OpenAPI interface. The concrete
// ingestion handler remains responsible for OIDC authorization and the
// shared multipart/transaction path.
func (a *API) IngestGarminMobileBatch(w http.ResponseWriter, r *http.Request, _ AthleteID) {
	if a.garminBatch == nil {
		writeError(w, http.StatusNotFound, "route unavailable")
		return
	}
	a.garminBatch.ServeHTTP(w, r)
}

func (a *API) GetGarminSyncStatus(w http.ResponseWriter, r *http.Request, _ AthleteID) {
	if a.garminStatus == nil {
		writeError(w, http.StatusNotFound, "route unavailable")
		return
	}
	a.garminStatus.ServeHTTP(w, r)
}
