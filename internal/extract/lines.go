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
	// words are the line's word boxes, in order. They are what table detection
	// and footnote finding need; the line text is a convenience for readers.
	words []word
}

type word struct {
	text string
	box  idm.Rect
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

		// The window is generous because a closing quote sits most of a line
		// height above the baseline while a descender sits below it.
		sameBaseline := math.Abs(p.box.Y0-last.box.Y0) <= 0.8*height
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
	pieces = withWordBreaks(pieces)
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
			box = union(box, p.box)
		}
		b.WriteString(p.text)
		if p.text != "" {
			lastByte = p.text[len(p.text)-1]
		}
	}
	_ = lastByte

	text := strings.Join(strings.Fields(b.String()), " ")
	if text == "" {
		return line{}
	}

	return line{
		text:       text,
		box:        box,
		fontSize:   capHeight(pieces, box),
		confidence: 1,
		words:      wordBoxes(pieces),
	}
}

// withWordBreaks makes the gap rule explicit: a break is a space piece between
// the two characters it separates. Real space glyphs are kept; synthetic ones
// are inserted at the same gaps that once produced a space in the text, so the
// word list and the line text are built from one sequence.
func withWordBreaks(pieces []piece) []piece {
	return splitWords(pieces)
}

// splitWords inserts synthetic word breaks at the gaps that already produced a
// space in the line text, so the word list matches the text exactly.
func splitWords(pieces []piece) []piece {
	out := make([]piece, 0, len(pieces))
	for i, p := range pieces {
		if i > 0 {
			prev := pieces[i-1]
			gap := prev.box.HorizontalGap(p.box)
			height := math.Max(prev.box.Height(), p.box.Height())
			if height > 0 && gap > 0.5*height && !isSpace(prev.text) {
				out = append(out, piece{text: " ", box: idm.Rect{
					X0: prev.box.X1, Y0: p.box.Y0, X1: p.box.X0, Y1: p.box.Y1,
				}})
			}
		}
		out = append(out, p)
	}
	return out
}

// wordBoxes merges character pieces into words, keeping each word's box. The
// pieces must already contain the synthetic space pieces from splitWords.
func wordBoxes(pieces []piece) []word {
	var out []word

	var (
		text strings.Builder
		box  idm.Rect
		open bool
	)

	flush := func() {
		if !open {
			return
		}
		if t := strings.TrimSpace(text.String()); t != "" {
			out = append(out, word{text: t, box: box})
		}
		text.Reset()
		open = false
	}

	for _, p := range pieces {
		// A break is a break whether it was a real space glyph or a synthetic
		// one inserted at a gap: both are space pieces in this sequence.
		if strings.TrimSpace(p.text) == "" {
			flush()
			continue
		}
		if open {
			box = union(box, p.box)
		} else {
			box = p.box
			open = true
		}
		text.WriteString(p.text)
	}
	flush()

	return out
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
	baseline, ok := baselineOf(pieces)
	if !ok {
		return box.Height()
	}

	top := math.Inf(-1)
	for _, p := range pieces {
		if y := p.box.Y1; y > top {
			top = y
		}
	}

	if cap := top - baseline; cap > 0 {
		return cap
	}
	return box.Height()
}

// baselineOf finds the line's baseline as the most common glyph bottom.
//
// It cannot be the lowest bottom: a line ending in a closing quote, an
// apostrophe, or a superscript has glyphs whose boxes sit entirely above the
// baseline, and taking the maximum makes those lines look like 4pt text. In a
// 250-page novel that turned most body lines into "headings".
func baselineOf(pieces []piece) (float64, bool) {
	const bucket = 0.5 // points

	counts := make(map[float64]int, len(pieces))
	for _, p := range pieces {
		if p.box.Height() <= 0 {
			continue
		}
		counts[math.Round(p.box.Y0/bucket)*bucket]++
	}
	if len(counts) == 0 {
		return 0, false
	}

	var best float64
	bestCount := 0
	for y, n := range counts {
		if n > bestCount || (n == bestCount && y < best) {
			best, bestCount = y, n
		}
	}
	return best, true
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

// isSpace reports whether a piece is a space glyph. Space glyphs have no height
// and arrive in content order between the words they separate.
func isSpace(text string) bool {
	return strings.TrimSpace(text) == ""
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
