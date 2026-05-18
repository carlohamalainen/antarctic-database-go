package validation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Verdict string

const (
	VerdictText         Verdict = "text"
	VerdictNotText      Verdict = "not_text" // whitespace, figure, table — anything not transcribable
	VerdictInconclusive Verdict = "inconclusive"
)

func ParseVerdict(s string) (Verdict, error) {
	switch Verdict(s) {
	case VerdictText, VerdictNotText, VerdictInconclusive:
		return Verdict(s), nil
	default:
		return "", fmt.Errorf("invalid verdict %q (want text|not_text|inconclusive)", s)
	}
}

type Review struct {
	ID            int64
	SampleID      int64
	Reviewer      string
	ReviewedAt    time.Time
	Verdict       Verdict
	UserText      string
	ClientVersion string
}

// RecordReview inserts a review row for the given sample. The review stores
// only the user's verdict and (for verdict=text) their typed transcription —
// any OCR-line matching / scoring is done by a downstream stage, not here.
//
// The UNIQUE(sample_id, reviewer) constraint surfaces as an INSERT error
// from SQLite if a reviewer tries to record a second review for the same
// sample.
func RecordReview(
	ctx context.Context,
	valDB *sql.DB,
	sampleID int64,
	reviewer string,
	verdict Verdict,
	userText, clientVersion string,
) (*Review, error) {
	if reviewer == "" {
		return nil, errors.New("reviewer must be non-empty")
	}
	switch verdict {
	case VerdictText:
		if userText == "" {
			return nil, errors.New("verdict=text requires non-empty user_text")
		}
	case VerdictNotText, VerdictInconclusive:
		if userText != "" {
			return nil, fmt.Errorf("verdict=%s must not have user_text", verdict)
		}
	default:
		return nil, fmt.Errorf("invalid verdict %q", verdict)
	}

	rv := &Review{
		SampleID:      sampleID,
		Reviewer:      reviewer,
		ReviewedAt:    time.Now().UTC(),
		Verdict:       verdict,
		ClientVersion: clientVersion,
	}

	var (
		userTextN sql.NullString
		clientN   sql.NullString
	)
	if verdict == VerdictText {
		rv.UserText = userText
		userTextN = sql.NullString{String: userText, Valid: true}
	}
	if clientVersion != "" {
		clientN = sql.NullString{String: clientVersion, Valid: true}
	}

	res, err := valDB.ExecContext(ctx, `
		INSERT INTO reviews
			(sample_id, reviewer, reviewed_at, verdict, user_text, client_version)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		sampleID, reviewer, formatTime(rv.ReviewedAt), string(verdict),
		userTextN, clientN,
	)
	if err != nil {
		return nil, fmt.Errorf("insert review: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	rv.ID = id
	return rv, nil
}
