package validation

import (
	"context"
	"database/sql"
	"fmt"
)

type ReviewerCount struct {
	Reviewer string
	Count    int
}

type NReviewsBucket struct {
	NReviews int // 0, 1, 2, ...
	Samples  int
}

type Progress struct {
	TotalSamples       int
	TotalReviews       int
	SamplesByCreator   []ReviewerCount
	ReviewsByReviewer  []ReviewerCount
	ReviewsByVerdict   []ReviewerCount // Reviewer field holds the verdict
	SamplesByNReviews  []NReviewsBucket
	SecondPassEligible []ReviewerCount // per reviewer: how many samples they could second-pass
}

// ComputeProgress returns aggregate counts about the validation DB. All
// queries hit only the validation DB — the source pipeline is not needed.
//
// Runs inside a read-only transaction so every count is taken against the
// same WAL snapshot — a reviewer submitting mid-call cannot make the
// per-verdict and per-reviewer breakdowns disagree on the total.
func ComputeProgress(ctx context.Context, valDB *sql.DB) (*Progress, error) {
	tx, err := valDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin read tx: %w", err)
	}
	defer tx.Rollback()

	p := &Progress{}

	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM samples`).Scan(&p.TotalSamples); err != nil {
		return nil, fmt.Errorf("count samples: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reviews`).Scan(&p.TotalReviews); err != nil {
		return nil, fmt.Errorf("count reviews: %w", err)
	}

	if err := scanReviewerCounts(ctx, tx,
		`SELECT created_by, COUNT(*) AS n FROM samples GROUP BY created_by ORDER BY n DESC, created_by`,
		&p.SamplesByCreator); err != nil {
		return nil, fmt.Errorf("samples by creator: %w", err)
	}
	if err := scanReviewerCounts(ctx, tx,
		`SELECT reviewer, COUNT(*) AS n FROM reviews GROUP BY reviewer ORDER BY n DESC, reviewer`,
		&p.ReviewsByReviewer); err != nil {
		return nil, fmt.Errorf("reviews by reviewer: %w", err)
	}
	if err := scanReviewerCounts(ctx, tx,
		`SELECT verdict, COUNT(*) AS n FROM reviews GROUP BY verdict ORDER BY n DESC, verdict`,
		&p.ReviewsByVerdict); err != nil {
		return nil, fmt.Errorf("reviews by verdict: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT n_reviews, COUNT(*) FROM (
			SELECT s.sample_id, COUNT(r.review_id) AS n_reviews
			FROM samples s LEFT JOIN reviews r USING (sample_id)
			GROUP BY s.sample_id
		)
		GROUP BY n_reviews ORDER BY n_reviews
	`)
	if err != nil {
		return nil, fmt.Errorf("samples by n_reviews: %w", err)
	}
	for rows.Next() {
		var b NReviewsBucket
		if err := rows.Scan(&b.NReviews, &b.Samples); err != nil {
			rows.Close()
			return nil, err
		}
		p.SamplesByNReviews = append(p.SamplesByNReviews, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// For each known reviewer, count samples that currently have exactly 1
	// review of verdict='text' by someone other than them — i.e. samples
	// where this person could BE the second reviewer to reach the
	// gold-standard target of 2 transcriptions to cross-check. Samples
	// whose first review was 'not_text' or 'inconclusive' don't need a
	// second pass and are excluded.
	// "Known reviewer" = anyone who has created a sample or done a review.
	rows, err = tx.QueryContext(ctx, `
		WITH people AS (
			SELECT created_by AS who FROM samples
			UNION
			SELECT reviewer    AS who FROM reviews
		)
		SELECT p.who, (
			SELECT COUNT(*) FROM samples s
			WHERE (SELECT COUNT(*) FROM reviews r WHERE r.sample_id = s.sample_id) = 1
			  AND EXISTS (SELECT 1 FROM reviews r WHERE r.sample_id = s.sample_id AND r.verdict = 'text')
			  AND NOT EXISTS (SELECT 1 FROM reviews r WHERE r.sample_id = s.sample_id AND r.reviewer = p.who)
		) AS eligible
		FROM people p
		ORDER BY eligible DESC, p.who
	`)
	if err != nil {
		return nil, fmt.Errorf("second-pass eligibility: %w", err)
	}
	for rows.Next() {
		var rc ReviewerCount
		if err := rows.Scan(&rc.Reviewer, &rc.Count); err != nil {
			rows.Close()
			return nil, err
		}
		p.SecondPassEligible = append(p.SecondPassEligible, rc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return p, nil
}

func scanReviewerCounts(ctx context.Context, tx *sql.Tx, query string, dst *[]ReviewerCount) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rc ReviewerCount
		if err := rows.Scan(&rc.Reviewer, &rc.Count); err != nil {
			return err
		}
		*dst = append(*dst, rc)
	}
	return rows.Err()
}
