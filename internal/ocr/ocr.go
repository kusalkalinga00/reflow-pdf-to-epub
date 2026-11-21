// Package ocr reads text from page images.
//
// It wraps Tesseract as a subprocess rather than linking it: a malformed page
// can then crash OCR without taking the conversion worker down, and the binary
// stays CGO-free apart from PDFium.
package ocr

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

// ErrUnavailable means no OCR engine was found on this machine.
var ErrUnavailable = errors.New("no OCR engine available")

// Line is one recognized line of text.
type Line struct {
	Text string
	// Box is in image pixels with the origin at the top left.
	Left, Top, Right, Bottom float64
	// Confidence is 0..1, averaged over the words in the line.
	Confidence float64
}

// Recognizer turns an image into lines. The image is PNG-encoded.
type Recognizer interface {
	Recognize(ctx context.Context, png []byte) ([]Line, error)
}

// Tesseract runs the tesseract binary.
type Tesseract struct {
	// Binary is the executable path. Empty means "tesseract" on PATH.
	Binary string
	// Language is a tesseract language code. Empty means "eng".
	Language string
}

// Available reports whether a tesseract binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("tesseract")
	return err == nil
}

// NewTesseract returns a recognizer, or ErrUnavailable when the binary is
// missing.
func NewTesseract() (*Tesseract, error) {
	path, err := exec.LookPath("tesseract")
	if err != nil {
		return nil, fmt.Errorf("%w: install tesseract to convert scanned PDFs", ErrUnavailable)
	}
	return &Tesseract{Binary: path}, nil
}

// Recognize reads the PNG from memory through tesseract's stdin and parses the
// TSV it writes to stdout.
func (t *Tesseract) Recognize(ctx context.Context, png []byte) ([]Line, error) {
	binary := t.Binary
	if binary == "" {
		binary = "tesseract"
	}
	language := t.Language
	if language == "" {
		language = "eng"
	}

	// #nosec G204 -- the binary comes from configuration and the page arrives
	// on stdin, so no path or text reaches the argument vector.
	cmd := exec.CommandContext(ctx, binary, "stdin", "stdout", "-l", language, "--psm", "3", "tsv")
	cmd.Stdin = bytes.NewReader(png)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("tesseract: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("tesseract: %w", err)
	}

	return parseTSV(out)
}

// minRecognisedCharacters is the amount of alphanumeric text a page must yield
// before its OCR output is kept.
//
// OCR exists to recover a scanned page's text. Run over artwork — a cover, a
// photograph, a decorative flourish — it returns a handful of stray letters and
// symbols, and putting those in the book is worse than an honest blank page.
// A page of prose produces hundreds of characters; a cover produced six.
const minRecognisedCharacters = 24

// LooksLikeText reports whether recognised lines carry enough text to be a
// text page rather than an image with incidental shapes in it.
func LooksLikeText(lines []Line) bool {
	count := 0
	for _, line := range lines {
		for _, r := range line.Text {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				count++
			}
		}
	}
	return count >= minRecognisedCharacters
}

// parseTSV groups tesseract's word rows into lines.
//
// The columns are: level, page, block, paragraph, line, word, left, top, width,
// height, confidence, text. Only word rows (level 5) carry text.
func parseTSV(data []byte) ([]Line, error) {
	type key struct{ block, paragraph, line int }

	var (
		order []key
		acc   = map[key]*lineAcc{}
	)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue // header row
		}

		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 12 {
			continue
		}

		level, err := strconv.Atoi(fields[0])
		if err != nil || level != 5 {
			continue
		}

		text := strings.TrimSpace(strings.Join(fields[11:], "\t"))
		if text == "" {
			continue
		}

		confidence, err := strconv.ParseFloat(fields[10], 64)
		if err != nil || confidence < 0 {
			continue
		}

		left, _ := strconv.ParseFloat(fields[6], 64)
		top, _ := strconv.ParseFloat(fields[7], 64)
		width, _ := strconv.ParseFloat(fields[8], 64)
		height, _ := strconv.ParseFloat(fields[9], 64)

		block, _ := strconv.Atoi(fields[2])
		paragraph, _ := strconv.Atoi(fields[3])
		lineNo, _ := strconv.Atoi(fields[4])
		k := key{block: block, paragraph: paragraph, line: lineNo}

		acc2, ok := acc[k]
		if !ok {
			acc2 = &lineAcc{}
			acc[k] = acc2
			order = append(order, k)
		}

		acc2.add(text, left, top, width, height, confidence)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	lines := make([]Line, 0, len(order))
	for _, k := range order {
		if line, ok := acc[k].line(); ok {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

type lineAcc struct {
	words      []string
	confidence float64
	weight     int

	minX, minY float64
	maxX, maxY float64
	started    bool
}

func (a *lineAcc) add(text string, left, top, width, height, confidence float64) {
	a.words = append(a.words, text)
	a.confidence += confidence
	a.weight++

	right := left + width
	bottom := top + height

	if !a.started {
		a.minX, a.minY, a.maxX, a.maxY = left, top, right, bottom
		a.started = true
		return
	}

	a.minX = min(a.minX, left)
	a.minY = min(a.minY, top)
	a.maxX = max(a.maxX, right)
	a.maxY = max(a.maxY, bottom)
}

func (a *lineAcc) line() (Line, bool) {
	if len(a.words) == 0 {
		return Line{}, false
	}

	confidence := 0.0
	if a.weight > 0 {
		confidence = a.confidence / float64(a.weight) / 100
	}

	return Line{
		Text:       strings.Join(a.words, " "),
		Left:       a.minX,
		Top:        a.minY,
		Right:      a.maxX,
		Bottom:     a.maxY,
		Confidence: clamp(confidence, 0, 1),
	}, true
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
