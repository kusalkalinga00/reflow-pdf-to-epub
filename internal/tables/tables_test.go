package tables

import (
	"reflect"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

func page() idm.Page {
	return idm.Page{Number: 1, Width: 595, Height: 842, Provenance: idm.ProvenanceDigital}
}

func word(text string, x0, y0, x1, y1 float64) idm.Word {
	return idm.Word{Text: text, Box: idm.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1}}
}

// invoice draws the grid the spike found on the real file: three columns at
// x = 45, 166, 388, 549 and rows at y = 268, 283, 318, 681.
func invoiceRules() []Rule {
	h := func(y, x0, x1 float64) Rule { return Rule{Horizontal: true, Y: y, X0: x0, X1: x1} }
	v := func(x, y0, y1 float64) Rule { return Rule{Horizontal: false, X0: x, X1: x, Y0: y0, Y1: y1} }

	return []Rule{
		h(268, 45, 549), h(283, 45, 549), h(318, 45, 549), h(681, 45, 549),
		v(45, 268, 681), v(166, 283, 681), v(388, 283, 681), v(549, 268, 681),
	}
}

func TestDetectRebuildsTheInvoiceGrid(t *testing.T) {
	words := []idm.Word{
		word("Date", 50, 290, 100, 300),
		word("Description", 170, 290, 260, 300),
		word("Amount", 400, 290, 460, 300),
		word("8", 50, 330, 60, 340),
		word("Meeting", 170, 330, 240, 340),
		word("1,500.00", 400, 330, 460, 340),
	}

	tables := Detect(invoiceRules(), words, page())
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}

	table := tables[0]
	if table.RowCount() != 3 {
		t.Errorf("rows = %d, want 3", table.RowCount())
	}
	// Four column rules bound three columns.
	if table.ColumnCount() != 3 {
		t.Errorf("columns = %d, want 3", table.ColumnCount())
	}

	want := [][]string{
		{"", "", ""},
		{"Date", "Description", "Amount"},
		{"8", "Meeting", "1,500.00"},
	}
	if !reflect.DeepEqual(table.Cells, want) {
		t.Errorf("cells = %+v, want %+v", table.Cells, want)
	}
}

func TestDetectJoinsWordsInOneCell(t *testing.T) {
	words := []idm.Word{
		word("Internal", 170, 330, 220, 340),
		word("meeting", 230, 330, 290, 340),
	}

	tables := Detect(invoiceRules(), words, page())
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	if got := tables[0].Cells[2][1]; got != "Internal meeting" {
		t.Errorf("cell = %q, want %q", got, "Internal meeting")
	}
}

func TestDetectIgnoresUnderlines(t *testing.T) {
	rules := []Rule{{Horizontal: true, Y: 500, X0: 100, X1: 400}}

	if tables := Detect(rules, nil, page()); tables != nil {
		t.Errorf("a single rule produced %d tables", len(tables))
	}
}

func TestDetectIgnoresShortDecoration(t *testing.T) {
	h := func(y, x0, x1 float64) Rule { return Rule{Horizontal: true, Y: y, X0: x0, X1: x1} }
	// Paragraph borders: long enough to be rules, but no vertical edges and no
	// interior structure, so no columns form.
	rules := []Rule{h(500, 72, 523), h(520, 72, 523)}

	tables := Detect(rules, nil, page())
	if len(tables) != 0 {
		t.Errorf("two parallel borders produced %d tables", len(tables))
	}
}

func TestDetectDeduplicatesDoubledLines(t *testing.T) {
	h := func(y, x0, x1 float64) Rule { return Rule{Horizontal: true, Y: y, X0: x0, X1: x1} }
	v := func(x, y0, y1 float64) Rule { return Rule{Horizontal: false, X0: x, X1: x, Y0: y0, Y1: y1} }

	// Every line drawn twice, 1.5pt apart, as the real invoice draws them.
	rules := []Rule{
		h(268, 45, 549), h(269.5, 45, 549),
		h(318, 45, 549), h(319.5, 45, 549),
		v(45, 268, 318), v(46.5, 268, 318), v(549, 268, 318),
	}

	words := []idm.Word{word("A", 50, 290, 60, 300)}

	tables := Detect(rules, words, page())
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	// Two doubled horizontal rules and two vertical edges bound a single
	// cell; the point is that the doubled lines did not double the grid.
	if tables[0].RowCount() != 1 || tables[0].ColumnCount() != 1 {
		t.Errorf("grid = %dx%d, want 1x1",
			tables[0].RowCount(), tables[0].ColumnCount())
	}
}

func TestDetectTwoSeparateTables(t *testing.T) {
	h := func(y, x0, x1 float64) Rule { return Rule{Horizontal: true, Y: y, X0: x0, X1: x1} }
	v := func(x, y0, y1 float64) Rule { return Rule{Horizontal: false, X0: x, X1: x, Y0: y0, Y1: y1} }

	rules := []Rule{
		h(100, 45, 200), h(150, 45, 200), v(45, 100, 150), v(200, 100, 150),
		h(400, 300, 500), h(450, 300, 500), v(300, 400, 450), v(500, 400, 450),
	}

	tables := Detect(rules, nil, page())
	if len(tables) != 2 {
		t.Fatalf("got %d tables, want 2", len(tables))
	}
	// Reading order: the table higher on the page (y=400..450) comes first.
	if tables[0].Top != 400 || tables[1].Top != 100 {
		t.Errorf("tables not top-down: %+v", tables)
	}
}

func TestWordsOutsideTheGridAreIgnored(t *testing.T) {
	words := []idm.Word{
		word("outside", 50, 700, 120, 710),
		word("inside", 50, 290, 110, 300),
	}

	tables := Detect(invoiceRules(), words, page())
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	if got := tables[0].Cells[1][0]; got != "inside" {
		t.Errorf("cell = %q, want %q", got, "inside")
	}
	found := false
	for _, row := range tables[0].Cells {
		for _, cell := range row {
			if cell == "outside" {
				found = true
			}
		}
	}
	if found {
		t.Error("a word outside the grid landed in a cell")
	}
}
