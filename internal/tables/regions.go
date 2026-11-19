package tables

import (
	"math"
	"sort"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// A region is the area between two or more horizontal rules that share an x
// extent. Regions are where a table can exist; the rows and columns inside
// come from the words, because producers rule the frame but not every cell.
type region struct {
	top, bottom float64
	left, right float64
}

// BuildTables recovers tables from ruled regions.
//
// For each region, the words inside are clustered into rows by baseline and
// into columns by overlapping x extents, and each word lands in one cell.
// This is the honest recovery for frame-ruled tables: the frame is drawn, the
// interior is not, so the words are the only witness of the grid inside.
//
// A region with fewer than two clustered rows or two columns is not a table
// and is dropped, which is what keeps ruled underlines and boxed totals
// labels out of the output.
func BuildTables(rules []Rule, words []idm.Word, page idm.Page) []Table {
	hRules, _ := longRules(rules)

	var out []Table
	for _, reg := range regions(hRules, words) {
		out = append(out, tablesIn(reg, hRules, words, 0)...)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Top > out[j].Top })
	return out
}

// tablesIn returns the tables inside a region, splitting it as often as it
// takes. Recursion matters: a layout box fails as a table, its content split
// yields an address block and a table, and a rule inside that table (the
// invoice rules its 6 November row as a band of its own) needs a further
// split.
func tablesIn(reg region, hRules []Rule, words []idm.Word, depth int) []Table {
	if t, ok := buildTable(words, reg); ok {
		return []Table{t}
	}
	if depth >= 3 {
		return nil
	}

	var out []Table
	for _, sub := range subRegions(reg, hRules, words) {
		out = append(out, tablesIn(sub, hRules, words, depth+1)...)
	}
	return out
}

// buildTable recovers one region's grid, applying the checks that keep
// non-tables out.
func buildTable(words []idm.Word, reg region) (Table, bool) {
	inside := wordsWithin(words, reg)
	if len(inside) < 4 {
		// A table needs at least a header row and one data row.
		return Table{}, false
	}

	rows := clusterRows(inside)
	cols := clusterColumns(inside)
	if len(rows) < 2 || len(cols) < 2 {
		return Table{}, false
	}

	cells := placeWords(inside, rows, cols)

	// A ruled box that also holds prose — the invoice's big frame carries an
	// address block above its table — yields rows whose words sit in a single
	// column. A real table row has content in at least two columns, so any
	// such row means the region is a layout box, not a table.
	for _, row := range cells {
		filled := 0
		for _, cell := range row {
			if cell != "" {
				filled++
			}
		}
		if filled < 2 {
			return Table{}, false
		}
	}

	// A real table's rows share column boundaries: the words in row r start
	// and end at (nearly) the same x as the words in row r+1. A wrapped title
	// above a byline never does — "with Amazon EKS" and "Faisal Memon" are
	// two visual columns that do not repeat — and emitting them as a table
	// eats the slide's title.
	if !columnsAlign(inside, rows) {
		return Table{}, false
	}

	return Table{
		Top:    reg.top,
		Bottom: reg.bottom,
		Left:   cols[0][0],
		Right:  cols[len(cols)-1][1],
		Cells:  cells,
	}, true
}

// columnsAlign reports whether the rows form a consistent grid.
//
// A table's rows cluster into the same number of columns under the same x
// separation, and start in the same place. Two centred lines — a wrapped
// title over a byline — do not: their word gaps differ, so the rows cluster
// into different column counts, and emitting them as a table eats the slide's
// title.
func columnsAlign(words []idm.Word, rows [][2]float64) bool {
	var rowColumns []int
	var leftmost []float64
	for _, row := range rows {
		rowWordsList := rowWords(words, row)
		if len(rowWordsList) == 0 {
			continue
		}
		rowColumns = append(rowColumns, columnCount(rowWordsList))
		if centres := wordCentres(rowWordsList); len(centres) > 0 {
			leftmost = append(leftmost, centres[0])
		}
	}
	if len(rowColumns) < 2 {
		return false
	}

	// Every row must itself cluster into at least two columns. A row that
	// clusters as one — a byline whose words sit close together — means the
	// region holds centred lines, not a grid. (Rows may differ in count: a
	// wrapped cell adds a column to one row.)
	for _, n := range rowColumns {
		if n < 2 {
			return false
		}
	}

	// And the first column must line up.
	const tolerance = 60
	for _, v := range leftmost[1:] {
		if math.Abs(v-leftmost[0]) > tolerance {
			return false
		}
	}
	return true
}

// wordCentres returns x centres in left-to-right order. Word lists arrive in
// content-stream order, which is not reading order.
func wordCentres(words []idm.Word) []float64 {
	sorted := append([]idm.Word(nil), words...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Box.X0 < sorted[j].Box.X0 })

	out := make([]float64, len(sorted))
	for i, w := range sorted {
		out[i] = (w.Box.X0 + w.Box.X1) / 2
	}
	return out
}

// subRegions splits a non-table region where its content changes shape.
//
// Two signals, in order of trust:
//
//  1. Interior rules: a rule strictly inside the region, spanning most of its
//     width, is a drawn boundary between two structures.
//  2. Column consistency: a layout box that holds prose above a table shows a
//     sharp change — prose rows fill one column, table rows fill several. The
//     boundary is where the fill changes, and no rule needs to be drawn there
//     (the invoice's table top is unruled).
//
// Both yield candidate sub-regions; the caller tests each with the same
// table checks.
func subRegions(reg region, hRules []Rule, words []idm.Word) []region {
	var interior []float64
	for _, r := range hRules {
		if r.Y >= reg.top-2 || r.Y <= reg.bottom+2 {
			continue
		}
		// Must overlap the region's x range substantially.
		cover := math.Min(r.X1, reg.right) - math.Max(r.X0, reg.left)
		if cover < (reg.right-reg.left)*0.5 {
			continue
		}
		interior = append(interior, r.Y)
	}

	sort.Float64s(interior)

	// Deduplicate doubled lines.
	var distinct []float64
	for _, y := range interior {
		if len(distinct) == 0 || y-distinct[len(distinct)-1] > 2 {
			distinct = append(distinct, y)
		}
	}

	// Sub-regions run from each boundary down to the next. Top-down order.
	bounds := append([]float64{reg.top}, distinct...)
	bounds = append(bounds, reg.bottom)

	var out []region
	for i := 0; i < len(bounds)-1; i++ {
		top, bottom := bounds[i], bounds[i+1]
		if top-bottom < 12 {
			// Too thin to hold a row.
			continue
		}
		out = append(out, region{top: top, bottom: bottom, left: reg.left, right: reg.right})
	}

	// Content split: the boundary where rows stop filling one column and
	// start filling several. Only applied when the rules drew no boundary —
	// a single sub-region spanning the whole region is not a split.
	if len(out) <= 1 {
		if cut, ok := fillChange(words, reg); ok {
			if reg.top-cut >= 12 && cut-reg.bottom >= 12 {
				// Replace the whole-region placeholder; keeping it would make
				// the caller try the unsplittable region again.
				out = out[:0]
				out = append(out,
					region{top: reg.top, bottom: cut, left: reg.left, right: reg.right},
					region{top: cut, bottom: reg.bottom, left: reg.left, right: reg.right},
				)
			}
		}
	}

	return out
}

// fillChange finds where a region's rows change from filling one column to
// filling several.
//
// A single two-column line is not a table (a date beside a company name reads
// as two columns). The signal that holds is a RUN: three or more consecutive
// rows with multiple columns, whose column positions repeat. The cut is the
// top of that run.
func fillChange(words []idm.Word, reg region) (float64, bool) {
	inside := wordsWithin(words, reg)
	if len(inside) < 6 {
		return 0, false
	}

	rows := clusterRows(inside)
	if len(rows) < 3 {
		return 0, false
	}

	// Row index of the first row of the longest multi-column run.
	//
	// A row only counts as multi-column when its words also span most of the
	// region's width: a title and a byline are two visual columns, but they
	// sit in the left half of the slide, and turning them into a table eats
	// the title. Real table rows reach across the region.
	width := reg.right - reg.left
	bestStart, bestLen := -1, 0
	runStart := -1
	for i := range rows {
		rowWordsList := rowWords(inside, rows[i])
		multi := columnCount(rowWordsList) >= 2 && rowSpan(rowWordsList) >= width*0.7
		if multi {
			if runStart < 0 {
				runStart = i
			}
			if length := i - runStart + 1; length > bestLen {
				bestStart, bestLen = runStart, length
			}
		} else {
			runStart = -1
		}
	}

	// A table needs a header row and at least two data rows.
	if bestStart < 0 || bestLen < 3 {
		return 0, false
	}

	// The run must not already start at the region's top: then the whole
	// region is the table and the caller would have accepted it.
	if bestStart == 0 {
		return 0, false
	}

	// Cut between the row above the run and the run's first row.
	above, first := rows[bestStart-1], rows[bestStart]
	return (above[0] + first[1]) / 2, true
}

// rowSpan is the x distance from a row's first word to its last.
func rowSpan(words []idm.Word) float64 {
	if len(words) == 0 {
		return 0
	}
	x0, x1 := words[0].Box.X0, words[0].Box.X1
	for _, w := range words[1:] {
		if w.Box.X0 < x0 {
			x0 = w.Box.X0
		}
		if w.Box.X1 > x1 {
			x1 = w.Box.X1
		}
	}
	return x1 - x0
}

// rowWords returns the words whose baseline sits in the row band.
func rowWords(words []idm.Word, row [2]float64) []idm.Word {
	var out []idm.Word
	for _, w := range words {
		if w.Box.Y0 >= row[0]-1 && w.Box.Y0 <= row[1]+1 {
			out = append(out, w)
		}
	}
	return out
}

// columnCount clusters a row's words into columns by x separation.
func columnCount(words []idm.Word) int {
	if len(words) < 2 {
		return len(words)
	}
	sorted := append([]idm.Word(nil), words...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Box.X0 < sorted[j].Box.X0 })

	columns := 1
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Box.X0 > sorted[i-1].Box.X1+18 {
			columns++
		}
	}
	return columns
}

// regions turns rule bands into areas that may contain a table.
//
// Bands come from the strict matcher, so a band of matching rules is the
// frame of one structure. Doubled lines collapse to one boundary, and a band
// that leaves fewer than two distinct boundaries is an underline.
func regions(hRules []Rule, words []idm.Word) []region {
	var out []region
	for _, band := range bands(hRules) {
		ys := make([]float64, 0, len(band))
		for _, r := range band {
			ys = append(ys, r.Y)
		}
		sort.Float64s(ys)

		var bounds []float64
		for _, y := range ys {
			if len(bounds) == 0 || y-bounds[len(bounds)-1] > 2 {
				bounds = append(bounds, y)
			}
		}
		if len(bounds) < 2 {
			continue
		}

		x0, x1 := bandRange(band)
		reg := region{
			top: bounds[len(bounds)-1], bottom: bounds[0],
			left: x0, right: x1,
		}

		// A frame drawn in segments yields one band per segment, all sharing
		// the same row boundaries. They are one region: the union of their
		// x extents. Without this, each column becomes its own "table" and
		// picks up whatever text shares its vertical band.
		merged := false
		for i := range out {
			if math.Abs(out[i].top-reg.top) <= 2 && math.Abs(out[i].bottom-reg.bottom) <= 2 {
				out[i].left = math.Min(out[i].left, reg.left)
				out[i].right = math.Max(out[i].right, reg.right)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, reg)
		}
	}
	return mergeStacked(out, words)
}

// mergeStacked joins regions that are vertically adjacent and hold words in
// the gap between them.
//
// A producer can rule each row of a long table as its own band. The invoice
// draws its 6 November row in a 15pt band of its own, between the band of the
// rows above and the band of the rows below; none of the three is a table on
// its own. Words living in the gap are the evidence that the bands are one
// structure rather than two tables with a caption between them.
func mergeStacked(regs []region, words []idm.Word) []region {
	sort.SliceStable(regs, func(i, j int) bool { return regs[i].top > regs[j].top })

	out := make([]region, 0, len(regs))
	for _, reg := range regs {
		merged := false
		for i := range out {
			upper := &out[i]
			if math.Abs(upper.left-reg.left) > 2 || math.Abs(upper.right-reg.right) > 2 {
				continue
			}
			gap := upper.bottom - reg.top
			if gap < 0 || gap > 24 {
				continue
			}
			gapRegion := region{top: upper.bottom, bottom: reg.top, left: reg.left, right: reg.right}
			if len(wordsWithin(words, gapRegion)) == 0 {
				continue
			}
			upper.bottom = reg.bottom
			merged = true
			break
		}
		if !merged {
			out = append(out, reg)
		}
	}
	return out
}

// wordsWithin returns the words whose centre sits inside the region.
//
// Containment by centre, not by box: real cell text grazes the rule it
// belongs to — a baseline close to the bottom rule still belongs inside —
// while text outside the region is centred well clear of it.
func wordsWithin(words []idm.Word, reg region) []idm.Word {
	var out []idm.Word
	for _, w := range words {
		cy := (w.Box.Y0 + w.Box.Y1) / 2
		if cy > reg.bottom && cy < reg.top && w.Box.X0 >= reg.left-2 && w.Box.X1 <= reg.right+2 {
			out = append(out, w)
		}
	}
	return out
}

// clusterRows groups words into rows by baseline.
//
// Words are sorted by their box bottoms (the baseline region). A gap larger
// than one text height starts a new row; smaller gaps are leading.
func clusterRows(words []idm.Word) [][2]float64 {
	sorted := append([]idm.Word(nil), words...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Box.Y0 != sorted[j].Box.Y0 {
			return sorted[i].Box.Y0 > sorted[j].Box.Y0
		}
		return sorted[i].Box.X0 < sorted[j].Box.X0
	})

	// Leading on real documents runs up to about 1.6 text heights; a wrapped
	// value line inside a cell is one row, not two, and splitting it loses the
	// label association entirely.
	height := medianHeight(sorted)
	gap := math.Max(1.6*height, 6)

	var rows [][2]float64
	for _, w := range sorted {
		if len(rows) == 0 || rows[len(rows)-1][0]-w.Box.Y0 > gap {
			rows = append(rows, [2]float64{w.Box.Y0, w.Box.Y0})
			continue
		}
		r := &rows[len(rows)-1]
		if w.Box.Y0 < r[0] {
			r[0] = w.Box.Y0
		}
		if w.Box.Y0 > r[1] {
			r[1] = w.Box.Y0
		}
	}
	return rows
}

// medianHeight is the median word-box height, the scale of leading.
func medianHeight(words []idm.Word) float64 {
	if len(words) == 0 {
		return 0
	}
	heights := make([]float64, len(words))
	for i, w := range words {
		heights[i] = w.Box.Height()
	}
	sort.Float64s(heights)
	return heights[len(heights)/2]
}

// clusterColumns merges overlapping x extents into column bands.
//
// Words in the same column overlap or nearly touch across rows; a gap wider
// than the pad is a column boundary. The pad is generous because column text
// does not line up to the point.
func clusterColumns(words []idm.Word) [][2]float64 {
	type span struct{ x0, x1 float64 }
	spans := make([]span, len(words))
	for i, w := range words {
		spans[i] = span{w.Box.X0, w.Box.X1}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].x0 < spans[j].x0 })

	const pad = 12

	var cols [][2]float64
	for _, s := range spans {
		if len(cols) == 0 || s.x0 > cols[len(cols)-1][1]+pad {
			cols = append(cols, [2]float64{s.x0, s.x1})
			continue
		}
		if s.x1 > cols[len(cols)-1][1] {
			cols[len(cols)-1][1] = s.x1
		}
	}
	return cols
}

// placeWords fills the grid: a word goes to the row whose band contains its
// baseline and the column whose band contains its centre.
func placeWords(words []idm.Word, rows, cols [][2]float64) [][]string {
	cells := make([][]string, len(rows))
	for i := range cells {
		cells[i] = make([]string, len(cols))
	}

	for _, w := range words {
		row := -1
		for i, r := range rows {
			if w.Box.Y0 >= r[0]-1 && w.Box.Y0 <= r[1]+1 {
				row = i
				break
			}
		}
		if row < 0 {
			continue
		}

		centre := (w.Box.X0 + w.Box.X1) / 2
		col := -1
		for j, c := range cols {
			if centre >= c[0]-1 && centre <= c[1]+1 {
				col = j
				break
			}
		}
		if col < 0 {
			continue
		}

		if cells[row][col] != "" {
			cells[row][col] += " "
		}
		cells[row][col] += w.Text
	}

	return cells
}
