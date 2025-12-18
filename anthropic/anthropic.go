package ats

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"time"

	"github.com/carlohamalainen/antarctic-database-go"

	"golang.org/x/time/rate"
)

type AnthropicClient struct {
	Client  *http.Client
	Limiter *rate.Limiter
	ApiKey  string
}

func NewAnthropicClient(rpm int, apiKey string) *AnthropicClient {
	rps := rate.Limit(float64(rpm) / 60.0 * 0.9)

	limiter := rate.NewLimiter(rps, 2) // burst of 2

	return &AnthropicClient{
		Client:  &http.Client{Timeout: 60 * time.Second},
		Limiter: limiter,
		ApiKey:  apiKey,
	}
}

func (c *AnthropicClient) Do(req *http.Request) (*http.Response, error) {
	if c == nil {
		return nil, fmt.Errorf("nil AnthropicClient passed to Do")
	}

	ctx := context.Background()
	err := c.Limiter.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}

	if req == nil {
		return nil, fmt.Errorf("nil req")
	}

	return c.Client.Do(req)
}

// anthropicOCR uses Anthropic's Claude API to perform OCR on an image converted from a PDF page
func AnthropicOCR(client *AnthropicClient, model string, pdfBytes []byte) (string, error) {
	if client == nil {
		return "", fmt.Errorf("nil client passed to AnthropicOCR")
	}

	// Function to check if the image size will fit within Anthropic's limits after base64 encoding
	checkSize := func(imgBytes []byte) (bool, float64, float64) {
		rawSize := len(imgBytes)
		// Base64 encoding increases size by ~33%
		estimatedB64Size := rawSize * 4 / 3
		return estimatedB64Size < 5*1024*1024, float64(rawSize) / 1048576, float64(estimatedB64Size) / 1048576
	}

	// Start with high quality 300 DPI conversion
	pngBytes, err := ats.ConvertPDFToPNG(pdfBytes, 300)
	if err != nil {
		return "", fmt.Errorf("failed to convert PDF to PNG: %w", err)
	}

	// Check if 300 DPI image will fit
	fits, rawMB, b64MB := checkSize(pngBytes)
	slog.Info("png size at 300 DPI",
		"bytes", len(pngBytes),
		"mb", rawMB,
		"estimated_base64_mb", b64MB,
		"fits_in_limit", fits)

	// If it doesn't fit, try 150 DPI
	if !fits {
		slog.Info("PNG too large for Anthropic (>5MB after base64), falling back to 150 DPI")
		pngBytes, err = ats.ConvertPDFToPNG(pdfBytes, 150)
		if err != nil {
			return "", fmt.Errorf("failed to convert PDF to PNG at lower resolution: %w", err)
		}

		fits, rawMB, b64MB = checkSize(pngBytes)
		slog.Info("png size at 150 DPI",
			"bytes", len(pngBytes),
			"mb", rawMB,
			"estimated_base64_mb", b64MB,
			"fits_in_limit", fits)

		// If still doesn't fit, try 100 DPI as last resort
		if !fits {
			slog.Info("PNG still too large, reducing to 100 DPI as last resort")
			pngBytes, err = ats.ConvertPDFToPNG(pdfBytes, 100)
			if err != nil {
				return "", fmt.Errorf("failed to convert PDF to PNG at lowest resolution: %w", err)
			}

			fits, rawMB, b64MB = checkSize(pngBytes)
			slog.Info("png size at 100 DPI",
				"bytes", len(pngBytes),
				"mb", rawMB,
				"estimated_base64_mb", b64MB,
				"fits_in_limit", fits)

			// If even 100 DPI is too large, we'll have to warn but try anyway
			if !fits {
				slog.Warn("PNG is still too large even at 100 DPI, API may reject it",
					"bytes", len(pngBytes),
					"estimated_base64_mb", b64MB)
			}
		}
	}

	// Convert PNG to base64
	base64Image := base64.StdEncoding.EncodeToString(pngBytes)

	// Structure for the API request
	type ContentSource struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}

	type Content struct {
		Type   string         `json:"type"`
		Source *ContentSource `json:"source,omitempty"`
		Text   string         `json:"text,omitempty"`
	}

	type Message struct {
		Role    string    `json:"role"`
		Content []Content `json:"content"`
	}

	type RequestBody struct {
		Model     string    `json:"model"`
		Messages  []Message `json:"messages"`
		MaxTokens int       `json:"max_tokens"`
	}

	// Prepare API request
	requestBody := RequestBody{
		Model: model,
		Messages: []Message{
			{
				Role: "user",
				Content: []Content{
					{
						Type: "image",
						Source: &ContentSource{
							Type:      "base64",
							MediaType: "image/png",
							Data:      base64Image,
						},
					},
					{
						Type: "text",
						Text: "Extract all text from this image (which is a PDF page). Return only the extracted text, with no additional comments or explanations. Preserve the exact formatting, paragraph structure, and layout as much as possible.",
					},
				},
			},
		},
		MaxTokens: 4096,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", client.ApiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error making request: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("nil response")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API returned non-200 status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Read the entire response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}

	// Log the raw response for debugging
	slog.Info("anthropic raw response", "response", string(bodyBytes))

	// First, try to unmarshal as a generic map to inspect the structure
	var rawResponse map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawResponse); err != nil {
		slog.Error("failed to parse response as map", "error", err)
	} else {
		// Print the keys at the top level to help debug
		keys := make([]string, 0, len(rawResponse))
		for k := range rawResponse {
			keys = append(keys, k)
		}
		slog.Info("response top-level keys", "keys", keys)
	}

	// Parse response according to the structure provided in the example
	type ResponseContent struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}

	type AnthropicResponse struct {
		ID         string            `json:"id"`
		Type       string            `json:"type"`
		Role       string            `json:"role"`
		Model      string            `json:"model"`
		Content    []ResponseContent `json:"content"`
		StopReason string            `json:"stop_reason"`
	}

	var result AnthropicResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		slog.Error("response parsing error", "error", err, "body", string(bodyBytes))
		return "", fmt.Errorf("error decoding response: %w", err)
	}

	slog.Info("parsed response",
		"id", result.ID,
		"type", result.Type,
		"role", result.Role,
		"model", result.Model,
		"content_length", len(result.Content),
		"stop_reason", result.StopReason)

	// Extract text from response
	var extractedText string
	for i, content := range result.Content {
		slog.Info("content item", "index", i, "type", content.Type, "text_length", len(content.Text))
		if content.Type == "text" {
			extractedText += content.Text
		}
	}

	return extractedText, nil
}

// AnthropicOCRWithBackoff provides retry logic with exponential backoff for anthropicOCR
func AnthropicOCRWithBackoff(client *AnthropicClient, model string, pdfBytes []byte, maxRetries int, initialBackoff time.Duration) (string, error) {
	var (
		result  string
		err     error
		retries int = 0
		backoff     = initialBackoff
	)

	for {
		result, err = AnthropicOCR(client, model, pdfBytes)

		if err == nil || retries >= maxRetries {
			return result, err
		}

		// sleepTime := time.Duration(float64(backoff) * float64(1<<uint(retries)))
		sleepTime := time.Duration(float64(backoff) * math.Pow(2, float64(retries)))

		jitter := time.Duration(float64(sleepTime) * 0.3 * (rand.Float64())) // 30% jitter
		sleepTime = sleepTime + jitter

		slog.Error("anthropicOCR failed", "nr_attempts", retries+1, "max_retries", maxRetries, "sleep_seconds", sleepTime.Seconds())

		time.Sleep(sleepTime)
		retries++
	}
}
