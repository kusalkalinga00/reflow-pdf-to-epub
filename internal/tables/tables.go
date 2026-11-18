// Package tables detects ruled tables and rebuilds them as grids.
//
// A ruled table is the honest case: the producer drew the grid, so the
// structure is in the file. Horizontal rules become row boundaries, the
// vertical edges shared by several rules become columns, and every word lands
// in exactly one cell.
package tables

import (
	"math"
	"sort"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// Rule is one drawn line segment.
//
// For a horizontal rule, Y is the line's y position and X0..X1 its extent.
// For a vertical rule, X0 is the line's x position (X1 equals it) and
// Y0..Y1 its extent.
type Rule struct {
	Horizontal bool
	Y          float64
	Y0, Y1     float64
	X0, X1     float64
	// Page is the 1-based source page the rule was drawn on; extraction
	// fills it so callers can group rules by page.
	Page int
}

// MinHRule and MinVRule drop decoration: underlines and borders that are not
// a grid. They are exported because extraction filters with the same
// thresholds, so the rule set is not built and then thrown away.
const (
	MinHRule = 80
	MinVRule = 12
)

// Detect returns the tables found among the rules, or nil.
func Detect(rules []Rule, words []idm.Word, page idm.Page) []Table {
	hRules, vRules := longRules(rules)

	grouped := bands(hRules)
	if len(grouped) == 0 {
		return nil
	}

	var out []Table
	for _, band := range grouped {
		rows, cols := gridOf(band, vRules)
		if len(rows) < 2 || len(cols) < 2 {
			continue
		}

		t := Table{Top: rows[0], Bottom: rows[len(rows)-1], Left: cols[0], Right: cols[len(cols)-1]}
		t.Cells = fill(rows, cols, words, page.Number)
		out = append(out, t)
	}

	// Reading order: top of the page first (y grows up in PDF space).
	sort.SliceStable(out, func(i, j int) bool { return out[i].Top > out[j].Top })

	return out
}

func longRules(rules []Rule) (horizontal, vertical []Rule) {
	for _, r := range rules {
		switch {
		case r.Horizontal && r.X1-r.X0 >= MinHRule:
			horizontal = append(horizontal, r)
		case !r.Horizontal && r.Y1-r.Y0 >= MinVRule:
			vertical = append(vertical, r)
		}
	}
	return horizontal, vertical
}

// bands groups horizontal rules whose x ranges match closely. Strict matching
// is deliberate: a frame-ruled table whose row separators are drawn as split
// segments does not match its own frame, so it is reported as undetected
// rather than reconstructed with wrong cells. Simple fully-ruled grids — the
// common case in invoices and forms — are matched exactly.
func bands(rules []Rule) [][]Rule {
	// Descending y (largest first): tables are read top-down everywhere else.
	sorted := append([]Rule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Y > sorted[j].Y })

	var out [][]Rule
	for _, r := range sorted {
		placed := false
		for i := len(out) - 1; i >= 0 && !placed; i-- {
			if matches(out[i], r) {
				out[i] = append(out[i], r)
				placed = true
			}
		}
		if !placed {
			out = append(out, []Rule{r})
		}
	}

	var tables [][]Rule
	for _, band := range out {
		if len(band) >= 2 {
			tables = append(tables, band)
		}
	}
	return tables
}

// matches reports whether a rule has essentially the same x extent as the
// band: each must cover most of the other. Doubled lines (1.5pt pairs) and
// small crop differences are within the tolerance.
func matches(band []Rule, r Rule) bool {
	x0, x1 := bandRange(band)
	width := x1 - x0
	if width <= 0 {
		return false
	}

	cover := math.Min(r.X1, x1) - math.Max(r.X0, x0)
	return cover >= width*0.8 && cover >= (r.X1-r.X0)*0.8
}

// bandRange returns the widest x extent of a band.
func bandRange(band []Rule) (float64, float64) {
	x0, x1 := band[0].X0, band[0].X1
	for _, r := range band[1:] {
		x0 = math.Min(x0, r.X0)
		x1 = math.Max(x1, r.X1)
	}
	return x0, x1
}

// gridOf returns the row y positions and column x positions of the band.
//
// Rows are the distinct rule y values; the doubled lines some producers draw
// (a 1.5pt pair) are merged. Columns come from vertical rules whose span
// covers most of the band, deduplicated to distinct x positions.
func gridOf(band, vRules []Rule) (rows, cols []float64) {
	var ys []float64
	for _, r := range band {
		ys = append(ys, r.Y)
	}
	sort.Float64s(ys)

	// y values within 2pt are the same rule drawn twice.
	prev := math.Inf(-1)
	for _, y := range ys {
		if y-prev <= 2 {
			continue
		}
		rows = append(rows, y)
		prev = y
	}

	left, right := bandRange(band)
	top, bottom := band[0].Y, band[len(band)-1].Y

	var xs []float64
	for _, v := range vRules {
		// A column edge must span most of the band's height and sit inside
		// its x range.
		if v.X0 < left-5 || v.X0 > right+5 {
			continue
		}
		// A column edge must overlap the band substantially, but it does not
		// need to span it: interior columns often start below the header rule.
		if v.Y1 < top+(bottom-top)*0.25 || v.Y0 > bottom-(bottom-top)*0.25 {
			continue
		}
		xs = append(xs, v.X0)
	}

	// Rules that start or stop mid-band mark column edges just as well as a
	// drawn vertical: a producer that rules each cell of a header row has told
	// us where the columns are.
	for _, r := range band {
		if r.X0 > left+5 {
			xs = append(xs, r.X0)
		}
		if r.X1 < right-5 {
			xs = append(xs, r.X1)
		}
	}

	sort.Float64s(xs)

	prev = math.Inf(-1)
	for _, x := range xs {
		if x-prev <= 3 {
			continue
		}
		cols = append(cols, x)
		prev = x
	}

	// The grid needs both edges even when the producer drew only one.
	if len(cols) > 0 {
		if !containsApprox(cols, left) {
			cols = append([]float64{left}, cols...)
		}
		if !containsApprox(cols, right) {
			cols = append(cols, right)
		}
	}

	return rows, cols
}

func containsApprox(values []float64, target float64) bool {
	for _, v := range values {
		if math.Abs(v-target) <= 5 {
			return true
		}
	}
	return false
}

// fill places each word in its cell. A word belongs to the row whose band
// contains its baseline and the column whose band contains its centre.
func fill(rows, cols []float64, words []idm.Word, page int) [][]string {
	cells := make([][]string, len(rows)-1)
	for i := range cells {
		cells[i] = make([]string, len(cols)-1)
	}

	for _, w := range words {
		// rows descends (y grows up in PDF space): cell i is bounded below by
		// rows[i] and above by rows[i+1]. A word belongs to cell i when its
		// top is below the upper rule and its bottom above the lower one.
		row := -1
		for i := 0; i < len(rows)-1; i++ {
			if w.Box.Y1 <= rows[i+1]+2 && w.Box.Y0 >= rows[i]-2 {
				row = i
				break
			}
		}
		if row < 0 {
			continue
		}

		col := -1
		centre := (w.Box.X0 + w.Box.X1) / 2
		for j := 0; j < len(cols)-1; j++ {
			if centre >= cols[j]-2 && centre < cols[j+1] {
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

// Table is a ruled table with its cells.
type Table struct {
	Left, Right, Top, Bottom float64

	// Cells[r][c] is the text of row r, column c. Empty cells are empty
	// strings.
	Cells [][]string `json:"cells"`
}

func (t Table) empty() bool {
	for _, row := range t.Cells {
		for _, cell := range row {
			if cell != "" {
				return false
			}
		}
	}
	return true
}

// RowCount and ColumnCount report the grid shape.
func (t Table) RowCount() int { return len(t.Cells) }

func (t Table) ColumnCount() int {
	if len(t.Cells) == 0 {
		return 0
	}
	return len(t.Cells[0])
}

// LongRules exposes the rule filter for diagnostics.
func LongRules(rules []Rule) (horizontal, vertical []Rule) {
	return longRules(rules)
}
