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
	for _, reg := range regions(hRules) {
		inside := wordsWithin(words, reg)
		if len(inside) < 4 {
			// A table needs at least a header row and one data row.
			continue
		}

		rows := clusterRows(inside)
		cols := clusterColumns(inside)
		if len(rows) < 2 || len(cols) < 2 {
			continue
		}

		cells := placeWords(inside, rows, cols)

		// A ruled box that also holds prose — the invoice's big frame carries
		// an address block above its table — yields rows whose words sit in a
		// single column. A real table row has content in at least two columns,
		// so any such row means the region is a layout box, not a table.
		tableLike := true
		for _, row := range cells {
			filled := 0
			for _, cell := range row {
				if cell != "" {
					filled++
				}
			}
			if filled < 2 {
				tableLike = false
				break
			}
		}
		if !tableLike {
			continue
		}

		t := Table{
			Top:    reg.top,
			Bottom: reg.bottom,
			Left:   cols[0][0],
			Right:  cols[len(cols)-1][1],
			Cells:  cells,
		}
		out = append(out, t)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Top > out[j].Top })
	return out
}

// regions turns rule bands into areas that may contain a table.
//
// Bands come from the strict matcher, so a band of matching rules is the
// frame of one structure. Doubled lines collapse to one boundary, and a
// band that leaves fewer than two distinct boundaries is an underline.
func regions(hRules []Rule) []region {
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
