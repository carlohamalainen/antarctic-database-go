package ats

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
)

// PDFAnalysisResponse is a wrapper that can handle both success and error responses
type PDFAnalysisResponse struct {
	// Standard fields from successful response
	Filename   string     `json:"filename,omitempty"`
	TotalPages int        `json:"total_pages,omitempty"`
	Pages      []PageInfo `json:"pages,omitempty"`

	// Error fields
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// IsError returns true if this response contains an error
func (r *PDFAnalysisResponse) IsError() bool {
	return r.Error != "" || r.Message != ""
}

// GetAnalysis returns the analysis data if no error, or an error if there is one
func (r *PDFAnalysisResponse) GetAnalysis() (PDFAnalysis, error) {
	if r.IsError() {
		msg := r.Error
		if r.Message != "" {
			msg += ": " + r.Message
		}
		return PDFAnalysis{}, errors.New(msg)
	}

	return PDFAnalysis{
		Filename:   r.Filename,
		TotalPages: r.TotalPages,
		Pages:      r.Pages,
	}, nil
}

// PDFAnalysis represents the top-level structure of the PDF analysis JSON
type PDFAnalysis struct {
	Filename   string     `json:"filename"`
	TotalPages int        `json:"total_pages"`
	Pages      []PageInfo `json:"pages"`
}

// PageInfo contains analysis information for a single page
type PageInfo struct {
	PageNumber int       `json:"page_number"`
	Text       TextData  `json:"text"`
	Images     ImageData `json:"images"`
}

// TextData contains text-related analysis for a page
type TextData struct {
	CommandsCount int `json:"commands_count"`
	TextBlocks    int `json:"text_blocks"`
	FontsUsed     int `json:"fonts_used"`
}

// ImageData contains image-related analysis for a page
type ImageData struct {
	Count   int           `json:"count"`
	Details []ImageDetail `json:"details"`
}

// ImageDetail contains details about a single image
type ImageDetail struct {
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	Colorspace    string  `json:"colorspace"`
	SizeBytes     int     `json:"size_bytes"`
	IsFullPage    bool    `json:"is_full_page"`
	CoverageRatio float64 `json:"coverage_ratio"`
	DPIEstimate   int     `json:"dpi_estimate"`
}

func analyzePDF(client *http.Client, apiURL, pdfPath string) (*PDFAnalysis, error) {
	reqBody, err := json.Marshal(map[string]string{
		"file_path": pdfPath,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var response PDFAnalysisResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return nil, fmt.Errorf("error parsing response: %w", err)
	}

	analysis, err := response.GetAnalysis()
	if err != nil {
		return nil, fmt.Errorf("API returned error: %w", err)
	}

	return &analysis, nil
}

func IsScannedPDF(data []byte) (bool, error) {
	dir, err := os.MkdirTemp("", "pdf-analysis")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)

	f := path.Join(dir, "tmp.pdf")

	if err := os.WriteFile(f, data, 0600); err != nil {
		return false, err
	}

	client := &http.Client{} // Don't use the caching client for the microservice!

	result, err := analyzePDF(client, "http://localhost:11000/analyze", f)
	if err != nil {
		return false, err
	}

	for _, page := range result.Pages {
		for _, imageData := range page.Images.Details {
			if imageData.IsFullPage {
				return true, nil
				// if (page.Text.TextBlocks > 0 || page.Text.CommandsCount > 0 || page.Text.FontsUsed > 0) {
				// 	return false, fmt.Errorf("error classifying PDF as text vs scanned: %+v", page)
				// }
			}
		}
	}

	return false, nil
}
