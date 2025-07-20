package cache

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestRealGitHubAPIRequest(t *testing.T) {
	dbFile, err := os.CreateTemp("", "github-api-cache-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	dbPath := dbFile.Name()
	dbFile.Close()
	defer os.Remove(dbPath)

	dbPathStr := dbPath
	client, err := NewHTTPClient(&dbPathStr)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// GitHub API endpoint that's stable and doesn't change often
	url := "https://api.github.com/zen"

	// First request - should hit the GitHub API
	startTime1 := time.Now()
	resp1, err := client.Get(url)
	duration1 := time.Since(startTime1)
	if err != nil {
		t.Fatalf("Failed on first request to GitHub API: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code for first request: %d", resp1.StatusCode)
	}

	body1, err := io.ReadAll(resp1.Body)
	if err != nil {
		t.Fatalf("Failed to read first response: %v", err)
	}

	if len(body1) == 0 {
		t.Fatalf("Empty response from GitHub API")
	}

	fmt.Printf("GitHub Zen: %s\n", string(body1))

	// Second request - should hit the cache
	startTime2 := time.Now()
	resp2, err := client.Get(url)
	duration2 := time.Since(startTime2)
	if err != nil {
		t.Fatalf("Failed on second request to GitHub API: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code for second request: %d", resp2.StatusCode)
	}

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("Failed to read second response: %v", err)
	}

	if string(body1) != string(body2) {
		t.Errorf("Response bodies don't match between requests")
	}

	timestampHeader := resp2.Header.Get("X-Cache-Timestamp")
	if timestampHeader == "" {
		t.Error("Expected X-Cache-Timestamp header in cached response")
	}

	t.Logf("First request duration: %v", duration1)
	t.Logf("Second request duration: %v", duration2)
	if duration2 >= duration1 {
		t.Logf("Warning: Second request was not faster than first. This might indicate caching issue.")
		// Don't fail the test on this since network conditions can vary
	}

	// Test another GitHub API endpoint that returns JSON
	jsonURL := "https://api.github.com/meta"

	startTimeJSON1 := time.Now()
	respJSON1, err := client.Get(jsonURL)
	durationJSON1 := time.Since(startTimeJSON1)
	if err != nil {
		t.Fatalf("Failed on first JSON request: %v", err)
	}
	defer respJSON1.Body.Close()

	if respJSON1.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code for first JSON request: %d", respJSON1.StatusCode)
	}

	contentType := respJSON1.Header.Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Logf("Warning: Expected JSON content type, got: %s", contentType)
	}

	var jsonData map[string]interface{}
	jsonBody1, err := io.ReadAll(respJSON1.Body)
	if err != nil {
		t.Fatalf("Failed to read JSON response: %v", err)
	}

	err = json.Unmarshal(jsonBody1, &jsonData)
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	startTimeJSON2 := time.Now()
	respJSON2, err := client.Get(jsonURL)
	durationJSON2 := time.Since(startTimeJSON2)
	if err != nil {
		t.Fatalf("Failed on second JSON request: %v", err)
	}
	defer respJSON2.Body.Close()

	t.Logf("First JSON request duration: %v", durationJSON1)
	t.Logf("Second JSON request duration: %v", durationJSON2)
	if duration2 >= duration1 {
		t.Logf("Warning: Second request was not faster than first. This might indicate caching issue.")
		// Don't fail the test on this since network conditions can vary
	}

	// Test request with query parameters
	paramsURL := "https://api.github.com/repos/golang/go/issues?state=closed&per_page=1"

	resp3, err := client.Get(paramsURL)
	if err != nil {
		t.Fatalf("Failed on request with query parameters: %v", err)
	}
	defer resp3.Body.Close()

	resp4, err := client.Get(paramsURL)
	if err != nil {
		t.Fatalf("Failed on second request with query parameters: %v", err)
	}
	defer resp4.Body.Close()

	// Final test: retrieve all cache entries and verify they contain our test URLs
	sqliteCache, err := NewSQLiteCache(dbPathStr)
	if err != nil {
		t.Fatalf("Failed to open cache directly: %v", err)
	}
	defer sqliteCache.Close()

	entries, err := sqliteCache.GetAll()
	if err != nil {
		t.Fatalf("Failed to get all cache entries: %v", err)
	}

	// We should have at least 3 entries (for 3 different URLs)
	if len(entries) < 3 {
		t.Errorf("Expected at least 3 cache entries, got %d", len(entries))
	}

	t.Logf("Cache contains %d entries", len(entries))

	// Print the first few entries for debugging
	for i, entry := range entries {
		if i >= 3 {
			break
		}
		t.Logf("Cache entry %d: %s %s (status=%d, body_size=%d bytes)",
			i+1, entry.Method, entry.Url, entry.StatusCode, len(entry.Body))
	}
}
