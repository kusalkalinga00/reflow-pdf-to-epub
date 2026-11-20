package render

import (
	"math"
	"sort"
	"strings"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/normalize"
)

// Paragraph is a run of consecutive line blocks that belong together.
//
// Extraction produces one block per visual line. Merging them is the first
// piece of structure the reader actually notices: a book rendered as one
// paragraph per line is technically readable and unpleasant to read.
type Paragraph struct {
	Kind     idm.Kind // KindParagraph, KindHeading or KindFigure
	Level    int      // 1..3, headings only
	Text     string
	Page     int
	Box      idm.Rect
	FontSize float64
	// Image is set for figures.
	Image *idm.Image
	// Table is set for recovered tables.
	Table *idm.TableGrid
	// Spans carry inline styling, when the source had any. They concatenate
	// to Text exactly, or they are nil.
	Spans []idm.Span
}

// GroupParagraphs merges line blocks into paragraphs and classifies headings.
//
// The heading rule is deliberately simple: a paragraph is a heading when it is
// at least 1.2x the body text size and short enough to be a title. The layout
// stage will replace this with a rule that uses position and surrounding
// context as well; until then, a conservative rule beats no structure at all.
func GroupParagraphs(blocks []idm.Block) []Paragraph {
	var out []Paragraph

	for _, b := range blocks {
		if b.Kind.IsFurniture() {
			continue
		}
		if b.Kind == idm.KindFigure && b.Image != nil {
			out = append(out, Paragraph{
				Kind: idm.KindFigure, Page: b.Page, Box: b.BBox, Image: b.Image,
			})
			continue
		}

		if b.Kind == idm.KindTable && b.Table != nil {
			out = append(out, Paragraph{
				Kind: idm.KindTable, Page: b.Page, Box: b.BBox, Table: b.Table,
			})
			continue
		}

		if text := strings.TrimSpace(b.Text); text == "" {
			continue
		}

		if n := len(out); n > 0 && shouldMerge(out[n-1], b) {
			out[n-1] = merge(out[n-1], b)
			continue
		}

		out = append(out, Paragraph{
			Kind:     idm.KindParagraph,
			Text:     strings.TrimSpace(b.Text),
			Page:     b.Page,
			Box:      b.BBox,
			FontSize: b.FontSize,
			Spans:    b.Spans,
		})
	}

	classifyHeadings(out)
	return out
}

// shouldMerge reports whether next continues the paragraph that prev ends.
func shouldMerge(prev Paragraph, next idm.Block) bool {
	if prev.Kind != idm.KindParagraph || next.Kind != idm.KindParagraph || prev.Page != next.Page {
		return false
	}
	if prev.FontSize <= 0 || next.FontSize <= 0 {
		return false
	}

	// A size change means a different role: a heading, a caption, a footnote.
	if math.Abs(prev.FontSize-next.FontSize) > 0.15*math.Max(prev.FontSize, next.FontSize) {
		return false
	}

	// Positive gap means next sits below prev. Overlap or reordering is not a
	// continuation.
	gap := prev.Box.Y0 - next.BBox.Y1
	lineHeight := math.Max(prev.FontSize, next.FontSize)
	if gap < -0.5*lineHeight {
		return false
	}

	// Normal leading leaves a gap well under one em; a paragraph break adds
	// roughly a line, so the threshold separates the two cases.
	return gap <= 0.75*lineHeight
}

func merge(prev Paragraph, next idm.Block) Paragraph {
	before := prev.Text
	prev.Text = normalize.JoinLines(prev.Text, next.Text)
	prev.Spans = mergeSpans(before, prev.Text, prev.Spans, next.Spans)
	prev.Box = idm.Rect{
		X0: math.Min(prev.Box.X0, next.BBox.X0),
		Y0: math.Min(prev.Box.Y0, next.BBox.Y0),
		X1: math.Max(prev.Box.X1, next.BBox.X1),
		Y1: math.Max(prev.Box.Y1, next.BBox.Y1),
	}
	if next.FontSize > prev.FontSize {
		prev.FontSize = next.FontSize
	}
	return prev
}

// mergeSpans joins the inline styling of two merged lines.
//
// The join inserts a space between the lines; the runs keep concatenating to
// the text because that space is added to the last span of the first line. Any
// mismatch drops the styling rather than keeping runs that do not reconstruct
// the paragraph.
func mergeSpans(before, after string, prev, next []idm.Span) []idm.Span {
	if len(prev) == 0 || len(next) == 0 {
		return nil
	}

	out := make([]idm.Span, 0, len(prev)+len(next))
	out = append(out, prev...)
	out[len(out)-1].Text += " "
	out = append(out, next...)

	var joined strings.Builder
	for _, s := range out {
		joined.WriteString(s.Text)
	}
	if joined.String() != after {
		return nil
	}
	return out
}

// spanText is the text a span list describes.
func spanText(spans []idm.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func classifyHeadings(paras []Paragraph) {
	if len(paras) == 0 {
		return
	}

	body := bodySize(paras)
	if body <= 0 {
		return
	}

	for i := range paras {
		p := &paras[i]
		if p.FontSize < 1.2*body {
			continue
		}
		if len([]rune(p.Text)) > 120 {
			continue
		}

		p.Kind = idm.KindHeading
		switch {
		case p.FontSize >= 1.8*body:
			p.Level = 1
		case p.FontSize >= 1.4*body:
			p.Level = 2
		default:
			p.Level = 3
		}
	}
}

// bodySize returns the size that most of the text is set in, weighted by
// character count rather than paragraph count.
//
// A plain median over paragraphs is wrong on short documents: one 20pt title
// and one 10pt paragraph give a median of 15, which makes the title look
// ordinary. Weighting by characters answers the actual question - what size is
// the body text - and stays stable as a book grows.
func bodySize(paras []Paragraph) float64 {
	chars := make(map[float64]int, 8)
	total := 0

	for _, p := range paras {
		if p.FontSize <= 0 {
			continue
		}
		n := len([]rune(p.Text))
		chars[p.FontSize] += n
		total += n
	}
	if total == 0 {
		return 0
	}

	sizes := make([]float64, 0, len(chars))
	for size := range chars {
		sizes = append(sizes, size)
	}
	sort.Float64s(sizes)

	seen := 0
	for _, size := range sizes {
		seen += chars[size]
		if seen*2 >= total {
			return size
		}
	}
	return sizes[len(sizes)-1]
}
