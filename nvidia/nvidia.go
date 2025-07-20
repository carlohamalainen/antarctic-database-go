package nvidia

import (
	"bufio"
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
	"strings"
	"time"

	"github.com/carlohamalainen/antarctic-database-go"

	"golang.org/x/time/rate"
)

// NvidiaClient holds the HTTP client and rate limiter for NVIDIA API requests
type NvidiaClient struct {
	Client  *http.Client
	Limiter *rate.Limiter
	ApiKey  string
}

// NewNvidiaClient creates a new client for NVIDIA OCR API with rate limiting
func NewNvidiaClient(rpm int, apiKey string) *NvidiaClient {
	rps := rate.Limit(float64(rpm) / 60.0 * 0.9)
	limiter := rate.NewLimiter(rps, 2) // burst of 2

	return &NvidiaClient{
		Client:  &http.Client{Timeout: 120 * time.Second},
		Limiter: limiter,
		ApiKey:  apiKey,
	}
}

// Do performs an HTTP request with rate limiting
func (c *NvidiaClient) Do(req *http.Request) (*http.Response, error) {
	ctx := context.Background()
	err := c.Limiter.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}

	return c.Client.Do(req)
}

// Message represents a single message in a chat conversation
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// MessagePart represents a part of a message
type MessagePart struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	AssetID string `json:"asset_id,omitempty"`
}

// Payload represents the request payload for the NVIDIA API
type Payload struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	TopP        float64   `json:"top_p"`
	Stream      bool      `json:"stream"`
}

// Choice represents a choice in the streaming response
type Choice struct {
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
}

// StreamResponse represents the streaming response structure
type StreamResponse struct {
	Choices []Choice `json:"choices"`
}

// AssetResponse represents the response from the NVCF assets endpoint
type AssetResponse struct {
	AssetID   string `json:"assetId"`
	UploadURL string `json:"uploadUrl"`
}

// UploadImage uploads an image to NVIDIA's asset endpoint
func UploadImage(client *NvidiaClient, imageData []byte, contentType, description string) (AssetResponse, error) {
	// Step 1: Request an Asset ID and Pre-signed Upload URL
	assetURL := "https://api.nvcf.nvidia.com/v2/nvcf/assets"

	assetPayload := map[string]string{
		"contentType": contentType,
		"description": description,
	}

	assetPayloadBytes, err := json.Marshal(assetPayload)
	if err != nil {
		return AssetResponse{}, fmt.Errorf("failed to marshal asset payload: %w", err)
	}

	req, err := http.NewRequest("POST", assetURL, bytes.NewReader(assetPayloadBytes))
	if err != nil {
		return AssetResponse{}, fmt.Errorf("failed to create asset request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+client.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return AssetResponse{}, fmt.Errorf("asset request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return AssetResponse{}, fmt.Errorf("asset request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var assetResp AssetResponse
	if err := json.NewDecoder(resp.Body).Decode(&assetResp); err != nil {
		return AssetResponse{}, fmt.Errorf("failed to decode asset response: %w", err)
	}

	// Step 2: Upload the Image Using the Pre-signed URL
	uploadReq, err := http.NewRequest("PUT", assetResp.UploadURL, bytes.NewReader(imageData))
	if err != nil {
		return AssetResponse{}, fmt.Errorf("failed to create upload request: %w", err)
	}

	uploadReq.Header.Set("Content-Type", contentType)
	uploadReq.Header["x-amz-meta-nvcf-asset-description"] = []string{description}

	uploadResp, err := client.Do(uploadReq)
	if err != nil {
		return AssetResponse{}, fmt.Errorf("upload request failed: %w", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(uploadResp.Body)
		return AssetResponse{}, fmt.Errorf("upload failed with status %d: %s", uploadResp.StatusCode, string(bodyBytes))
	}

	bodyBytes, _ := io.ReadAll(uploadResp.Body)
	slog.Info("read body of upload", "body", string(bodyBytes))

	return assetResp, nil
}

// EncodeImageToBase64 encodes image data as base64 for embedding in prompts
func EncodeImageToBase64(imageData []byte) string {
	return base64.StdEncoding.EncodeToString(imageData)
}

// RunNvidiaOCR performs OCR on an image using NVIDIA's API
func RunNvidiaOCR(client *NvidiaClient, model string, asset AssetResponse, useAssetUpload bool, imageData []byte) (string, error) {
	invokeURL := "https://integrate.api.nvidia.com/v1/chat/completions"
	stream := true

	headers := map[string]string{
		"Authorization": "Bearer " + client.ApiKey,
	}
	if stream {
		headers["Accept"] = "text/event-stream"
	} else {
		headers["Accept"] = "application/json"
	}

	var prompt string
	if useAssetUpload {
		prompt = `You are a precise OCR system. Your only task is to extract text from this image with exact fidelity.
		Instructions:
		- Extract ALL text from the image with perfect accuracy
		- Maintain exact spacing and line breaks as they appear
		- If you can't read a character with certainty, represent it with [?]
		- If text is arranged in columns, preserve the column structure
		- Preserve any bullets, numbering, or indentation
		- For tables, use plain text formatting with spaces to align columns
		- Do not add ANY explanatory text, headers, or comments
		- Do not describe the image or its content
		- Return ONLY the extracted text
		- This is your input image: <img src="data:image/png;asset_id,` + asset.AssetID + `" />`
	} else {
		// Base64 encode the image and embed directly in the prompt
		base64Image := EncodeImageToBase64(imageData)
		prompt = `You are a precise OCR system. Your only task is to extract text from this image with exact fidelity.
		Instructions:
		- Extract ALL text from the image with perfect accuracy
		- Maintain exact spacing and line breaks as they appear
		- If you can't read a character with certainty, represent it with [?]
		- If text is arranged in columns, preserve the column structure
		- Preserve any bullets, numbering, or indentation
		- For tables, use plain text formatting with spaces to align columns
		- Do not add ANY explanatory text, headers, or comments
		- Do not describe the image or its content
		- Return ONLY the extracted text

		<img src="data:image/png;base64,` + base64Image + `" />`
	}

	payload := Payload{
		Model: model,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens:   1024,
		Temperature: 0.0, // maximum determinism
		TopP:        1.0, // maximum determinism
		Stream:      stream,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON: %w", err)
	}

	req, err := http.NewRequest("POST", invokeURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")

	// Only add asset references header if using asset upload method
	if useAssetUpload {
		req.Header["NVCF-INPUT-ASSET-REFERENCES"] = []string{asset.AssetID}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if stream {
		// For stream mode, collect the full response text
		fullResponse := ""
		scanner := bufio.NewScanner(resp.Body)

		for scanner.Scan() {
			line := scanner.Text()
			// Skip the [DONE] message or empty data
			if line == "data: [DONE]" || !strings.HasPrefix(line, "data: {") {
				continue
			}

			// Parse the JSON object after "data: "
			jsonStr := strings.TrimPrefix(line, "data: ")
			var data StreamResponse
			if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
				continue
			}

			// Extract the content from the delta
			if len(data.Choices) > 0 {
				content := data.Choices[0].Delta.Content
				fullResponse += content
			}
		}

		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("error reading response: %w", err)
		}
		slog.Debug("nvidia full response", "response", fullResponse)

		return fullResponse, nil
	} else {
		body, err := io.ReadAll(resp.Body)
		slog.Debug("nvidia raw response", "response", string(body))

		if err != nil {
			return "", err
		}
		return string(body), nil
	}
}

// RunNvidiaOCRWithBackoff performs OCR with exponential backoff for retries
func RunNvidiaOCRWithBackoff(client *NvidiaClient, model string, asset AssetResponse, useAssetUpload bool, imageData []byte, maxRetries int, initialBackoff time.Duration) (string, error) {
	var (
		result  string
		err     error
		retries int = 0
		backoff     = initialBackoff
	)

	for {
		result, err = RunNvidiaOCR(client, model, asset, useAssetUpload, imageData)

		if err == nil || retries >= maxRetries {
			return result, err
		}

		sleepTime := time.Duration(float64(backoff) * math.Pow(2, float64(retries)))

		jitter := time.Duration(rand.Float64() * float64(sleepTime) * 0.3) // 30% jitter
		sleepTime = sleepTime + jitter

		slog.Error("RunNvidiaOCR failed", "nr_attempts", retries+1, "max_retries", maxRetries, "sleep_seconds", sleepTime.Seconds())

		time.Sleep(sleepTime)
		retries++
	}
}

// ProcessPDFWithOCR processes a PDF with OCR
func ProcessPDFWithOCR(client *NvidiaClient, model string, pdfData []byte, useAssetUpload bool) (string, error) {
	// Convert PDF to PNG
	pngData, err := ats.ConvertPDFToPNG(pdfData, 300)
	if err != nil {
		return "", fmt.Errorf("failed to convert PDF to PNG: %w", err)
	}

	var asset AssetResponse

	// Upload image if using asset upload
	if useAssetUpload {
		asset, err = UploadImage(client, pngData, "image/png", "pdf-ocr")
		if err != nil {
			return "", fmt.Errorf("failed to upload image: %w", err)
		}
	}

	// Run OCR
	text, err := RunNvidiaOCR(client, model, asset, useAssetUpload, pngData)
	if err != nil {
		return "", fmt.Errorf("failed to run OCR: %w", err)
	}

	return text, nil
}
