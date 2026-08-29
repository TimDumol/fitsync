package migrations

import "embed"

// FS contains the versioned SQLite schema used by fitnessd.
//
//go:embed *.sql
var FS embed.FS
