// Package auth implements the Pocket ID/OIDC boundary for fitnessd.
package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

const (
	PendingLifetime = 15 * time.Minute
	maxTokenBytes   = 16 << 10
	maxBuckets      = 4096
)

// Principal is the verified identity used for all application authorization.
// Username is display-only and is never used as an authorization key.
type Principal struct {
	Issuer      string
	Subject     string
	Username    string
	AthleteID   string
	HouseholdID string
	Scopes      map[string]bool
}

func (p Principal) HasScope(scope string) bool { return p.Scopes[scope] }

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// Error is safe to expose to an API caller. It deliberately contains no token
// or claim material.
type Error struct {
	Code        string
	Status      int
	Description string
}

func (e *Error) Error() string { return e.Code }

type discoveryDocument struct {
	Issuer                string   `json:"issuer"`
	JWKSURI               string   `json:"jwks_uri"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// Discovery contains the advertised OAuth endpoints. The values are retained
// for metadata/configuration consumers, but are never logged.
type Discovery struct {
	Issuer                string
	JWKSURI               string
	AuthorizationEndpoint string
	TokenEndpoint         string
	UserinfoEndpoint      string
	ScopesSupported       []string
}

// Authenticator validates OAuth access tokens and resolves immutable identity
// bindings in SQLite.
type Authenticator struct {
	db           *sql.DB
	issuer       string
	resource     string
	keys         keyfunc.Keyfunc
	discovery    Discovery
	client       *http.Client
	limit        *bucketLimiter
	trustedProxy bool
	close        context.CancelFunc
}

// New fetches discovery from the exact configured issuer and uses its
// advertised jwks_uri. keyfunc refreshes periodically and on unknown kids.
func New(ctx context.Context, database *sql.DB, issuer, resource string, client *http.Client, trustedProxy ...bool) (*Authenticator, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" || resource == "" {
		return nil, errors.New("OIDC issuer and MCP resource are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	discovery, err := fetchDiscovery(ctx, issuer, client)
	if err != nil {
		return nil, err
	}
	if discovery.Issuer != issuer {
		return nil, errors.New("OIDC discovery issuer does not match configured issuer")
	}
	if err := validHTTPURL(discovery.JWKSURI); err != nil {
		return nil, fmt.Errorf("invalid jwks_uri: %w", err)
	}
	if discovery.UserinfoEndpoint != "" {
		if err := validHTTPURL(discovery.UserinfoEndpoint); err != nil {
			return nil, fmt.Errorf("invalid userinfo_endpoint: %w", err)
		}
	}
	keyCtx, cancel := context.WithCancel(ctx)
	keys, err := keyfunc.NewDefaultOverrideCtx(keyCtx, []string{discovery.JWKSURI}, keyfunc.Override{
		Client:           client,
		RefreshInterval:  time.Hour,
		HTTPTimeout:      10 * time.Second,
		RateLimitWaitMax: 5 * time.Second,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("configure JWKS: %w", err)
	}
	proxy := false
	if len(trustedProxy) > 0 {
		proxy = trustedProxy[0]
	}
	return &Authenticator{
		db: database, issuer: issuer, resource: resource, keys: keys,
		discovery: Discovery{Issuer: discovery.Issuer, JWKSURI: discovery.JWKSURI,
			AuthorizationEndpoint: discovery.AuthorizationEndpoint, TokenEndpoint: discovery.TokenEndpoint,
			UserinfoEndpoint: discovery.UserinfoEndpoint,
			ScopesSupported:  append([]string(nil), discovery.ScopesSupported...)},
		client: client,
		limit:  newBucketLimiter(), trustedProxy: proxy, close: cancel,
	}, nil
}

func (a *Authenticator) Close() {
	if a != nil && a.close != nil {
		a.close()
	}
}
func (a *Authenticator) Issuer() string       { return a.issuer }
func (a *Authenticator) Resource() string     { return a.resource }
func (a *Authenticator) Discovery() Discovery { return a.discovery }

func fetchDiscovery(ctx context.Context, issuer string, client *http.Client) (discoveryDocument, error) {
	endpoint := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("create OIDC discovery request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("fetch OIDC discovery: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return discoveryDocument{}, fmt.Errorf("OIDC discovery returned HTTP %d", response.StatusCode)
	}
	var document discoveryDocument
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&document); err != nil {
		return discoveryDocument{}, fmt.Errorf("decode OIDC discovery: %w", err)
	}
	if document.Issuer == "" || document.JWKSURI == "" {
		return discoveryDocument{}, errors.New("OIDC discovery is missing issuer or jwks_uri")
	}
	return document, nil
}

func validHTTPURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return errors.New("URL must be absolute HTTP(S)")
	}
	return nil
}

// Authenticate validates a bearer OAuth access token. requiredScope may be
// empty for the MCP transport, where individual tools check their scopes.
func (a *Authenticator) Authenticate(r *http.Request, requiredScope string) (Principal, error) {
	if cached, ok := PrincipalFromContext(r.Context()); ok {
		if requiredScope != "" && !cached.HasScope(requiredScope) {
			return Principal{}, insufficientScope(requiredScope)
		}
		return cached, nil
	}
	if !a.limit.allow("ip:"+ClientIP(r, a.trustedProxy), time.Now()) {
		return Principal{}, &Error{Code: "rate_limited", Status: http.StatusTooManyRequests, Description: "too many requests"}
	}
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(value, "Bearer ")) == "" {
		return Principal{}, &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "bearer access token required"}
	}
	raw := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	if len(raw) > maxTokenBytes {
		return Principal{}, &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token is too large"}
	}
	claims := accessClaims{}
	token, err := jwt.ParseWithClaims(raw, &claims, a.keys.KeyfuncCtx(r.Context()),
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(a.issuer), jwt.WithAudience(a.resource), jwt.WithExpirationRequired())
	if err != nil || token == nil || !token.Valid {
		return Principal{}, &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token validation failed"}
	}
	if typ, _ := token.Header["typ"].(string); strings.EqualFold(typ, "id_token") || strings.EqualFold(typ, "id-token") || claims.Nonce != "" || len(claims.AuthTime) != 0 {
		return Principal{}, &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "ID tokens are not accepted"}
	}
	if claims.Subject == "" {
		return Principal{}, &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token subject is missing"}
	}
	scopes := parseScopes(claims.Scope, claims.SCP)
	if requiredScope != "" && !scopes[requiredScope] {
		return Principal{}, insufficientScope(requiredScope)
	}
	key := a.issuer + "\x00" + claims.Subject
	if !a.limit.allow(key, time.Now()) {
		return Principal{}, &Error{Code: "rate_limited", Status: http.StatusTooManyRequests, Description: "too many requests"}
	}

	username := claims.PreferredUsername
	if username == "" {
		username = claims.Username
	}
	principal, err := a.resolveIdentity(r.Context(), claims.Subject, username, raw, scopes)
	if err != nil {
		return Principal{}, err
	}
	return principal, nil
}

// userInfoUsername obtains the display username when a provider keeps user
// claims out of JWT access tokens. The UserInfo subject must match the already
// validated access-token subject before any username is trusted for pending
// identity approval.
func (a *Authenticator) userInfoUsername(ctx context.Context, accessToken, subject string) (string, error) {
	if a.discovery.UserinfoEndpoint == "" {
		return "", &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token username is unavailable"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.discovery.UserinfoEndpoint, nil)
	if err != nil {
		return "", &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token username is unavailable"}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := a.client.Do(request)
	if err != nil {
		return "", &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token username is unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token username is unavailable"}
	}

	var claims struct {
		Subject           string `json:"sub"`
		PreferredUsername string `json:"preferred_username"`
		Username          string `json:"username"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&claims); err != nil {
		return "", &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token username is unavailable"}
	}
	if claims.Subject != subject {
		return "", &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "access token subject does not match user information"}
	}
	username := strings.TrimSpace(claims.PreferredUsername)
	if username == "" {
		username = strings.TrimSpace(claims.Username)
	}
	if username == "" {
		return "", &Error{Code: "invalid_token", Status: http.StatusUnauthorized, Description: "user information has no username"}
	}
	return username, nil
}

type accessClaims struct {
	jwt.RegisteredClaims
	Scope             string          `json:"scope"`
	SCP               []string        `json:"scp"`
	PreferredUsername string          `json:"preferred_username"`
	Username          string          `json:"username"`
	Nonce             string          `json:"nonce"`
	AuthTime          json.RawMessage `json:"auth_time"`
}

func parseScopes(scope string, scp []string) map[string]bool {
	result := make(map[string]bool)
	for _, item := range strings.Fields(scope) {
		result[item] = true
	}
	for _, item := range scp {
		if strings.TrimSpace(item) != "" {
			result[item] = true
		}
	}
	return result
}

func insufficientScope(scope string) error {
	return &Error{Code: "insufficient_scope", Status: http.StatusForbidden, Description: "required scope is missing: " + scope}
}

func (a *Authenticator) resolveIdentity(ctx context.Context, subject, username, accessToken string, scopes map[string]bool) (Principal, error) {
	var principal Principal
	var active int
	var disabled sql.NullInt64
	var storedUsername string
	err := a.db.QueryRowContext(ctx, `SELECT i.athlete_id, a.household_id, a.active, i.disabled_at, COALESCE(i.signed_username,'')
		FROM athlete_identities i JOIN athletes a ON a.id=i.athlete_id
		WHERE i.issuer=? AND i.subject=?`, a.issuer, subject).Scan(&principal.AthleteID, &principal.HouseholdID, &active, &disabled, &storedUsername)
	if errors.Is(err, sql.ErrNoRows) {
		if username == "" {
			username, err = a.userInfoUsername(ctx, accessToken, subject)
			if err != nil {
				return Principal{}, err
			}
		}
		if err := a.upsertPending(ctx, subject, username); err != nil {
			return Principal{}, &Error{Code: "authorization_unavailable", Status: http.StatusInternalServerError, Description: "identity lookup failed"}
		}
		return Principal{}, &Error{Code: "identity_pending", Status: http.StatusForbidden, Description: "identity approval is pending"}
	}
	if err != nil {
		return Principal{}, &Error{Code: "authorization_unavailable", Status: http.StatusInternalServerError, Description: "identity lookup failed"}
	}
	if active != 1 || disabled.Valid {
		return Principal{}, &Error{Code: "identity_disabled", Status: http.StatusForbidden, Description: "identity is disabled"}
	}
	if username == "" {
		username = storedUsername
	}
	principal.Issuer, principal.Subject, principal.Username, principal.Scopes = a.issuer, subject, username, scopes
	return principal, nil
}

func (a *Authenticator) upsertPending(ctx context.Context, subject, username string) error {
	now := time.Now().UnixMilli()
	_, err := a.db.ExecContext(ctx, `INSERT INTO pending_identities
		(id, issuer, subject, signed_username, first_seen_at, last_seen_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(issuer, subject) DO UPDATE SET signed_username=excluded.signed_username,
		last_seen_at=excluded.last_seen_at, expires_at=excluded.expires_at`,
		uuid.NewString(), a.issuer, subject, nullableString(username), now, now, time.Now().Add(PendingLifetime).UnixMilli())
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// AuthorizeTarget verifies that a resolved principal may address the target.
// It intentionally returns the same denial for an absent or foreign athlete.
func (a *Authenticator) AuthorizeTarget(ctx context.Context, principal Principal, targetID string) error {
	if targetID == "" {
		return nil
	}
	var found int
	if err := a.db.QueryRowContext(ctx, "SELECT 1 FROM athletes WHERE id=? AND household_id=?", targetID, principal.HouseholdID).Scan(&found); err != nil {
		return &Error{Code: "forbidden", Status: http.StatusForbidden, Description: "athlete is not available"}
	}
	return nil
}

// Middleware authenticates the MCP transport and puts the principal in the
// request context. Tool handlers perform the scope and target checks.
func (a *Authenticator) Middleware(requiredScope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := a.Authenticate(r, requiredScope)
		if err != nil {
			writeAuthError(w, a.resource, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
}

func writeAuthError(w http.ResponseWriter, resource string, err error) {
	var authErr *Error
	if errors.As(err, &authErr) {
		if authErr.Status == http.StatusUnauthorized {
			metadata := protectedResourceMetadataURL(resource)
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadata+`", scope="fitness:read fitness:write"`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(authErr.Status)
		_, _ = w.Write([]byte(`{"error":"` + authErr.Code + `"}`))
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
}

func protectedResourceMetadataURL(resource string) string {
	parsed, err := url.Parse(resource)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return resource + "/.well-known/oauth-protected-resource"
	}
	parsed.Path = "/.well-known/oauth-protected-resource"
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String()
}

type bucket struct {
	limiter *rate.Limiter
	touched time.Time
}
type bucketLimiter struct {
	mu      sync.Mutex
	buckets map[string]bucket
}

func newBucketLimiter() *bucketLimiter { return &bucketLimiter{buckets: make(map[string]bucket)} }

func (l *bucketLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for name, item := range l.buckets {
		if now.Sub(item.touched) > 10*time.Minute {
			delete(l.buckets, name)
		}
	}
	item, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxBuckets {
			var oldest string
			var at time.Time
			for name, candidate := range l.buckets {
				if oldest == "" || candidate.touched.Before(at) {
					oldest, at = name, candidate.touched
				}
			}
			delete(l.buckets, oldest)
		}
		item = bucket{limiter: rate.NewLimiter(rate.Limit(10), 20)}
	}
	item.touched = now
	l.buckets[key] = item
	return item.limiter.AllowN(now, 1)
}

// Admin provides local-only identity management operations.
type Admin struct{ db *sql.DB }

func NewAdmin(database *sql.DB) *Admin { return &Admin{db: database} }

type PendingIdentity struct {
	ID, Issuer, Subject, Username      string
	FirstSeenAt, LastSeenAt, ExpiresAt int64
}

func (a *Admin) ListPending(ctx context.Context) ([]PendingIdentity, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id, issuer, subject, COALESCE(signed_username,''), first_seen_at, last_seen_at, expires_at FROM pending_identities ORDER BY first_seen_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PendingIdentity
	for rows.Next() {
		var item PendingIdentity
		if err := rows.Scan(&item.ID, &item.Issuer, &item.Subject, &item.Username, &item.FirstSeenAt, &item.LastSeenAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// Bind provisions an identity from an administrator-verified Pocket ID
// subject. The caller is responsible for obtaining the subject and username
// from Pocket ID's admin API; this method enforces the local binding
// invariants and never treats the username as an authorization key.
func (a *Admin) Bind(ctx context.Context, issuer, subject, username, handle string) error {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	subject = strings.TrimSpace(subject)
	username = strings.TrimSpace(username)
	handle = strings.TrimSpace(handle)
	if issuer == "" || subject == "" {
		return errors.New("issuer and subject are required")
	}
	if handle != "tim" && handle != "jopi" {
		return errors.New("athlete must be tim or jopi")
	}
	if username != handle {
		return errors.New("Pocket ID username does not match selected athlete")
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(cause error) error { _ = tx.Rollback(); return cause }

	var athleteID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM athletes WHERE handle=?", handle).Scan(&athleteID); err != nil {
		return rollback(err)
	}

	var existingAthleteID string
	err = tx.QueryRowContext(ctx, "SELECT athlete_id FROM athlete_identities WHERE issuer=? AND subject=?", issuer, subject).Scan(&existingAthleteID)
	if err == nil && existingAthleteID != athleteID {
		return rollback(errors.New("subject is already bound to another athlete"))
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return rollback(err)
	}

	var existingID, existingIssuer, existingSubject string
	var existingDisabled sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, issuer, subject, disabled_at FROM athlete_identities WHERE athlete_id=?`, athleteID).Scan(&existingID, &existingIssuer, &existingSubject, &existingDisabled)
	if err == nil {
		if existingIssuer == issuer && existingSubject == subject && !existingDisabled.Valid {
			if _, err := tx.ExecContext(ctx, "DELETE FROM pending_identities WHERE issuer=? AND subject=?", issuer, subject); err != nil {
				return rollback(err)
			}
			return tx.Commit()
		}
		if existingDisabled.Valid {
			return rollback(errors.New("athlete identity binding is disabled"))
		}
		return rollback(errors.New("athlete already has a different identity binding"))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return rollback(err)
	}

	now := time.Now().UnixMilli()
	identityID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO athlete_identities (id, athlete_id, issuer, subject, signed_username, approved_at, created_at) VALUES (?,?,?,?,?,?,?)`, identityID, athleteID, issuer, subject, username, now, now); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE athletes SET active=1 WHERE id=?", athleteID); err != nil {
		return rollback(err)
	}
	// The existing audit event records an administrator-approved identity
	// provisioning, whether the source was a pending login or Pocket ID API.
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_audit_log (id, athlete_id, identity_id, event, occurred_at) VALUES (?,?,?,?,?)`, uuid.NewString(), athleteID, identityID, "approve", now); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM pending_identities WHERE issuer=? AND subject=?", issuer, subject); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

func (a *Admin) Approve(ctx context.Context, pendingID, handle string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(cause error) error { _ = tx.Rollback(); return cause }
	var issuer, subject, username string
	var expires int64
	if err := tx.QueryRowContext(ctx, `SELECT issuer, subject, COALESCE(signed_username,''), expires_at FROM pending_identities WHERE id=?`, pendingID).Scan(&issuer, &subject, &username, &expires); err != nil {
		return rollback(err)
	}
	if expires <= time.Now().UnixMilli() {
		return rollback(errors.New("pending identity has expired"))
	}
	var athleteID, householdID string
	if err := tx.QueryRowContext(ctx, "SELECT id, household_id FROM athletes WHERE handle=?", handle).Scan(&athleteID, &householdID); err != nil {
		return rollback(err)
	}
	if username == "" || username != handle {
		return rollback(errors.New("issuer-provided username does not match selected athlete"))
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM athlete_identities WHERE athlete_id=?", athleteID).Scan(&count); err != nil {
		return rollback(err)
	}
	if count != 0 {
		return rollback(errors.New("athlete already has an identity binding"))
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM athlete_identities WHERE issuer=? AND subject=?", issuer, subject).Scan(&count); err != nil {
		return rollback(err)
	}
	if count != 0 {
		return rollback(errors.New("identity is already bound"))
	}
	now := time.Now().UnixMilli()
	identityID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO athlete_identities (id, athlete_id, issuer, subject, signed_username, approved_at, created_at) VALUES (?,?,?,?,?,?,?)`, identityID, athleteID, issuer, subject, username, now, now); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE athletes SET active=1 WHERE id=?", athleteID); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_audit_log (id, athlete_id, identity_id, event, occurred_at) VALUES (?,?,?,?,?)`, uuid.NewString(), athleteID, identityID, "approve", now); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM pending_identities WHERE id=?", pendingID); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

func (a *Admin) Disable(ctx context.Context, handle string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(cause error) error { _ = tx.Rollback(); return cause }
	var athleteID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM athletes WHERE handle=?", handle).Scan(&athleteID); err != nil {
		return rollback(err)
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, "UPDATE athletes SET active=0 WHERE id=?", athleteID); err != nil {
		return rollback(err)
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM athlete_identities WHERE athlete_id=? AND disabled_at IS NULL", athleteID)
	if err != nil {
		return rollback(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return rollback(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return rollback(err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE athlete_identities SET disabled_at=? WHERE id=?", now, id); err != nil {
			return rollback(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO identity_audit_log (id, athlete_id, identity_id, event, occurred_at) VALUES (?,?,?,?,?)`, uuid.NewString(), athleteID, id, "disable", now); err != nil {
			return rollback(err)
		}
	}
	return tx.Commit()
}

func (a *Admin) ListIdentities(ctx context.Context) ([]map[string]any, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT i.id, a.handle, i.issuer, i.subject, i.approved_at, i.disabled_at FROM athlete_identities i JOIN athletes a ON a.id=i.athlete_id ORDER BY a.handle`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var id, handle, issuer, subject string
		var approved int64
		var disabled sql.NullInt64
		if err := rows.Scan(&id, &handle, &issuer, &subject, &approved, &disabled); err != nil {
			return nil, err
		}
		item := map[string]any{"id": id, "athlete": handle, "issuer": issuer, "subject": subject, "approved_at": approved, "active": !disabled.Valid}
		result = append(result, item)
	}
	return result, rows.Err()
}

func ClientIP(r *http.Request, trustedProxy bool) string {
	if trustedProxy {
		if value := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(value) != nil {
			return value
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
