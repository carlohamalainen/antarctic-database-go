package validation

import (
	"context"
	"database/sql"
)

const Schema = `
CREATE TABLE IF NOT EXISTS samples (
  sample_id          INTEGER PRIMARY KEY,
  document_id        TEXT    NOT NULL,
  document_url       TEXT    NOT NULL,
  page_nr            INTEGER NOT NULL,
  x_fraction         REAL    NOT NULL CHECK (x_fraction >= 0 AND x_fraction < 1),
  y_fraction         REAL    NOT NULL CHECK (y_fraction >= 0 AND y_fraction < 1),
  created_by         TEXT    NOT NULL,
  created_at         TEXT    NOT NULL,
  source_sha256      TEXT    NOT NULL,
  full_image_png     BLOB    NOT NULL,
  snippet_image_png  BLOB    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_samples_doc_page ON samples(document_id, page_nr);
CREATE INDEX IF NOT EXISTS idx_samples_created_by ON samples(created_by);

CREATE TABLE IF NOT EXISTS reviews (
  review_id        INTEGER PRIMARY KEY,
  sample_id        INTEGER NOT NULL REFERENCES samples(sample_id),
  reviewer         TEXT    NOT NULL,
  reviewed_at      TEXT    NOT NULL,
  verdict          TEXT    NOT NULL CHECK (verdict IN ('text','not_text','inconclusive')),
  user_text        TEXT,
  client_version   TEXT,
  CHECK (
    (verdict = 'text'  AND user_text IS NOT NULL) OR
    (verdict <> 'text' AND user_text IS NULL)
  ),
  UNIQUE (sample_id, reviewer)
);
CREATE INDEX IF NOT EXISTS idx_reviews_sample   ON reviews(sample_id);
CREATE INDEX IF NOT EXISTS idx_reviews_reviewer ON reviews(reviewer);

CREATE VIEW IF NOT EXISTS report AS
SELECT
  r.reviewed_at,
  r.reviewer                                                            AS review_user,
  CASE WHEN r.reviewer = s.created_by THEN NULL ELSE s.created_by END   AS original_sample_user,
  s.document_id,
  s.document_url,
  s.page_nr,
  s.x_fraction,
  s.y_fraction,
  r.verdict,
  r.user_text
FROM reviews r
JOIN samples s USING (sample_id)
ORDER BY r.reviewed_at;
`

// InitSchema creates the validation tables/indexes/view if they don't exist.
// Safe to call repeatedly.
func InitSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, Schema)
	return err
}
