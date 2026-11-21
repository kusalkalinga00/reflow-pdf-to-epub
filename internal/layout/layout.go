// Package layout puts extracted blocks into reading order and removes page
// furniture.
//
// Extraction returns text in the order the PDF drew it, which is close to
// reading order for simple documents and wrong for anything with columns: a
// two-column page alternates between columns, line by line. This stage fixes
// the order and strips the running headers and footers that would otherwise
// appear in the middle of the text.
//
// Columns are found per band, not per page. A page mixes structures — an
// invoice carries a two-column header over a one-grid table over paired
// totals — and a page-wide gutter search sees their union, which is neither
// structure. The page is cut into horizontal bands at its clear vertical
// gaps, and each band is ordered by its own shape: parallel text columns read
// column by column, paired short cells read row by row, and everything else
// reads line by line.
package layout

import (
	"math"
	"sort"
	"strings"

	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/idm"
)

// Apply returns a copy of doc with blocks in reading order and page furniture
// marked. The input document is not modified.
func Apply(doc idm.Document) idm.Document {
	byPage := make([][]idm.Block, len(doc.Pages))
	for _, b := range doc.Blocks {
		if b.Page < 1 || b.Page > len(byPage) {
			continue
		}
		byPage[b.Page-1] = append(byPage[b.Page-1], b)
	}

	markFurniture(byPage, doc.Pages)

	for i := range byPage {
		byPage[i] = orderPage(byPage[i], doc.Pages[i])
	}

	var blocks []idm.Block
	for _, page := range byPage {
		blocks = append(blocks, page...)
	}

	return doc.WithBlocks(blocks)
}

// orderPage puts one page's blocks into reading order.
func orderPage(blocks []idm.Block, page idm.Page) []idm.Block {
	body := make([]idm.Block, 0, len(blocks))
	var furniture []idm.Block

	for _, b := range blocks {
		if b.Kind.IsFurniture() {
			furniture = append(furniture, b)
			continue
		}
		body = append(body, b)
	}

	var out []idm.Block
	margin := contentMargin(body)
	for _, band := range splitBands(body) {
		out = append(out, orderBand(band, page.Width, margin)...)
	}
	return append(out, furniture...)
}

// contentMargin is the left edge of the page's text: where its paragraphs
// and tables start.
func contentMargin(blocks []idm.Block) float64 {
	margin := math.Inf(1)
	for _, b := range blocks {
		margin = math.Min(margin, b.BBox.X0)
	}
	if math.IsInf(margin, 1) {
		return 0
	}
	return margin
}

// bandGap is the vertical whitespace that separates two structures on a
// page. Lines inside one structure sit closer than this — a line pitch is
// its height plus a few points of leading, so lines gap by single digits —
// while the gap around a heading, between an address block and the table
// below it, or before a boxed total starts in the high teens.
const bandGap = 15

// splitBands cuts blocks into horizontal bands: runs of blocks whose vertical
// extents come within bandGap of the run below, top-down.
func splitBands(blocks []idm.Block) [][]idm.Block {
	sorted := append([]idm.Block(nil), blocks...)
	sortTopDown(sorted)

	var bands [][]idm.Block
	var cur []idm.Block
	bottom := 0.0

	for _, b := range sorted {
		if len(cur) > 0 && bottom-b.BBox.Y1 > bandGap {
			bands = append(bands, cur)
			cur = nil
		}
		if len(cur) == 0 {
			bottom = b.BBox.Y0
		} else if b.BBox.Y0 < bottom {
			bottom = b.BBox.Y0
		}
		cur = append(cur, b)
	}
	if len(cur) > 0 {
		bands = append(bands, cur)
	}
	return bands
}

// orderBand orders one band by its own shape. margin is the page's text
// margin, which tells a page's own columns from a structure floating inside
// it.
func orderBand(blocks []idm.Block, pageWidth, margin float64) []idm.Block {
	start, end, ok := findGutter(blocks, pageWidth)
	if !ok {
		return singleColumn(lineOrder(blocks))
	}

	left, right, _ := splitColumns(blocks, start, end)

	// Paired cells away from the page's text margin are a floating grid —
	// an invoice's totals box, each label beside its value below the table —
	// and read row by row so each label keeps its value. Paired columns at
	// the margin are the page's own text, however short their lines, and
	// read column by column.
	if rowsPaired(left, right) && columnStart(left) > margin+marginTolerance {
		return singleColumn(lineOrder(blocks))
	}

	return orderColumns(blocks, start, end)
}

// marginTolerance is how far right of the page's text margin a column pair
// may start and still be the page's own text flow.
const marginTolerance = 15

// columnStart is a column's left edge.
func columnStart(blocks []idm.Block) float64 {
	if len(blocks) == 0 {
		return 0
	}
	x0 := blocks[0].BBox.X0
	for _, b := range blocks[1:] {
		x0 = math.Min(x0, b.BBox.X0)
	}
	return x0
}

// wideColumn is the width at which a column holds running text rather than a
// label or a number. Two columns this wide are parallel text; two narrower
// paired ones are a grid.
const wideColumn = 150

// splitColumns divides blocks at a gutter. A block that crosses the gutter
// belongs to neither column: it spans them.
func splitColumns(blocks []idm.Block, gutterStart, gutterEnd float64) (left, right, spanning []idm.Block) {
	for _, b := range blocks {
		switch {
		case b.BBox.X0 < gutterEnd && b.BBox.X1 > gutterStart:
			spanning = append(spanning, b)
		case b.BBox.X1 <= gutterStart+2:
			b.Column = 0
			left = append(left, b)
		default:
			b.Column = 1
			right = append(right, b)
		}
	}
	return left, right, spanning
}

// rowsPaired reports whether the two columns form a row grid: the same
// number of lines, each left line sharing its vertical position with a right
// one. A label/value grid pairs up this way; parallel text columns do not,
// because one runs longer or carries a heading the other lacks.
func rowsPaired(left, right []idm.Block) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}

	llines, rlines := linesOf(left), linesOf(right)
	if len(llines) != len(rlines) {
		return false
	}
	for i := range llines {
		lBottom, lTop := lineRange(llines[i])
		rBottom, rTop := lineRange(rlines[i])
		if rTop < lBottom || lTop < rBottom {
			return false
		}
	}
	return true
}

// columnWidth is the horizontal span of a column's blocks.
func columnWidth(blocks []idm.Block) float64 {
	if len(blocks) == 0 {
		return 0
	}
	x0, x1 := blocks[0].BBox.X0, blocks[0].BBox.X1
	for _, b := range blocks[1:] {
		x0 = math.Min(x0, b.BBox.X0)
		x1 = math.Max(x1, b.BBox.X1)
	}
	return x1 - x0
}

// findGutter looks for a vertical band that no narrow block crosses. A full
// width title covers the whole page, so wide blocks are ignored; otherwise one
// heading would hide the gutter of the columns beneath it.
func findGutter(blocks []idm.Block, pageWidth float64) (start, end float64, ok bool) {
	if pageWidth <= 0 {
		return 0, 0, false
	}

	const bin = 1.0
	bins := int(pageWidth / bin)
	if bins <= 0 {
		return 0, 0, false
	}

	covered := make([]bool, bins)
	for _, b := range blocks {
		if b.BBox.Width() >= 0.6*pageWidth {
			continue
		}
		from := clampInt(int(b.BBox.X0/bin), 0, bins-1)
		to := clampInt(int(b.BBox.X1/bin), 0, bins-1)
		for i := from; i <= to; i++ {
			covered[i] = true
		}
	}

	bestStart, bestEnd, bestWidth := -1, -1, 0
	runStart := -1

	for i := 0; i <= bins; i++ {
		if i < bins && !covered[i] {
			if runStart < 0 {
				runStart = i
			}
			continue
		}
		if runStart >= 0 {
			width := float64(i-runStart) * bin
			// The gutter must sit in the middle of the page and be wide
			// enough that it cannot be a word space.
			mid := float64(runStart)*bin + width/2
			if width >= 10 && mid > 0.2*pageWidth && mid < 0.8*pageWidth && width > float64(bestWidth) {
				bestStart, bestEnd, bestWidth = runStart, i, int(width)
			}
			runStart = -1
		}
	}

	if bestStart < 0 {
		return 0, 0, false
	}

	start = float64(bestStart) * bin
	end = float64(bestEnd) * bin

	// A gutter is only real if both sides carry text.
	left, right := 0, 0
	for _, b := range blocks {
		switch {
		case b.BBox.X1 <= start+2:
			left++
		case b.BBox.X0 >= end-2:
			right++
		}
	}
	if left < 2 || right < 2 {
		return 0, 0, false
	}

	return start, end, true
}

// orderColumns emits the left column, then the right column, keeping any
// block that spans the gutter at its vertical position.
func orderColumns(blocks []idm.Block, gutterStart, gutterEnd float64) []idm.Block {
	left, right, spanning := splitColumns(blocks, gutterStart, gutterEnd)

	left = lineOrder(left)
	right = lineOrder(right)
	sortTopDown(spanning)

	// A narrow strip beside a left column is an annotation on that column's
	// header — dates beside a résumé entry, a total beside a label — and
	// reads with the header, not after the body below it.
	if header, ok := stripHeader(left); ok && columnWidth(right) < wideColumn {
		return orderWithStrip(left, right, spanning, header)
	}

	out := make([]idm.Block, 0, len(blocks))
	li, ri := 0, 0

	for _, s := range spanning {
		// Everything above the spanning block is read before it.
		for li < len(left) && left[li].BBox.Y1 > s.BBox.Y1 {
			out = append(out, left[li])
			li++
		}
		for ri < len(right) && right[ri].BBox.Y1 > s.BBox.Y1 {
			out = append(out, right[ri])
			ri++
		}
		out = append(out, s)
	}

	out = append(out, left[li:]...)
	out = append(out, right[ri:]...)
	return out
}

// indentTolerance is how far right of a column's margin a line may start and
// still be part of the column's header. Indented bodies — bulleted duties,
// list items — start further in than this.
const indentTolerance = 8

// stripHeader reports whether the left column begins with a header run: the
// leading lines that start at the column's left margin. It returns the
// number of lines in the run.
//
// The header is what a margin annotation belongs to. A résumé entry's title
// and employer sit at the margin; its duties are indented below them; the
// dates beside the entry read between the two.
func stripHeader(left []idm.Block) (int, bool) {
	lines := linesOf(left)
	if len(lines) == 0 {
		return 0, false
	}

	margin := left[0].BBox.X0
	for _, b := range left[1:] {
		margin = math.Min(margin, b.BBox.X0)
	}

	header := 0
	for _, line := range lines {
		if line[0].BBox.X0 > margin+indentTolerance {
			break
		}
		header++
	}
	if header == 0 {
		return 0, false
	}
	return header, true
}

// orderWithStrip emits the left column's header, then the right-hand strip
// that annotates it, then the body: the indented left lines and the spanning
// blocks, in reading order.
func orderWithStrip(left, right, spanning []idm.Block, header int) []idm.Block {
	var out []idm.Block

	// Spanning blocks above the header come first.
	top := left[0].BBox.Y1
	var below []idm.Block
	for _, s := range spanning {
		if s.BBox.Y1 > top {
			out = append(out, s)
		} else {
			below = append(below, s)
		}
	}

	out = append(out, left[:header]...)
	out = append(out, right...)

	body := left[header:]
	li := 0
	for _, s := range below {
		for li < len(body) && body[li].BBox.Y1 > s.BBox.Y1 {
			out = append(out, body[li])
			li++
		}
		out = append(out, s)
	}
	out = append(out, body[li:]...)
	return out
}

// singleColumn marks blocks as one column.
func singleColumn(blocks []idm.Block) []idm.Block {
	for i := range blocks {
		blocks[i].Column = 0
	}
	return blocks
}

// lineOrder orders blocks by line: lines top-down, and left to right within a
// line. Blocks on one line do not share a baseline exactly — a label and the
// value beside it sit a fraction of a point apart — so a plain top-down sort
// interleaves them by whichever is a hair higher. Line grouping reads them
// the way the page shows them.
func lineOrder(blocks []idm.Block) []idm.Block {
	var out []idm.Block
	for _, line := range linesOf(blocks) {
		sorted := append([]idm.Block(nil), line...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].BBox.X0 < sorted[j].BBox.X0
		})
		out = append(out, sorted...)
	}
	return out
}

// linesOf groups blocks whose vertical extents overlap into lines, top-down.
// Overlap chains: three blocks that each overlap a neighbour are one line
// even where the first and last do not overlap each other.
func linesOf(blocks []idm.Block) [][]idm.Block {
	sorted := append([]idm.Block(nil), blocks...)
	sortTopDown(sorted)

	var lines [][]idm.Block
	var cur []idm.Block
	bottom := 0.0

	for _, b := range sorted {
		if len(cur) > 0 && b.BBox.Y1 < bottom {
			lines = append(lines, cur)
			cur = nil
		}
		if len(cur) == 0 {
			bottom = b.BBox.Y0
		} else if b.BBox.Y0 < bottom {
			bottom = b.BBox.Y0
		}
		cur = append(cur, b)
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// lineRange is a line's vertical extent.
func lineRange(line []idm.Block) (bottom, top float64) {
	bottom, top = line[0].BBox.Y0, line[0].BBox.Y1
	for _, b := range line[1:] {
		bottom = math.Min(bottom, b.BBox.Y0)
		top = math.Max(top, b.BBox.Y1)
	}
	return bottom, top
}

func sortTopDown(blocks []idm.Block) {
	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].BBox.Y1 != blocks[j].BBox.Y1 {
			return blocks[i].BBox.Y1 > blocks[j].BBox.Y1
		}
		return blocks[i].BBox.X0 < blocks[j].BBox.X0
	})
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// furnitureZone is the fraction of the page height at the top and bottom where
// a running header or footer can live. Five percent is a header at 95% of the
// page; anything further in is body text, and treating it as furniture silently
// deletes content.
const furnitureZone = 0.95

// markFurniture flags running headers and footers in place.
//
// A block is furniture when it sits at the top or bottom of the page and its
// text, with digits masked, repeats on at least half the pages. Masking is what
// makes page numbers match the same pattern on every page.
func markFurniture(byPage [][]idm.Block, pages []idm.Page) {
	if len(pages) < 3 {
		return
	}

	type key struct {
		top  bool
		text string
	}

	seen := make(map[key]map[int]bool)
	threshold := (len(pages) + 1) / 2

	for pageIndex, blocks := range byPage {
		page := pages[pageIndex]
		for _, b := range blocks {
			if b.Kind.IsFurniture() || b.BBox.Height() <= 0 {
				continue
			}

			top := b.BBox.Y1 >= furnitureZone*page.Height
			bottom := b.BBox.Y0 <= (1-furnitureZone)*page.Height
			if !top && !bottom {
				continue
			}

			k := key{top: top, text: maskDigits(b.Text)}
			if seen[k] == nil {
				seen[k] = make(map[int]bool)
			}
			seen[k][pageIndex] = true
		}
	}

	repeated := make(map[key]bool, len(seen))
	for k, pageSet := range seen {
		if len(pageSet) >= threshold || hasConsecutiveRun(pageSet, furnitureRun) {
			repeated[k] = true
		}
	}
	if len(repeated) == 0 {
		return
	}

	for pageIndex, blocks := range byPage {
		page := pages[pageIndex]
		for i := range blocks {
			b := &blocks[i]
			if b.Kind.IsFurniture() || b.BBox.Height() <= 0 {
				continue
			}

			top := b.BBox.Y1 >= furnitureZone*page.Height
			bottom := b.BBox.Y0 <= (1-furnitureZone)*page.Height
			if !top && !bottom {
				continue
			}

			if repeated[key{top: top, text: maskDigits(b.Text)}] {
				if top {
					b.Kind = idm.KindHeader
				} else {
					b.Kind = idm.KindFooter
				}
			}
		}
	}
}

// furnitureRun is the number of consecutive pages at whose edge the same text
// makes it running furniture, even when it is not the book's majority form.
//
// A producer can change the header or footer partway through, or put it in
// two blocks for the first chapter and one block after that. A global "half
// the pages" test then leaves the minority variant in the text: one real book
// kept its footer on the first eight pages, where the page number and title
// were separate blocks. Text repeated at the page edge on consecutive pages
// is still repetition.
const furnitureRun = 3

// hasConsecutiveRun reports whether the page set contains a run of at least
// n consecutive page indices.
func hasConsecutiveRun(pages map[int]bool, n int) bool {
	if n <= 1 || len(pages) < n {
		return false
	}

	last := 0
	for page := range pages {
		if page > last {
			last = page
		}
	}

	count := 0
	for page := 0; page <= last; page++ {
		if !pages[page] {
			count = 0
			continue
		}
		count++
		if count >= n {
			return true
		}
	}
	return false
}

// maskDigits replaces digit runs with a placeholder so that "Page 42" and
// "Page 43" are recognised as the same running header.
func maskDigits(s string) string {
	var b strings.Builder
	inDigits := false

	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
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

	return strings.Join(strings.Fields(b.String()), " ")
}
