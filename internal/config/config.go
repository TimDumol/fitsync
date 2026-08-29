// Package config loads fitnessd's non-secret runtime configuration.
package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress           = ":8080"
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadBodyTimeout   = 30 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultDatabasePath      = "/data/fitness.db"
	defaultIssuer            = "https://auth.aries.timdumol.com"
	defaultResource          = "https://api.aries.timdumol.com/mcp"
)

// Config contains runtime settings. Secret material is deliberately not part
// of this structure; callers should use Secret when they explicitly need one.
type Config struct {
	Address           string
	DatabasePath      string
	ReadHeaderTimeout time.Duration
	ReadBodyTimeout   time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	ArchiveRoot       string
	Issuer            string
	Resource          string
	TrustedProxy      bool
	IngestTokenHashes map[string][sha256.Size]byte
}

// Load reads FITNESSD_* settings and applies safe defaults. It does not log
// environment values or read any secret unless Secret is called explicitly.
func Load() (Config, error) {
	c := Config{
		Address:           envOrDefault("FITNESSD_ADDR", defaultAddress),
		DatabasePath:      envOrDefaultAlias("FITNESS_DB_PATH", "DB_PATH", defaultDatabasePath),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadBodyTimeout:   defaultReadBodyTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		ShutdownTimeout:   defaultShutdownTimeout,
		ArchiveRoot:       envOrDefaultAlias("FITNESS_ARCHIVE_ROOT", "ARCHIVE_DIR", "/data"),
		Issuer:            envOrDefault("POCKETID_ISSUER", defaultIssuer),
		Resource:          envOrDefault("MCP_RESOURCE", defaultResource),
		IngestTokenHashes: make(map[string][sha256.Size]byte),
	}
	if value := os.Getenv("TRUST_PROXY"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil { return Config{}, errors.New("TRUST_PROXY must be true or false") }
		c.TrustedProxy = parsed
	}

	for _, setting := range []struct {
		name   string
		target *time.Duration
	}{
		{"FITNESSD_READ_HEADER_TIMEOUT", &c.ReadHeaderTimeout},
		{"FITNESSD_READ_BODY_TIMEOUT", &c.ReadBodyTimeout},
		{"FITNESSD_WRITE_TIMEOUT", &c.WriteTimeout},
		{"FITNESSD_IDLE_TIMEOUT", &c.IdleTimeout},
		{"FITNESSD_SHUTDOWN_TIMEOUT", &c.ShutdownTimeout},
	} {
		if value, ok := os.LookupEnv(setting.name); ok {
			duration, err := time.ParseDuration(value)
			if err != nil || duration <= 0 {
				return Config{}, fmt.Errorf("%s must be a positive duration", setting.name)
			}
			*setting.target = duration
		}
	}
	for _, athlete := range []string{"tim", "jopi"} {
		name := "FITNESS_INGEST_" + strings.ToUpper(athlete) + "_TOKEN"
		secret, present, err := OptionalSecret(name)
		if err != nil {
			return Config{}, err
		}
		if present {
			if secret == "" {
				return Config{}, fmt.Errorf("%s must not be empty", name)
			}
			c.IngestTokenHashes[athlete] = sha256.Sum256([]byte(secret))
		}
	}
	return c, nil
}

// Secret reads a secret from NAME_FILE when configured, or from NAME. Setting
// both is rejected so deployments cannot silently use the wrong credential.
func Secret(name string) (string, error) {
	fileName := os.Getenv(name + "_FILE")
	direct, directSet := os.LookupEnv(name)
	if fileName != "" && directSet {
		return "", errors.New("both direct and file-backed secret are configured")
	}
	if fileName != "" {
		value, err := os.ReadFile(fileName)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name+"_FILE", err)
		}
		return strings.TrimSpace(string(value)), nil
	}
	if directSet {
		return direct, nil
	}
	return "", fmt.Errorf("secret %s is not configured", name)
}

// OptionalSecret is the file-backed equivalent for optional credentials.
func OptionalSecret(name string) (string, bool, error) {
	fileName := os.Getenv(name + "_FILE")
	direct, directSet := os.LookupEnv(name)
	if fileName != "" && directSet {
		return "", false, errors.New("both direct and file-backed secret are configured")
	}
	if fileName != "" {
		value, err := os.ReadFile(fileName)
		if err != nil {
			return "", false, fmt.Errorf("read %s: %w", name+"_FILE", err)
		}
		return strings.TrimSpace(string(value)), true, nil
	}
	if directSet {
		return direct, true, nil
	}
	return "", false, nil
}

func envOrDefault(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok && value != "" {
		return value
	}
	return fallback
}

func envOrDefaultAlias(primary, alias, fallback string) string {
	if value, ok := os.LookupEnv(primary); ok && value != "" { return value }
	if value, ok := os.LookupEnv(alias); ok && value != "" { return value }
	return fallback
}

// ParsePositiveInt is kept small and dependency-free for command-line bounds.
func ParsePositiveInt(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return n, nil
}
