package ats

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/PuerkitoBio/goquery"
)

// URL is a simple wrapper for url strings.
type URL string

// UtasDocument represents a record from the wps_missing.csv file
type UtasDocument struct {
	MeetingYear     int
	MeetingType     string
	MeetingNumber   int
	MeetingName     string
	PaperType       string
	PaperNumber     int
	PaperRevision   int
	PaperName       string
	DetailedAgendas string
	Agendas         string
	Parties         string
	URL             string
	IsTrueGap       bool
	Notes           string
}

// Helper to get the raw HTML of a selection
func GetRawHTML(s *goquery.Selection) string {
	if len(s.Nodes) == 0 {
		return ""
	}

	var buf bytes.Buffer

	err := html.Render(&buf, s.Get(0))
	if err != nil {
		panic("internal error when rendering html: " + err.Error())
	}

	return buf.String()
}

func Download(url URL, timeout time.Duration, client *http.Client) ([]byte, int, time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Second) // FIXME should take a context
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, string(url), nil)
	if err != nil {
		slog.Error(err.Error())
		return []byte{}, 0, time.Time{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error(err.Error())
		return []byte{}, 0, time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status code: got %v", resp.StatusCode)
		slog.Error(err.Error())
		return []byte{}, 0, time.Time{}, err
	}

	doc, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error(err.Error())
		return []byte{}, 0, time.Time{}, err
	}

	t := time.Now()

	cacheTimestamp := resp.Header.Get("X-Cache-Timestamp")

	if cacheTimestamp != "" {
		cacheTimestampParsed, err := time.Parse(time.RFC3339, cacheTimestamp)
		if err != nil {
			return []byte{}, 0, time.Time{}, err
		}

		t = cacheTimestampParsed
	}

	return doc, resp.StatusCode, t, nil
}

func DownloadWithRetry(url string, timeout time.Duration, client *http.Client, maxRetries int, baseDelay time.Duration) (responseBody []byte, responseCode int, responseTimestamp time.Time, err error) {
	delay := baseDelay

	for attempt := 1; attempt <= maxRetries; attempt++ {
		responseBody, responseCode, responseTimestamp, err = Download(URL(url), timeout, client)
		if err == nil {
			return responseBody, responseCode, responseTimestamp, nil
		}

		slog.Info("Attempt failed", "attempt_nr", attempt, "max_tries", maxRetries, "error", err, "delay", delay)

		if attempt >= maxRetries {
			break
		}

		time.Sleep(delay)
		delay *= 2 // Exponential backoff
	}

	return responseBody, responseCode, responseTimestamp, fmt.Errorf("download failed after %d attempts: %w", maxRetries, err)
}

// ReadMissingWPsCsv reads the data/raw/wps_missing.csv file and returns it as a slice of UtasDocument
func ReadMissingWPsCsv(filePath string) ([]UtasDocument, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	_, err = reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read records: %w", err)
	}

	documents := make([]UtasDocument, 0, len(records))

	for _, record := range records {
		if len(record) < 14 {
			return nil, fmt.Errorf("record with insufficient fields: %+v", record)
		}

		meetingYear, err := strconv.Atoi(record[0])
		if err != nil {
			return nil, fmt.Errorf("invalid meeting year: %s, caused error: %w", record[0], err)
		}

		meetingNumber, err := strconv.Atoi(record[2])
		if err != nil {
			return nil, fmt.Errorf("invalid meeting number: %s, caused error: %w", record[2], err)
		}

		paperNumber, err := strconv.Atoi(record[5])
		if err != nil {
			return nil, fmt.Errorf("invalid paper number: %s, caused error: %w", record[5], err)
		}

		paperRevision, err := strconv.Atoi(record[6])
		if err != nil {
			// Paper revision might be empty, which is fine
			paperRevision = 0
		}

		isTrueGap := strings.ToUpper(record[12]) == "TRUE"

		document := UtasDocument{
			MeetingYear:     meetingYear,
			MeetingType:     record[1],
			MeetingNumber:   meetingNumber,
			MeetingName:     record[3],
			PaperType:       record[4],
			PaperNumber:     paperNumber,
			PaperRevision:   paperRevision,
			PaperName:       record[7],
			DetailedAgendas: record[8],
			Agendas:         record[9],
			Parties:         record[10],
			URL:             strings.TrimSpace(record[11]),
			IsTrueGap:       isTrueGap,
			Notes:           record[13],
		}

		documents = append(documents, document)
	}

	return documents, nil
}
