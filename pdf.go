package ats

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ConvertPDFToPNG converts a PDF page to PNG format
func ConvertPDFToPNG(pdfData []byte, dpi int) ([]byte, error) {
	if dpi <= 0 {
		dpi = 300 // Default to 300 DPI if not specified or invalid
	}

	// Check if pdftoppm is available
	_, err := exec.LookPath("pdftoppm")
	if err != nil {
		return nil, fmt.Errorf("pdftoppm not found, this tool is required: %w", err)
	}

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "pdf_conversion")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Clean up when done

	// Write PDF data to temp file
	tempPDFPath := filepath.Join(tempDir, "input.pdf")
	if err := os.WriteFile(tempPDFPath, pdfData, 0600); err != nil {
		return nil, fmt.Errorf("failed to write temp PDF file: %w", err)
	}

	// Define output file prefix (pdftoppm adds -1.png for the first page)
	outputPrefix := filepath.Join(tempDir, "output")

	// Use pdftoppm to convert PDF to PNG with high resolution
	// -f 1 -l 1: Process only the first page
	// -png: Output PNG format
	// -r: Resolution in DPI
	// -singlefile: Create a single file (instead of one per page)
	cmd := exec.Command(
		"pdftoppm",
		"-f", "1",
		"-l", "1",
		"-png",
		"-r", fmt.Sprintf("%d", dpi),
		"-singlefile",
		tempPDFPath,
		outputPrefix,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("conversion failed: %w, stderr: %s", err, stderr.String())
	}

	// Read the generated PNG (pdftoppm adds "-1" if not using singlefile)
	pngPath := outputPrefix + ".png"
	pngData, err := os.ReadFile(pngPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read output PNG: %w", err)
	}

	return pngData, nil
}
