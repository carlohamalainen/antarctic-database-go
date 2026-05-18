package validation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
)

// pageRef is the in-memory tuple we sample against. ~100 bytes per row × 16k
// pages ≈ 2 MB of resident state — cheap.
type pageRef struct {
	docID  string
	docURL string
	pageNr int
}

// Sampler holds an in-memory snapshot of the source pipeline's OCR'd page
// list, loaded once at server startup. Pick() is then a constant-time
// in-memory operation — no source-DB queries on the request path.
//
// Persistence (rendered images, sha256, INSERT into samples) is handled
// by the caller via RecordSample, keeping this type purely about
// sampling.
type Sampler struct {
	pages []pageRef

	mu  sync.Mutex
	rng *rand.Rand
}

// LoadSampler preloads the page index. Call this ONCE at startup; reuse
// the returned Sampler across all requests.
func LoadSampler(ctx context.Context, srcDB *sql.DB) (*Sampler, error) {
	rows, err := srcDB.QueryContext(ctx, `
		SELECT id, url, page_nr FROM pages WHERE status = 'ocr-done'
	`)
	if err != nil {
		return nil, fmt.Errorf("preload pages: %w", err)
	}
	defer rows.Close()

	var pages []pageRef
	for rows.Next() {
		var p pageRef
		if err := rows.Scan(&p.docID, &p.docURL, &p.pageNr); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, errors.New("source DB has no OCR'd pages")
	}

	return &Sampler{
		pages: pages,
		rng:   rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}, nil
}

func (s *Sampler) NumPages() int { return len(s.pages) }

// Pick returns a uniformly-random preloaded page with an (x, y) fraction
// in [0, 1) × [0, 1). Pure in-memory; no DB I/O.
func (s *Sampler) Pick() *Candidate {
	s.mu.Lock()
	idx := s.rng.IntN(len(s.pages))
	xFrac := s.rng.Float64()
	yFrac := s.rng.Float64()
	s.mu.Unlock()
	p := s.pages[idx]
	return &Candidate{
		DocumentID:  p.docID,
		DocumentURL: p.docURL,
		PageNr:      p.pageNr,
		XFraction:   xFrac,
		YFraction:   yFrac,
	}
}
