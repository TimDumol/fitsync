package httpapi

import (
	"errors"
	"net"
	"net/http"
	"strings"

	fitnessauth "github.com/timdumol/fitsync/internal/auth"
)

// Authorizer is deliberately injected so the handlers can be tested with a
// fake household principal before Pocket ID middleware lands in Step 7.
type Authorizer interface {
	Authorize(r *http.Request, athleteID string) error
}

type HouseholdAuthorizer interface {
	HouseholdID(r *http.Request) (string, error)
}

// FakeHouseholdAuthorizer represents an already-authorized household in tests.
type FakeHouseholdAuthorizer struct{}

func (FakeHouseholdAuthorizer) Authorize(_ *http.Request, _ string) error { return nil }

// LocalOnlyAuthorizer keeps these handlers off the public interface until the
// real authenticated authorizer is installed.
type LocalOnlyAuthorizer struct{}

func (LocalOnlyAuthorizer) Authorize(r *http.Request, _ string) error {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("local authorization required")
}

// PocketIDAuthorizer adapts the verified OIDC principal to the REST
// Authorizer interface. The target check is always household-scoped.
type PocketIDAuthorizer struct {
	Authenticator *fitnessauth.Authenticator
}

func NewPocketIDAuthorizer(authenticator *fitnessauth.Authenticator) PocketIDAuthorizer {
	return PocketIDAuthorizer{Authenticator: authenticator}
}

func (a PocketIDAuthorizer) Authorize(r *http.Request, athleteID string) error {
	if a.Authenticator == nil {
		return errors.New("OIDC authorization is unavailable")
	}
	required := "fitness:read"
	if (r.Method == http.MethodPost || r.Method == http.MethodPut) &&
		(strings.HasPrefix(r.URL.Path, "/api/v2/") || r.URL.Path == "/api/v1/sync/push") {
		required = "fitness:write"
	}
	principal, err := a.Authenticator.Authenticate(r, required)
	if err != nil {
		return err
	}
	return a.Authenticator.AuthorizeTarget(r.Context(), principal, athleteID)
}

func (a PocketIDAuthorizer) Middleware(next http.Handler) http.Handler {
	if a.Authenticator == nil {
		return next
	}
	return a.Authenticator.Middleware("", next)
}

func writeAuthorizationError(w http.ResponseWriter, err error) {
	var authErr *fitnessauth.Error
	if errors.As(err, &authErr) {
		writeError(w, authErr.Status, authErr.Code)
		return
	}
	writeError(w, http.StatusForbidden, "forbidden")
}
