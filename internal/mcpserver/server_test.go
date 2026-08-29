package mcpserver

import (
	"testing"

	fitnessauth "github.com/timdumol/fitsync/internal/auth"
	"github.com/timdumol/fitsync/internal/httpapi"
)

func TestNewRegistersTools(t *testing.T) {
	if _, err := New(&httpapi.API{}, &fitnessauth.Authenticator{}); err != nil {
		t.Fatalf("New returned error: %v", err)
	}
}
