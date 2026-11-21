package render

import (
	"sort"
	"strings"
	"unicode"

	"github.com/kusalkalinga00/reflow/internal/idm"
)

// Footnote detection thresholds.
//
// A footnote is a short, small-type paragraph in the bottom band of a page that
// begins with a marker. The rules are geometric because that is all a PDF
// offers: there is no structure saying "this is a note", only where the type
// sits and how big it is.
const (
	// footnoteBand is the fraction of the page height, measured from the
	// bottom, where notes live.
	footnoteBand = 0.34
	// footnoteSize is the largest note size as a fraction of the page's body
	// size. Notes are set smaller than the text they annotate.
	footnoteSize = 0.92
	// footnoteMinRunes and footnoteMaxRunes bound a note. The lower bound
	// rejects bare page numbers and stray marks; the upper bound rejects body
	// paragraphs that happen to end a page in a smaller face.
	footnoteMinRunes = 12
	footnoteMaxRunes = 400
	// footnoteBodyRunes is the length above which a paragraph counts as
	// evidence of the page's body size.
	footnoteBodyRunes = 120
)

// MarkFootnotes labels page-bottom notes on grouped paragraphs.
//
// It is exported so the quality report measures the same classification the
// writer renders: the report must describe the book the reader opens. The
// result is a pure function of the document, so calling it twice is harmless.
func MarkFootnotes(paras []Paragraph, pages []idm.Page) int {
	return markFootnotes(paras, pages)
}

// markFootnotes classifies page-bottom notes on the paragraphs.
//
// It returns how many paragraphs became notes. Classification is per page and
// uses the page's own body size, so a book with a small face or a large one is
// judged the same way.
func markFootnotes(paras []Paragraph, pages []idm.Page) int {
	if len(pages) == 0 {
		return 0
	}

	fallback := bodySize(paras)

	byPage := make(map[int][]int, len(pages))
	for i := range paras {
		byPage[paras[i].Page] = append(byPage[paras[i].Page], i)
	}

	marked := 0
	for page, indexes := range byPage {
		if page < 1 || page > len(pages) {
			continue
		}
		height := pages[page-1].Height
		if height <= 0 {
			continue
		}

		body := pageBodySize(paras, indexes, fallback)
		if body <= 0 {
			continue
		}

		// Distance from the page's top in PDF space, where y grows upward.
		band := footnoteBand * height
		for _, i := range indexes {
			p := &paras[i]
			if p.Kind != idm.KindParagraph {
				continue
			}
			if p.FontSize <= 0 || p.FontSize > body*footnoteSize {
				continue
			}
			if height-p.Box.Y1 < band {
				continue
			}

			n := len([]rune(strings.TrimSpace(p.Text)))
			if n < footnoteMinRunes || n > footnoteMaxRunes {
				continue
			}
			if _, ok := footnoteMarker(p); !ok {
				continue
			}
			if !endsSentence(p.Text) {
				// A note finishes a sentence. A diagram label, a caption and
				// a single-line callout do not, and they are exactly the
				// small type that sits at the bottom of a page.
				continue
			}

			marker, _ := footnoteMarker(p)
			if !referenced(paras, indexes, i, marker) {
				// A note is referenced from the text it annotates. Without a
				// reference the paragraph is more likely a diagram label, a
				// code sample's output, or a numbered step — all of which are
				// small type at the foot of a page, and none of which is a
				// note.
				continue
			}

			p.Kind = idm.KindFootnote
			marked++
		}
	}

	return marked
}

// referenced reports whether a marker is cited in the text before its note.
//
// Two forms count: a raised marker, which is what a footnote reference looks
// like, and a bracketed number, which is the other convention ("...as shown
// [3]."). Both are evidence the page itself points at the note.
func referenced(paras []Paragraph, pageIndexes []int, note int, marker string) bool {
	if marker == "" {
		return false
	}

	for _, i := range pageIndexes {
		if i >= note {
			break
		}
		p := paras[i]
		if p.Page != paras[note].Page {
			continue
		}

		for _, span := range p.Spans {
			if span.Super && strings.TrimSpace(span.Text) == marker {
				return true
			}
		}
		if strings.Contains(p.Text, "["+marker+"]") {
			return true
		}
	}
	return false
}

// pageBodySize estimates the size the page's body text is set in.
//
// It uses only paragraphs long enough to be prose, and falls back to the whole
// document when a page has none: a title page or an illustration page says
// nothing about the book's body size.
func pageBodySize(paras []Paragraph, indexes []int, fallback float64) float64 {
	var sizes []float64
	for _, i := range indexes {
		p := paras[i]
		if p.Kind != idm.KindParagraph || p.FontSize <= 0 {
			continue
		}
		if len([]rune(p.Text)) >= footnoteBodyRunes {
			sizes = append(sizes, p.FontSize)
		}
	}
	if len(sizes) == 0 {
		return fallback
	}
	return median(sizes)
}

// footnoteMarker is the note's leading reference.
//
// Printed notes begin with the mark their reference carries: a number, an
// asterisk, a dagger, a double dagger, a section sign, sometimes set as a
// superscript. A paragraph that starts with one of those and then continues is
// a note; a paragraph that starts with a year or a quantity is not.
func footnoteMarker(p *Paragraph) (string, bool) {
	if len(p.Spans) > 0 && p.Spans[0].Super {
		if marker, ok := parseMarker(strings.TrimSpace(p.Spans[0].Text)); ok {
			return marker, true
		}
	}
	return parseMarker(strings.TrimSpace(p.Text))
}

// parseMarker reads a leading note marker from text.
func parseMarker(text string) (string, bool) {
	runes := []rune(text)
	if len(runes) < 2 {
		return "", false
	}

	n := 0
	for n < len(runes) && n < 3 {
		r := runes[n]
		if !unicode.IsDigit(r) && !isNoteMark(r) {
			break
		}
		n++
	}
	if n == 0 {
		return "", false
	}

	// The marker must be followed by a space, not by more word or by
	// punctuation: "1990" is a year, and "1) Data preparation" and
	// "2.2) Tokenize" are diagram labels or numbered steps. Only a bare
	// marker followed by prose is a note.
	next := runes[n]
	if next != ' ' && next != '\t' {
		return "", false
	}

	// After the marker comes prose, not more notation: "1 . Grasses" is a
	// numbered cell in a table, and its marker is followed by a full stop.
	rest := strings.TrimSpace(text[n:])
	if rest == "" {
		return "", false
	}
	first := []rune(rest)[0]
	if !unicode.IsLetter(first) && first != '"' && first != '\u201c' {
		return "", false
	}

	marker := string(runes[:n])
	if digits, ok := allDigits(marker); ok && digits > 3 {
		return "", false
	}
	return marker, true
}

// endsSentence reports whether text closes like prose.
func endsSentence(text string) bool {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	for i := len(runes) - 1; i >= 0; i-- {
		switch runes[i] {
		case ' ', '\t':
			continue
		case '.', '!', '?', '\u2026', '"', '\'', '\u201d', '\u2019':
			return true
		}
		return false
	}
	return false
}

func isNoteMark(r rune) bool {
	switch r {
	case '*', '\u2020', '\u2021', '\u00a7', '\u00b6':
		return true
	}
	return false
}

func allDigits(s string) (int, bool) {
	count := 0
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return 0, false
		}
		count++
	}
	return count, count > 0
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
