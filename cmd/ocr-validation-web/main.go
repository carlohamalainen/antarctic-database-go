// ocr-validation-web is a small HTTP server providing the human-review UI
// for OCR validation. The source pipeline DB is opened READ-ONLY and the
// page index is cached in memory at startup so per-request sample picks
// are sub-millisecond.
//
// Companion CLI:
//   - ocr-users   manage reviewer accounts
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
	_ "time/tzdata"

	_ "modernc.org/sqlite"

	"github.com/carlohamalainen/antarctic-database-go/users"
	"github.com/carlohamalainen/antarctic-database-go/validation"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

const (
	defaultSrcDB     = "data/processed/document-pipeline.sqlite3"
	defaultValDB     = "data/processed/ocr-validation.sqlite3"
	defaultUsersDB   = "data/processed/ocr-users.sqlite3"
	defaultAddr      = "127.0.0.1:8080"
	sessionPurgeTick = 1 * time.Hour
)

type Server struct {
	log       *slog.Logger
	srcDB     *sql.DB
	srcSHA256 string
	valDB     *sql.DB
	usersDB   *sql.DB
	sampler   *validation.Sampler
	tmpl      *template.Template
}

func main() {
	var (
		srcPath   = flag.String("db", defaultSrcDB, "path to document-pipeline sqlite (opened read-only)")
		valPath   = flag.String("val-db", defaultValDB, "path to validation sqlite")
		usersPath = flag.String("users-db", defaultUsersDB, "path to users sqlite")
		addr      = flag.String("addr", defaultAddr, "listen address")
		debug     = flag.Bool("debug", false, "enable debug logging")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	srv, err := newServer(log, *srcPath, *valPath, *usersPath)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer srv.close()

	mux := srv.routes()
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Background: periodically purge expired sessions
	go func() {
		t := time.NewTicker(sessionPurgeTick)
		defer t.Stop()
		for range t.C {
			n, err := users.PurgeExpiredSessions(context.Background(), srv.usersDB)
			if err != nil {
				log.Warn("purge sessions", "err", err)
			} else if n > 0 {
				log.Info("purged expired sessions", "count", n)
			}
		}
	}()

	log.Info("listening", "addr", *addr,
		"src_db", *srcPath, "val_db", *valPath, "users_db", *usersPath,
		"page_index_size", srv.sampler.NumPages(),
		"src_sha256", srv.srcSHA256)

	if err := httpSrv.ListenAndServe(); err != nil {
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func newServer(log *slog.Logger, srcPath, valPath, usersPath string) (*Server, error) {
	ctx := context.Background()

	// Hash the source DB file before opening so the logs record exactly which
	// bytes this run validated against.
	srcSHA, err := sha256File(srcPath)
	if err != nil {
		return nil, fmt.Errorf("hash source DB: %w", err)
	}
	log.Info("source DB hashed", "path", srcPath, "sha256", srcSHA)

	// 1. Source DB (read-only, immutable). Coverage check, then preload page index.
	srcDB, err := openSrcReadOnly(srcPath)
	if err != nil {
		return nil, fmt.Errorf("open source DB: %w", err)
	}
	log.Info("source DB opened", "path", srcPath)

	if err := verifyCoverage(ctx, log, srcDB); err != nil {
		_ = srcDB.Close()
		return nil, err
	}

	sampler, err := validation.LoadSampler(ctx, srcDB)
	if err != nil {
		_ = srcDB.Close()
		return nil, fmt.Errorf("load sampler: %w", err)
	}
	log.Info("sampler ready", "pages", sampler.NumPages())

	// 2. Validation DB (read/write WAL).
	valDB, err := validation.OpenValidationDB(valPath)
	if err != nil {
		_ = srcDB.Close()
		return nil, fmt.Errorf("open validation DB: %w", err)
	}
	if err := validation.InitSchema(ctx, valDB); err != nil {
		_ = srcDB.Close()
		_ = valDB.Close()
		return nil, fmt.Errorf("init validation schema: %w", err)
	}

	// 3. Users DB (read/write WAL).
	usersDB, err := users.Open(usersPath)
	if err != nil {
		_ = srcDB.Close()
		_ = valDB.Close()
		return nil, fmt.Errorf("open users DB: %w", err)
	}
	if err := users.InitSchema(ctx, usersDB); err != nil {
		_ = srcDB.Close()
		_ = valDB.Close()
		_ = usersDB.Close()
		return nil, fmt.Errorf("init users schema: %w", err)
	}

	// 4. Templates.
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &Server{
		log:       log,
		srcDB:     srcDB,
		srcSHA256: srcSHA,
		valDB:     valDB,
		usersDB:   usersDB,
		sampler:   sampler,
		tmpl:      tmpl,
	}, nil
}

// sha256File streams the file at path through SHA-256 and returns the
// lowercase hex digest. Computed before opening the source DB so the log
// records exactly the on-disk bytes the operator pointed us at.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Server) close() {
	_ = s.srcDB.Close()
	_ = s.valDB.Close()
	_ = s.usersDB.Close()
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Static
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Authed
	mux.HandleFunc("POST /review/start-new", s.requireAuth(s.handleStartNew))
	mux.HandleFunc("POST /review/start-second-pass", s.requireAuth(s.handleStartSecondPass))
	mux.HandleFunc("POST /review/submit", s.requireAuth(s.handleSubmit))
	mux.HandleFunc("GET /image/{id}/{kind}", s.requireAuth(s.handleImage))
	mux.HandleFunc("GET /details", s.requireAuth(s.handleDetails))
	mux.HandleFunc("GET /export", s.requireAuth(s.handleExport))
	mux.HandleFunc("GET /dashboard/stats", s.requireAuth(s.handleDashboardStats))

	return mux
}

// renderPage executes the named content template into the layout shell.
// Pages are full HTML documents.
func (s *Server) renderPage(w http.ResponseWriter, contentName string, data any) {
	inner, err := s.renderTemplate(contentName, data)
	if err != nil {
		s.log.Error("render content", "name", contentName, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "layout", struct {
		Content template.HTML
	}{template.HTML(inner)}); err != nil {
		s.log.Error("render layout", "err", err)
	}
}

// --- Source DB (read-only, immutable) ---

func openSrcReadOnly(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("stat db: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", url.PathEscape(abs))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(8)
	return db, nil
}

// verifyCoverage runs the same coverage sanity check the CLI does.
func verifyCoverage(ctx context.Context, log *slog.Logger, db *sql.DB) error {
	t0 := time.Now()
	var pagesNoOCR, ocrNoPage, zeroOCR, partialOCR int

	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pages p
		WHERE NOT EXISTS (SELECT 1 FROM ocr o WHERE o.id = p.id AND o.page_nr = p.page_nr)`).Scan(&pagesNoOCR); err != nil {
		return fmt.Errorf("count pages without ocr: %w", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ocr o
		WHERE NOT EXISTS (SELECT 1 FROM pages p WHERE p.id = o.id AND p.page_nr = o.page_nr)`).Scan(&ocrNoPage); err != nil {
		return fmt.Errorf("count orphan ocr rows: %w", err)
	}
	if err := db.QueryRowContext(ctx, `
		WITH per_doc AS (
			SELECT p.id, COUNT(*) AS n_pages, COUNT(o.page_text) AS n_ocr
			FROM pages p LEFT JOIN ocr o ON o.id = p.id AND o.page_nr = p.page_nr
			GROUP BY p.id
		)
		SELECT
			COALESCE(SUM(CASE WHEN n_ocr = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN n_ocr > 0 AND n_ocr < n_pages THEN 1 ELSE 0 END), 0)
		FROM per_doc`).Scan(&zeroOCR, &partialOCR); err != nil {
		return fmt.Errorf("per-doc coverage: %w", err)
	}

	log.Info("coverage check",
		"pages_without_ocr", pagesNoOCR, "ocr_without_page", ocrNoPage,
		"docs_zero_ocr", zeroOCR, "docs_partial_ocr", partialOCR,
		"elapsed", time.Since(t0))

	if pagesNoOCR != 0 || ocrNoPage != 0 || zeroOCR != 0 || partialOCR != 0 {
		return fmt.Errorf("OCR pipeline coverage incomplete: pages_without_ocr=%d ocr_without_page=%d docs_zero_ocr=%d docs_partial_ocr=%d",
			pagesNoOCR, ocrNoPage, zeroOCR, partialOCR)
	}
	return nil
}
