package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/carlohamalainen/antarctic-database-go/pageimg"
	"github.com/carlohamalainen/antarctic-database-go/validation"
)

const clientVersion = "ocr-validation-web/0.1"

// displayLoc is the timezone used for human-facing timestamps. Stored
// timestamps stay UTC; only display crosses into local time.
var displayLoc = func() *time.Location {
	loc, err := time.LoadLocation("Australia/Brisbane")
	if err != nil {
		panic(fmt.Sprintf("load Australia/Brisbane timezone (is tzdata installed?): %v", err))
	}
	return loc
}()

type dashboardData struct {
	Username        string
	Stats           *validation.Progress
	SecondPassCount int
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	username, ok := s.tryAuth(w, r)
	if !ok {
		s.renderPage(w, "login", map[string]any{"Error": ""})
		return
	}
	data, err := s.buildDashboardData(r.Context(), username)
	if err != nil {
		s.log.Error("dashboard data", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, "dashboard", data)
}

func (s *Server) buildDashboardData(ctx context.Context, username string) (*dashboardData, error) {
	stats, err := validation.ComputeProgress(ctx, s.valDB)
	if err != nil {
		return nil, err
	}
	count, err := validation.SecondPassEligibleCount(ctx, s.valDB, username)
	if err != nil {
		return nil, err
	}
	return &dashboardData{Username: username, Stats: stats, SecondPassCount: count}, nil
}

// handleDashboardStats is polled by the dashboard's #stats panel on a 10s
// interval (data-on:interval). Returns an SSE patch that re-renders both
// the #stats section and the #actions block, picking up activity from
// other users (e.g. their submission may have changed your second-pass
// eligibility, which gates the second-pass button).
func (s *Server) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	username, _ := userFromCtx(r.Context())
	data, err := s.buildDashboardData(r.Context(), username)
	if err != nil {
		s.log.Error("dashboard stats refresh", "err", err)
		return
	}
	statsHTML, err := s.renderTemplate("stats", data)
	if err != nil {
		s.log.Error("render stats", "err", err)
		return
	}
	actionsHTML, err := s.renderTemplate("actions", data)
	if err != nil {
		s.log.Error("render actions", "err", err)
		return
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElements(statsHTML + actionsHTML); err != nil {
		s.log.Error("sse patch", "err", err)
	}
}

// --- Sample-start handlers (SSE patches into #review-section) ---

type reviewFormData struct {
	SampleID     int64
	DocumentID   string
	DocumentURL  string
	PageNr       int
	XFraction    float64
	YFraction    float64
	IsSecondPass bool
}

func (s *Server) handleStartNew(w http.ResponseWriter, r *http.Request) {
	username, _ := userFromCtx(r.Context())
	ctx := r.Context()

	sse := datastar.NewSSE(w, r)

	// Immediate feedback: pdftoppm + image manipulation takes a few hundred
	// ms, which feels like a frozen page. Patch a loading placeholder into
	// #review-section before doing the work; SSE flushes it instantly.
	if loadingHTML, err := s.renderTemplate("review-loading", nil); err == nil {
		_ = sse.PatchElements(loadingHTML)
	}

	patchError := func(msg string) {
		html := fmt.Sprintf(
			`<div id="review-section" class="review"><p class="error">Error: %s</p></div>`,
			htmlEscape(msg))
		_ = sse.PatchElements(html)
	}

	c := s.sampler.Pick()

	var pdf []byte
	if err := s.srcDB.QueryRowContext(ctx,
		`SELECT page_pdf FROM pages WHERE id = ? AND page_nr = ?`,
		c.DocumentID, c.PageNr).Scan(&pdf); err != nil {
		s.log.Error("fetch page pdf", "err", err, "doc_id", c.DocumentID, "page_nr", c.PageNr)
		patchError("could not fetch page PDF: " + err.Error())
		return
	}

	full, snippet, err := pageimg.Render(ctx, pdf, c.XFraction, c.YFraction, pageimg.Options{})
	if err != nil {
		s.log.Error("render page", "err", err)
		patchError("could not render page: " + err.Error())
		return
	}

	sample, err := validation.RecordSample(ctx, s.valDB, c, username, s.srcSHA256, full, snippet)
	if err != nil {
		s.log.Error("record sample", "err", err)
		patchError("could not record sample: " + err.Error())
		return
	}
	s.log.Info("new sample created",
		"sample_id", sample.ID, "reviewer", username,
		"doc_id", sample.DocumentID, "page_nr", sample.PageNr,
		"full_png_bytes", len(full), "snippet_png_bytes", len(snippet))

	formHTML, err := s.renderTemplate("review-form", reviewFormData{
		SampleID:    sample.ID,
		DocumentID:  sample.DocumentID,
		DocumentURL: sample.DocumentURL,
		PageNr:      sample.PageNr,
		XFraction:   sample.XFraction,
		YFraction:   sample.YFraction,
	})
	if err != nil {
		s.log.Error("render review-form", "err", err)
		patchError("render failed")
		return
	}
	if err := sse.PatchElements(formHTML); err != nil {
		s.log.Error("sse patch review-form", "err", err)
	}
}

func (s *Server) handleStartSecondPass(w http.ResponseWriter, r *http.Request) {
	username, _ := userFromCtx(r.Context())
	sample, err := validation.PickForSecondPass(r.Context(), s.valDB, username)
	if errors.Is(err, validation.ErrNoCandidate) {
		s.sseFragment(w, r, "review-no-candidate", nil)
		return
	}
	if err != nil {
		s.log.Error("pick second-pass", "err", err)
		s.sseError(w, r, "could not pick a sample: "+err.Error())
		return
	}
	s.sseFragment(w, r, "review-form", reviewFormData{
		SampleID:     sample.ID,
		DocumentID:   sample.DocumentID,
		DocumentURL:  sample.DocumentURL,
		PageNr:       sample.PageNr,
		XFraction:    sample.XFraction,
		YFraction:    sample.YFraction,
		IsSecondPass: true,
	})
}

// --- Submit ---

type confirmationData struct {
	ReviewID   int64
	SampleID   int64
	Verdict    string
	ReviewedAt string
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	username, _ := userFromCtx(r.Context())
	if err := r.ParseForm(); err != nil {
		s.sseError(w, r, "bad form: "+err.Error())
		return
	}
	sampleID, err := strconv.ParseInt(r.FormValue("sample_id"), 10, 64)
	if err != nil || sampleID == 0 {
		s.sseError(w, r, "missing/invalid sample_id")
		return
	}
	rawVerdict := r.FormValue("verdict")
	if rawVerdict == "" {
		s.sseError(w, r, "please pick a verdict before submitting")
		return
	}
	verdict, err := validation.ParseVerdict(rawVerdict)
	if err != nil {
		s.sseError(w, r, err.Error())
		return
	}
	userText := r.FormValue("user_text")
	if verdict != validation.VerdictText {
		userText = "" // server-side guard; UI also enforces
	}

	rv, err := validation.RecordReview(r.Context(), s.valDB,
		sampleID, username, verdict, userText, clientVersion)
	if err != nil {
		s.log.Error("record review", "err", err, "sample_id", sampleID, "reviewer", username)
		s.sseError(w, r, "could not record review: "+err.Error())
		return
	}
	s.log.Info("review recorded",
		"review_id", rv.ID, "sample_id", sampleID,
		"reviewer", username, "verdict", verdict)

	sse := datastar.NewSSE(w, r)

	// Patch 1: replace #review-section with confirmation
	confHTML, err := s.renderTemplate("review-confirmation", confirmationData{
		ReviewID:   rv.ID,
		SampleID:   rv.SampleID,
		Verdict:    string(rv.Verdict),
		ReviewedAt: rv.ReviewedAt.In(displayLoc).Format("2006-01-02 15:04:05 MST"),
	})
	if err != nil {
		s.log.Error("render confirmation", "err", err)
		return
	}
	if err := sse.PatchElements(confHTML); err != nil {
		s.log.Error("sse patch confirmation", "err", err)
		return
	}

	// Patch 2: replace #stats and #actions with refreshed versions. Second-pass
	// eligibility (which gates the second-pass button + its hint count) lives
	// in the actions block, so submitting the last available second-pass
	// sample needs to refresh both.
	dd, err := s.buildDashboardData(r.Context(), username)
	if err != nil {
		s.log.Error("dashboard data after submit", "err", err)
		return
	}
	statsHTML, err := s.renderTemplate("stats", dd)
	if err != nil {
		s.log.Error("render stats", "err", err)
		return
	}
	actionsHTML, err := s.renderTemplate("actions", dd)
	if err != nil {
		s.log.Error("render actions", "err", err)
		return
	}
	if err := sse.PatchElements(statsHTML + actionsHTML); err != nil {
		s.log.Error("sse patch stats+actions", "err", err)
		return
	}
}

// --- Image (served from samples BLOBs) ---

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	kind := r.PathValue("kind")
	var data []byte
	switch kind {
	case "full":
		data, err = validation.GetSampleFullImage(r.Context(), s.valDB, id)
	case "snippet":
		data, err = validation.GetSampleSnippetImage(r.Context(), s.valDB, id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if errors.Is(err, validation.ErrNoCandidate) {
			http.NotFound(w, r)
			return
		}
		s.log.Error("get sample image", "sample_id", id, "kind", kind, "err", err)
		http.Error(w, "load failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// --- Details ---

type detailRow struct {
	ReviewedAt         string
	ReviewUser         string
	OriginalSampleUser string
	DocumentID         string
	DocumentIDShort    string
	DocumentURL        string
	PageNr             int
	XFraction          float64
	YFraction          float64
	Verdict            string
	UserText           string
}

func (s *Server) handleDetails(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queryReportRows(r.Context())
	if err != nil {
		s.log.Error("details query", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, "details", map[string]any{"Rows": rows})
}

func (s *Server) queryReportRows(ctx context.Context) ([]detailRow, error) {
	rows, err := s.valDB.QueryContext(ctx, `
		SELECT reviewed_at, review_user, COALESCE(original_sample_user, ''),
		       document_id, document_url, page_nr, x_fraction, y_fraction,
		       verdict, COALESCE(user_text, '')
		FROM report
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []detailRow
	for rows.Next() {
		var r detailRow
		if err := rows.Scan(
			&r.ReviewedAt, &r.ReviewUser, &r.OriginalSampleUser,
			&r.DocumentID, &r.DocumentURL, &r.PageNr, &r.XFraction, &r.YFraction,
			&r.Verdict, &r.UserText,
		); err != nil {
			return nil, err
		}
		r.DocumentIDShort = r.DocumentID
		if len(r.DocumentIDShort) > 12 {
			r.DocumentIDShort = r.DocumentIDShort[:12] + "…"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Export ---

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/tab-separated-values; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ocr-validation-report.tsv"`)
	fmt.Fprintln(w, "reviewed_at\treview_user\toriginal_sample_user\tdocument_id\tdocument_url\tpage_nr\tx_fraction\ty_fraction\tverdict\tuser_text")

	rows, err := s.valDB.QueryContext(r.Context(), `
		SELECT reviewed_at, review_user, COALESCE(original_sample_user, ''),
		       document_id, document_url, page_nr, x_fraction, y_fraction,
		       verdict, COALESCE(user_text, '')
		FROM report
	`)
	if err != nil {
		s.log.Error("export query", "err", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			ts, ru, osu, doc, url, verdict, ut string
			page                               int
			xfrac, yfrac                       float64
		)
		if err := rows.Scan(&ts, &ru, &osu, &doc, &url, &page, &xfrac, &yfrac, &verdict, &ut); err != nil {
			s.log.Error("export scan", "err", err)
			return
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%.6f\t%.6f\t%s\t%s\n",
			ts, ru, osu, doc, url, page, xfrac, yfrac,
			verdict, sanitizeTab(ut))
	}
}

func sanitizeTab(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			out = append(out, ' ')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}

// --- Healthz ---

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.valDB.PingContext(r.Context()); err != nil {
		http.Error(w, "validation DB down", http.StatusInternalServerError)
		return
	}
	if err := s.usersDB.PingContext(r.Context()); err != nil {
		http.Error(w, "users DB down", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "ok")
}

// --- Helpers ---

func (s *Server) renderTemplate(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (s *Server) sseFragment(w http.ResponseWriter, r *http.Request, name string, data any) {
	html, err := s.renderTemplate(name, data)
	if err != nil {
		s.log.Error("render fragment", "name", name, "err", err)
		s.sseError(w, r, "render failed")
		return
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElements(html); err != nil {
		s.log.Error("sse patch", "err", err)
	}
}

func (s *Server) sseError(w http.ResponseWriter, r *http.Request, msg string) {
	html := fmt.Sprintf(
		`<div id="review-section" class="review"><p class="error">Error: %s</p></div>`,
		htmlEscape(msg))
	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElements(html)
}

func htmlEscape(s string) string {
	r := []rune(s)
	out := make([]rune, 0, len(r))
	for _, c := range r {
		switch c {
		case '<':
			out = append(out, []rune("&lt;")...)
		case '>':
			out = append(out, []rune("&gt;")...)
		case '&':
			out = append(out, []rune("&amp;")...)
		case '"':
			out = append(out, []rune("&quot;")...)
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

