package ingest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	fitnessauth "github.com/timdumol/fitsync/internal/auth"
)

func TestMobileOIDCIngestAndStatus(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var identityServer *httptest.Server
	identityServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": identityServer.URL, "jwks_uri": identityServer.URL + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
				"kty": "RSA", "kid": "mobile-test", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer identityServer.Close()

	handler, database, _ := testHandler(t)
	if _, err := database.Exec(`INSERT INTO athlete_identities
		(id, athlete_id, issuer, subject, signed_username, approved_at, created_at)
		VALUES ('mobile-identity', '00000000-0000-0000-0000-000000000002', ?, 'mobile-subject', 'tim', 1, 1)`, identityServer.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE athletes SET active=1 WHERE handle='tim'"); err != nil {
		t.Fatal(err)
	}
	authenticator, err := fitnessauth.New(context.Background(), database, identityServer.URL, "https://api.example.test/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer authenticator.Close()
	handler.SetAuthenticator(authenticator)

	token := func(scope string) string {
		claims := jwt.MapClaims{
			"iss": identityServer.URL, "sub": "mobile-subject", "aud": "https://api.example.test/mcp",
			"exp": time.Now().Add(time.Hour).Unix(), "nbf": time.Now().Add(-time.Minute).Unix(), "scope": scope,
		}
		value := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		value.Header["kid"] = "mobile-test"
		raw, err := value.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	const athleteID = "00000000-0000-0000-0000-000000000002"
	body, contentType := multipartBatch(t, []recordFixture{{dataset: "activities", key: "mobile-activity", payload: json.RawMessage(`{"athlete":"jopi","latitude":40.1}`)}}, nil, "")
	request := httptest.NewRequest(http.MethodPost, "/api/v2/athletes/"+athleteID+"/ingest/garmin/batch", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token("fitness:write"))
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	handler.MobileBatchHandler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"athlete":"tim"`)) {
		t.Fatalf("mobile ingest = %d %s", recorder.Code, recorder.Body.String())
	}
	var storedAthlete string
	if err := database.QueryRow("SELECT athlete_id FROM garmin_records WHERE record_key='mobile-activity'").Scan(&storedAthlete); err != nil {
		t.Fatal(err)
	}
	if storedAthlete != athleteID {
		t.Fatalf("mobile body changed target athlete to %s", storedAthlete)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v2/athletes/"+athleteID+"/garmin/sync-status", nil)
	statusRequest.Header.Set("Authorization", "Bearer "+token("fitness:read"))
	statusRecorder := httptest.NewRecorder()
	handler.MobileStatusHandler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK || !bytes.Contains(statusRecorder.Body.Bytes(), []byte(`"activities":"2026-01-02"`)) {
		t.Fatalf("mobile status = %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	missingScope := httptest.NewRequest(http.MethodPost, "/api/v2/athletes/"+athleteID+"/ingest/garmin/batch", bytes.NewReader(body))
	missingScope.Header.Set("Authorization", "Bearer "+token("fitness:read"))
	missingScope.Header.Set("Content-Type", contentType)
	missingScopeRecorder := httptest.NewRecorder()
	handler.MobileBatchHandler().ServeHTTP(missingScopeRecorder, missingScope)
	if missingScopeRecorder.Code != http.StatusForbidden {
		t.Fatalf("missing write scope status = %d, want 403", missingScopeRecorder.Code)
	}

	foreign := httptest.NewRequest(http.MethodGet, "/api/v2/athletes/00000000-0000-0000-0000-000000000099/garmin/sync-status", nil)
	foreign.Header.Set("Authorization", "Bearer "+token("fitness:read"))
	foreignRecorder := httptest.NewRecorder()
	handler.MobileStatusHandler().ServeHTTP(foreignRecorder, foreign)
	if foreignRecorder.Code != http.StatusForbidden {
		t.Fatalf("foreign target status = %d, want 403", foreignRecorder.Code)
	}
}
