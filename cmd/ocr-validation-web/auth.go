package main

import (
	"context"
	"net/http"

	"github.com/carlohamalainen/antarctic-database-go/users"
)

const sessionCookie = "ocr_session"

// Go trick: unexported named type ctxKey (rather than a string 
// like "user") to avoid colision in context.Context
type ctxKey int
const userKey ctxKey = 0

func userFromCtx(ctx context.Context) (string, bool) {
	u, ok := ctx.Value(userKey).(string)
	return u, ok
}

// requireAuth wraps a handler so that requests without a valid session cookie
// are redirected to "/" (which renders the login form). On valid sessions the
// expiry is rolled forward and the username is attached to the request context.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, ok := s.tryAuth(w, r)
		if !ok {
			s.unauthorized(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), userKey, username)
		next(w, r.WithContext(ctx))
	}
}

// tryAuth reads the session cookie, validates+extends it, and (on success)
// rewrites the cookie's expiry to match. Returns ("", false) when there's no
// valid session.
func (s *Server) tryAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	username, newExp, err := users.ValidateAndExtend(r.Context(), s.usersDB, c.Value)
	if err != nil {
		// Clear the bad cookie
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		return "", false
	}
	// Roll cookie expiry forward to match the extended session
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: c.Value, Path: "/",
		Expires: newExp, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	return username, true
}

// unauthorized: send 401 for SSE/non-GET requests so Datastar sees a
// proper error; redirect plain GETs to "/" so a typed URL hits login.
func (s *Server) unauthorized(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Accept") == "text/event-stream" || r.Method != http.MethodGet {
		http.Error(w, "session expired — please log in again", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	u, err := users.Authenticate(r.Context(), s.usersDB, username, password)
	if err != nil {
		s.log.Info("login failed", "username", users.NormalizeUsername(username))
		// Re-render login with a generic error (don't leak which is wrong)
		s.renderPage(w, "login", map[string]any{"Error": "invalid username or password"})
		return
	}
	token, exp, err := users.CreateSession(r.Context(), s.usersDB, u)
	if err != nil {
		s.log.Error("create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		Expires: exp, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	s.log.Info("login ok", "username", u)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = users.DeleteSession(r.Context(), s.usersDB, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

