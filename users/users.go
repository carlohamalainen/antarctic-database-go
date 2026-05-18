// Package users manages reviewer accounts and HTTP sessions for the
// OCR-validation web app. The store is a separate SQLite file
// (data/processed/ocr-users.sqlite3 by default); passwords are bcrypt-hashed.
package users

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

const Schema = `
CREATE TABLE IF NOT EXISTS users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    username   TEXT NOT NULL REFERENCES users(username),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_username ON sessions(username);
CREATE INDEX IF NOT EXISTS idx_sessions_expires  ON sessions(expires_at);
`

const (
	SessionLifetime = 48 * time.Hour
	MinPasswordLen  = 8
	timeFormat      = time.RFC3339Nano
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrSessionInvalid     = errors.New("session invalid or expired")
	ErrUserExists         = errors.New("user already exists")
	ErrNoSuchUser         = errors.New("no such user")
)

func Open(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", err)
	}
	dsn := fmt.Sprintf("file:%s", url.PathEscape(abs))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open users sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping users sqlite: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("users sqlite %s: %w", pragma, err)
		}
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func InitSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, Schema)
	return err
}

// NormalizeUsername lowercases and trims. All public APIs accept any case
// from the caller and store the normalized form.
func NormalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validateUsername(u string) error {
	if u == "" {
		return errors.New("username cannot be empty")
	}
	if len(u) > 64 {
		return errors.New("username too long")
	}
	for _, r := range u {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if !ok {
			return fmt.Errorf("invalid character %q in username (allowed: a-z 0-9 - _ .)", r)
		}
	}
	return nil
}

func validatePassword(p string) error {
	if len(p) < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	return nil
}

func CreateUser(ctx context.Context, db *sql.DB, username, password string) error {
	u := NormalizeUsername(username)
	if err := validateUsername(u); err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		u, string(hash), time.Now().UTC().Format(timeFormat))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("%w: %s", ErrUserExists, u)
		}
		return err
	}
	return nil
}

func SetPassword(ctx context.Context, db *sql.DB, username, password string) error {
	u := NormalizeUsername(username)
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE username = ?`, string(hash), u)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchUser, u)
	}
	return nil
}

func DeleteUser(ctx context.Context, db *sql.DB, username string) error {
	u := NormalizeUsername(username)
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE username = ?`, u); err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `DELETE FROM users WHERE username = ?`, u)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchUser, u)
	}
	return nil
}

type UserRow struct {
	Username  string
	CreatedAt time.Time
}

func ListUsers(ctx context.Context, db *sql.DB) ([]UserRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT username, created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var r UserRow
		var ts string
		if err := rows.Scan(&r.Username, &ts); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(timeFormat, ts)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Authenticate returns the canonical (lowercased) username on success.
// On failure it always returns ErrInvalidCredentials, never leaking whether
// the username exists.
func Authenticate(ctx context.Context, db *sql.DB, username, password string) (string, error) {
	u := NormalizeUsername(username)
	var hash string
	err := db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE username = ?`, u).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend cycles bcrypt-comparing a known-wrong hash so the response
		// time doesn't reveal whether the user exists.
		_ = bcrypt.CompareHashAndPassword(
			[]byte("$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXY1234"),
			[]byte(password))
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}
	return u, nil
}

// CreateSession returns a new opaque session token and its absolute expiry.
func CreateSession(ctx context.Context, db *sql.DB, username string) (token string, expires time.Time, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	now := time.Now().UTC()
	expires = now.Add(SessionLifetime)
	_, err = db.ExecContext(ctx,
		`INSERT INTO sessions (token, username, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, NormalizeUsername(username), now.Format(timeFormat), expires.Format(timeFormat))
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// ValidateAndExtend looks up the session, returns its username, and rolls
// the expiry forward by SessionLifetime so active users don't get logged
// out mid-task. Returns ErrSessionInvalid for missing / expired tokens.
func ValidateAndExtend(ctx context.Context, db *sql.DB, token string) (username string, newExpiry time.Time, err error) {
	if token == "" {
		return "", time.Time{}, ErrSessionInvalid
	}
	var u, expS string
	err = db.QueryRowContext(ctx, `SELECT username, expires_at FROM sessions WHERE token = ?`, token).Scan(&u, &expS)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, ErrSessionInvalid
	}
	if err != nil {
		return "", time.Time{}, err
	}
	exp, err := time.Parse(timeFormat, expS)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parse session expiry: %w", err)
	}
	if time.Now().After(exp) {
		return "", time.Time{}, ErrSessionInvalid
	}
	newExpiry = time.Now().UTC().Add(SessionLifetime)
	if _, err := db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE token = ?`,
		newExpiry.Format(timeFormat), token); err != nil {
		return "", time.Time{}, err
	}
	return u, newExpiry, nil
}

func DeleteSession(ctx context.Context, db *sql.DB, token string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// PurgeExpiredSessions cleans up rows past their expires_at. Safe to call
// periodically from a background goroutine.
func PurgeExpiredSessions(ctx context.Context, db *sql.DB) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(timeFormat))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
