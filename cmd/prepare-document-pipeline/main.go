package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/carlohamalainen/antarctic-database-go"
	"github.com/carlohamalainen/antarctic-database-go/cache"
)

type Config struct {
	Timeout           time.Duration
	HttpCache         string
	NewPipelineDbFile string
	DocumentSummary   string
	Quick             bool
	UtasDir           string
	WpsCsv            string
}

func parseArgs() (Config, error) {
	timeoutArg := flag.Duration("timeout", 60*time.Second, "Timeout (s)")
	httpCacheArg := flag.String("http-cache", "", "Absolute path to sqlite http cache")
	newPipelineDbFileArg := flag.String("new-pipeline-db-file", "", "Absolute path to sqlite pipeline file")
	documentSummaryArg := flag.String("document-summary", "", "Parquet summary file")
	quick := flag.Bool("quick", false, "Quick run on subset of data")
	utasDirArg := flag.String("utas-raw-pdfs", "", "Absolute path to directory of manually saved UTAS PDF documents")
	wpsCsvArg := flag.String("wps-csv", "", "Absolute path to wps csv raw file")

	flag.Parse()

	config := Config{
		Timeout:           *timeoutArg,
		HttpCache:         *httpCacheArg,
		NewPipelineDbFile: *newPipelineDbFileArg,
		DocumentSummary:   *documentSummaryArg,
		Quick:             *quick,
		UtasDir:           *utasDirArg,
		WpsCsv:            *wpsCsvArg,
	}

	if config.HttpCache == "" {
		return config, fmt.Errorf("need -http-cache")
	}

	if config.NewPipelineDbFile == "" {
		return config, fmt.Errorf("need -new-pipeline-db-file")
	}

	if config.DocumentSummary == "" {
		return config, fmt.Errorf("need -document-summary")
	}

	if config.UtasDir == "" {
		return config, fmt.Errorf("need -utas-raw-pdfs")
	}

	if config.WpsCsv == "" {
		return config, fmt.Errorf("need -wps-csv")
	}

	return config, nil
}

func setupLogger() *slog.Logger {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}))
	slog.SetDefault(logger)
	return logger
}

func processPipeline(config Config) error {
	client, err := cache.NewHTTPClient(&config.HttpCache)
	if err != nil {
		return fmt.Errorf("failed to setup HTTP client: %w", err)
	}
	cache_ := client.Transport.(*cache.Cache)

	defer func() {
		err := client.Transport.(*cache.Cache).SqliteCache.Close()
		if err != nil {
			slog.Error("failed to close sqlite", "error", err.Error())
		}
	}()

	records, err := ats.CollectAllDocuments(cache_, client, config.Timeout, config.WpsCsv, config.UtasDir, config.Quick)
	if err != nil {
		return fmt.Errorf("document collection failed: %w", err)
	}

	slog.Debug("writing parquet summary")
	if err := ats.WriteRecords(config.DocumentSummary, records); err != nil {
		return fmt.Errorf("failed to write records: %w", err)
	}

	slog.Debug("downloading documents")

	db, err := ats.SetupDatabase(config.NewPipelineDbFile)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := ats.DownloadDocuments(db, config.Timeout, client, records); err != nil {
		return err
	}

	err = ats.SplitScannedPDFs(db)
	if err != nil {
		return err
	}

	_, err = db.Exec("PRAGMA wal_checkpoint(FULL)")
	if err != nil {
		return err
	}

	return nil
}

func main() {
	setupLogger()

	config, err := parseArgs()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	slog.Info("starting")

	if err := processPipeline(config); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	slog.Info("completed successfully")
}
