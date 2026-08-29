package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretFileAndConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FITNESS_TEST_SECRET_FILE", path)
	got, err := Secret("FITNESS_TEST_SECRET")
	if err != nil || got != "file-secret" {
		t.Fatalf("Secret() = %q, %v", got, err)
	}

	t.Setenv("FITNESS_TEST_SECRET", "direct-secret")
	if _, err := Secret("FITNESS_TEST_SECRET"); err == nil {
		t.Fatal("Secret accepted both direct and file-backed values")
	}
}

func TestLoadRejectsNonPositiveDuration(t *testing.T) {
	t.Setenv("FITNESSD_READ_BODY_TIMEOUT", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a non-positive timeout")
	}
}
