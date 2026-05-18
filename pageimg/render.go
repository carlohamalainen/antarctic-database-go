// Package pageimg renders a per-page PDF blob to an annotated PNG, with an
// unfilled translucent yellow circle drawn at a given (x, y) fraction to
// mark the region the reviewer is being asked to transcribe.
//
// Rendering shells out to `pdftoppm` (poppler).
package pageimg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
)

type Options struct {
	DPI            int // default 150
	CircleRadiusPx int // default DPI/8 — radius of the unfilled yellow marker circle (sits within one line of 12pt body text)
	CircleStrokePx int // default 3 — thickness of the circle outline
	SnippetHalfPx  int // default 300 — pixels above and below the centre included in the snippet crop (full page width)
}

func (o *Options) defaults() {
	if o.DPI <= 0 {
		o.DPI = 150
	}
	if o.CircleRadiusPx <= 0 {
		// Scale with DPI so the visual size of the marker stays consistent
		// regardless of render resolution. At 150 DPI this gives radius ~19 px
		// (diameter ~38 px), small enough to sit within a single line of 12pt
		// body text without bleeding into neighbours.
		o.CircleRadiusPx = o.DPI / 8
	}
	if o.CircleStrokePx <= 0 {
		o.CircleStrokePx = 3
	}
	if o.SnippetHalfPx <= 0 {
		o.SnippetHalfPx = 300
	}
}

// Render runs pdftoppm on pdfBytes, draws an unfilled yellow circle at
// (xFraction × width, yFraction × height), and returns the full-page PNG
// plus a snippet PNG cropped to a horizontal band centred vertically on
// the same point. The snippet keeps the full page width so the reviewer
// can read text continuing rightward of the marker.
// xFraction and yFraction must be in [0, 1).
func Render(ctx context.Context, pdfBytes []byte, xFraction, yFraction float64, opts Options) (full, snippet []byte, err error) {
	if len(pdfBytes) == 0 {
		return nil, nil, errors.New("empty pdf input")
	}
	if xFraction < 0 || xFraction >= 1 {
		return nil, nil, fmt.Errorf("x_fraction out of range [0, 1): %f", xFraction)
	}
	if yFraction < 0 || yFraction >= 1 {
		return nil, nil, fmt.Errorf("y_fraction out of range [0, 1): %f", yFraction)
	}
	opts.defaults()

	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, nil, fmt.Errorf("pdftoppm not found in PATH (install poppler): %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "pageimg-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "page.pdf")
	if err := os.WriteFile(pdfPath, pdfBytes, 0o600); err != nil {
		return nil, nil, err
	}

	prefix := filepath.Join(tmpDir, "page")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-singlefile", "-png", "-r", fmt.Sprint(opts.DPI), pdfPath, prefix)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return nil, nil, fmt.Errorf("pdftoppm failed: %w (output: %s)", err, string(combined))
	}

	pngPath := prefix + ".png"
	src, err := loadPNG(pngPath)
	if err != nil {
		return nil, nil, fmt.Errorf("decode rendered png: %w", err)
	}
	annotated := overlayCircle(src, xFraction, yFraction, opts)

	full, err = encodePNG(annotated)
	if err != nil {
		return nil, nil, fmt.Errorf("encode full png: %w", err)
	}
	snippetImg := cropSnippet(annotated, yFraction, opts)
	snippet, err = encodePNG(snippetImg)
	if err != nil {
		return nil, nil, fmt.Errorf("encode snippet png: %w", err)
	}
	return full, snippet, nil
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// cropSnippet returns a horizontal band of the annotated image at full
// page width, centred vertically on the marker point. The reviewer
// transcribes some words starting at the marker and reading rightward, so
// preserving full width matters; the vertical band bounds the surrounding
// context.
func cropSnippet(annotated *image.RGBA, yFraction float64, opts Options) image.Image {
	b := annotated.Bounds()
	cy := b.Min.Y + int(float64(b.Dy())*yFraction)
	top := max(b.Min.Y, cy-opts.SnippetHalfPx)
	bot := min(b.Max.Y, cy+opts.SnippetHalfPx)
	return annotated.SubImage(image.Rect(b.Min.X, top, b.Max.X, bot))
}

// overlayCircle draws an unfilled translucent yellow ring of the given
// radius and stroke thickness, centred at (xFraction × width, yFraction × height).
// "Unfilled" = only the ring is coloured; the inside is left untouched so
// the reviewer can read the underlying text.
func overlayCircle(src image.Image, xFraction, yFraction float64, opts Options) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, src, b.Min, draw.Src)

	cx := b.Min.X + int(float64(b.Dx())*xFraction)
	cy := b.Min.Y + int(float64(b.Dy())*yFraction)
	rOuter := opts.CircleRadiusPx
	rInner := max(0, opts.CircleRadiusPx-opts.CircleStrokePx)
	rOuterSq := rOuter * rOuter
	rInnerSq := rInner * rInner

	yellow := color.NRGBA{R: 230, G: 200, B: 0, A: 220}

	// Build an alpha mask of the annular ring, then composite a yellow
	// uniform through it onto the destination — image/draw handles alpha
	// blending correctly via draw.Over.
	x0 := max(b.Min.X, cx-rOuter)
	x1 := min(b.Max.X, cx+rOuter+1)
	y0 := max(b.Min.Y, cy-rOuter)
	y1 := min(b.Max.Y, cy+rOuter+1)
	maskRect := image.Rect(x0, y0, x1, y1)
	mask := image.NewAlpha(maskRect)
	for y := y0; y < y1; y++ {
		dy := y - cy
		dy2 := dy * dy
		for x := x0; x < x1; x++ {
			dx := x - cx
			d2 := dx*dx + dy2
			if d2 <= rOuterSq && d2 >= rInnerSq {
				mask.SetAlpha(x, y, color.Alpha{A: 255})
			}
		}
	}
	draw.DrawMask(out, maskRect, &image.Uniform{C: yellow}, image.Point{}, mask, maskRect.Min, draw.Over)

	return out
}
