package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/timdumol/fitsync/internal/db"
)

func TestOIDCAccessTokenValidationAndPendingApproval(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "jwks_uri": server.URL + "/jwks", "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token", "userinfo_endpoint": server.URL + "/userinfo"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})}}})
		case "/userinfo":
			if r.Header.Get("Authorization") == "" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"sub": "userinfo-sub", "preferred_username": "jopi"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	database, err := db.OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO athlete_identities (id, athlete_id, issuer, subject, signed_username, approved_at, created_at) VALUES ('identity-tim', '00000000-0000-0000-0000-000000000002', ?, 'tim-sub', 'tim', 1, 1)`, server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE athletes SET active=1 WHERE handle='tim'"); err != nil {
		t.Fatal(err)
	}
	authenticator, err := New(context.Background(), database, server.URL, "https://api.example.test/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer authenticator.Close()

	valid := func(subject, username string, extra map[string]any) *http.Request {
		claims := jwt.MapClaims{"iss": server.URL, "sub": subject, "aud": "https://api.example.test/mcp", "exp": time.Now().Add(time.Hour).Unix(), "nbf": time.Now().Add(-time.Minute).Unix(), "scope": "fitness:read fitness:write", "preferred_username": username}
		for k, v := range extra {
			claims[k] = v
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "test-key"
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/athletes/00000000-0000-0000-0000-000000000002/health/daily", nil)
		request.Header.Set("Authorization", "Bearer "+raw)
		return request
	}
	principal, err := authenticator.Authenticate(valid("tim-sub", "tim", nil), "fitness:read")
	if err != nil {
		t.Fatal(err)
	}
	if principal.AthleteID != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("resolved athlete = %s", principal.AthleteID)
	}
	storedPrincipal, err := authenticator.Authenticate(valid("tim-sub", "", map[string]any{"scope": "fitness:read"}), "fitness:read")
	if err != nil {
		t.Fatalf("bound identity without token username rejected: %v", err)
	}
	if storedPrincipal.Username != "tim" {
		t.Fatalf("stored username = %q, want tim", storedPrincipal.Username)
	}
	if err := authenticator.AuthorizeTarget(context.Background(), principal, "00000000-0000-0000-0000-000000000003"); err != nil {
		t.Fatalf("same-household target rejected: %v", err)
	}

	_, err = authenticator.Authenticate(valid("new-sub", "jopi", nil), "fitness:read")
	if err == nil || err.(*Error).Code != "identity_pending" {
		t.Fatalf("unknown identity error = %v, want identity_pending", err)
	}
	var pendingID string
	if err := database.QueryRow("SELECT id FROM pending_identities WHERE subject='new-sub'").Scan(&pendingID); err != nil {
		t.Fatal(err)
	}
	if err := NewAdmin(database).Approve(context.Background(), pendingID, "jopi"); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticator.Authenticate(valid("new-sub", "jopi", nil), "fitness:read"); err != nil {
		t.Fatalf("approved identity rejected: %v", err)
	}

	_, err = authenticator.Authenticate(valid("userinfo-sub", "", map[string]any{"scope": "openid profile fitness:read fitness:write"}), "fitness:read")
	if err == nil || err.(*Error).Code != "identity_pending" {
		t.Fatalf("userinfo identity error = %v, want identity_pending", err)
	}
	var signedUsername string
	if err := database.QueryRow("SELECT signed_username FROM pending_identities WHERE subject='userinfo-sub'").Scan(&signedUsername); err != nil {
		t.Fatal(err)
	}
	if signedUsername != "jopi" {
		t.Fatalf("userinfo signed username = %q, want jopi", signedUsername)
	}

	_, err = authenticator.Authenticate(valid("mismatched-sub", "", map[string]any{"scope": "openid profile fitness:read fitness:write"}), "fitness:read")
	if err == nil || err.(*Error).Code != "invalid_token" {
		t.Fatalf("mismatched userinfo error = %v, want invalid_token", err)
	}

	checks := []struct {
		name    string
		request *http.Request
		scope   string
	}{
		{"wrong audience", valid("tim-sub", "tim", map[string]any{"aud": "other"}), "fitness:read"},
		{"wrong issuer", valid("tim-sub", "tim", map[string]any{"iss": "other"}), "fitness:read"},
		{"missing scope", valid("tim-sub", "tim", map[string]any{"scope": "fitness:read"}), "fitness:write"},
		{"expired", valid("tim-sub", "tim", map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}), "fitness:read"},
		{"future nbf", valid("tim-sub", "tim", map[string]any{"nbf": time.Now().Add(time.Minute).Unix()}), "fitness:read"},
		{"ID token", valid("tim-sub", "tim", map[string]any{"nonce": "synthetic"}), "fitness:read"},
	}
	for _, check := range checks {
		if _, err := authenticator.Authenticate(check.request, check.scope); err == nil {
			t.Errorf("%s accepted token", check.name)
		}
	}

	// A different signing algorithm never reaches identity lookup.
	hmac := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": server.URL, "sub": "tim-sub", "aud": "https://api.example.test/mcp", "exp": time.Now().Add(time.Hour).Unix(), "scope": "fitness:read"})
	text, err := hmac.SignedString([]byte("not-a-key"))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+text)
	if _, err := authenticator.Authenticate(request, "fitness:read"); err == nil {
		t.Fatal("HS256 token accepted")
	}
}

func TestAdminBindIsIdempotentAndRejectsConflicts(t *testing.T) {
	database, err := db.OpenAndMigrate(context.Background(), filepath.Join(t.TempDir(), "fitness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	admin := NewAdmin(database)
	issuer := "https://auth.example.test"
	if err := admin.Bind(context.Background(), issuer, "tim-subject", "tim", "tim"); err != nil {
		t.Fatalf("bind tim: %v", err)
	}

	var active int
	if err := database.QueryRow("SELECT active FROM athletes WHERE handle='tim'").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("tim active = %d, want 1", active)
	}
	var identityCount, auditCount int
	if err := database.QueryRow("SELECT count(*) FROM athlete_identities WHERE issuer=? AND subject=?", issuer, "tim-subject").Scan(&identityCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT count(*) FROM identity_audit_log WHERE event='approve'").Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if identityCount != 1 || auditCount != 1 {
		t.Fatalf("after first bind identity_count=%d audit_count=%d, want 1 and 1", identityCount, auditCount)
	}

	if _, err := database.Exec(`INSERT INTO pending_identities (id, issuer, subject, signed_username, first_seen_at, last_seen_at, expires_at) VALUES ('pending-tim', ?, 'tim-subject', 'tim', 1, 1, 9999999999999)`, issuer); err != nil {
		t.Fatal(err)
	}
	if err := admin.Bind(context.Background(), issuer, "tim-subject", "tim", "tim"); err != nil {
		t.Fatalf("idempotent bind tim: %v", err)
	}
	var pendingCount int
	if err := database.QueryRow("SELECT count(*) FROM pending_identities WHERE issuer=? AND subject=?", issuer, "tim-subject").Scan(&pendingCount); err != nil {
		t.Fatal(err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending count after idempotent bind = %d, want 0", pendingCount)
	}
	if err := database.QueryRow("SELECT count(*) FROM identity_audit_log WHERE event='approve'").Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count after idempotent bind = %d, want 1", auditCount)
	}

	if err := admin.Bind(context.Background(), issuer, "tim-subject", "tim", "jopi"); err == nil {
		t.Fatal("subject was rebound to another athlete")
	}
	if err := admin.Bind(context.Background(), issuer, "jopi-subject", "wrong", "jopi"); err == nil {
		t.Fatal("mismatched Pocket ID username was accepted")
	}
	if err := admin.Bind(context.Background(), issuer, "jopi-subject", "jopi", "jopi"); err != nil {
		t.Fatalf("bind jopi: %v", err)
	}
}
