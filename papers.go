package ats

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/carlohamalainen/antarctic-database-go/cache"
)

type PageOCR struct {
	ID        string
	URL       string
	PageNr    int
	PageText  string
	Timestamp time.Time
}

type FullText struct {
	ID           string
	URL          string
	DocumentText string
	Timestamp    time.Time
}

type PaperRecord struct {
	MeetingYear   int    `parquet:"meeting_year,required"`
	MeetingType   string `parquet:"meeting_type,required"`
	MeetingNumber int    `parquet:"meeting_number,required"`
	MeetingName   string `parquet:"meeting_name,required"`
	Party         string `parquet:"party,required"`
	Category      string `parquet:"category,required"`
	PageUrl       string `parquet:"page_url,required"`
	PageNr        int    `parquet:"page_nr,required"`
	PayloadJson   string `parquet:"payload_json,required"`

	PaperId       int    `parquet:"paper_id,required"`
	PaperType     string `parquet:"party_type,required"`
	PaperName     string `parquet:"paper_name,required"`
	PaperNumber   int    `parquet:"paper_number,required"`
	PaperRevision int    `parquet:"paper_revision,required"`
	PaperLanguage string `parquet:"paper_language,required"`
	PaperUrl      string `parquet:"paper_url,required"`
	PaperExists   bool   `parquet:"exists,required"`

	Agendas []string `parquet:"agendas,optional"`
	Parties []string `parquet:"parties,optional"`

	AttachmentId       int    `parquet:"attachment_id,optional"`
	AttachmentName     string `parquet:"attachment_name,optional"`
	AttachmentLanguage string `parquet:"attachment_language,optional"`
	AttachmentUrl      string `parquet:"attachment_url,optional"`
	AttachmentExists   bool   `parquet:"attachment_exists,optional"`
}

type DatasetRecord struct {
	Url       string    `parquet:"url,required"`
	Timestamp time.Time `parquet:"timestamp,required"`
	Blob      []byte    `parquet:"blob,required"`
}

func SetupDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("PRAGMA journal_mode = WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to set journal mode: %w", err)
	}

	_, err = db.Exec(`
    CREATE TABLE IF NOT EXISTS documents (
        id TEXT PRIMARY KEY,
        url TEXT,
        document BLOB,
        timestamp DATETIME,
		is_scanned BOOLEAN DEFAULT FALSE,
        status TEXT DEFAULT 'new'
    )`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
    CREATE TABLE IF NOT EXISTS pages (
        id TEXT,
        url TEXT,
        page_nr INTEGER,
        page_pdf BLOB,
        status TEXT DEFAULT 'extracted',
        PRIMARY KEY (id, page_nr),
        FOREIGN KEY (id) REFERENCES documents(id) ON DELETE CASCADE
    )`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
    CREATE TABLE IF NOT EXISTS ocr (
        id TEXT,
        page_nr INTEGER,
        page_text TEXT,
        method TEXT,
        timestamp DATETIME,
        PRIMARY KEY (id, page_nr, method, timestamp),
        FOREIGN KEY (id, page_nr) REFERENCES pages(id, page_nr) ON DELETE CASCADE
    )`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
    CREATE TABLE IF NOT EXISTS full_text (
        id TEXT,
        document_text TEXT,
        method TEXT,
        timestamp DATETIME,
        PRIMARY KEY (id, method, timestamp),
        FOREIGN KEY (id) REFERENCES documents(id) ON DELETE CASCADE
    )`)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func Sha256sum(x []byte) string {
	h := sha256.New()
	h.Write(x)

	return fmt.Sprintf("%x", h.Sum(nil))
}

func DownloadDocuments(db *sql.DB, timeout time.Duration, client *http.Client, xs []PaperRecord) error {
	urls := []string{}

	hashes := map[string]string{}

	for _, x := range xs {
		urls = append(urls, x.PaperUrl)
	}

	slices.Sort(urls)
	urls = slices.Compact(urls)

	for _, url := range urls {

		t0 := time.Now()

		slog.Info("downloading", "url", url)
		responseBody, responseCode, responseTimestamp, err := DownloadWithRetry(url, timeout, client, 10, 2*time.Second)
		if err != nil {
			return err
		}
		elapsed := time.Since(t0)

		slog.Info("downloaded", "url", url, "size", len(responseBody), "duration_ms", elapsed.Milliseconds())

		if responseCode != 200 {
			slog.Info("bad response code", "url", url, "response_code", responseCode)
			continue
		}

		if strings.Contains(string(responseBody), "Page not found") {
			// silly 404 page, ends up being cached with response code 200
			// due to middleware handling the redirect!
			//
			// https://www.ats.aq/devAS/Error/NotFound
			//
			slog.Info("page does not exist", "url", url, "response_code", 404)
			continue
		}

		// checking for dups
		h := Sha256sum(append([]byte(url), responseBody...))
		seenUrl, seen := hashes[h]
		if seen {
			return fmt.Errorf("duplicate %s vs %s", url, seenUrl)
		} else {
			hashes[h] = url
		}

		isScanned := false

		if strings.HasSuffix(url, ".pdf") {
			isScanned, err = IsScannedPDF(responseBody)
			if err != nil {
				slog.Debug("failed to parse PDF", "error", err)
				return err
			}
		}

		extension := path.Ext(url)

		slog.Info("status", "url", url, "size", len(responseBody), "extension", extension, "is_scanned", isScanned)

		document_id, err := InsertDocument(db, url, responseBody, responseTimestamp, isScanned)
		if err != nil {
			return err
		}
		slog.Info("inserted", "url", url, "document_id", document_id)

		if elapsed.Milliseconds() < 100 {
			// probable cache hit so no sleep
			continue
		}

		slog.Info("sleeping")
		time.Sleep(3 * time.Second)
	}

	return nil
}

// InsertDocument adds a new document to the database.
// If a document with the same ID already exists, it returns an error.
func InsertDocument(db *sql.DB, url string, document []byte, timestamp time.Time, isScanned bool) (string, error) {
	id := Sha256sum(append([]byte(url), document...))

	stmt, err := db.Prepare(`
        INSERT INTO documents (id, url, document, timestamp, is_scanned, status)
        VALUES (?, ?, ?, ?, ?, 'new')
    `)
	if err != nil {
		return "", fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(id, url, document, timestamp, isScanned)
	if err != nil {
		return "", fmt.Errorf("failed to insert document: %w", err)
	}

	return id, nil
}

func extractPageNumber(filename string) int {
	base := filepath.Base(filename)
	numStr := strings.TrimPrefix(base, "output-")
	numStr = strings.TrimSuffix(numStr, ".pdf")

	num, err := strconv.Atoi(numStr)
	if err != nil {
		panic(err) // should never happen
	}
	return num
}

// SplitScannedPDFs scans the documents table for documents with status 'new'
// and splits them into pages, saving each page in the pages table
func SplitScannedPDFs(db *sql.DB) error {
	// Get all scanned documents with status 'new'
	rows, err := db.Query("SELECT id, url, document FROM documents WHERE status = 'new' AND is_scanned = TRUE")
	if err != nil {
		return fmt.Errorf("failed to query documents: %w", err)
	}
	defer rows.Close()

	// Process each document
	for rows.Next() {
		var (
			docID    string
			docUrl   string
			docBytes []byte
		)

		if err := rows.Scan(&docID, &docUrl, &docBytes); err != nil {
			return fmt.Errorf("failed to scan document row: %w", err)
		}

		slog.Info("splitting", "document_url", docUrl, "document_id", docID)

		tmpFile, err := os.CreateTemp("", "pdf-*.pdf")
		if err != nil {
			return fmt.Errorf("failed to create temp file for doc %s: %w", docID, err)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := tmpFile.Write(docBytes); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write to temp file for doc %s: %w", docID, err)
		}
		tmpFile.Close()

		outDir, err := os.MkdirTemp("", "pdf-pages")
		if err != nil {
			return fmt.Errorf("failed to create output directory for doc %s: %w", docID, err)
		}
		defer os.RemoveAll(outDir)

		outputPattern := filepath.Join(outDir, "output-%d.pdf")

		cmd := exec.Command("pdfseparate", tmpPath, outputPattern)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("pdfseparate failed: %w, output: %s", err, output)
		}

		files, err := filepath.Glob(filepath.Join(outDir, "output-*.pdf"))
		if err != nil {
			return fmt.Errorf("failed to list output files: %w", err)
		}

		// Start a transaction for inserting pages
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for doc %s: %w", docID, err)
		}

		for _, file := range files {
			i := extractPageNumber(file)

			if !strings.HasSuffix(file, ".pdf") {
				continue
			}

			// Read the page file
			pageBytes, err := os.ReadFile(file)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to read page file %s for doc %s: %w", file, docID, err)
			}

			// Insert into pages table
			_, err = tx.Exec(
				"INSERT INTO pages (id, url, page_nr, page_pdf, status) VALUES (?, ?, ?, ?, 'extracted')",
				docID, docUrl, i, pageBytes,
			)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to insert page %d for doc %s: %w", i+1, docID, err)
			}
		}

		// Update document status to 'pages_extracted'
		_, err = tx.Exec(
			"UPDATE documents SET status = 'pages_extracted' WHERE id = ?",
			docID,
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update document status for %s: %w", docID, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for doc %s: %w", docID, err)
		}

		fmt.Printf("Processed document %s: extracted %d pages\n", docID, len(files))
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating document rows: %w", err)
	}

	return nil
}

func saveDocument(docUrl string, data []byte, outputDir string) error {
	parsed, err := url.Parse(docUrl)
	if err != nil {
		return fmt.Errorf("failed to parse url: %w", err)
	}

	relPath := filepath.Join(outputDir, strings.TrimPrefix(parsed.Path, "/"))

	dir := filepath.Dir(relPath)

	filePath := relPath

	fmt.Println(dir)
	if err := os.MkdirAll(dir, fs.ModePerm); err != nil {
		return fmt.Errorf("failed to make directory %s: %w", dir, err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	slog.Info("saved", "url", docUrl, "size", len(data), "filepath", filePath)

	return nil
}

func SaveDocuments(timeout time.Duration, client *http.Client, xs []PaperRecord, outputDir string) (int, error) {
	count := 0

	urls := []string{}

	for _, x := range xs {
		urls = append(urls, x.PaperUrl)
	}

	slices.Sort(urls)
	urls = slices.Compact(urls)

	for _, url := range urls {
		slog.Info("downloading", "url", url)
		responseBody, responseCode, responseTimestamp, err := Download(URL(url), timeout, client)
		if err != nil {
			return 0, fmt.Errorf("failed to download %s: %w", url, err)
		}

		slog.Info("downloaded", "url", url, "size", len(responseBody), "timestamp", responseTimestamp)

		if responseCode != 200 {
			slog.Info("bad response code", "url", url, "response_code", responseCode)
			continue
		}

		if strings.Contains(string(responseBody), "Page not found") {
			// silly 404 page, ends up being cached with response code 200
			// due to middleware handling the redirect!
			//
			// https://www.ats.aq/devAS/Error/NotFound
			//
			slog.Info("page does not exist", "url", url, "response_code", 404)
			continue
		}

		if err := saveDocument(url, responseBody, outputDir); err != nil {
			return 0, fmt.Errorf("failed to save document %s to %s: %w", url, outputDir, err)
		}

		count++
	}

	return count, nil
}

func SaveOcrTextFiles(dbFile string, outputDir string) (int, error) {
	count := 0

	db, err := sql.Open("sqlite3", "file:"+dbFile+"?mode=ro")
	if err != nil {
		return 0, fmt.Errorf("could not open db: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT
			p.id,
			p.url,
			p.page_nr,
			o.page_text,
			o.timestamp
		FROM pages p
		JOIN (
			SELECT id, page_nr, MAX(timestamp) AS latest_ts
			FROM ocr
			GROUP BY id, page_nr
		) latest_ocr ON p.id = latest_ocr.id AND p.page_nr = latest_ocr.page_nr
		JOIN ocr o ON o.id = latest_ocr.id AND o.page_nr = latest_ocr.page_nr AND o.timestamp = latest_ocr.latest_ts
		ORDER BY p.id, p.page_nr, o.timestamp DESC
	`)
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	type document map[int]string // page nr => text   (pages start from 1)

	documents := map[string]document{}

	for rows.Next() {
		var entry PageOCR
		if err := rows.Scan(&entry.ID, &entry.URL, &entry.PageNr, &entry.PageText, &entry.Timestamp); err != nil {
			return 0, fmt.Errorf("row scan failed: %w", err)
		}

		if !strings.HasSuffix(entry.URL, ".pdf") {
			return 0, fmt.Errorf("not a PDF: %s", entry.URL)
		}

		doc, ok := documents[entry.ID]

		if !ok {
			doc = make(document)
		}

		_, pageExists := doc[entry.PageNr]

		if pageExists {
			return 0, fmt.Errorf("in document %s we found page %d repeated", entry.ID, entry.PageNr)
		} else {
			doc[entry.PageNr] = entry.PageText
		}

		documents[entry.ID] = doc
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("row error: %w", err)
	}

	rows, err = db.Query(`
	SELECT
		id,
		url,
		COUNT(DISTINCT page_nr) AS page_count
	FROM pages
	GROUP BY id, url
	ORDER BY id
	`)

	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var pageId, pageUrl string
		var pageCount int

		if err := rows.Scan(&pageId, &pageUrl, &pageCount); err != nil {
			return 0, fmt.Errorf("row scan failed: %w", err)
		}

		document, ok := documents[pageId]
		if !ok {
			return 0, fmt.Errorf("missing document: %s", pageId)
		}

		if len(document) != pageCount {
			return 0, fmt.Errorf("we expected %d pages but got %d", len(document), pageCount)
		}

		// ok, save this as a text file

		text := ""

		for i := 1; i <= pageCount; i++ {
			text += document[i]
			text += "\n"
		}

		fmt.Println(pageId, pageUrl)

		pageUrlTxt := strings.Replace(pageUrl, ".pdf", ".txt", 1)

		err := saveDocument(pageUrlTxt, []byte(text), outputDir)
		if err != nil {
			return 0, fmt.Errorf("failed to save document: %w", err)
		}

		count++
	}

	if err := rows.Err(); err != nil {
		return 0, err
	}

	return count, nil
}

func SaveFullTextFiles(dbFile string, outputDir string) (int, error) {
	count := 0

	db, err := sql.Open("sqlite3", "file:"+dbFile+"?mode=ro")
	if err != nil {
		return 0, fmt.Errorf("could not open database: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`
			SELECT
			d.id,
			d.url,
			ft.document_text,
			ft.timestamp
		FROM documents d
		JOIN (
			SELECT id,  MAX(timestamp) AS latest_ts
			FROM full_text
			GROUP BY id
		) latest ON d.id = latest.id
		JOIN full_text ft ON ft.id = latest.id AND ft.timestamp = latest.latest_ts
		ORDER BY d.id, ft.timestamp DESC
	`)
	if err != nil {
		return 0, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	documents := map[string]struct{}{} // id => {}

	for rows.Next() {
		var entry FullText
		if err := rows.Scan(&entry.ID, &entry.URL, &entry.DocumentText, &entry.Timestamp); err != nil {
			return 0, fmt.Errorf("row scan failed: %w", err)
		}

		_, ok := documents[entry.ID]

		if ok {
			return 0, fmt.Errorf("repeated ID: %s", entry.ID)
		}

		documents[entry.ID] = struct{}{}

		ext := path.Ext(entry.URL)

		urlTxt := ""

		switch ext {
		case ".pdf":
			urlTxt = entry.URL[:len(entry.URL)-4] + ".txt"
		case ".doc":
			urlTxt = entry.URL[:len(entry.URL)-4] + ".txt"
		case ".docx":
			urlTxt = entry.URL[:len(entry.URL)-5] + ".txt"
		default:
			return 0, fmt.Errorf("unknown extension: %s", ext)
		}

		err := saveDocument(urlTxt, []byte(entry.DocumentText), outputDir)
		if err != nil {
			return 0, fmt.Errorf("failed to save document %s: %w", urlTxt, err)
		}

		count++
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("row error: %w", err)
	}

	return count, nil
}

func Sanity(dbFile string) error {

	db, err := sql.Open("sqlite3", "file:"+dbFile+"?mode=ro")
	if err != nil {
		return fmt.Errorf("could not open db: %w", err)
	}
	defer db.Close()

	// 1. check if any rows in "documents" have status != 'pages_extracted' or 'fulltext-done'; if yes, print id, url, status and return error
	rows, err := db.Query(`
		SELECT id, url, status
		FROM documents
		WHERE status NOT IN ('pages_extracted', 'fulltext-done')
	`)
	if err != nil {
		return fmt.Errorf("failed to query documents: %w", err)
	}
	defer rows.Close()

	var hasInvalidStatus bool
	for rows.Next() {
		var id, url, status string
		if err := rows.Scan(&id, &url, &status); err != nil {
			return fmt.Errorf("failed to scan document row: %w", err)
		}
		fmt.Printf("Document id=%s, url=%s, status=%s\n", id, url, status)
		hasInvalidStatus = true
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating document rows: %w", err)
	}

	if hasInvalidStatus {
		return fmt.Errorf("found documents with invalid status")
	}

	// 2. check if any rows in "pages" have status != 'ocr-done'
	rows, err = db.Query(`
		SELECT id, page_nr, status
		FROM pages
		WHERE status != 'ocr-done'
	`)
	if err != nil {
		return fmt.Errorf("failed to query pages: %w", err)
	}
	defer rows.Close()

	var hasInvalidPageStatus bool
	for rows.Next() {
		var id string
		var pageNr int
		var status string
		if err := rows.Scan(&id, &pageNr, &status); err != nil {
			return fmt.Errorf("failed to scan page row: %w", err)
		}
		fmt.Printf("Page id=%s, page_nr=%d, status=%s\n", id, pageNr, status)
		hasInvalidPageStatus = true
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating page rows: %w", err)
	}

	if hasInvalidPageStatus {
		return fmt.Errorf("found pages with invalid status")
	}

	// 3. for each id, page_nr in "pages", ensure it exists in "ocr"; if any missing print id, page_nr, return error
	rows, err = db.Query(`
		SELECT p.id, p.page_nr
		FROM pages p
		LEFT JOIN (
			SELECT DISTINCT id, page_nr
			FROM ocr
		) o ON p.id = o.id AND p.page_nr = o.page_nr
		WHERE o.id IS NULL
	`)
	if err != nil {
		return fmt.Errorf("failed to query missing OCR: %w", err)
	}
	defer rows.Close()

	var hasMissingOCR bool
	for rows.Next() {
		var id string
		var pageNr int
		if err := rows.Scan(&id, &pageNr); err != nil {
			return fmt.Errorf("failed to scan missing OCR row: %w", err)
		}
		fmt.Printf("Missing OCR for id=%s, page_nr=%d\n", id, pageNr)
		hasMissingOCR = true
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating missing OCR rows: %w", err)
	}

	if hasMissingOCR {
		return fmt.Errorf("found pages missing OCR entries")
	}

	return nil
}

func ingestManualDocuments(cache_ *cache.Cache, csvFile string, utasDir string) ([]PaperRecord, error) {
	xs, err := ReadMissingWPsCsv(csvFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read missing wps csv file: %s, error: %w", csvFile, err)
	}

	urls := []string{}

	for _, d := range xs {
		if d.URL != "" {
			urls = append(urls, d.URL)
		}
	}

	slices.Sort(urls)
	urls = slices.Compact(urls)

	records := []PaperRecord{}

	for _, url := range urls {
		filename := path.Join(utasDir, path.Base(url))

		fileInfo, err := os.Stat(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		body, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %s, error: %w", filename, err)
		}

		slog.Info("manually ingested document", "url", url, "filename", filename, "size", len(body))

		resp := &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}
		resp.Header.Set("X-Cache-Timestamp", fileInfo.ModTime().Format(time.RFC3339))

		const method = "GET"

		err = cache_.Delete(cache.Url(url), method)
		if err != nil {
			return nil, fmt.Errorf("failed to remove cache for %s, error: %w", url, err)
		}

		_, err = cache_.Set(resp, url, method)
		if err != nil {
			return nil, fmt.Errorf("failed to set cache for %s, error: %w", url, err)
		}

		record := PaperRecord{
			PaperUrl: url,
		}

		records = append(records, record)
	}

	return records, nil
}

// TODO This ought to be auto-generated in metadata.go; requested by Nadiah for clearer display.
func PaperTypeToStringShort(m PaperType) string {
	switch m {
	case PaperType_AD:
		return "ad"
	case PaperType_BP:
		return "bp"
	case PaperType_IP:
		return "ip"
	case PaperType_SP:
		return "sp"
	case PaperType_WP:
		return "wp"
	case PaperType_All:
		panic("should not be returning 'All' here")
	default:
		panic("internal error")
	}
}

// TODO This should be auto-generated.
// TODO should drop trailing underscores in the autogen code too.
func CategoryToStringShort(m Category) string {
	switch m {
	case Category_Area_Protection_and_Management_Plans_General:
		return "Area Protection and Management Plans General"
	case Category_Biological_Prospecting:
		return "Biological Prospecting"
	case Category_CEP_Strategy_Discussions:
		return "CEP Strategy Discussions"
	case Category_Climate_Change:
		return "Climate Change"
	case Category_Comprehensive_Environmental_Evaluations:
		return "Comprehensive Environmental Evaluations"
	case Category_Cooperation_with_Other_Organisations:
		return "Cooperation with Other Organisations"
	case Category_Drilling:
		return "Drilling"
	case Category_Educational_issues:
		return "Educational issues"
	case Category_Emergency_report_and_contingency_planning:
		return "Emergency report and contingency planning"
	case Category_Environmental_Domains_Analysis:
		return "Environmental Domains Analysis"
	case Category_Environmental_Impact_Assessment_EIA_Other_EIA_Matters:
		return "Environmental Impact Assessment EIA Other EIA Matters"
	case Category_Environmental_Monitoring_and_Reporting:
		return "Environmental Monitoring and Reporting"
	case Category_Environmental_Protection_General:
		return "Environmental Protection General"
	case Category_Exchange_of_Information:
		return "Exchange of Information"
	case Category_Fauna_and_Flora_General:
		return "Fauna and Flora_General"
	case Category_Historic_Sites_and_Monuments:
		return "Historic Sites and Monuments"
	case Category_Human_Footprint_and_wilderness_values:
		return "Human Footprint and wilderness values"
	case Category_Inspections:
		return "Inspections"
	case Category_Institutional_and_legal_matters:
		return "Institutional and legal matters"
	case Category_International_Polar_Year:
		return "International Polar Year"
	case Category_Liability:
		return "Liability"
	case Category_Management_Plans:
		return "Management Plans"
	case Category_Marine_Acoustics:
		return "Marine Acoustics"
	case Category_Marine_Protected_Areas:
		return "Marine Protected Areas"
	case Category_Marine_living_resources:
		return "Marine living resources"
	case Category_Mineral_resources:
		return "Mineral resources"
	case Category_Multiyear_strategic_workplan:
		return "Multiyear strategic workplan"
	case Category_Nonnative_Species_and_Quarantine:
		return "Nonnative Species and Quarantine"
	case Category_Opening_statements:
		return "Opening statements"
	case Category_Operation_of_the_Antarctic_Treaty_system_General_:
		return "Operation of the Antarctic Treaty system General"
	case Category_Operation_of_the_Antarctic_Treaty_system_Reports_:
		return "Operation of the Antarctic Treaty system Reports"
	case Category_Operation_of_the_Antarctic_Treaty_system_The_Secretariat:
		return "Operation of the Antarctic Treaty system The Secretariat"
	case Category_Operation_of_the_CEP:
		return "Operation of the CEP"
	case Category_Operational_issues:
		return "Operational issues"
	case Category_Prevention_of_marine_pollution:
		return "Prevention of marine pollution"
	case Category_Repair_and_remediation_of_environmental_damage:
		return "Repair and remediation of environmental damage"
	case Category_Safety_and_Operations_in_Antarctica:
		return "Safety and Operations in Antarctica"
	case Category_Science_issues:
		return "Science issues"
	case Category_Search_and_Rescue:
		return "Search and Rescue"
	case Category_Site_Guidelines_for_Visitors:
		return "Site Guidelines for Visitors"
	case Category_Specially_Protected_Species:
		return "Specially Protected Species"
	case Category_State_of_the_Antarctic_Environment_Report_SAER:
		return "State of the Antarctic Environment Report SAER"
	case Category_Sub_glacial_Lakes:
		return "Sub glacial Lakes"
	case Category_Tourism_and_NG_Activities:
		return "Tourism and NG_Activities"
	case Category_Waste_management_and_disposal:
		return "Waste management and disposal"
	case Category_All:
		return "ALL"
		// panic("should not have 'All' here")
	default:
		panic("internal error")
	}
}
func collectDocuments(timeout time.Duration, client *http.Client, quick bool) ([]PaperRecord, error) {
	records := []PaperRecord{}
	paperTypes := []PaperType{PaperType_IP, PaperType_WP}
	quickMeetings := []Meeting_Integer{Meeting_Integer_ATCM_I_Canberra_1961, Meeting_Integer_ATCM_46_CEP_26_Kochi_2024}
	quickMaxNr := 10

	counts := map[string]int{}
	for i := range paperTypes {
		for j := range quickMeetings {
			counts[PaperTypeToString(paperTypes[i])+Meeting_IntegerToString(quickMeetings[j])] = 0
		}
	}

	// cats := []Category{Category_All}
	cats := CategoryKeys
	cats = append(cats, Category_All) // FIXME

	seen := map[string]struct{}{}

	for _, paperType := range paperTypes {
		for _, meeting := range Meeting_IntegerKeys {
			for _, category := range cats { // CategoryKeys {
				meetingType := MeetingType_ATCM_Antarctic_Treaty_Consultative_Meeting
				party := Party_All

				if quick && !slices.Contains(quickMeetings, meeting) {
					continue
				}

				slog.Debug("loop",
					"paper_type", PaperTypeToString(paperType),
					"meeting", Meeting_IntegerToString(meeting),
					"meeting_type", MeetingTypeToString(meetingType))

				page := 1
				for page > 0 {
					slog.Debug("loop", "page", page)

					if quick && counts[PaperTypeToString(paperType)+Meeting_IntegerToString(meeting)] >= quickMaxNr {
						break
					}

					url := BuildSearchMeetingDocuments(meetingType, meeting, party, paperType, category, page)

					slog.Info("downloading", "url", url)

					responseBody, responseCode, responseTimestamp, err := DownloadWithRetry(url, timeout, client, 10, 2*time.Second)
					if err != nil {
						return nil, fmt.Errorf("bad response: %w, url: %s, response_code: %d, timestamp: %s",
							err, url, responseCode, responseTimestamp)
					}

					document := Document{}
					if err := json.NewDecoder(bytes.NewReader(responseBody)).Decode(&document); err != nil {
						return nil, fmt.Errorf("failed to decode document: %w", err)
					}

					for _, item := range document.Payload {
						if quick && counts[PaperTypeToString(paperType)+Meeting_IntegerToString(meeting)] >= quickMaxNr {
							break
						}

						links := DownloadLinks(item)

						for _, link := range links {
							ok, err := ValidateDocumentLink(client, link.Url)
							if err != nil {
								return nil, fmt.Errorf("failed to validate document link: %w", err)
							}

							_, alreadySeen := seen[link.Url]
							if alreadySeen {
								continue
								// panic("repeat")
							}
							seen[link.Url] = struct{}{}

							meetingNumber, err := strconv.Atoi(item.Meeting_number)
							if err != nil {
								return nil, fmt.Errorf("failed to convert meeting number to integer: %s %w", item.Meeting_number, err)
							}

							paperRecord := PaperRecord{
								MeetingYear:   item.Meeting_year,
								MeetingType:   item.Meeting_type,
								MeetingNumber: meetingNumber,
								MeetingName:   item.Meeting_name,

								Party:       PartyToString(party),
								Category:    CategoryToStringShort(category),
								PageUrl:     url,
								PageNr:      page,
								PayloadJson: string(responseBody),

								PaperType:     PaperTypeToStringShort(paperType),
								PaperId:       item.Paper_id,
								PaperName:     strings.TrimSpace(item.Name),
								PaperNumber:   item.Number,
								PaperRevision: item.Revision,
								PaperLanguage: link.Language.String(),
								PaperUrl:      link.Url,
								PaperExists:   ok,
							}

							paperRecord.Parties = make([]string, 0)
							for _, party := range item.Parties {
								paperRecord.Parties = append(paperRecord.Parties, party.Name)
							}

							paperRecord.Agendas = make([]string, 0)
							for _, agenda := range item.Agendas {
								paperRecord.Agendas = append(paperRecord.Agendas, agenda.Number) // by Number they mean things like "ATCM 13"
							}

							for _, attachment := range item.Attachments {
								attachmentUrl := AttachmentLink(attachment)

								ok, err := ValidateDocumentLink(client, attachmentUrl.Url)
								if err != nil {
									return nil, fmt.Errorf("failed to validate attachment link: %w", err)
								}

								paperRecordWithAttachment := paperRecord
								paperRecordWithAttachment.AttachmentId = attachment.Attachment_id
								paperRecordWithAttachment.AttachmentName = attachment.Name
								paperRecordWithAttachment.AttachmentLanguage = attachmentUrl.Language.String()
								paperRecordWithAttachment.AttachmentUrl = attachmentUrl.Url
								paperRecordWithAttachment.AttachmentExists = ok

								records = append(records, paperRecordWithAttachment)
							}

							if len(item.Attachments) == 0 {
								records = append(records, paperRecord)
							}

							if quick {
								counts[PaperTypeToString(paperType)+Meeting_IntegerToString(meeting)]++
							}
						}
					}

					page = int(document.Pager.Next)
				}
			}
		}
	}

	return records, nil
}

func CollectAllDocuments(cache_ *cache.Cache, client *http.Client, timeout time.Duration, wpsCsv string, utasDir string, quick bool) ([]PaperRecord, error) {

	manualRecords, err := ingestManualDocuments(cache_, wpsCsv, utasDir)
	if err != nil {
		return nil, fmt.Errorf("failed to ingest manual documents: %w", err)
	}

	records, err := collectDocuments(timeout, client, quick)
	if err != nil {
		return nil, fmt.Errorf("failed to collect documents: %w", err)
	}

	records = append(records, manualRecords...)

	return records, nil
}
