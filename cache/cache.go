package cache

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/elazarl/goproxy"
)

type Url string

type RequestKey struct {
	Method string
	URL    Url
}

type CacheRow struct {
	StatusCode int
	Status     string
	Headers    string
	Method     string
	Url        Url
	Body       []byte
	Timestamp  time.Time
}

// HeadersToJSON converts http.Header to a JSON string for storage in sqlite.
func HeadersToJSON(headers http.Header) (string, error) {
	jsonBytes, err := json.Marshal(headers)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

func HeadersFromJSON(jsonStr string) (http.Header, error) {
	headers := make(http.Header)
	err := json.Unmarshal([]byte(jsonStr), &headers)
	if err != nil {
		return nil, err
	}
	return headers, nil
}

type Cache struct {
	SqliteCache *SQLiteCache
	Transport   http.RoundTripper
}

func (c *Cache) Get(url Url, method string) (*http.Response, error) {
	row, err := c.SqliteCache.Get(RequestKey{URL: url, Method: method})
	if err != nil {
		return nil, err
	}

	if row == nil {
		return nil, nil // did not find, not an error
	}

	headers, err := HeadersFromJSON(row.Headers)
	if err != nil {
		return nil, err
	}

	resp := &http.Response{
		Status:     row.Status,
		StatusCode: row.StatusCode,
		Body:       io.NopCloser(bytes.NewReader(row.Body)),
		Header:     headers,
	}

	resp.Header.Set("X-Cache-Timestamp", row.Timestamp.Format(time.RFC3339))

	return resp, nil
}

func (c *Cache) Set(resp *http.Response, url string, method string) (*http.Response, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	headers, err := HeadersToJSON(resp.Header)
	if err != nil {
		return nil, err
	}

	row := CacheRow{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    headers,
		Method:     method,
		Url:        Url(url),
		Body:       body,
		Timestamp:  time.Now(),
	}

	if err := c.SqliteCache.Set(row); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Cache) Delete(url Url, method string) error {
	key := RequestKey{URL: url, Method: method}
	return c.SqliteCache.Delete(key)
}

func (c *Cache) transport() http.RoundTripper {
	if c.Transport != nil {
		return c.Transport
	}
	return http.DefaultTransport
}

func (c *Cache) RoundTrip(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	method := req.Method

	key := RequestKey{Method: req.Method, URL: Url(url)}

	resp, err := c.Get(Url(url), req.Method)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		return resp, nil
	}

	resp, err = c.transport().RoundTrip(req)
	if err != nil {
		return nil, err
	}

	slog.Debug("caching", "key", key)

	resp, err = c.Set(resp, url, method)

	if err != nil {
		return nil, err
	}
	return resp, nil
}

func NewCache(dbPath string) (*Cache, error) {
	sqliteCache, err := NewSQLiteCache(dbPath)
	if err != nil {
		return nil, err
	}

	c := Cache{SqliteCache: sqliteCache}

	return &c, nil
}

// NewHTTPClient creates an HTTP client with optional sqlite caching.
//
// If dbFile is nil, it returns a standard HTTP client without caching.
// Otherwise, it creates a client that caches responses in the specified sqlite file.
// The dbFile path must be absolute.
//
// Returns:
//   - An *http.Client configured with the appropriate transport
//   - An error if creating the cache fails
//
// The client logs information about its configuration on creation.
func NewHTTPClient(dbFile *string) (*http.Client, error) {
	if dbFile == nil {
		slog.Info("created fallback http client without caching")
		return &http.Client{}, nil
	}

	if !filepath.IsAbs(*dbFile) {
		err := fmt.Errorf("need absolute path for file, got: %s", *dbFile)
		slog.Error(err.Error())
		return &http.Client{}, err
	}

	c, err := NewCache(*dbFile)
	if err != nil {
		return nil, err
	}

	slog.Info("created http client with caching", "filename", *dbFile)

	return &http.Client{Transport: c}, nil
}

func NewCachedProxyHandler(cache *Cache) (http.Handler, error) {
	setCA, err := tls.X509KeyPair(goproxy.CA_CERT, goproxy.CA_KEY)
	if err != nil {
		return nil, fmt.Errorf("invalid certificate: %w", err)
	}

	goproxy.GoproxyCa = setCA

	proxy := goproxy.NewProxyHttpServer()

	proxy.CertStore = &CertStorage{}

	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	proxy.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		TLSHandshakeTimeout: 10 * time.Second,
		Proxy:               http.ProxyFromEnvironment,
	}

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		url := req.URL.String()

		cachedResp, err := cache.Get(Url(url), req.Method)
		if err != nil {
			slog.Error("failed to retrieve from cache", "error", err.Error())
			os.Exit(1)
		}

		if cachedResp != nil {
			slog.Debug("OnRequest cache hit", "url", url, "method", req.Method)
			cachedResp.Request = req // Important! goproxy expects this linkage.
			return req, cachedResp
		}

		// Store the request key for later use.
		slog.Debug("OnRequest cache miss", "url", url, "method", req.Method)
		ctx.UserData = RequestKey{URL: Url(url), Method: req.Method}
		return req, nil
	})

	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if resp != nil && ctx.UserData != nil {
			if resp.Request == nil {
				slog.Error("OnResponse and resp.Request == nil")
				os.Exit(1)
			}
			if cacheKey, ok := ctx.UserData.(RequestKey); ok && shouldCache(resp) {
				cachedResp, err := cloneResponse(resp)
				if err != nil {
					slog.Error("failed to clone response", "error", err.Error())
					os.Exit(1)
				}

				slog.Debug("OnResponse setting cache", "url", cacheKey.URL, "method", cacheKey.Method)
				cache.Set(cachedResp, string(cacheKey.URL), cacheKey.Method)
			}
		}
		return resp
	})

	return proxy, nil
}

func shouldCache(resp *http.Response) bool {
	return resp.StatusCode == 200
}

func cloneResponse(resp *http.Response) (*http.Response, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Restore the original body for further use.
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	clone := &http.Response{
		Status:           resp.Status,
		StatusCode:       resp.StatusCode,
		Proto:            resp.Proto,
		ProtoMajor:       resp.ProtoMajor,
		ProtoMinor:       resp.ProtoMinor,
		Header:           make(http.Header),
		ContentLength:    resp.ContentLength,
		TransferEncoding: resp.TransferEncoding,
		Close:            resp.Close,
		Uncompressed:     resp.Uncompressed,
		Request:          resp.Request,
	}

	for k, vv := range resp.Header {
		for _, v := range vv {
			clone.Header.Add(k, v)
		}
	}

	clone.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	return clone, nil
}

// RunServer starts an HTTP server that gracefully handles shutdown signals.
//
// It creates a cancellable context and sets up signal handlers for graceful shutdown
// when receiving SIGINT (Ctrl+C) or SIGTERM signals. The server runs in a separate
// goroutine, and upon shutdown signal, it:
//  1. Logs the shutdown intent
//  2. Initiates graceful shutdown with a 10-second timeout
//  3. Waits for all connections to complete or timeout
//  4. Logs completion status
//
// Parameters:
//   - addr: Network address for the server to listen on (e.g., ":8080")
//   - handler: HTTP handler to process incoming requests
//
// Returns:
//   - context.CancelFunc: Function that can be called to trigger server shutdown programmatically
func RunServer(addr string, handler http.Handler) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("Starting HTTP proxy server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		slog.Info("Context canceled, shutting down server")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}

		slog.Info("server shutdown complete")
	}()

	go func() {
		<-stop
		slog.Info("shutdown signal received, cancelling context")
		cancel()

		wg.Wait()
		slog.Info("all components shut down successfully")

	}()

	return cancel
}

type CertStorage struct {
	certs sync.Map
}

func (cs *CertStorage) Fetch(hostname string, gen func() (*tls.Certificate, error)) (*tls.Certificate, error) {
	if value, ok := cs.certs.Load(hostname); ok {
		return value.(*tls.Certificate), nil
	}

	cert, err := gen()
	if err != nil {
		return nil, err
	}

	actual, _ := cs.certs.LoadOrStore(hostname, cert)
	return actual.(*tls.Certificate), nil
}

type SQLiteCache struct {
	db *sql.DB
}

// NewSQLiteCache creates a new SQLite cache with the given database file path
func NewSQLiteCache(dbPath string) (*SQLiteCache, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	cache := &SQLiteCache{db: db}
	if err := cache.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return cache, nil
}

func (c *SQLiteCache) Close() error {
	return c.db.Close()
}

func (c *SQLiteCache) initialize() error {
	query := `
	CREATE TABLE IF NOT EXISTS cache (
		method TEXT NOT NULL,
		url TEXT NOT NULL,
		status_code INTEGER NOT NULL,
		status TEXT NOT NULL,
		headers TEXT NOT NULL,
		body BLOB,
		timestamp DATETIME NOT NULL,
		PRIMARY KEY (method, url)
	);
	`
	_, err := c.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create cache table: %w", err)
	}
	return nil
}

func (c *SQLiteCache) Get(key RequestKey) (*CacheRow, error) {
	query := `
	SELECT method, url, status_code, status, headers, body, timestamp
	FROM cache
	WHERE method = ? AND url = ?
	`
	rows := c.db.QueryRow(query, key.Method, string(key.URL))

	var row CacheRow
	var timestamp string

	err := rows.Scan(
		&row.Method,
		&row.Url,
		&row.StatusCode,
		&row.Status,
		&row.Headers,
		&row.Body,
		&timestamp,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Not found, but not an error
		}
		return nil, fmt.Errorf("failed to query cache: %w", err)
	}

	row.Timestamp, err = time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	return &row, nil
}

// Set inserts or updates a CacheRow in SQLite
// Returns an error if the row exists with different headers or body
// Uses a transaction to ensure atomicity and prevent race conditions
func (c *SQLiteCache) Set(row CacheRow) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// Ensure transaction is either committed or rolled back
	defer func() {
		if tx != nil {
			tx.Rollback() // Rollback does nothing if transaction was already committed
		}
	}()

	// Check if the row exists within the transaction
	query := `
	SELECT headers, body FROM cache
	WHERE method = ? AND url = ?
	`
	rows := tx.QueryRow(query, row.Method, string(row.Url))

	var existingHeaders string
	var existingBody []byte
	err = rows.Scan(&existingHeaders, &existingBody)

	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to check existing cache entry: %w", err)
		}
		// Row doesn't exist, insert it
		insertQuery := `
		INSERT INTO cache (method, url, status_code, status, headers, body, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		`
		_, err = tx.Exec(
			insertQuery,
			row.Method,
			string(row.Url),
			row.StatusCode,
			row.Status,
			row.Headers,
			row.Body,
			row.Timestamp.Format(time.RFC3339),
		)
		if err != nil {
			return fmt.Errorf("failed to insert cache entry: %w", err)
		}
	} else {
		// Row exists, check if headers or body match
		if existingHeaders != row.Headers {
			return errors.New("cache entry exists with different headers")
		}
		if !bytes.Equal(existingBody, row.Body) {
			return errors.New("cache entry exists with different body")
		}
		// If both match, we can just leave the existing entry as is
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	tx = nil // Prevent rollback in deferred function

	return nil
}

// Delete removes a cache entry for the specified URL and method from SQLite
// Returns nil if the entry was deleted or didn't exist
// Returns an error if there was a problem executing the query
func (c *SQLiteCache) Delete(key RequestKey) error {
	query := `
	DELETE FROM cache
	WHERE method = ? AND url = ?
	`
	_, err := c.db.Exec(query, key.Method, string(key.URL))
	if err != nil {
		return fmt.Errorf("failed to delete cache entry: %w", err)
	}
	return nil
}

func (c *SQLiteCache) GetAll() ([]CacheRow, error) {
	query := `
	SELECT method, url, status_code, status, headers, body, timestamp
	FROM cache
	ORDER BY timestamp DESC
	`
	rows, err := c.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query cache: %w", err)
	}
	defer rows.Close()

	var result []CacheRow
	for rows.Next() {
		var row CacheRow
		var timestamp string

		err := rows.Scan(
			&row.Method,
			&row.Url,
			&row.StatusCode,
			&row.Status,
			&row.Headers,
			&row.Body,
			&timestamp,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan cache row: %w", err)
		}

		// Parse the timestamp
		row.Timestamp, err = time.Parse(time.RFC3339, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}

		result = append(result, row)
	}

	// Check for errors during iteration
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return result, nil
}
