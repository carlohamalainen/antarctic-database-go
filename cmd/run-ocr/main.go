package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"database/sql"

	"github.com/carlohamalainen/antarctic-database-go"
	anthropic "github.com/carlohamalainen/antarctic-database-go/anthropic"
	nvidia "github.com/carlohamalainen/antarctic-database-go/nvidia"

	_ "github.com/mattn/go-sqlite3"
)

const nvidiaRequestsPerMinute = 40
const anthropicRequestsPerMinute = 40 // FIXME no idea what this is on my plan

// Page represents a row from the pages table
type Page struct {
	ID      string
	URL     string
	PageNr  int
	PagePDF []byte // BLOB data
	Status  string
}

func GetExtractedPages(db *sql.DB, limit int) ([]Page, error) {
	query := `
		SELECT id, url, page_nr, page_pdf, status
		FROM pages
		WHERE status = 'extracted'
		ORDER BY id DESC
		LIMIT ?
		`

	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var page Page
		if err := rows.Scan(&page.ID, &page.URL, &page.PageNr, &page.PagePDF, &page.Status); err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pages, nil
}

func InsertOCRAndUpdateStatus(db *sql.DB, id string, pageNr int, pageText string, method string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer tx.Rollback() // noop if tx has been committed

	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000-07:00")

	_, err = tx.Exec(`
		INSERT INTO ocr (id, page_nr, page_text, method, timestamp)
		VALUES (?, ?, ?, ?, ?)`,
		id, pageNr, pageText, method, timestamp)

	if err != nil {
		return fmt.Errorf("failed to insert OCR record for %s page %d: %w", id, pageNr, err)
	}

	_, err = tx.Exec(`
		UPDATE pages
		SET status = 'ocr-done'
		WHERE id = ? AND page_nr = ?`,
		id, pageNr)

	if err != nil {
		return fmt.Errorf("failed to update status for %s page %d: %w", id, pageNr, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func workerNvidia(wg *sync.WaitGroup, db *sql.DB, client *nvidia.NvidiaClient, model string, i int, page Page, useAssetUpload bool) {
	defer wg.Done()

	startTime := time.Now()

	logger := slog.With("index", i, "page_id", page.ID, "page_url", page.URL, "page_nr", page.PageNr, "page_status", page.Status, "model", model, "service", "nvidia", "use_asset_upload", useAssetUpload)

	var t0 time.Time

	t0 = time.Now()
	png, err := ats.ConvertPDFToPNG(page.PagePDF, 300)
	if err != nil {
		panic(err)
	}
	durationToPNG := time.Since(t0).Milliseconds()
	logger.Info("step complete", "duration_ms", durationToPNG, "stage", "to_png")

	var asset nvidia.AssetResponse
	var durationUpload int64 = 0

	if useAssetUpload {
		t0 = time.Now()
		asset, err = nvidia.UploadImage(client, png, "image/png", page.ID)
		if err != nil {
			panic(err)
		}
		durationUpload = time.Since(t0).Milliseconds()
		logger.Info("step complete", "duration_ms", durationUpload, "stage", "upload")
	} else {
		// No upload needed when using base64
		logger.Info("skipping upload step (using base64)", "stage", "upload")
	}

	t0 = time.Now()
	// text, err := nvidia.RunNvidiaOCR(client, model, asset, useAssetUpload, png)
	text, err := nvidia.RunNvidiaOCRWithBackoff(client, model, asset, useAssetUpload, png, 20, 2*time.Second)
	if err != nil {
		panic(err)
	}
	durationLLM := time.Since(t0).Milliseconds()
	logger.Info("step complete", "duration_ms", durationLLM, "stage", "llm")

	t0 = time.Now()
	methodSuffix := "/assetUpload"
	if !useAssetUpload {
		methodSuffix = "/base64"
	}
	if err := InsertOCRAndUpdateStatus(db, page.ID, page.PageNr, text, "nvidia/"+model+methodSuffix); err != nil {
		panic(err)
	}
	durationUpdateOCRTable := time.Since(t0).Milliseconds()
	logger.Info("step complete", "duration_ms", durationUpdateOCRTable, "stage", "update_ocr_table")

	durationOverall := time.Since(startTime).Seconds()
	logger.Info("step complete", "duration_ms", durationOverall, "stage", "overall")
}

func workerAnthropic(wg *sync.WaitGroup, db *sql.DB, client *anthropic.AnthropicClient, model string, i int, page Page) {
	defer wg.Done()

	startTime := time.Now()

	logger := slog.With("index", i, "page_id", page.ID, "page_url", page.URL, "page_nr", page.PageNr, "page_status", page.Status, "model", model, "service", "anthropic")

	// The PDF to PNG conversion is now handled inside the anthropicOCR function
	t0 := time.Now()
	text, err := anthropic.AnthropicOCR(client, model, page.PagePDF)
	// Alternatively with retries:
	// text, err := anthropicOCRWithBackoff(client, model, page.PagePDF, 3, 2*time.Second)
	if err != nil {
		panic(err)
	}
	durationLLM := time.Since(t0).Milliseconds()
	logger.Info("step complete", "duration_ms", durationLLM, "stage", "llm_with_conversion")

	t0 = time.Now()
	if err := InsertOCRAndUpdateStatus(db, page.ID, page.PageNr, text, "anthropic/"+model); err != nil {
		panic(err)
	}
	durationUpdateOCRTable := time.Since(t0).Milliseconds()
	logger.Info("step complete", "duration_ms", durationUpdateOCRTable, "stage", "update_ocr_table")

	durationOverall := time.Since(startTime).Seconds()
	logger.Info("step complete", "duration_ms", durationOverall, "stage", "overall")
}

func main() {
	// NVIDIA models
	nvidiaModel := "meta/llama-4-maverick-17b-128e-instruct"
	// nvidiaModel := "meta/llama-4-scout-17b-16e-instruct"
	// nvidiaModel := "google/gemma-3-27b-it"

	// Anthropic models
	anthropicModel := "claude-3-7-sonnet-20250219"

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	pipelineDbFile := flag.String("pipeline-db-file", "", "Absolute path to sqlite pipeline database")
	ocrService := flag.String("service", "nvidia", "OCR service to use: 'nvidia' or 'anthropic'")
	batchSize := flag.Int("batch-size", 10, "Number of pages to process in each batch")
	useAssetUpload := flag.Bool("use-asset-upload", false, "If true, upload images to NVCF assets API. If false, use base64 encoding")

	flag.Parse()

	if *pipelineDbFile == "" {
		slog.Error(fmt.Errorf("need -pipeline-db-file").Error())
		os.Exit(1)
	}

	// Validate the service selection
	if *ocrService != "nvidia" && *ocrService != "anthropic" {
		slog.Error(fmt.Errorf("invalid service: %s, must be 'nvidia' or 'anthropic'", *ocrService).Error())
		os.Exit(1)
	}

	// Log encoding method
	if *ocrService == "nvidia" {
		if *useAssetUpload {
			slog.Info("Using NVCF asset upload for image encoding")
		} else {
			slog.Info("Using base64 encoding for images (direct embedding)")
		}
	}

	var nvidiaClient *nvidia.NvidiaClient
	var anthropicClient *anthropic.AnthropicClient

	if *ocrService == "nvidia" {
		nvidiaApiKey := os.Getenv("NVIDIA_API_KEY")
		if nvidiaApiKey == "" {
			panic(fmt.Errorf("NVIDIA_API_KEY environment variable not set"))
		}
		nvidiaClient = nvidia.NewNvidiaClient(nvidiaRequestsPerMinute, nvidiaApiKey)
	} else {
		anthropicApiKey := os.Getenv("ANTHROPIC_API_KEY")
		if anthropicApiKey == "" {
			panic(fmt.Errorf("ANTHROPIC_API_KEY environment variable not set"))
		}
		anthropicClient = anthropic.NewAnthropicClient(anthropicRequestsPerMinute, anthropicApiKey)
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
		pages, err := GetExtractedPages(db, *batchSize)
		if err != nil {
			panic(err)
		}

		if len(pages) == 0 {
			break
		}

		var wg sync.WaitGroup

		for i, page := range pages {
			wg.Add(1)

			if *ocrService == "nvidia" {
				go workerNvidia(&wg, db, nvidiaClient, nvidiaModel, i, page, *useAssetUpload)
			} else {
				go workerAnthropic(&wg, db, anthropicClient, anthropicModel, i, page)
			}
		}

		slog.Info("waiting for workers to finish")
		wg.Wait()
	}

	slog.Info("success")
}
