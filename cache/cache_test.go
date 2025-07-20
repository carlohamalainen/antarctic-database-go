package cache

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestHeadersJSONConversion(t *testing.T) {
	// Create test headers
	headers := make(http.Header)
	headers.Add("Content-Type", "application/json")
	headers.Add("X-Test", "Value1")
	headers.Add("X-Test", "Value2") // Add a duplicate key to test multiple values

	// Convert to JSON
	json, err := HeadersToJSON(headers)
	if err != nil {
		t.Fatalf("Failed to convert headers to JSON: %v", err)
	}

	// Convert back from JSON
	restored, err := HeadersFromJSON(json)
	if err != nil {
		t.Fatalf("Failed to convert JSON back to headers: %v", err)
	}

	// Check if restored headers match original
	if len(restored) != len(headers) {
		t.Errorf("Restored headers length %d does not match original length %d", len(restored), len(headers))
	}

	for key, values := range headers {
		restoredValues, ok := restored[key]
		if !ok {
			t.Errorf("Key %s not found in restored headers", key)
			continue
		}

		if len(values) != len(restoredValues) {
			t.Errorf("Values count for key %s doesn't match: original=%d, restored=%d",
				key, len(values), len(restoredValues))
			continue
		}

		for i, value := range values {
			if value != restoredValues[i] {
				t.Errorf("Value mismatch for key %s at index %d: expected=%s, got=%s",
					key, i, value, restoredValues[i])
			}
		}
	}
}

func TestCache(t *testing.T) {
	// Create a temporary database file
	dbFile, err := os.CreateTemp("", "cache-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	dbPath := dbFile.Name()
	dbFile.Close()
	defer os.Remove(dbPath)

	// Create cache
	cache, err := NewCache(dbPath)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	defer cache.SqliteCache.Close()

	// Test data
	url := "https://example.com/test"
	method := "GET"
	body := []byte("test response body")
	statusCode := 200
	status := "200 OK"

	// Create sample headers
	headers := make(http.Header)
	headers.Add("Content-Type", "text/plain")
	headers.Add("Cache-Control", "no-cache")

	// Create a response to cache
	resp := &http.Response{
		Status:     status,
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     headers,
	}

	// Cache the response
	_, err = cache.Set(resp, url, method)
	if err != nil {
		t.Fatalf("Failed to cache response: %v", err)
	}

	// Retrieve from cache
	cachedResp, err := cache.Get(Url(url), method)
	if err != nil {
		t.Fatalf("Failed to retrieve from cache: %v", err)
	}

	if cachedResp == nil {
		t.Fatalf("Expected cached response, got nil")
	}

	// Verify status code
	if cachedResp.StatusCode != statusCode {
		t.Errorf("Expected status code %d, got %d", statusCode, cachedResp.StatusCode)
	}

	// Verify status
	if cachedResp.Status != status {
		t.Errorf("Expected status %s, got %s", status, cachedResp.Status)
	}

	// Verify headers
	for key, values := range headers {
		cachedValues, ok := cachedResp.Header[key]
		if !ok {
			t.Errorf("Header %s not found in cached response", key)
			continue
		}

		if len(values) != len(cachedValues) {
			t.Errorf("Header values count for %s doesn't match: original=%d, cached=%d",
				key, len(values), len(cachedValues))
			continue
		}

		for i, value := range values {
			if value != cachedValues[i] {
				t.Errorf("Header value mismatch for %s at index %d: expected=%s, got=%s",
					key, i, value, cachedValues[i])
			}
		}
	}

	// Verify body
	cachedBody, err := io.ReadAll(cachedResp.Body)
	if err != nil {
		t.Fatalf("Failed to read cached body: %v", err)
	}

	if !bytes.Equal(cachedBody, body) {
		t.Errorf("Body mismatch: expected=%s, got=%s", body, cachedBody)
	}

	// Verify timestamp header is present
	timestampHeader := cachedResp.Header.Get("X-Cache-Timestamp")
	if timestampHeader == "" {
		t.Error("X-Cache-Timestamp header not found")
	} else {
		_, err := time.Parse(time.RFC3339, timestampHeader)
		if err != nil {
			t.Errorf("Invalid timestamp format: %v", err)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	// Create a temporary database file
	dbFile, err := os.CreateTemp("", "cache-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	dbPath := dbFile.Name()
	dbFile.Close()
	defer os.Remove(dbPath)

	// Create cache
	cache, err := NewCache(dbPath)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	defer cache.SqliteCache.Close()

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test server response"))
	}))
	defer server.Close()

	// Create a test request
	req, err := http.NewRequest("GET", server.URL+"/test", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// First request - should hit the server
	resp1, err := cache.RoundTrip(req)
	if err != nil {
		t.Fatalf("Failed on first round trip: %v", err)
	}
	defer resp1.Body.Close()

	// Read response body
	body1, err := io.ReadAll(resp1.Body)
	if err != nil {
		t.Fatalf("Failed to read first response: %v", err)
	}

	if string(body1) != "test server response" {
		t.Errorf("Unexpected first response body: %s", body1)
	}

	// Second request - should hit the cache
	resp2, err := cache.RoundTrip(req)
	if err != nil {
		t.Fatalf("Failed on second round trip: %v", err)
	}
	defer resp2.Body.Close()

	// Read response body
	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("Failed to read second response: %v", err)
	}

	if string(body2) != "test server response" {
		t.Errorf("Unexpected second response body: %s", body2)
	}

	// Verify timestamp header is present in the cached response
	timestampHeader := resp2.Header.Get("X-Cache-Timestamp")
	if timestampHeader == "" {
		t.Error("X-Cache-Timestamp header not found in cached response")
	}
}

func TestHTTPClient(t *testing.T) {
	// Create a temporary database file
	dbFile, err := os.CreateTemp("", "cache-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	dbPath := dbFile.Name()
	dbFile.Close()
	defer os.Remove(dbPath)

	// Create HTTP client with caching
	dbPathStr := dbPath // Need a variable for the pointer
	client, err := NewHTTPClient(&dbPathStr)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test http client response"))
	}))
	defer server.Close()

	// First request - should hit the server
	resp1, err := client.Get(server.URL + "/test")
	if err != nil {
		t.Fatalf("Failed on first request: %v", err)
	}
	defer resp1.Body.Close()

	// Read response body
	body1, err := io.ReadAll(resp1.Body)
	if err != nil {
		t.Fatalf("Failed to read first response: %v", err)
	}

	if string(body1) != "test http client response" {
		t.Errorf("Unexpected first response body: %s", body1)
	}

	// Second request - should hit the cache
	resp2, err := client.Get(server.URL + "/test")
	if err != nil {
		t.Fatalf("Failed on second request: %v", err)
	}
	defer resp2.Body.Close()

	// Read response body
	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("Failed to read second response: %v", err)
	}

	if string(body2) != "test http client response" {
		t.Errorf("Unexpected second response body: %s", body2)
	}

	// Verify timestamp header is present in the cached response
	timestampHeader := resp2.Header.Get("X-Cache-Timestamp")
	if timestampHeader == "" {
		t.Error("X-Cache-Timestamp header not found in cached response")
	}
}

func TestSQLiteCache(t *testing.T) {
	// Create a temporary database file
	dbFile, err := os.CreateTemp("", "sqlitecache-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	dbPath := dbFile.Name()
	dbFile.Close()
	defer os.Remove(dbPath)

	// Create SQLite cache
	cache, err := NewSQLiteCache(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite cache: %v", err)
	}
	defer cache.Close()

	// Test data
	key := RequestKey{Method: "GET", URL: "https://example.com/test"}
	headers, _ := HeadersToJSON(http.Header{"Content-Type": []string{"text/plain"}})

	row := CacheRow{
		Method:     key.Method,
		Url:        key.URL,
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    headers,
		Body:       []byte("test body"),
		Timestamp:  time.Now(),
	}

	// Test setting a row
	err = cache.Set(row)
	if err != nil {
		t.Fatalf("Failed to set cache row: %v", err)
	}

	// Test getting the row
	retrievedRow, err := cache.Get(key)
	if err != nil {
		t.Fatalf("Failed to get cache row: %v", err)
	}

	if retrievedRow == nil {
		t.Fatalf("Expected cache row, got nil")
	}

	// Verify fields
	if retrievedRow.Method != row.Method {
		t.Errorf("Method mismatch: expected=%s, got=%s", row.Method, retrievedRow.Method)
	}

	if retrievedRow.Url != row.Url {
		t.Errorf("URL mismatch: expected=%s, got=%s", row.Url, retrievedRow.Url)
	}

	if retrievedRow.StatusCode != row.StatusCode {
		t.Errorf("Status code mismatch: expected=%d, got=%d", row.StatusCode, retrievedRow.StatusCode)
	}

	if retrievedRow.Status != row.Status {
		t.Errorf("Status mismatch: expected=%s, got=%s", row.Status, retrievedRow.Status)
	}

	if retrievedRow.Headers != row.Headers {
		t.Errorf("Headers mismatch: expected=%s, got=%s", row.Headers, retrievedRow.Headers)
	}

	if !bytes.Equal(retrievedRow.Body, row.Body) {
		t.Errorf("Body mismatch: expected=%s, got=%s", row.Body, retrievedRow.Body)
	}

	// Test GetAll
	rows, err := cache.GetAll()
	if err != nil {
		t.Fatalf("Failed to get all cache rows: %v", err)
	}

	if len(rows) != 1 {
		t.Errorf("Expected 1 row, got %d", len(rows))
	}
}
