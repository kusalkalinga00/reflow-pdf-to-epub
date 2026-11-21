package layout

import (
	"reflect"
	"testing"

	"github.com/kusalkalinga00/reflow/internal/idm"
)

func page(n int) idm.Page {
	return idm.Page{
		Number: n, Width: 595, Height: 842,
		Provenance: idm.ProvenanceDigital, TextCoverage: 0.4,
	}
}

func block(text string, pageNo int, x0, y0, x1, y1 float64) idm.Block {
	return idm.Block{
		Kind:       idm.KindParagraph,
		Text:       text,
		Page:       pageNo,
		BBox:       idm.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1},
		Confidence: 1,
		FontSize:   y1 - y0,
	}
}

func texts(blocks []idm.Block) []string {
	out := make([]string, len(blocks))
	for i, b := range blocks {
		out[i] = b.Text
	}
	return out
}

func document(pages int, blocks ...idm.Block) idm.Document {
	doc := idm.Document{
		Meta:   idm.Metadata{Title: "Test", Language: "en"},
		Blocks: blocks,
	}
	for i := 1; i <= pages; i++ {
		doc.Pages = append(doc.Pages, page(i))
	}
	return doc
}

func TestSingleColumnSortsTopDown(t *testing.T) {
	doc := document(1,
		block("third", 1, 72, 500, 520, 510),
		block("first", 1, 72, 700, 520, 710),
		block("second", 1, 72, 600, 520, 610),
	)

	got := texts(Apply(doc).Blocks)
	want := []string{"first", "second", "third"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTwoColumnsReadLeftFirst(t *testing.T) {
	// Content-stream order alternates between columns, which is exactly the
	// failure this stage exists to fix.
	doc := document(1,
		block("L1", 1, 72, 700, 280, 712),
		block("R1", 1, 320, 700, 530, 712),
		block("L2", 1, 72, 680, 280, 692),
		block("R2", 1, 320, 680, 530, 692),
		block("L3", 1, 72, 660, 280, 672),
		block("R3", 1, 320, 660, 530, 672),
	)

	out := Apply(doc)
	got := texts(out.Blocks)
	want := []string{"L1", "L2", "L3", "R1", "R2", "R3"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}

	for _, b := range out.Blocks {
		if b.Text[0] == 'L' && b.Column != 0 {
			t.Errorf("%s is in column %d, want 0", b.Text, b.Column)
		}
		if b.Text[0] == 'R' && b.Column != 1 {
			t.Errorf("%s is in column %d, want 1", b.Text, b.Column)
		}
	}
}

func TestSpanningBlockKeepsItsPlace(t *testing.T) {
	doc := document(1,
		block("L1", 1, 72, 660, 280, 672),
		block("R1", 1, 320, 660, 530, 672),
		block("Title", 1, 72, 700, 530, 720), // spans the gutter
		block("L2", 1, 72, 640, 280, 652),
		block("R2", 1, 320, 640, 530, 652),
	)

	got := texts(Apply(doc).Blocks)
	want := []string{"Title", "L1", "L2", "R1", "R2"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestNoGutterWithoutTextOnBothSides(t *testing.T) {
	// One indented block looks like a gap but there is nothing to its right.
	doc := document(1,
		block("first", 1, 72, 700, 520, 712),
		block("indented", 1, 200, 680, 400, 692),
		block("third", 1, 72, 660, 520, 672),
	)

	for _, b := range Apply(doc).Blocks {
		if b.Column != 0 {
			t.Errorf("%q is in column %d, want a single column", b.Text, b.Column)
		}
	}
}

func TestFurnitureIsDetected(t *testing.T) {
	var blocks []idm.Block
	for p := 1; p <= 4; p++ {
		blocks = append(blocks,
			block("The Long Road", p, 72, 800, 300, 810),
			block("Body text.", p, 72, 600, 520, 612),
			block(pageNumberText(p), p, 300, 30, 320, 40),
		)
	}
	// A one-off line at the top must survive.
	blocks = append(blocks, block("Preface", 1, 72, 780, 200, 790))

	out := Apply(document(4, blocks...))

	var headers, footers, body int
	for _, b := range out.Blocks {
		switch b.Kind {
		case idm.KindHeader:
			headers++
		case idm.KindFooter:
			footers++
		default:
			body++
		}
	}

	if headers != 4 {
		t.Errorf("headers = %d, want 4", headers)
	}
	if footers != 4 {
		t.Errorf("footers = %d, want 4", footers)
	}
	// Four body lines plus the unique "Preface".
	if body != 5 {
		t.Errorf("body blocks = %d, want 5", body)
	}

	for _, b := range out.Blocks {
		if b.Text == "Preface" && b.Kind.IsFurniture() {
			t.Error("a line that appears on one page was removed as furniture")
		}
	}
}

func TestFurnitureNeedsRepeatedText(t *testing.T) {
	var blocks []idm.Block
	for p := 1; p <= 4; p++ {
		blocks = append(blocks,
			block("Page heading "+string(rune('A'+p-1)), p, 72, 800, 300, 810),
			block("Body text.", p, 72, 600, 520, 612),
		)
	}

	out := Apply(document(4, blocks...))
	for _, b := range out.Blocks {
		if b.Kind.IsFurniture() {
			t.Errorf("%q was removed but never repeats", b.Text)
		}
	}
}

func TestFurnitureNotDetectedInShortDocuments(t *testing.T) {
	var blocks []idm.Block
	for p := 1; p <= 2; p++ {
		blocks = append(blocks,
			block("The Long Road", p, 72, 800, 300, 810),
			block("Body text.", p, 72, 600, 520, 612),
		)
	}

	out := Apply(document(2, blocks...))
	for _, b := range out.Blocks {
		if b.Kind.IsFurniture() {
			t.Errorf("%q was removed from a two-page document", b.Text)
		}
	}
}

func TestApplyKeepsPagesOrderedAndValid(t *testing.T) {
	doc := document(2,
		block("p2 b", 2, 72, 600, 520, 612),
		block("p1 a", 1, 72, 600, 520, 612),
	)

	out := Apply(doc)
	if err := out.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	got := texts(out.Blocks)
	want := []string{"p1 a", "p2 b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// TestApplyPreservesEveryFieldExceptBlocks walks the struct so a field added
// later is covered without anyone remembering to update this test.
func TestApplyPreservesEveryFieldExceptBlocks(t *testing.T) {
	doc := document(1, block("body", 1, 72, 600, 520, 612))
	doc.Meta = idm.Metadata{
		Title: "The Long Road", Authors: []string{"A. Author"}, Language: "en",
		Publisher: "Tool Factory", Subject: "Subject", SourceHash: "abc",
		SourceName: "book.pdf",
	}
	doc.Outline = []idm.OutlineEntry{{Title: "One", Page: 1, Level: 1}}

	out := Apply(doc)

	before := reflect.ValueOf(doc)
	after := reflect.ValueOf(out)
	for i := 0; i < before.NumField(); i++ {
		name := before.Type().Field(i).Name
		if name == "Blocks" {
			continue
		}
		if !reflect.DeepEqual(before.Field(i).Interface(), after.Field(i).Interface()) {
			t.Errorf("Apply changed or dropped %q", name)
		}
	}
}

func TestApplyKeepsTheOutline(t *testing.T) {
	doc := document(2,
		block("p1", 1, 72, 600, 520, 612),
		block("p2", 2, 72, 600, 520, 612),
	)
	doc.Outline = []idm.OutlineEntry{{Title: "Chapter One", Page: 1, Level: 1}}

	out := Apply(doc)
	if len(out.Outline) != 1 || out.Outline[0].Title != "Chapter One" {
		t.Fatalf("outline = %+v, want it preserved", out.Outline)
	}
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	doc := document(1,
		block("second", 1, 72, 600, 520, 612),
		block("first", 1, 72, 700, 520, 712),
	)
	before := append([]idm.Block(nil), doc.Blocks...)

	Apply(doc)

	if !reflect.DeepEqual(doc.Blocks, before) {
		t.Error("Apply modified the caller's blocks")
	}
}

func TestMaskDigits(t *testing.T) {
	tests := []struct{ in, want string }{
		{"42", "#"},
		{"Page 42", "page #"},
		{"Chapter 7 - The Road", "chapter # - the road"},
		{"iv", "iv"},
		{"  spaced   out  ", "spaced out"},
	}

	for _, tt := range tests {
		if got := maskDigits(tt.in); got != tt.want {
			t.Errorf("maskDigits(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func pageNumberText(n int) string {
	return string([]byte{byte('0' + n)})
}

func TestRightAlignedDatesStayWithTheirEntries(t *testing.T) {
	// A résumé: one column of entries with a narrow, right-aligned date strip.
	// The dates must not be swept to the end of the page as a second column.
	doc := document(1,
		block("Software Engineer", 1, 40, 700, 340, 712),
		block("Sep 2020 – Jan 2022", 1, 480, 700, 555, 712),
		block("LKLoop", 1, 40, 680, 340, 692),
		block("Developed plugins", 1, 60, 660, 340, 672),
		block("IT Consultant", 1, 40, 600, 340, 612),
		block("Aug 2020 – Apr 2022", 1, 480, 600, 555, 612),
		block("Cortex Institute", 1, 40, 580, 340, 592),
	)

	got := texts(Apply(doc).Blocks)
	want := []string{
		"Software Engineer", "Sep 2020 – Jan 2022",
		"LKLoop", "Developed plugins",
		"IT Consultant", "Aug 2020 – Apr 2022",
		"Cortex Institute",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestParallelColumnsInABandReadColumnWise(t *testing.T) {
	// The invoice's header: a label and its value on the left, a label and
	// an address on the right, sharing lines. The columns read whole, so
	// each label keeps its value.
	doc := document(1,
		block("Invoice Date:", 1, 46, 668.6, 111.3, 676.7),
		block("12 November, 2026", 1, 45.8, 651.8, 141.1, 661.4),
		block("INVOICE TO", 1, 308.7, 668.6, 371.7, 676.4),
		block("Artin (PVT) Ltd", 1, 308.4, 652.7, 383.6, 662.3),
		block("26/7A", 1, 308.9, 636.8, 336.6, 645.9),
		block("1st Lane", 1, 308.6, 622.8, 348.0, 630.6),
	)

	got := texts(Apply(doc).Blocks)
	want := []string{
		"Invoice Date:", "12 November, 2026",
		"INVOICE TO", "Artin (PVT) Ltd", "26/7A", "1st Lane",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestPairedCellsReadRowByRow(t *testing.T) {
	// The invoice's totals: each label shares its line with its value. The
	// pairs read together, not all labels then all values.
	// The page's text sits at the margin above; the totals box floats below
	// it, aligned with the table's amount column.
	doc := document(1,
		block("10 November, 2026", 1, 45.8, 177.8, 144.2, 187.4),
		block("New Code Changes Review", 1, 167.4, 177.4, 306.7, 187.6),
		block("LKR 1,500.00", 1, 472.8, 177.8, 540.2, 187.4),
		block("Sub Total", 1, 318.3, 130.8, 367.1, 138.7),
		block("LKR 15,000.00", 1, 474.2, 128.9, 549.4, 138.7),
		block("Total Amount", 1, 318.2, 94.1, 389.6, 101.8),
		block("LKR 15,000.00", 1, 474.0, 92.1, 549.5, 101.9),
	)

	got := texts(Apply(doc).Blocks)
	want := []string{
		"10 November, 2026", "New Code Changes Review", "LKR 1,500.00",
		"Sub Total", "LKR 15,000.00",
		"Total Amount", "LKR 15,000.00",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestSideBySideEntriesReadColumnWise(t *testing.T) {
	// The CV's education: two degree entries side by side, each several
	// lines, with the section heading over the left one. Reading line by
	// line would interleave the two degrees.
	doc := document(1,
		block("EDUCATION", 1, 35.8, 600.1, 109.1, 609.7),
		block("BEng (Hons) in Software Engineering", 1, 34.6, 568.6, 225.8, 580.2),
		block("London Metropolitan University (UK)", 1, 34.7, 554.8, 218.1, 566.3),
		block("First Class", 1, 34.7, 542.1, 85.8, 550.3),
		block("Level 5 Pearson BTEC HND in Computing", 1, 300.6, 569.7, 513.7, 580.3),
		block("(Software Engineering)", 1, 300.3, 554.7, 420.9, 566.3),
		block("Pearson (formerly Edexcel)", 1, 300.6, 539.8, 436.8, 551.3),
	)

	got := texts(Apply(doc).Blocks)
	want := []string{
		"EDUCATION",
		"BEng (Hons) in Software Engineering",
		"London Metropolitan University (UK)",
		"First Class",
		"Level 5 Pearson BTEC HND in Computing",
		"(Software Engineering)",
		"Pearson (formerly Edexcel)",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestAMarginStripReadsWithTheHeaderNotAfterTheBody(t *testing.T) {
	// A résumé entry: title and employer at the margin, duties indented
	// below them, dates in a narrow strip beside the title. The dates read
	// with the entry — after the employer, before the duties — not after
	// every line of the body.
	doc := document(1,
		block("Junior Web Developer (Freelance)", 1, 33, 511.3, 216.4, 522.8),
		block("LKLoop", 1, 33, 495.1, 70.2, 504.7),
		block("Developed custom themes and plugins", 1, 52.4, 476.2, 374.1, 487.4),
		block("Developed dashboards using bootstrap", 1, 52.4, 461.2, 304.3, 472.0),
		block("Sep 2020 -", 1, 446.3, 511.3, 506.1, 522.2),
		block("Jan 2022", 1, 509.1, 513.3, 556.7, 522.2),
	)

	got := texts(Apply(doc).Blocks)
	want := []string{
		"Junior Web Developer (Freelance)",
		"LKLoop",
		"Sep 2020 -", "Jan 2022",
		"Developed custom themes and plugins",
		"Developed dashboards using bootstrap",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestBlocksOnOneLineReadLeftToRight(t *testing.T) {
	// A label and the link beside it sit a fraction of a point apart in y.
	// A pure top-down sort interleaves them by whichever is the hair higher;
	// the line they share reads left to right.
	doc := document(1,
		block("GitHub -", 1, 46.0, 304.5, 89.1, 313.0),
		block("github.com/kusalkalinga00", 1, 91.0, 302.3, 229.2, 313.5),
		block("LinkedIn -", 1, 46.0, 289.7, 94.6, 298.0),
		block("linkedin.com/in/kusal-kalinga", 1, 97.8, 287.2, 246.4, 298.4),
	)

	got := texts(Apply(doc).Blocks)
	want := []string{
		"GitHub -", "github.com/kusalkalinga00",
		"LinkedIn -", "linkedin.com/in/kusal-kalinga",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestFurnitureRepeatedOnConsecutivePagesIsRemoved(t *testing.T) {
	// One real book kept its footer on the first eight pages only, split
	// into a page number and a title; the rest of the book had a merged
	// footer. A global majority test leaves the eight-page variant behind,
	// so consecutive repetition at the page edge has to count.
	var blocks []idm.Block
	for page := 1; page <= 10; page++ {
		blocks = append(blocks, block("Body text.", page, 72, 700, 500, 710))
	}
	for page := 2; page <= 4; page++ {
		blocks = append(blocks,
			block("P a g e | "+string(rune('0'+page)), page, 22, 15, 74, 25),
			block("The Book Title – The Author", page, 90, 15, 343, 25),
		)
	}
	// Two consecutive pages are not a running head.
	blocks = append(blocks,
		block("Only here", 8, 72, 15, 300, 25),
		block("Only here", 9, 72, 15, 300, 25),
	)

	out := Apply(document(10, blocks...))

	furniture := 0
	for _, b := range out.Blocks {
		if b.Kind.IsFurniture() {
			furniture++
		}
	}
	if furniture != 6 {
		t.Errorf("furniture blocks = %d, want 6 (two lines on pages 2-4)", furniture)
	}

	for _, b := range out.Blocks {
		if b.Text == "Only here" && b.Kind.IsFurniture() {
			t.Error("a footer on two pages was removed; three is the threshold")
		}
	}
}
