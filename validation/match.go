package validation

import (
	"strings"

	"github.com/hbollon/go-edlib"
)

// MatchMethodLevenshtein is the identifier stored in reviews.match_method.
const MatchMethodLevenshtein = "edlib.Levenshtein-v1"

// matchAlgorithm is the swap-point: replace with e.g. edlib.JaroWinkler
// (and update MatchMethodLevenshtein / its constant name to match).
const matchAlgorithm = edlib.Levenshtein

// MatchLine fuzzy-matches the human-typed text against the lines of an OCR
// page. It returns the original (un-normalized) best-matching line, a
// similarity score in [0, 1], and the method identifier.
//
// Returns ("", 0, method) when the page has no candidate lines.
//
// The matcher is *content-only*; it does not consider y-position, so it can
// be confused by repeated headers / page numbers / multi-column layouts.
// Always inspect the score in downstream analysis.
func MatchLine(human, pageText string) (line string, score float64, method string) {
	method = MatchMethodLevenshtein
	hNorm := normalizeForMatch(human)
	if hNorm == "" {
		return "", 0, method
	}

	bestScore := -1.0
	bestLine := ""
	for raw := range strings.SplitSeq(pageText, "\n") {
		c := normalizeForMatch(raw)
		if c == "" {
			continue
		}
		s, err := edlib.StringsSimilarity(hNorm, c, matchAlgorithm)
		if err != nil {
			continue
		}
		sf := float64(s)
		if sf > bestScore {
			bestScore = sf
			bestLine = strings.TrimSpace(raw)
		}
	}
	if bestScore < 0 {
		return "", 0, method
	}
	return bestLine, bestScore, method
}

func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	return strings.Join(strings.Fields(s), " ")
}
