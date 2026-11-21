package tables

import (
	"testing"

	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/idm"
)

func pg() idm.Page {
	return idm.Page{Number: 1, Width: 595, Height: 842, Provenance: idm.ProvenanceDigital}
}

func w(text string, x0, y0, x1, y1 float64) idm.Word {
	return idm.Word{Text: text, Box: idm.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1}}
}

func hrule(y, x0, x1 float64) Rule {
	return Rule{Horizontal: true, Y: y, X0: x0, X1: x1}
}

// frameRules match the invoice's main table: a frame at 268 and 681, with
// doubled lines, and split header rules that deliberately do NOT match the
// frame — so only the frame forms a region, and the interior grid must come
// from the words.
func frameRules() []Rule {
	return []Rule{
		hrule(266.9, 45, 549.3), hrule(268.4, 45, 549.3),
		hrule(680.7, 45, 549.3), hrule(682.2, 45, 549.3),
		// Split header separators: a different x extent on purpose.
		hrule(281.9, 45, 166.3), hrule(283.4, 166.3, 388.1), hrule(283.4, 458.6, 549.3),
	}
}

func TestBuildTablesRecoversAGridFromTheFrame(t *testing.T) {
	// Top-down: PDF y grows up, so the header row nearest the top rule at
	// 680.7 has the LARGEST y, and later rows sit lower.
	words := []idm.Word{
		// Header row.
		w("Date", 50, 660, 100, 670), w("Task", 170, 660, 210, 670),
		w("Hours", 400, 660, 440, 670), w("Total", 470, 660, 540, 670),
		// Two body rows.
		w("8 Oct", 50, 620, 95, 630), w("Meeting", 170, 620, 240, 630),
		w("1", 405, 620, 415, 630), w("1,500.00", 470, 620, 540, 630),
		w("16 Oct", 50, 580, 95, 590), w("Review", 170, 580, 230, 590),
		w("1", 405, 580, 415, 590), w("2,000.00", 470, 580, 540, 590),
	}

	tables := BuildTables(frameRules(), words, pg())
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}

	table := tables[0]
	if table.RowCount() != 3 {
		t.Errorf("rows = %d, want 3 (header + two body rows):\n%v", table.RowCount(), table.Cells)
	}
	if table.ColumnCount() != 4 {
		t.Errorf("columns = %d, want 4:\n%v", table.ColumnCount(), table.Cells)
	}

	if got, want := table.Cells[0][0], "Date"; got != want {
		t.Errorf("header[0] = %q, want %q", got, want)
	}
	if got, want := table.Cells[1][1], "Meeting"; got != want {
		t.Errorf("row1[1] = %q, want %q", got, want)
	}
	if got, want := table.Cells[2][3], "2,000.00"; got != want {
		t.Errorf("row2[3] = %q, want %q", got, want)
	}
	if got, want := table.Cells[1][2], "1"; got != want {
		t.Errorf("row1[2] = %q, want %q (Hours column)", got, want)
	}
}

func TestBuildTablesJoinsWordsWithinACell(t *testing.T) {
	words := []idm.Word{
		w("Date", 50, 660, 100, 670), w("Task", 170, 660, 210, 670),
		w("Internal", 170, 620, 230, 630), w("meeting", 240, 620, 300, 630),
		w("1", 50, 620, 55, 630), w("x", 400, 620, 410, 630),
		w("3", 50, 580, 55, 590), w("4", 170, 580, 175, 590),
	}

	tables := BuildTables(frameRules(), words, pg())
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	if got, want := tables[0].Cells[1][1], "Internal meeting"; got != want {
		t.Errorf("cell = %q, want %q", got, want)
	}
}

func TestBuildTablesDropsATwoRowRegion(t *testing.T) {
	// The invoice's totals box: a frame at 727 and 759 with four words in two
	// visual lines, but only one clustered row of words per line pair...
	// Two lines of words IS two rows; what must be dropped is a region whose
	// words form one row or one column.
	rules := []Rule{
		hrule(727.2, 317.7, 549.3), hrule(728.7, 317.7, 549.3),
		hrule(758.7, 317.7, 549.3), hrule(760.2, 317.7, 549.3),
	}

	// A single line of text inside: one row, no grid.
	words := []idm.Word{w("Total", 330, 741, 380, 749), w("15,000.00", 460, 741, 540, 749)}

	if tables := BuildTables(rules, words, pg()); len(tables) != 0 {
		t.Errorf("one row of words produced %d tables, want 0", len(tables))
	}

	// Two lines: a 2x2 totals table, which is real and should be recovered.
	// A second line well below the first: a real 2x2 totals table. The gap
	// must exceed one leading (up to 1.6 text heights), as a wrapped line
	// inside one cell would not.
	words = append(words,
		w("Sub", 330, 725, 360, 733), w("5,000.00", 460, 725, 540, 733),
	)
	tables := BuildTables(rules, words, pg())
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	if tables[0].RowCount() != 2 || tables[0].ColumnCount() != 2 {
		t.Errorf("grid = %dx%d, want 2x2", tables[0].RowCount(), tables[0].ColumnCount())
	}
}

func TestBuildTablesIgnoresASingleUnderline(t *testing.T) {
	rules := []Rule{hrule(500, 72, 523), hrule(501.5, 72, 523)}
	words := []idm.Word{w("Heading", 100, 480, 200, 492)}

	if tables := BuildTables(rules, words, pg()); len(tables) != 0 {
		t.Errorf("an underline produced %d tables", len(tables))
	}
}

func TestBuildTablesSplitsARegionAtContentChange(t *testing.T) {
	// The invoice's big frame: address prose at the top, the main table
	// below, and no rule drawn between them. The split must come from the
	// rows' shape.
	rules := []Rule{
		hrule(680.7, 45, 549.3), hrule(682.2, 45, 549.3),
		hrule(281.9, 45, 549.3), hrule(283.4, 45, 549.3),
	}

	// Prose: single-column lines, top of the region.
	words := []idm.Word{
		w("INVOICE", 60, 660, 130, 670), w("TO", 140, 660, 165, 670),
		w("Artin", 60, 630, 110, 640), w("Ltd", 120, 630, 150, 640),
		w("26/7A", 60, 600, 100, 610), w("Lane", 110, 600, 150, 610),
	}
	// Table: multi-column rows, all starting at the same left edge.
	for r, y := range []float64{546, 505, 462, 420, 376, 333} {
		_ = r
		words = append(words,
			w("8 Oct", 50, y, 95, y+10),
			w("Meeting", 170, y, 240, y+10),
			w("1", 320, y, 330, y+10),
			w("1,500.00", 450, y, 540, y+10),
		)
	}

	tables := BuildTables(rules, words, pg())
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1 (the prose must not become one): %v", len(tables), tables)
	}

	table := tables[0]
	if got, want := table.Cells[0][0], "8 Oct"; got != want {
		t.Errorf("first cell = %q, want %q", got, want)
	}
	// Six data rows: the fixture's "header" row is one of the identical
	// data rows, so the grid is 6 rows, not 7.
	if table.RowCount() != 6 {
		t.Errorf("rows = %d, want 6", table.RowCount())
	}
}

func TestBuildTablesRejectsCentredTitleOverByline(t *testing.T) {
	// A wrapped title centred above a centred byline: two visual columns that
	// do not repeat. Emitted as a table it eats the slide's title.
	rules := []Rule{
		hrule(268, 175, 542), hrule(60, 175, 542),
	}

	words := []idm.Word{
		w("with", 175, 209, 264, 259),
		w("Amazon", 282, 209, 445, 259),
		w("EKS", 466, 209, 542, 259),
		w("Faisal", 202, 68, 270, 105),
		w("Memon", 283, 68, 373, 105),
		w("(EmbarkX)", 386, 68, 516, 105),
	}

	if tables := BuildTables(rules, words, pg()); len(tables) != 0 {
		t.Errorf("centred lines produced %d tables, want 0", len(tables))
	}
}

func TestWordsOutsideTheRegionAreExcluded(t *testing.T) {
	words := []idm.Word{
		w("Date", 50, 660, 100, 670), w("Task", 170, 660, 210, 670),
		w("x", 50, 620, 60, 630), w("y", 170, 620, 180, 630),
		// Above the top rule: body text.
		w("outside", 100, 700, 200, 710),
		// Beyond the right edge.
		w("beyond", 560, 620, 640, 630),
	}

	tables := BuildTables(frameRules(), words, pg())
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	for _, row := range tables[0].Cells {
		for _, cell := range row {
			if cell == "outside" || cell == "beyond" {
				t.Errorf("word outside the region landed in the grid: %v", tables[0].Cells)
			}
		}
	}
}
