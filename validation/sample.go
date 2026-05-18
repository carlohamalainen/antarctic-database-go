package validation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoCandidate is returned when a sampling function finds nothing to return.
var ErrNoCandidate = errors.New("no candidate sample available")

type Sample struct {
	ID           int64
	DocumentID   string
	DocumentURL  string
	PageNr       int
	XFraction    float64
	YFraction    float64
	CreatedBy    string
	CreatedAt    time.Time
	SourceSHA256 string
}

// Candidate is the in-memory result of a Sampler.Pick — a chosen page +
// (x, y) fraction in [0, 1) × [0, 1) with no DB I/O. The handler renders
// the images and then hands the Candidate (plus PNG bytes) to RecordSample
// to persist.
type Candidate struct {
	DocumentID  string
	DocumentURL string
	PageNr      int
	XFraction   float64
	YFraction   float64
}

// RecordSample inserts a new sample row with the rendered images stored
// as BLOBs. Returns the persisted Sample (without the BLOB bytes loaded —
// fetch those via GetSampleFullImage / GetSampleSnippetImage when serving
// /image requests).
func RecordSample(
	ctx context.Context,
	valDB *sql.DB,
	c *Candidate,
	reviewer, srcSHA256 string,
	fullPNG, snippetPNG []byte,
) (*Sample, error) {
	if reviewer == "" {
		return nil, errors.New("reviewer must be non-empty")
	}
	if srcSHA256 == "" {
		return nil, errors.New("srcSHA256 must be non-empty")
	}
	if len(fullPNG) == 0 || len(snippetPNG) == 0 {
		return nil, errors.New("fullPNG and snippetPNG must be non-empty")
	}
	s := &Sample{
		DocumentID:   c.DocumentID,
		DocumentURL:  c.DocumentURL,
		PageNr:       c.PageNr,
		XFraction:    c.XFraction,
		YFraction:    c.YFraction,
		CreatedBy:    reviewer,
		CreatedAt:    time.Now().UTC(),
		SourceSHA256: srcSHA256,
	}
	res, err := valDB.ExecContext(ctx, `
		INSERT INTO samples
			(document_id, document_url, page_nr, x_fraction, y_fraction,
			 created_by, created_at, source_sha256,
			 full_image_png, snippet_image_png)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.DocumentID, s.DocumentURL, s.PageNr, s.XFraction, s.YFraction,
		s.CreatedBy, formatTime(s.CreatedAt), s.SourceSHA256,
		fullPNG, snippetPNG)
	if err != nil {
		return nil, fmt.Errorf("insert sample: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	s.ID = id
	return s, nil
}

// GetSampleFullImage returns the stored full-page PNG bytes for the sample.
func GetSampleFullImage(ctx context.Context, valDB *sql.DB, sampleID int64) ([]byte, error) {
	var b []byte
	err := valDB.QueryRowContext(ctx,
		`SELECT full_image_png FROM samples WHERE sample_id = ?`,
		sampleID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("sample %d: %w", sampleID, ErrNoCandidate)
	}
	if err != nil {
		return nil, fmt.Errorf("get sample %d full image: %w", sampleID, err)
	}
	return b, nil
}

// GetSampleSnippetImage returns the stored snippet PNG bytes for the sample.
func GetSampleSnippetImage(ctx context.Context, valDB *sql.DB, sampleID int64) ([]byte, error) {
	var b []byte
	err := valDB.QueryRowContext(ctx,
		`SELECT snippet_image_png FROM samples WHERE sample_id = ?`,
		sampleID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("sample %d: %w", sampleID, ErrNoCandidate)
	}
	if err != nil {
		return nil, fmt.Errorf("get sample %d snippet image: %w", sampleID, err)
	}
	return b, nil
}

// PickForSecondPass returns a uniformly-random sample that needs a second
// reviewer to reach the 2-reviewer gold standard, where `reviewer` would
// BE that second reviewer. Concretely: the sample currently has exactly 1
// review, that review's verdict is 'text', and the review is not by
// `reviewer`. Returns ErrNoCandidate if there is nothing to second-review.
//
// Samples that already have ≥2 reviews are intentionally excluded — a
// third reviewer would be an extra opinion, not part of the gold-standard
// workflow. Samples whose first review was 'not_text' or 'inconclusive'
// are also excluded — only transcriptions need an independent second
// reading; the other verdicts don't carry transcription content to
// cross-check.
func PickForSecondPass(ctx context.Context, valDB *sql.DB, reviewer string) (*Sample, error) {
	if reviewer == "" {
		return nil, errors.New("reviewer must be non-empty")
	}
	var s Sample
	var createdAt string
	err := valDB.QueryRowContext(ctx, `
		SELECT s.sample_id, s.document_id, s.document_url, s.page_nr,
		       s.x_fraction, s.y_fraction,
		       s.created_by, s.created_at, s.source_sha256
		FROM samples s
		WHERE (SELECT COUNT(*) FROM reviews r WHERE r.sample_id = s.sample_id) = 1
		  AND EXISTS (
		      SELECT 1 FROM reviews r WHERE r.sample_id = s.sample_id AND r.verdict = 'text'
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM reviews r WHERE r.sample_id = s.sample_id AND r.reviewer = ?
		  )
		ORDER BY RANDOM()
		LIMIT 1
	`, reviewer).Scan(
		&s.ID, &s.DocumentID, &s.DocumentURL, &s.PageNr,
		&s.XFraction, &s.YFraction,
		&s.CreatedBy, &createdAt, &s.SourceSHA256,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoCandidate
	}
	if err != nil {
		return nil, fmt.Errorf("pick second-pass sample: %w", err)
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	s.CreatedAt = t
	return &s, nil
}

// SecondPassEligibleCount returns how many samples currently have exactly
// 1 review with verdict='text' where that review is not by `reviewer`.
// Unlike the per-reviewer counts in Progress.SecondPassEligible, this
// works for any reviewer regardless of whether they appear in the
// "people" list — useful for brand-new users who haven't created or
// reviewed anything yet.
func SecondPassEligibleCount(ctx context.Context, valDB *sql.DB, reviewer string) (int, error) {
	if reviewer == "" {
		return 0, errors.New("reviewer must be non-empty")
	}
	var n int
	err := valDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM samples s
		WHERE (SELECT COUNT(*) FROM reviews r WHERE r.sample_id = s.sample_id) = 1
		  AND EXISTS (SELECT 1 FROM reviews r WHERE r.sample_id = s.sample_id AND r.verdict = 'text')
		  AND NOT EXISTS (SELECT 1 FROM reviews r WHERE r.sample_id = s.sample_id AND r.reviewer = ?)
	`, reviewer).Scan(&n)
	return n, err
}

// GetSample looks up a sample by its primary key.
func GetSample(ctx context.Context, valDB *sql.DB, sampleID int64) (*Sample, error) {
	var s Sample
	var createdAt string
	err := valDB.QueryRowContext(ctx, `
		SELECT sample_id, document_id, document_url, page_nr,
		       x_fraction, y_fraction,
		       created_by, created_at, source_sha256
		FROM samples
		WHERE sample_id = ?
	`, sampleID).Scan(
		&s.ID, &s.DocumentID, &s.DocumentURL, &s.PageNr,
		&s.XFraction, &s.YFraction,
		&s.CreatedBy, &createdAt, &s.SourceSHA256,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("sample %d: %w", sampleID, ErrNoCandidate)
	}
	if err != nil {
		return nil, fmt.Errorf("get sample %d: %w", sampleID, err)
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	s.CreatedAt = t
	return &s, nil
}
