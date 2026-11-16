package extract

import (
	"math"
	"strings"

	"github.com/klippa-app/go-pdfium/responses"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// line is one visual line of text after assembling its characters.
type line struct {
	text       string
	box        idm.Rect
	fontSize   float64
	confidence float64
}

// piece is one character with its box.
type piece struct {
	text string
	box  idm.Rect
}

// buildLines groups characters into lines.
//
// Characters are used rather than PDFium's rect grouping: the rect mode
// corrupts text on real documents (it turned "The long road bent" into
// "The long roa d bh d" on a CUPS-generated PDF), while the character list is
// exact.
//
// PDFium already decides where lines break and marks them with newline
// characters, so that is the primary signal. Geometry is only used for two
// things: separating columns inside a line, and clustering when a document
// emits no newline markers at all.
func buildLines(chars []*responses.GetPageTextStructuredChar) []line {
	runs, sawNewline := splitRuns(chars)
	if len(runs) == 0 {
		return nil
	}

	if !sawNewline {
		return clusterByBaseline(flatten(runs))
	}

	var lines []line
	for _, run := range runs {
		lines = append(lines, splitRunByGap(run)...)
	}
	return lines
}

// splitRuns divides the character stream at newline markers.
func splitRuns(chars []*responses.GetPageTextStructuredChar) ([][]piece, bool) {
	var (
		runs       [][]piece
		current    []piece
		sawNewline bool
	)

	for _, c := range chars {
		if c == nil || c.Text == "" {
			continue
		}
		if isNewline(c.Text) {
			sawNewline = true
			if len(current) > 0 {
				runs = append(runs, current)
				current = nil
			}
			continue
		}

		box := rectOf(c)
		if !box.Valid() {
			continue
		}
		current = append(current, piece{text: c.Text, box: box})
	}

	if len(current) > 0 {
		runs = append(runs, current)
	}
	return runs, sawNewline
}

// splitRunByGap separates columns that PDFium reports as one line. A justified
// word space is a few points; a gutter is tens of points.
func splitRunByGap(run []piece) []line {
	var (
		lines []line
		start int
	)

	for i := 1; i < len(run); i++ {
		gap := run[i-1].box.HorizontalGap(run[i].box)
		height := math.Max(run[i-1].box.Height(), run[i].box.Height())
		if height > 0 && gap > 2*height {
			lines = appendLine(lines, joinPieces(run[start:i]))
			start = i
		}
	}

	return appendLine(lines, joinPieces(run[start:]))
}

// clusterByBaseline groups characters by their baseline. It is the fallback for
// documents where PDFium emits no newline markers.
func clusterByBaseline(pieces []piece) []line {
	if len(pieces) == 0 {
		return nil
	}

	var (
		lines []line
		cur   []piece
	)

	for _, p := range pieces {
		if len(cur) == 0 {
			cur = append(cur, p)
			continue
		}

		last := cur[len(cur)-1]
		height := math.Max(last.box.Height(), p.box.Height())

		sameBaseline := math.Abs(p.box.Y0-last.box.Y0) <= 0.5*height
		smallGap := height <= 0 || p.box.HorizontalGap(last.box) <= 2*height

		if sameBaseline && smallGap {
			cur = append(cur, p)
			continue
		}

		lines = appendLine(lines, joinPieces(cur))
		cur = []piece{p}
	}

	return appendLine(lines, joinPieces(cur))
}

func flatten(runs [][]piece) []piece {
	n := 0
	for _, r := range runs {
		n += len(r)
	}

	out := make([]piece, 0, n)
	for _, r := range runs {
		out = append(out, r...)
	}
	return out
}

// joinPieces assembles a line's characters and inserts the spaces a PDF relies
// on positioning to show instead of space glyphs.
func joinPieces(pieces []piece) line {
	if len(pieces) == 0 {
		return line{}
	}

	height := 0.0
	for _, p := range pieces {
		if h := p.box.Height(); h > height {
			height = h
		}
	}

	box := pieces[0].box
	var (
		b        strings.Builder
		lastByte byte
	)

	for i, p := range pieces {
		if i > 0 {
			gap := pieces[i-1].box.HorizontalGap(p.box)
			// Kerning and glyph side bearings leave gaps of a point or two
			// between letters; half an em is a word boundary.
			if height > 0 && gap > 0.5*height && lastByte != ' ' && !strings.HasPrefix(p.text, " ") {
				b.WriteByte(' ')
				lastByte = ' '
			}
			box = union(box, p.box)
		}
		b.WriteString(p.text)
		lastByte = p.text[len(p.text)-1]
	}

	text := strings.Join(strings.Fields(b.String()), " ")
	if text == "" {
		return line{}
	}

	return line{text: text, box: box, fontSize: capHeight(pieces, box), confidence: 1}
}

// capHeight estimates the text size from the line's ascender-to-baseline
// distance.
//
// The bounding box height is not usable as a size: a line containing a
// descender ("page", "gently") is roughly 30% taller than the same line without
// one, which is enough to make heading detection classify body text as a
// heading. The baseline is the highest glyph bottom in the line; the cap height
// is the tallest glyph top above it. Both are stable across lines set in the
// same font.
func capHeight(pieces []piece, box idm.Rect) float64 {
	baseline := math.Inf(-1)
	top := math.Inf(-1)

	for _, p := range pieces {
		if y := p.box.Y0; y > baseline {
			baseline = y
		}
		if y := p.box.Y1; y > top {
			top = y
		}
	}

	if cap := top - baseline; cap > 0 {
		return cap
	}
	return box.Height()
}

// appendLine adds a non-empty line. Runs that contain only spaces produce no
// text and must not become blocks.
func appendLine(lines []line, l line) []line {
	if l.text == "" {
		return lines
	}
	return append(lines, l)
}

func rectOf(c *responses.GetPageTextStructuredChar) idm.Rect {
	p := c.PointPosition
	return idm.Rect{X0: p.Left, Y0: p.Bottom, X1: p.Right, Y1: p.Top}
}

func isNewline(text string) bool {
	return text == "\n" || text == "\r" || text == "\r\n"
}

func union(a, b idm.Rect) idm.Rect {
	return idm.Rect{
		X0: math.Min(a.X0, b.X0),
		Y0: math.Min(a.Y0, b.Y0),
		X1: math.Max(a.X1, b.X1),
		Y1: math.Max(a.Y1, b.Y1),
	}
}
