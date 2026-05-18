package validation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newSrcDB returns an in-memory SQLite DB with the document-pipeline schema
// and a small fixed dataset.
func newSrcDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE documents (id TEXT PRIMARY KEY, url TEXT, document BLOB, timestamp TEXT, is_scanned INTEGER, status TEXT);
		CREATE TABLE pages (id TEXT, url TEXT, page_nr INTEGER, page_pdf BLOB, status TEXT, PRIMARY KEY(id, page_nr));
		CREATE TABLE ocr   (id TEXT, page_nr INTEGER, page_text TEXT, method TEXT, timestamp TEXT, PRIMARY KEY(id, page_nr));

		INSERT INTO documents VALUES
			('doc1','http://example.com/1', NULL, '2025-05-13 04:04:37.461+00:00', 1, 'pages_extracted'),
			('doc2','http://example.com/2', NULL, '2025-05-13 04:04:43.572+00:00', 1, 'pages_extracted');

		INSERT INTO pages VALUES
			('doc1','http://example.com/1',1,NULL,'ocr-done'),
			('doc1','http://example.com/1',2,NULL,'ocr-done'),
			('doc2','http://example.com/2',1,NULL,'ocr-done');

		INSERT INTO ocr VALUES
			('doc1',1,'Hello world' || char(10) || 'This is page one' || char(10) || 'Goodbye','nvidia/test','2025-05-13 04:04:37.461+00:00'),
			('doc1',2,'Page two header' || char(10) || 'More content here','nvidia/test','2025-05-13 04:04:43.572+00:00'),
			('doc2',1,'Document two only page' || char(10) || 'Final line','nvidia/test','2025-05-13 04:04:47.654+00:00');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// newValDB returns an in-memory validation DB with schema applied.
func newValDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := InitSchema(context.Background(), db); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	return db
}

// newRandomSample picks a uniformly-random OCR'd page from the source
// pipeline DB, generates a y-fraction in [0, 1), and inserts a new sample
// row owned by reviewer.
//
// Test-only: the production path uses Sampler.NewRandomSample, which
// preloads the page index in memory and avoids the per-call
// `ORDER BY RANDOM()` scan against the source DB.
func newRandomSample(
	ctx context.Context,
	valDB, srcDB *sql.DB,
	reviewer string,
	rng *rand.Rand,
) (*Sample, error) {
	if reviewer == "" {
		return nil, errors.New("reviewer must be non-empty")
	}

	s := &Sample{
		CreatedBy:    reviewer,
		CreatedAt:    time.Now().UTC(),
		XFraction:    rng.Float64(),
		YFraction:    rng.Float64(),
		SourceSHA256: "test-sha",
	}

	err := srcDB.QueryRowContext(ctx, `
		SELECT id, url, page_nr
		FROM pages
		WHERE status = 'ocr-done'
		ORDER BY RANDOM()
		LIMIT 1
	`).Scan(&s.DocumentID, &s.DocumentURL, &s.PageNr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoCandidate
	}
	if err != nil {
		return nil, fmt.Errorf("pick random page: %w", err)
	}

	res, err := valDB.ExecContext(ctx, `
		INSERT INTO samples
			(document_id, document_url, page_nr, x_fraction, y_fraction, created_by, created_at, source_sha256, full_image_png, snippet_image_png)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.DocumentID, s.DocumentURL, s.PageNr, s.XFraction, s.YFraction, s.CreatedBy, formatTime(s.CreatedAt), s.SourceSHA256, []byte("test-full-png"), []byte("test-snippet-png"))
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

func TestInitSchemaIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := range 3 {
		if err := InitSchema(ctx, db); err != nil {
			t.Fatalf("init %d: %v", i, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("samples should start empty, got %d", n)
	}
}

func TestNewRandomSampleInserts(t *testing.T) {
	ctx := context.Background()
	src := newSrcDB(t)
	val := newValDB(t)
	rng := rand.New(rand.NewPCG(1, 2))

	s, err := newRandomSample(ctx, val, src, "alice", rng)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == 0 || s.DocumentID == "" || s.DocumentURL == "" || s.PageNr == 0 {
		t.Errorf("sample missing fields: %+v", s)
	}
	if s.YFraction < 0 || s.YFraction >= 1 {
		t.Errorf("y_fraction out of range: %f", s.YFraction)
	}
	if s.CreatedBy != "alice" {
		t.Errorf("created_by mismatch: %q", s.CreatedBy)
	}

	got, err := GetSample(ctx, val, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DocumentID != s.DocumentID || got.PageNr != s.PageNr {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, s)
	}
}

func TestPickForSecondPass(t *testing.T) {
	ctx := context.Background()
	src := newSrcDB(t)
	val := newValDB(t)
	rng := rand.New(rand.NewPCG(42, 99))

	// alice creates a sample
	s, err := newRandomSample(ctx, val, src, "alice", rng)
	if err != nil {
		t.Fatal(err)
	}

	// no second-pass yet (no one has reviewed it)
	if _, err := PickForSecondPass(ctx, val, "bob"); !errors.Is(err, ErrNoCandidate) {
		t.Errorf("expected ErrNoCandidate before any review, got %v", err)
	}

	// alice records her own review (verdict=text, so eligible for second-pass)
	if _, err := RecordReview(ctx, val, s.ID, "alice", VerdictText, "hello world", ""); err != nil {
		t.Fatal(err)
	}

	// bob should now find this sample available for second-pass
	got, err := PickForSecondPass(ctx, val, "bob")
	if err != nil {
		t.Fatalf("expected sample for bob, got err: %v", err)
	}
	if got.ID != s.ID {
		t.Errorf("bob got sample %d, want %d", got.ID, s.ID)
	}

	// alice should NOT see it (she's already reviewed it)
	if _, err := PickForSecondPass(ctx, val, "alice"); !errors.Is(err, ErrNoCandidate) {
		t.Errorf("alice should not see her own already-reviewed sample, got %v", err)
	}

	// bob reviews — sample now has 2 reviews (gold standard reached).
	// Carol must NOT see it for second-pass: she'd be a 3rd reviewer, not
	// part of the workflow.
	if _, err := RecordReview(ctx, val, s.ID, "bob", VerdictText, "hello world", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := PickForSecondPass(ctx, val, "carol"); !errors.Is(err, ErrNoCandidate) {
		t.Errorf("carol must NOT be eligible after sample reaches 2 reviews, got %v", err)
	}

	// New sample, alice reviews as not_text — sample should NOT be
	// eligible for second-pass (only text verdicts need cross-check).
	rng2 := rand.New(rand.NewPCG(11, 22))
	sNot, err := newRandomSample(ctx, val, src, "alice", rng2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordReview(ctx, val, sNot.ID, "alice", VerdictNotText, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := PickForSecondPass(ctx, val, "bob"); !errors.Is(err, ErrNoCandidate) {
		t.Errorf("not_text first-review should NOT be second-pass eligible, got %v", err)
	}
}

func TestRecordReviewUniqueness(t *testing.T) {
	ctx := context.Background()
	src := newSrcDB(t)
	val := newValDB(t)
	rng := rand.New(rand.NewPCG(7, 8))

	s, err := newRandomSample(ctx, val, src, "alice", rng)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordReview(ctx, val,s.ID, "alice", VerdictNotText, "", ""); err != nil {
		t.Fatal(err)
	}
	_, err = RecordReview(ctx, val,s.ID, "alice", VerdictNotText, "", "")
	if err == nil {
		t.Fatal("expected UNIQUE violation, got nil")
	}
	if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "constraint") {
		t.Errorf("expected UNIQUE/constraint error, got: %v", err)
	}
}

func TestRecordReviewVerdictTextRequiresUserText(t *testing.T) {
	ctx := context.Background()
	src := newSrcDB(t)
	val := newValDB(t)
	rng := rand.New(rand.NewPCG(3, 4))

	s, err := newRandomSample(ctx, val, src, "alice", rng)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordReview(ctx, val,s.ID, "alice", VerdictText, "", ""); err == nil {
		t.Errorf("verdict=text with empty text should error")
	}
	if _, err := RecordReview(ctx, val,s.ID, "alice", VerdictNotText, "something", ""); err == nil {
		t.Errorf("verdict=blank with non-empty text should error")
	}
}

func TestProgressCounts(t *testing.T) {
	ctx := context.Background()
	src := newSrcDB(t)
	val := newValDB(t)
	rng := rand.New(rand.NewPCG(11, 12))

	sA, err := newRandomSample(ctx, val, src, "alice", rng)
	if err != nil {
		t.Fatal(err)
	}
	sB, err := newRandomSample(ctx, val, src, "alice", rng)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordReview(ctx, val,sA.ID, "alice", VerdictNotText, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordReview(ctx, val,sA.ID, "bob", VerdictInconclusive, "", ""); err != nil {
		t.Fatal(err)
	}
	// sB has zero reviews

	p, err := ComputeProgress(ctx, val)
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalSamples != 2 || p.TotalReviews != 2 {
		t.Errorf("totals: samples=%d reviews=%d, want 2/2", p.TotalSamples, p.TotalReviews)
	}

	wantBuckets := map[int]int{0: 1, 2: 1}
	for _, b := range p.SamplesByNReviews {
		if want, ok := wantBuckets[b.NReviews]; !ok || want != b.Samples {
			t.Errorf("unexpected bucket %+v", b)
		}
		delete(wantBuckets, b.NReviews)
	}
	if len(wantBuckets) != 0 {
		t.Errorf("missing buckets: %v", wantBuckets)
	}

	// Bob has 1 sample he could second-pass (sA, since alice reviewed it but he did too — wait, bob reviewed sA)
	// So bob is *not* eligible for sA. He has 0 second-pass eligibility.
	// Alice is also not eligible for sA (she reviewed it). Neither for sB (no other reviewer).
	// "carol" doesn't appear because she isn't a known reviewer.
	got := map[string]int{}
	for _, rc := range p.SecondPassEligible {
		got[rc.Reviewer] = rc.Count
	}
	if got["alice"] != 0 || got["bob"] != 0 {
		t.Errorf("unexpected second-pass eligibility: %+v", got)
	}

	// Now add carol reviewing sB with verdict=text → bob and alice both
	// become eligible for sB (not_text/inconclusive first reviews would
	// NOT trigger second-pass eligibility under the new policy).
	if _, err := RecordReview(ctx, val, sB.ID, "carol", VerdictText, "hello", ""); err != nil {
		t.Fatal(err)
	}
	p2, err := ComputeProgress(ctx, val)
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]int{}
	for _, rc := range p2.SecondPassEligible {
		got[rc.Reviewer] = rc.Count
	}
	if got["alice"] != 1 || got["bob"] != 1 {
		t.Errorf("after carol's review, alice and bob should each have 1 eligible (sB), got %+v", got)
	}
	// carol must NOT be eligible for sA — it already has 2 reviews
	// (alice+bob), so a 3rd reviewer is not part of the gold-standard flow.
	if got["carol"] != 0 {
		t.Errorf("carol should have 0 eligible (sA already at gold standard), got %d", got["carol"])
	}
}

func TestReportView(t *testing.T) {
	ctx := context.Background()
	src := newSrcDB(t)
	val := newValDB(t)
	rng := rand.New(rand.NewPCG(5, 6))

	s, err := newRandomSample(ctx, val, src, "alice", rng)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordReview(ctx, val,s.ID, "alice", VerdictNotText, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordReview(ctx, val,s.ID, "bob", VerdictNotText, "", ""); err != nil {
		t.Fatal(err)
	}

	rows, err := val.QueryContext(ctx, `
		SELECT review_user, original_sample_user FROM report ORDER BY review_user
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		reviewer  string
		original  sql.NullString
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.reviewer, &r.original); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 report rows, got %d", len(got))
	}
	// alice reviewed her own sample → original_sample_user is NULL
	if got[0].reviewer != "alice" || got[0].original.Valid {
		t.Errorf("alice row wrong: %+v", got[0])
	}
	// bob reviewed alice's sample → original_sample_user = 'alice'
	if got[1].reviewer != "bob" || !got[1].original.Valid || got[1].original.String != "alice" {
		t.Errorf("bob row wrong: %+v", got[1])
	}
}
