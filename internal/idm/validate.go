package idm

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// ErrInvalid is wrapped by every error returned from Validate, so callers can
// use errors.Is without matching on message text.
var ErrInvalid = errors.New("invalid document")

// Problem is one failed invariant, with a path that points at the offending
// value: "blocks[12].page", "pages[3].width".
type Problem struct {
	Path    string
	Message string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// ValidationError carries every problem found in one pass, rather than
// stopping at the first one. A pipeline stage that emits a broken document
// should produce a diagnosable report, not a shrug.
type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "invalid document (%d problem", len(e.Problems))
	if len(e.Problems) != 1 {
		b.WriteByte('s')
	}
	b.WriteByte(')')
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		b.WriteString(p.String())
	}
	return b.String()
}

// Unwrap makes errors.Is(err, ErrInvalid) true.
func (e *ValidationError) Unwrap() error { return ErrInvalid }

// Validate checks every structural invariant the rest of the pipeline relies
// on. A document that passes is safe for EPUB assembly and for the quality
// score to read; it does not mean the conversion was good, only well formed.
func (d Document) Validate() error {
	var problems []Problem
	add := func(path, format string, args ...any) {
		problems = append(problems, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
	}

	if d.Meta.Language == "" {
		add("meta.language", "must be set (BCP 47, e.g. \"en\")")
	}

	if len(d.Pages) == 0 {
		add("pages", "must contain at least one page")
	}

	for i, page := range d.Pages {
		path := fmt.Sprintf("pages[%d]", i)

		if page.Number != i+1 {
			add(path+".number", "must be %d, got %d", i+1, page.Number)
		}
		if !positiveFinite(page.Width) {
			add(path+".width", "must be positive and finite, got %v", page.Width)
		}
		if !positiveFinite(page.Height) {
			add(path+".height", "must be positive and finite, got %v", page.Height)
		}

		switch page.Rotation {
		case 0, 90, 180, 270:
		default:
			add(path+".rotation", "must be 0, 90, 180, or 270, got %d", page.Rotation)
		}

		if !page.Provenance.Valid() {
			add(path+".provenance", "must be %q or %q, got %q",
				ProvenanceDigital, ProvenanceOCR, page.Provenance)
		}

		if !(page.TextCoverage >= 0 && page.TextCoverage <= 1) {
			add(path+".textCoverage", "must be within [0,1], got %v", page.TextCoverage)
		}
	}

	for i, entry := range d.Outline {
		path := fmt.Sprintf("outline[%d]", i)

		if strings.TrimSpace(entry.Title) == "" {
			add(path+".title", "must not be empty")
		}
		if entry.Page < 0 || entry.Page > len(d.Pages) {
			add(path+".page", "must be within [0,%d], got %d", len(d.Pages), entry.Page)
		}
		if entry.Level < 1 {
			add(path+".level", "must be at least 1, got %d", entry.Level)
		}
		if entry.Y < 0 || math.IsNaN(entry.Y) || math.IsInf(entry.Y, 0) {
			add(path+".y", "must be zero or a positive finite number, got %v", entry.Y)
		}
	}

	prevPage := 0
	for i, block := range d.Blocks {
		path := fmt.Sprintf("blocks[%d]", i)

		if !block.Kind.Valid() {
			add(path+".kind", "unknown kind %q", block.Kind)
		}

		switch {
		case block.Page < 1 || block.Page > len(d.Pages):
			add(path+".page", "must be within [1,%d], got %d", len(d.Pages), block.Page)
		case block.Page < prevPage:
			add(path+".page", "blocks must be in reading order: page %d follows page %d",
				block.Page, prevPage)
		default:
			prevPage = block.Page
		}

		if block.Column < 0 {
			add(path+".column", "must not be negative, got %d", block.Column)
		}

		if !(block.Confidence >= 0 && block.Confidence <= 1) {
			add(path+".confidence", "must be within [0,1], got %v", block.Confidence)
		}

		if block.FontSize < 0 || math.IsNaN(block.FontSize) || math.IsInf(block.FontSize, 0) {
			add(path+".fontSize", "must be zero or a positive finite number, got %v", block.FontSize)
		}

		if !block.BBox.Valid() {
			add(path+".bbox", "must have finite, ordered coordinates, got %v", block.BBox)
		}

		if block.Kind == KindHeading {
			if block.Level < 1 || block.Level > 6 {
				add(path+".level", "heading level must be within [1,6], got %d", block.Level)
			}
		} else if block.Level != 0 {
			add(path+".level", "only headings carry a level, got %d for kind %q",
				block.Level, block.Kind)
		}

		if block.Image != nil && block.Image.MediaType == "" {
			add(path+".image.mediaType", "must be set when image data is present")
		}

		if block.Kind == KindTable {
			if block.Table == nil {
				add(path+".table", "a table block needs a grid")
			} else {
				width := -1
				for r, row := range block.Table.Cells {
					if len(row) == 0 {
						add(fmt.Sprintf("%s.table.cells[%d]", path, r), "must not be empty")
						continue
					}
					if width == -1 {
						width = len(row)
					} else if len(row) != width {
						add(fmt.Sprintf("%s.table.cells[%d]", path, r),
							"rows must have the same number of columns: got %d, want %d", len(row), width)
					}
				}
			}
		}

		// Figures may be image-only; tables carry their text in the grid.
		// Every other kind must carry text.
		if strings.TrimSpace(block.Text) == "" && block.Kind != KindFigure && block.Kind != KindTable {
			add(path+".text", "must not be empty for kind %q", block.Kind)
		}

		if len(block.Spans) > 0 {
			var joined strings.Builder
			for j, span := range block.Spans {
				if span.Text == "" {
					add(fmt.Sprintf("%s.spans[%d].text", path, j), "must not be empty")
				}
				joined.WriteString(span.Text)
			}
			if joined.String() != block.Text {
				add(path+".spans", "spans must concatenate to text, got %q", joined.String())
			}
		}

		for j, w := range block.Words {
			if w.Text == "" || strings.TrimSpace(w.Text) == "" {
				add(fmt.Sprintf("%s.words[%d].text", path, j), "must not be empty")
			}
			if !w.Box.Valid() {
				add(fmt.Sprintf("%s.words[%d].box", path, j), "must have ordered coordinates")
			}
		}
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func positiveFinite(v float64) bool {
	return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v)
}
