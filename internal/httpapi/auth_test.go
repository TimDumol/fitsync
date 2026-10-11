package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	fitnessauth "github.com/timdumol/fitsync/internal/auth"
)

func TestLegacySyncReadIsHouseholdScoped(t *testing.T) {
	database := testAPIDB(t)
	const ownHousehold = "00000000-0000-0000-0000-000000000001"
	foreignHousehold, foreignAthlete := newV7(t), newV7(t)
	if _, err := database.Exec("INSERT INTO households (id,name,created_at) VALUES (?, 'Synthetic foreign household', 0)", foreignHousehold); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO athletes (id,handle,household_id,created_at) VALUES (?, 'synthetic-foreign', ?, 0)", foreignAthlete, foreignHousehold); err != nil {
		t.Fatal(err)
	}
	for _, athlete := range []string{timID, foreignAthlete} {
		if _, err := database.Exec(`INSERT INTO app_documents
			(id,athlete_id,document_key,content_raw,client_updated_at,server_synced_at)
			VALUES (?, ?, 'synthetic', '{}', 1, 1)`, newV7(t), athlete); err != nil {
			t.Fatal(err)
		}
	}
	handler := Handler(NewAPI(database, NewPocketIDAuthorizer(&fitnessauth.Authenticator{})))
	for _, household := range []string{ownHousehold, ""} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/sync/pull?since=0", nil)
		request = request.WithContext(fitnessauth.WithPrincipal(request.Context(), fitnessauth.Principal{
			AthleteID: timID, HouseholdID: household, Scopes: map[string]bool{"fitness:read": true},
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if household == "" {
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("missing household: status = %d, want 403", recorder.Code)
			}
			continue
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
		}
		var response SyncPullResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Changes) != 1 || response.Changes[0].AthleteId.String() != timID {
			t.Fatalf("unexpected sync changes: %+v", response.Changes)
		}
	}
}

func TestMissingAuthorizerDeniesRequests(t *testing.T) {
	handler := Handler(NewAPI(testAPIDB(t), nil))
	for _, remote := range []string{"127.0.0.1:1234", "192.0.2.1:1234"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
		request.RemoteAddr = remote
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("remote %s: status = %d, want 403", remote, recorder.Code)
		}
	}
}

func TestReadScopeCannotMutateWorkouts(t *testing.T) {
	// Authenticate uses an already verified context principal, as it does
	// after transport middleware. Denial must happen before the target lookup.
	authorizer := NewPocketIDAuthorizer(&fitnessauth.Authenticator{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/api/v2/athletes/"+timID+"/workouts/"+newV7(t), nil)
			request = request.WithContext(fitnessauth.WithPrincipal(request.Context(), fitnessauth.Principal{
				AthleteID: timID, Scopes: map[string]bool{"fitness:read": true},
			}))
			err := authorizer.Authorize(request, timID)
			var authErr *fitnessauth.Error
			if !errors.As(err, &authErr) || authErr.Code != "insufficient_scope" || authErr.Status != http.StatusForbidden {
				t.Fatalf("authorization = %v, want insufficient_scope/403", err)
			}
		})
	}
}
