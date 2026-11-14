package extract

import (
	"math"
	"strings"

	"github.com/klippa-app/go-pdfium/responses"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// line is one visual line of text after merging adjacent runs.
type line struct {
	text     string
	box      idm.Rect
	fontSize float64
}

// piece is a single styled run as PDFium reports it.
type piece struct {
	text string
	box  idm.Rect
}

// buildLines merges text rects into lines.
//
// PDFium already groups characters into rects of "same line, same font", so a
// bold word splits one visual line into three rects. They are re-joined when
// they share a baseline and sit close together. A large horizontal gap is not
// joined, because that is what a column gutter looks like.
func buildLines(rects []*responses.GetPageTextStructuredRect) []line {
	var (
		lines   []line
		current []piece
	)

	flush := func() {
		if len(current) == 0 {
			return
		}
		if l := joinPieces(current); l.text != "" {
			lines = append(lines, l)
		}
		current = current[:0]
	}

	for _, r := range rects {
		if r == nil {
			continue
		}
		// Whitespace-only runs carry no glyph box of their own; joining them
		// would corrupt both the bounding box and the spacing heuristics.
		if strings.TrimSpace(r.Text) == "" {
			continue
		}

		box := rectOf(r)
		if box.Area() <= 0 {
			continue
		}

		if len(current) > 0 && sameLine(current[len(current)-1].box, box) {
			current = append(current, piece{text: r.Text, box: box})
			continue
		}

		flush()
		current = append(current, piece{text: r.Text, box: box})
	}
	flush()

	return lines
}

func rectOf(r *responses.GetPageTextStructuredRect) idm.Rect {
	p := r.PointPosition
	return idm.Rect{X0: p.Left, Y0: p.Bottom, X1: p.Right, Y1: p.Top}
}

// sameLine reports whether next continues the line that prev ends.
func sameLine(prev, next idm.Rect) bool {
	height := prev.Height()
	if height <= 0 {
		return false
	}
	if prev.VerticalOverlap(next) < 0.6*height {
		return false
	}

	// A font change keeps runs adjacent; a column gutter or a tab does not.
	gap := prev.HorizontalGap(next)
	return gap <= math.Max(height, next.Height())
}

// joinPieces concatenates runs and inserts the spaces that the rect grouping
// dropped. Word spacing inside a run is preserved; the gap between runs is not.
func joinPieces(pieces []piece) line {
	box := pieces[0].box
	fontSize := pieces[0].box.Height()

	var parts []string
	parts = append(parts, pieces[0].text)

	prev := pieces[0]
	for _, p := range pieces[1:] {
		last := parts[len(parts)-1]
		gap := prev.box.HorizontalGap(p.box)
		if gap > 0.2*fontSize && !strings.HasSuffix(last, " ") && !strings.HasPrefix(p.text, " ") {
			parts = append(parts, " ")
		}
		parts = append(parts, p.text)

		box = union(box, p.box)
		if h := p.box.Height(); h > fontSize {
			fontSize = h
		}
		prev = p
	}

	text := strings.Join(strings.Fields(strings.Join(parts, "")), " ")
	return line{text: text, box: box, fontSize: fontSize}
}

func union(a, b idm.Rect) idm.Rect {
	return idm.Rect{
		X0: math.Min(a.X0, b.X0),
		Y0: math.Min(a.Y0, b.Y0),
		X1: math.Max(a.X1, b.X1),
		Y1: math.Max(a.Y1, b.Y1),
	}
}
