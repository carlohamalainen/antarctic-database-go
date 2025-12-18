package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// Document represents a row from the 'documents' table
type Document struct {
	ID       string
	URL      string
	Document []byte // BLOB data
}

type MicroserviceResponse struct {
	WroteFile string `json:"wrote_file,omitempty"`
	Error     string `json:"error,omitempty"`
}

func GetUnprocessedDocuments(db *sql.DB, limit int) ([]Document, error) {
	query := `
		SELECT id, url, document
		FROM documents
		WHERE is_scanned = FALSE AND status = 'new'
		ORDER BY id DESC
		LIMIT ?
		`

	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var documents []Document
	for rows.Next() {
		var document Document
		if err := rows.Scan(&document.ID, &document.URL, &document.Document); err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return documents, nil
}

func convertToText(client *http.Client, srcPath string) (string, error) {
	apiURL := "http://localhost:11000/extract"

	if client == nil {
		return "", fmt.Errorf("client is nil")
	}

	reqBody, err := json.Marshal(map[string]string{"file_path": srcPath})
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}

	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("error sending request: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("nil response")
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)

	if err != nil {
		return "", fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var response MicroserviceResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return "", fmt.Errorf("error parsing response: %w", err)
	}

	if response.Error != "" {
		return "", fmt.Errorf("document microservice reported error: %s", response.Error)
	}

	if response.WroteFile == "" {
		return "", fmt.Errorf("document microservice did not write a file and error also empty")
	}

	slog.Info("wrote text file using microservice", "src", srcPath, "dest", response.WroteFile)

	return response.WroteFile, nil
}

func InsertFulltextAndUpdateStatus(db *sql.DB, id string, documentText []byte) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer tx.Rollback() // noop if tx has been committed

	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000-07:00")

	_, err = tx.Exec(`
		INSERT INTO full_text (id, document_text, method, timestamp)
		VALUES (?, ?, ?, ?)`,
		id, documentText, "local-microservice", timestamp)

	if err != nil {
		return fmt.Errorf("failed to insert full-text record for %s: %w", id, err)
	}

	_, err = tx.Exec(`
		UPDATE documents
		SET status = 'fulltext-done'
		WHERE id = ?`,
		id)

	if err != nil {
		return fmt.Errorf("failed to update status for %s: %w", id, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func convertDocument(document Document) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "pdf_conversion")
	if err != nil {
		return []byte{}, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	tempFilePath := filepath.Join(tempDir, path.Ext(document.URL))
	if err := os.WriteFile(tempFilePath, document.Document, 0600); err != nil {
		return []byte{}, fmt.Errorf("failed to write temp PDF file: %w", err)
	}

	txtFileName, err := convertToText(&http.Client{}, tempFilePath)

	if err != nil {
		return []byte{}, fmt.Errorf("call to microservice failed: %w", err)
	}

	documentText, err := os.ReadFile(txtFileName)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to read extracted text file: %w", err)
	}

	return documentText, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	pipelineDbFile := flag.String("pipeline-db-file", "", "Absolute path to sqlite pipeline database")
	batchSize := flag.Int("batch-size", 10, "Number of pages to process in each batch")

	flag.Parse()

	if *pipelineDbFile == "" {
		slog.Error(fmt.Errorf("need -pipeline-db-file").Error())
		os.Exit(1)
	}

	db, err := sql.Open("sqlite3", *pipelineDbFile)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	_, err = db.Exec("PRAGMA journal_mode = WAL")
	if err != nil {
		panic(err)
	}

	for {
		documents, err := GetUnprocessedDocuments(db, *batchSize)
		if err != nil {
			panic(err)
		}

		if len(documents) == 0 {
			break
		}

		for _, document := range documents {
			fmt.Println(document.ID, document.URL)

			documentText, err := convertDocument(document)
			if err != nil {
				panic(err) // FIXME
			}

			if err := InsertFulltextAndUpdateStatus(db, document.ID, documentText); err != nil {
				panic(err) // FIXME
			}

		}
	}

	slog.Info("success")
}
