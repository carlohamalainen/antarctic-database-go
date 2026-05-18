package users

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := InitSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateUserAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)

	if err := CreateUser(ctx, db, "Alice", "secretpw1"); err != nil {
		t.Fatal(err)
	}
	// Username should be normalized
	u, err := Authenticate(ctx, db, "ALICE", "secretpw1")
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if u != "alice" {
		t.Errorf("got username %q, want alice", u)
	}
	if _, err := Authenticate(ctx, db, "alice", "wrong-pw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: got %v, want ErrInvalidCredentials", err)
	}
	if _, err := Authenticate(ctx, db, "nobody", "anything"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("missing user: got %v, want ErrInvalidCredentials", err)
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	if err := CreateUser(ctx, db, "alice", "secretpw1"); err != nil {
		t.Fatal(err)
	}
	if err := CreateUser(ctx, db, "alice", "another1!"); !errors.Is(err, ErrUserExists) {
		t.Errorf("got %v, want ErrUserExists", err)
	}
}

func TestPasswordTooShort(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	if err := CreateUser(ctx, db, "alice", "short"); err == nil || !strings.Contains(err.Error(), "at least") {
		t.Errorf("expected password length error, got %v", err)
	}
}

func TestSessionRoundtripAndExtend(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	if err := CreateUser(ctx, db, "alice", "secretpw1"); err != nil {
		t.Fatal(err)
	}
	tok, exp, err := CreateSession(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	if time.Until(exp) < SessionLifetime-time.Minute {
		t.Errorf("session expiry too soon: %v", exp)
	}

	// Roll the expiry backward to verify extension lifts it forward again
	if _, err := db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE token = ?`,
		time.Now().UTC().Add(time.Hour).Format(timeFormat), tok); err != nil {
		t.Fatal(err)
	}
	u, newExp, err := ValidateAndExtend(ctx, db, tok)
	if err != nil {
		t.Fatal(err)
	}
	if u != "alice" {
		t.Errorf("got %q, want alice", u)
	}
	if time.Until(newExp) < SessionLifetime-time.Minute {
		t.Errorf("expiry not extended: %v", newExp)
	}
}

func TestSessionExpired(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	if err := CreateUser(ctx, db, "alice", "secretpw1"); err != nil {
		t.Fatal(err)
	}
	tok, _, err := CreateSession(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	// Force expiry into the past
	if _, err := db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE token = ?`,
		"2000-01-01T00:00:00Z", tok); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateAndExtend(ctx, db, tok); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("got %v, want ErrSessionInvalid", err)
	}
}

func TestDeleteUserCascadesSessions(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	if err := CreateUser(ctx, db, "alice", "secretpw1"); err != nil {
		t.Fatal(err)
	}
	tok, _, _ := CreateSession(ctx, db, "alice")
	if err := DeleteUser(ctx, db, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateAndExtend(ctx, db, tok); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("session should be gone after user delete, got %v", err)
	}
}
