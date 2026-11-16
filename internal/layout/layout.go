// Package layout puts extracted blocks into reading order and removes page
// furniture.
//
// Extraction returns text in the order the PDF drew it, which is close to
// reading order for simple documents and wrong for anything with columns: a
// two-column page alternates between columns, line by line. This stage fixes
// the order and strips the running headers and footers that would otherwise
// appear in the middle of the text.
package layout

import (
	"sort"
	"strings"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
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

	start, end, ok := findGutter(body, page.Width)
	if !ok {
		sortTopDown(body)
		for i := range body {
			body[i].Column = 0
		}
		return append(body, furniture...)
	}

	ordered := orderColumns(body, start, end)
	return append(ordered, furniture...)
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
	var left, right, spanning []idm.Block

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

	sortTopDown(left)
	sortTopDown(right)
	sortTopDown(spanning)

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
		if len(pageSet) >= threshold {
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
