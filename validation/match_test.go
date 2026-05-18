package validation

import "testing"

func TestMatchLine(t *testing.T) {
	page := "Hello world\nThis is a test of the OCR system\nGoodbye for now"

	tests := []struct {
		name      string
		human     string
		wantLine  string
		minScore  float64
	}{
		{"exact", "This is a test of the OCR system", "This is a test of the OCR system", 0.99},
		{"case insensitive", "this is a test of the ocr system", "This is a test of the OCR system", 0.99},
		{"one typo", "This is a test of the OCR systm", "This is a test of the OCR system", 0.9},
		{"prefix match", "Hello world", "Hello world", 0.99},
		{"trailing spaces", "  Goodbye for now  ", "Goodbye for now", 0.99},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line, score, method := MatchLine(tc.human, page)
			if line != tc.wantLine {
				t.Errorf("line = %q, want %q (score=%.3f)", line, tc.wantLine, score)
			}
			if score < tc.minScore {
				t.Errorf("score = %.3f, want >= %.3f", score, tc.minScore)
			}
			if method == "" {
				t.Errorf("method should not be empty")
			}
		})
	}
}

func TestMatchLineEmptyHuman(t *testing.T) {
	line, score, _ := MatchLine("", "anything\nhere")
	if line != "" || score != 0 {
		t.Errorf("empty human should yield empty match, got line=%q score=%.3f", line, score)
	}
}

func TestMatchLineEmptyPage(t *testing.T) {
	line, score, _ := MatchLine("hello", "")
	if line != "" || score != 0 {
		t.Errorf("empty page should yield empty match, got line=%q score=%.3f", line, score)
	}
}

func TestMatchLinePicksBestNotFirst(t *testing.T) {
	page := "totally unrelated line\nclose match here\nperfect-ish answer\n"
	line, score, _ := MatchLine("perfect-ish answer", page)
	if line != "perfect-ish answer" {
		t.Fatalf("got %q (score %.3f), want perfect-ish line", line, score)
	}
	if score < 0.99 {
		t.Fatalf("score %.3f too low for exact match", score)
	}
}
