package extract

import (
	"math"
	"strings"
	"unicode"

	"github.com/klippa-app/go-pdfium/responses"

	"github.com/kusalkalinga00/reflow/internal/idm"
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
	// styled carries every character of the line with its inline style, when
	// the line has any styling at all. It is what links and superscripts are
	// built from: the characters keep their exact spacing, so a styled run can
	// split a word without moving a space.
	styled []styledPiece
}

// styledPiece is one character with its box and whether it is raised.
type styledPiece struct {
	text  string
	box   idm.Rect
	super bool
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

	size := capHeight(pieces, box)
	baseline, _ := baselineOf(pieces)
	super := pieceStyles(pieces, size, baseline)

	styled := make([]styledPiece, len(pieces))
	for i, p := range pieces {
		styled[i] = styledPiece{text: p.text, box: p.box, super: super[i]}
	}

	return line{
		text:       text,
		box:        box,
		fontSize:   size,
		confidence: 1,
		words:      wordBoxes(pieces),
		styled:     styled,
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

// superscriptSpans finds raised, smaller glyph runs in a line.
//
// A footnote marker, an ordinal suffix and an exponent all have the same shape
// in a PDF: a short run of glyphs set smaller than the line and lifted above
// its baseline. That is the only inline styling extraction can infer reliably,
// because it has geometry and nothing else.
//
// The rule is narrow because a false positive is visible: quote marks, hyphens
// and the asterisk in a code sample are small glyphs whose ink sits above the
// baseline, and lifting them would corrupt text throughout the book. So a run
// must be small, raised, short, attached to the word before it, and made of
// digits, note marks, or an ordinal suffix. Everything else stays plain text.
// pieceStyles marks each piece of a line as raised annotation or ordinary text.
//
// The rules are narrow because a false positive is visible: quote marks, hyphens
// and the asterisk in a code sample are small glyphs whose ink sits above the
// baseline, and lifting them would corrupt text throughout the book. So a run
// must be small, raised, short, attached to the word before it, and made of
// digits, note marks, or an ordinal suffix. Everything else stays plain text.
func pieceStyles(pieces []piece, size, baseline float64) []bool {
	super := make([]bool, len(pieces))
	if size <= 0 {
		return super
	}

	var (
		begin  int
		raised bool
	)

	flush := func(end int) {
		if end <= begin || !raised {
			return
		}

		run := pieces[begin:end]
		text := pieceText(run)
		marked := looksLikeSuperscript(text, run, pieceBefore(pieces, begin), size)
		if marked && begin > 0 {
			marked = attachedTo(pieces[begin-1], run[0], size)
		}
		if marked && end < len(pieces) && isLetterPiece(pieces[end]) {
			// A marker ends a word or is followed by punctuation. A letter
			// right after the run means it was part of the word's own type,
			// lifted by a font quirk rather than set as annotation.
			marked = false
		}
		if !marked {
			return
		}
		for i := begin; i < end; i++ {
			if strings.TrimSpace(pieces[i].text) != "" {
				super[i] = true
			}
		}
	}

	for i, p := range pieces {
		if strings.TrimSpace(p.text) == "" {
			// A space ends a raised run: the marker is the glyphs only, and
			// the space belongs to the text that follows it.
			if raised && begin < i {
				flush(i)
				begin = i
				raised = false
			}
			continue
		}

		up := p.box.Height() > 0 &&
			p.box.Height() <= 0.8*size &&
			p.box.Y0 >= baseline+0.25*size

		if begin < i && up != raised {
			flush(i)
			begin = i
		}
		if begin == i {
			raised = up
		}
	}
	flush(len(pieces))

	return super
}

// pieceBefore is the piece before index, or the zero piece at the start of a
// line.
func pieceBefore(pieces []piece, index int) piece {
	if index == 0 {
		return piece{}
	}
	return pieces[index-1]
}

// superscriptSpans is the span form of pieceStyles, used by tests and by any
// caller that does not need link geometry.
func superscriptSpans(pieces []piece, size, baseline float64) []idm.Span {
	styles := pieceStyles(pieces, size, baseline)

	var (
		spans []idm.Span
		cur   strings.Builder
		super bool
		open  bool
	)
	flush := func() {
		if !open || cur.Len() == 0 {
			return
		}
		spans = append(spans, idm.Span{Text: cur.String(), Super: super})
		cur.Reset()
		open = false
	}
	sawSuper := false
	for i, p := range pieces {
		if strings.TrimSpace(p.text) == "" {
			cur.WriteString(p.text)
			continue
		}
		if !open {
			super, open = styles[i], true
		} else if styles[i] != super {
			flush()
			super, open = styles[i], true
		}
		if super {
			sawSuper = true
		}
		cur.WriteString(p.text)
	}
	flush()

	if !sawSuper {
		return nil
	}
	return spans
}

// endsInDigit reports whether a piece ends in a digit.
func endsInDigit(p piece) bool {
	runes := []rune(strings.TrimSpace(p.text))
	if len(runes) == 0 {
		return false
	}
	return unicode.IsDigit(runes[len(runes)-1])
}

// isLetterPiece reports whether a piece begins with a letter.
func isLetterPiece(p piece) bool {
	for _, r := range p.text {
		return unicode.IsLetter(r)
	}
	return false
}

// attachedTo reports whether a raised run directly continues the glyph before
// it. A marker is printed against its word; a stray asterisk in code is not.
func attachedTo(prev, first piece, size float64) bool {
	if strings.TrimSpace(prev.text) == "" {
		return false
	}
	return prev.box.HorizontalGap(first.box) <= 0.2*size
}

// looksLikeSuperscript accepts the runs that can only be markers or suffixes,
// and rejects the punctuation and small-caps glyphs that share their geometry.
func looksLikeSuperscript(text string, pieces []piece, before piece, size float64) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	runes := []rune(trimmed)
	if len(runes) > 3 {
		return false
	}

	digits, marks, letters := 0, 0, 0
	for _, r := range runes {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		case r == '*' || r == '\u2020' || r == '\u2021' || r == '\u00a7' || r == '\u00b6':
			marks++
		default:
			return false
		}
	}
	switch {
	case digits == len(runes):
		// A number: a reference marker.
	case marks == len(runes) && len(runes) <= 2:
		// An asterisk, dagger or section sign.
	case letters > 0 && digits == 0 && letters <= 2 && endsInDigit(before):
		// An ordinal suffix such as "1st" or "2nd": the digits are set in the
		// body face, the suffix is lifted.
	case letters > 0 && digits > 0 && letters <= 2:
		// A marker printed as one run, such as "1st".
	default:
		return false
	}

	// Small caps are also smaller than the line, but they sit on the
	// baseline; only a run lifted off it counts.
	height := 0.0
	for _, p := range pieces {
		if h := p.box.Height(); h > height {
			height = h
		}
	}
	return height > 0 && height <= 0.75*size
}

// pieceText concatenates a piece run.
func pieceText(pieces []piece) string {
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(p.text)
	}
	return b.String()
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
