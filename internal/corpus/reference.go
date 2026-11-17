package corpus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNoReference means pdftotext is not installed.
var ErrNoReference = errors.New("pdftotext is not available")

// ReferenceText extracts a PDF's text with pdftotext, for measuring our output
// against a second engine when no ground truth exists.
//
// The comparison has a known bias: pdftotext keeps running headers, footers and
// page numbers, which this pipeline deliberately removes. Recall and word error
// rate are therefore lower bounds, not verdicts. Agreement is still useful —
// it is where the two engines disagree that someone should look.
func ReferenceText(path string) (string, error) {
	binary := os.Getenv("REFLOW_PDFTOTEXT")
	if binary == "" {
		binary = "pdftotext"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return "", fmt.Errorf("%w: install poppler-utils for -reference", ErrNoReference)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out, err := exec.CommandContext(ctx, binary, "-enc", "UTF-8", "-q", path, "-").Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext %s: %w", path, err)
	}

	return stripRepeatedEdges(string(out)), nil
}

// stripRepeatedEdges removes running headers and footers from reference text.
//
// pdftotext keeps page furniture that this pipeline deliberately removes, so a
// naive comparison scores correct behaviour as lost text: a slide deck looked
// like it had 86% recall until its 233 repeated footers were accounted for.
// Removing the same pattern from the reference makes the comparison fair.
func stripRepeatedEdges(text string) string {
	pages := strings.Split(text, "")
	if len(pages) < 3 {
		return text
	}

	type edge string
	counts := map[edge]int{}

	for _, page := range pages {
		lines := nonEmptyLines(page)
		if len(lines) == 0 {
			continue
		}
		// A header is the first line, a footer the last; on a one-line page it
		// is both and counts once.
		counts[edge(maskDigits(lines[0]))]++
		if len(lines) > 1 {
			counts[edge(maskDigits(lines[len(lines)-1]))]++
		}
	}

	threshold := (len(pages) + 1) / 2

	var b strings.Builder
	for i, page := range pages {
		lines := strings.Split(page, "\n")
		var kept []string

		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && counts[edge(maskDigits(trimmed))] >= threshold {
				continue
			}
			kept = append(kept, line)
		}

		b.WriteString(strings.Join(kept, "\n"))
		if i < len(pages)-1 {
			b.WriteString("\f")
		}
	}

	// Whitespace differences would make the same line look different.
	return strings.Join(strings.Fields(b.String()), " ")
}

func nonEmptyLines(page string) []string {
	var out []string
	for _, line := range strings.Split(page, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// maskDigits replaces digit runs with a placeholder so "Slide 42" and
// "Slide 43" count as the same footer.
func maskDigits(s string) string {
	var b strings.Builder
	inDigits := false

	for _, r := range strings.ToLower(s) {
		if r >= '0' && r <= '9' {
			if !inDigits {
				b.WriteByte('#')
				inDigits = true
			}
			continue
		}
		inDigits = false
		b.WriteRune(r)
	}

	// Whitespace differences would make the same line look different.
	return strings.Join(strings.Fields(b.String()), " ")
}
