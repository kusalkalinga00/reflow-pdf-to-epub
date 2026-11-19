package layout

import (
	"reflect"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
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
