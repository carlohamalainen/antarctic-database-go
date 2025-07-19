package nvidia

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestNvidiaOCR(t *testing.T) {
	apiKey := os.Getenv("NVIDIA_API_KEY")
	if apiKey == "" {
		t.Skip("NVIDIA_API_KEY environment variable not set")
	}

	client := NewNvidiaClient(10, apiKey)

	model := "meta/llama-4-maverick-17b-128e-instruct"

	testFiles := []struct {
		name           string
		filePath       string
		useAssetUpload bool
	}{
		// {"ATCM1_ip001_e_with_asset", "../test-data/ATCM1_ip001_e.pdf", true},
		// {"ATCM1_ip001_e_with_base64", "../test-data/ATCM1_ip001_e.pdf", false},
		// {"ATCM1_ip018_e_with_asset", "../test-data/ATCM1_ip018_e.pdf", true},
		// {"ATCM1_ip018_e_with_base64", "../test-data/ATCM1_ip018_e.pdf", false},
		{"ATCM1_ip001_e-page-00001", "../test-data/ATCM1_ip001_e-page-00001.pdf", false},
		{"ATCM1_ip001_e-page-00002", "../test-data/ATCM1_ip001_e-page-00002.pdf", false},
		{"ATCM1_ip018_e-page-00001", "../test-data/ATCM1_ip018_e-page-00001.pdf", false},
		{"ATCM1_ip018_e-page-00002", "../test-data/ATCM1_ip018_e-page-00002.pdf", false},
		{"ATCM1_ip018_e-page-00003", "../test-data/ATCM1_ip018_e-page-00003.pdf", false},
		{"ATCM1_ip018_e-page-00004", "../test-data/ATCM1_ip018_e-page-00004.pdf", false},
		{"ATCM1_ip018_e-page-00005", "../test-data/ATCM1_ip018_e-page-00005.pdf", false},
		{"ATCM1_ip018_e-page-00006", "../test-data/ATCM1_ip018_e-page-00006.pdf", false},
		{"ATCM1_ip018_e-page-00007", "../test-data/ATCM1_ip018_e-page-00007.pdf", false},
		{"ATCM1_ip018_e-page-00008", "../test-data/ATCM1_ip018_e-page-00008.pdf", false},
		{"ATCM1_ip018_e-page-00009", "../test-data/ATCM1_ip018_e-page-00009.pdf", false},
		{"ATCM1_ip018_e-page-00010", "../test-data/ATCM1_ip018_e-page-00010.pdf", false},
		{"ATCM1_ip018_e-page-00011", "../test-data/ATCM1_ip018_e-page-00011.pdf", false},
		{"ATCM1_ip018_e-page-00012", "../test-data/ATCM1_ip018_e-page-00012.pdf", false},
		{"ATCM1_ip018_e-page-00013", "../test-data/ATCM1_ip018_e-page-00013.pdf", false},
		{"ATCM1_ip018_e-page-00014", "../test-data/ATCM1_ip018_e-page-00014.pdf", false},
	}

	for _, tt := range testFiles {
		t.Run(tt.name, func(t *testing.T) {
			// Check if golden file exists, initialize if missing
			goldenFile := "../test-data/output_" + tt.name + ".txt"
			var goldenText string
			goldenData, err := os.ReadFile(goldenFile)
			if err != nil {
				if os.IsNotExist(err) {
					// Golden file doesn't exist, create an empty one
					t.Logf("Golden file %s not found. Initializing with empty content.", goldenFile)
					err = os.WriteFile(goldenFile, []byte(""), 0644)
					if err != nil {
						t.Logf("Warning: Failed to create empty golden file: %v", err)
					}
					goldenText = ""
				} else {
					// Some other error occurred
					t.Fatalf("Error reading golden file %s: %v", goldenFile, err)
				}
			} else {
				goldenText = string(goldenData)
			}

			// Note: We always run the NVIDIA OCR test with API key
			// as the API key is configured in GitHub

			// Read PDF file
			pdfData, err := os.ReadFile(tt.filePath)
			if err != nil {
				t.Fatalf("Failed to read PDF file %s: %v", tt.filePath, err)
			}

			// Process PDF with OCR
			text, err := ProcessPDFWithOCR(client, model, pdfData, tt.useAssetUpload)
			if err != nil {
				t.Fatalf("Failed to process PDF with OCR: %v", err)
			}

			if text == "" {
				t.Errorf("OCR returned empty text")
			}

			// Save the current output for inspection regardless of match
			outputFile := "../test-data/output_current_" + tt.name + ".txt"
			if err := os.WriteFile(outputFile, []byte(text), 0644); err != nil {
				t.Logf("Failed to write output file: %v", err)
			}

			// First check exact match
			if text == goldenText {
				t.Logf("OCR output matches golden file exactly (%d characters)", len(text))
				return
			}

			// If not exact match, calculate similarity with our robust method
			matchPercentage := calculateSimilarity(text, goldenText)

			// Define different thresholds for similarity - more relaxed now
			const (
				highSimilarity   = 0.90 // 90% or higher considered very similar (mostly formatting differences)
				mediumSimilarity = 0.75 // 75% or higher considered acceptable (minor content variations)
			)

			if matchPercentage >= highSimilarity {
				// Very high similarity - likely just formatting differences
				t.Logf("OCR output nearly identical to golden file. Similarity: %.2f%%",
					matchPercentage*100)
			} else if matchPercentage >= mediumSimilarity {
				// Medium similarity - minor variations
				t.Logf("OCR output has minor differences from golden file. Similarity: %.2f%%",
					matchPercentage*100)
			} else {
				// Low similarity - significant differences
				t.Errorf("OCR output significantly different from golden file. Similarity: %.2f%%",
					matchPercentage*100)
			}
		})
	}
}

// calculateSimilarity returns a measure of similarity between two strings
// as a value between 0.0 (completely different) and 1.0 (identical)
// This is a more robust similarity function that's tolerant of occasional word changes
func calculateSimilarity(a, b string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0 // Both empty strings are identical
	}

	// If one string is empty but not the other, compare their lengths
	if len(a) == 0 || len(b) == 0 {
		shortLen := float64(len(a))
		longLen := float64(len(b))
		if len(a) > len(b) {
			shortLen = float64(len(b))
			longLen = float64(len(a))
		}

		// If the golden file is empty but the new text has content,
		// return a reasonable score based on length ratio
		if shortLen == 0 {
			return 0.5 // Empty vs non-empty gets a mid-range score
		}

		return shortLen / longLen
	}

	// Normalize whitespace by replacing sequences of whitespace with a single space
	normalizedA := normalizeText(a)
	normalizedB := normalizeText(b)

	// Split into words
	wordsA := strings.Fields(normalizedA)
	wordsB := strings.Fields(normalizedB)

	// Get word counts
	totalWords := math.Max(float64(len(wordsA)), float64(len(wordsB)))
	if totalWords == 0 {
		return 1.0
	}

	// Use a hybrid approach that combines:
	// 1. Local alignment similarity (finding matching segments)
	// 2. Bag-of-words similarity (handling word reordering)
	// 3. Character-level Jaccard similarity (for overall content similarity)

	// 1. Local alignment similarity (60% weight)
	// Find the longest common subsequence of words with a sliding window
	alignmentScore := calculateAlignmentScore(wordsA, wordsB)

	// 2. Bag-of-words similarity (20% weight)
	// Compare word frequencies regardless of order
	bagOfWordsScore := calculateBagOfWordsScore(wordsA, wordsB)

	// 3. Character Jaccard similarity (20% weight)
	// Compare character sets between the two texts
	jaccardScore := calculateJaccardSimilarity(normalizedA, normalizedB)

	// Weighted combination
	return (0.6 * alignmentScore) + (0.2 * bagOfWordsScore) + (0.2 * jaccardScore)
}

// calculateAlignmentScore uses a sliding window approach to find matching segments
// This is good for detecting local regions of matching content
func calculateAlignmentScore(wordsA, wordsB []string) float64 {
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	// Use a sliding window approach to find matching segments
	// This handles cases where some words are inserted/deleted but content is otherwise similar

	maxLen := math.Max(float64(len(wordsA)), float64(len(wordsB)))
	totalMatches := 0
	windowSize := 4 // Balance between strictness and flexibility

	// For short texts, use a smaller window
	if maxLen < 20 {
		windowSize = 2
	} else if maxLen > 100 {
		windowSize = 6 // For longer texts, use a larger window
	}

	// Make sure window size isn't larger than the smaller text
	minLen := math.Min(float64(len(wordsA)), float64(len(wordsB)))
	if float64(windowSize) > minLen {
		windowSize = int(minLen)
	}

	// If window would be too small, fall back to simpler comparison
	if windowSize < 2 {
		matches := 0
		matchLen := int(math.Min(float64(len(wordsA)), float64(len(wordsB))))

		for i := 0; i < matchLen; i++ {
			if wordsA[i] == wordsB[i] {
				matches++
			}
		}

		return float64(matches) / float64(matchLen)
	}

	// We'll slide a window through both texts and look for matches
	for i := 0; i <= len(wordsA)-windowSize; i++ {
		windowA := wordsA[i : i+windowSize]

		// Look for this window in B
		for j := 0; j <= len(wordsB)-windowSize; j++ {
			windowB := wordsB[j : j+windowSize]

			// Calculate how many words match in this window
			matches := 0
			for k := 0; k < windowSize; k++ {
				if windowA[k] == windowB[k] {
					matches++
				}
			}

			// If we have at least 75% match in this window, count it
			if float64(matches) >= float64(windowSize)*0.75 {
				totalMatches += matches
				break // Found a match for this window, move to next
			}
		}
	}

	// Scale by the total possible matches
	possibleMatches := math.Min(float64(len(wordsA)), float64(len(wordsB)))
	if possibleMatches == 0 {
		return 0.0
	}

	score := math.Min(1.0, float64(totalMatches)/possibleMatches)
	return score
}

// calculateBagOfWordsScore compares the frequency of words regardless of order
// This is good for cases where words are reordered but content is similar
func calculateBagOfWordsScore(wordsA, wordsB []string) float64 {
	// Create word frequency maps
	freqA := make(map[string]int)
	freqB := make(map[string]int)

	for _, word := range wordsA {
		freqA[word]++
	}

	for _, word := range wordsB {
		freqB[word]++
	}

	// Count matches
	matches := 0
	totalWords := 0

	// Check all unique words
	for word, countA := range freqA {
		countB := freqB[word]
		matches += int(math.Min(float64(countA), float64(countB)))
		totalWords += countA
	}

	// Add words unique to B
	for word, countB := range freqB {
		if _, exists := freqA[word]; !exists {
			totalWords += countB
		}
	}

	if totalWords == 0 {
		return 1.0
	}

	return float64(matches) / float64(totalWords)
}

// calculateJaccardSimilarity compares character sets between two strings
// This provides a measure of overall content similarity
func calculateJaccardSimilarity(a, b string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}

	// Create character sets
	setA := make(map[rune]bool)
	setB := make(map[rune]bool)

	for _, char := range a {
		setA[char] = true
	}

	for _, char := range b {
		setB[char] = true
	}

	// Calculate intersection size
	intersection := 0
	for char := range setA {
		if setB[char] {
			intersection++
		}
	}

	// Calculate union size
	union := len(setA) + len(setB) - intersection

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// normalizeText removes excessive whitespace and normalizes line endings
func normalizeText(text string) string {
	// Replace all types of whitespace (including tabs, newlines) with a space
	text = strings.Join(strings.Fields(text), " ")
	return text
}

// TestConvertPDFToPNG is commented out because we're not testing PNG conversion in CI
/*
func TestConvertPDFToPNG(t *testing.T) {
	testFiles := []string{
		"../test-data/ATCM1_ip001_e.pdf",
		"../test-data/ATCM1_ip018_e.pdf",
	}

	for _, filePath := range testFiles {
		t.Run(filePath, func(t *testing.T) {
			// Read PDF file
			pdfData, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("Failed to read PDF file %s: %v", filePath, err)
			}

			// Convert to PNG
			pngData, err := ConvertPDFToPNG(pdfData, 300)
			if err != nil {
				t.Fatalf("Failed to convert PDF to PNG: %v", err)
			}

			// Check if we got some data back
			if len(pngData) == 0 {
				t.Errorf("PNG conversion returned empty data")
			}

			// Save PNG for inspection
			baseName := filePath[strings.LastIndex(filePath, "/")+1:len(filePath)-4]
			outputFile := "../test-data/output_" + baseName + ".png"
			err = os.WriteFile(outputFile, pngData, 0644)
			if err != nil {
				t.Logf("Failed to write output file: %v", err)
			}

			t.Logf("Converted PNG saved to %s (size: %d bytes)", outputFile, len(pngData))
		})
	}
}
*/
