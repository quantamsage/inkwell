// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds runtime settings. Every field has a sensible default for local
// development so the app runs with a plain `go run ./cmd/web`.
type Config struct {
	// Addr is the TCP address the HTTP server listens on.
	Addr string
	// DatabasePath is the SQLite database file location.
	DatabasePath string
	// MaxDocumentBytes caps the size of a document's Markdown content and of
	// imported files.
	MaxDocumentBytes int64
	// SecureCookies marks cookies Secure (enable when served over HTTPS).
	SecureCookies bool
}

const (
	defaultAddr             = "127.0.0.1:8080"
	defaultDatabasePath     = "data/markdown-editor.db"
	defaultMaxDocumentBytes = 5 << 20 // 5 MiB
)

// Load reads configuration from the environment.
//
//	ADDR                 listen address (default 127.0.0.1:8080)
//	DATABASE_PATH        SQLite file path (default data/markdown-editor.db)
//	MAX_DOCUMENT_BYTES   max document/import size in bytes (default 5242880)
//	SECURE_COOKIES       "true" to set the Secure flag on cookies (default false)
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:             defaultAddr,
		DatabasePath:     defaultDatabasePath,
		MaxDocumentBytes: defaultMaxDocumentBytes,
	}

	if v := strings.TrimSpace(getenv("ADDR")); v != "" {
		cfg.Addr = v
	}
	if v := strings.TrimSpace(getenv("DATABASE_PATH")); v != "" {
		cfg.DatabasePath = v
	}
	if v := strings.TrimSpace(getenv("MAX_DOCUMENT_BYTES")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1024 || n > 100<<20 {
			return Config{}, fmt.Errorf("MAX_DOCUMENT_BYTES must be an integer between 1024 and %d", 100<<20)
		}
		cfg.MaxDocumentBytes = n
	}
	if v := strings.TrimSpace(getenv("SECURE_COOKIES")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("SECURE_COOKIES must be a boolean: %w", err)
		}
		cfg.SecureCookies = b
	}
	return cfg, nil
}
