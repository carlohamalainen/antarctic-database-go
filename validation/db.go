package validation

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// timeFormat is the canonical layout we use for TEXT timestamps in the
// validation DB. RFC3339Nano is sortable lexicographically.
const timeFormat = time.RFC3339Nano

// OpenValidationDB opens (and creates if missing) a SQLite database file with
// settings appropriate for several reviewers writing concurrently.
func OpenValidationDB(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", err)
	}
	dsn := fmt.Sprintf("file:%s", url.PathEscape(abs))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open validation sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping validation sqlite: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("validation sqlite %s: %w", pragma, err)
		}
	}
	// One writer at a time; reads can be concurrent but we keep this small
	// to avoid SQLITE_BUSY storms.
	db.SetMaxOpenConns(1)
	return db, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(timeFormat) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	return t, nil
}
