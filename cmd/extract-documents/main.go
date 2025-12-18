package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/carlohamalainen/antarctic-database-go"
	"github.com/carlohamalainen/antarctic-database-go/cache"
)

var timeout time.Duration

var client *http.Client

func main() {
	var err error

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	timeoutArg := flag.Duration("timeout", 60*time.Second, "Timeout (s)")
	dbFileArg := flag.String("http-cache", "", "Absolute path to sqlite file cache")
	pipelineDbFileArg := flag.String("pipeline-db-file", "", "Pipeline pipeline sqlite file")
	outputDir := flag.String("output-dir", "", "Absolute path to output directory")
	outputParquetFile := flag.String("output-parquet-file", "", "Absolute path to output parquet file")
	utasDirArg := flag.String("utas-raw-pdfs", "", "Absolute path to directory of manually saved UTAS PDF documents")
	wpsCsvArg := flag.String("wps-csv", "", "Absolute path to wps csv raw file")

	flag.Parse()

	timeout = *timeoutArg

	if *dbFileArg == "" {
		slog.Error(fmt.Errorf("http-cache").Error())
		os.Exit(1)
	}

	if *pipelineDbFileArg == "" {
		slog.Error(fmt.Errorf("need -pipeline-db-file").Error())
		os.Exit(1)
	}

	if *outputDir == "" {
		slog.Error(fmt.Errorf("need -output-dir").Error())
		os.Exit(1)
	}

	if *outputParquetFile == "" {
		slog.Error(fmt.Errorf("need -output-parquet-file").Error())
		os.Exit(1)
	}

	if *utasDirArg == "" {
		slog.Error(fmt.Errorf("need -utas-raw-pdfs").Error())
		os.Exit(1)
	}

	if *wpsCsvArg == "" {
		slog.Error(fmt.Errorf("need -wps-csv").Error())
		os.Exit(1)
	}

	slog.Info("starting")

	client, err = cache.NewHTTPClient(dbFileArg)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	if client == nil {
		slog.Error("nil client")
		os.Exit(1)
	}
	cache_ := client.Transport.(*cache.Cache)
	defer func() {
		err := client.Transport.(*cache.Cache).SqliteCache.Close()
		if err != nil {
			logger.Error("failed to close sqlite", "error", err.Error())
		}
	}()

	slog.Info("sanity checks")
	err = ats.Sanity(*pipelineDbFileArg)
	if err != nil {
		panic(err)
	}

	records, err := ats.CollectAllDocuments(cache_, client, timeout, *wpsCsvArg, *utasDirArg, false)
	if err != nil {
		panic(err)
	}

	var nrDocuments, nrOCR, nrFullText int

	if err := ats.WriteRecords(*outputParquetFile, records); err != nil {
		panic(err)
	}

	// Save all the PDF, doc, docx files.
	nrDocuments, err = ats.SaveDocuments(timeout, client, records, *outputDir)
	if err != nil {
		panic(err)
	}

	// Save OCR outputs.
	nrOCR, err = ats.SaveOcrTextFiles(*pipelineDbFileArg, *outputDir)
	if err != nil {
		panic(err)
	}

	// Save "fulltext" outputs from modern documents.
	nrFullText, err = ats.SaveFullTextFiles(*pipelineDbFileArg, *outputDir)
	if err != nil {
		panic(err)
	}

	if nrDocuments != nrOCR+nrFullText {
		panic(fmt.Errorf("mismatch: %d != %d + %d", nrDocuments, nrOCR, nrFullText))
	}

	slog.Info("saved documents", "nr_documents", nrDocuments,
		"nr_ocr", nrOCR,
		"nr_fulltext", nrFullText)

}
