package render

import (
	"strings"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// footnoteDoc is one page of body text with a note beneath it.
func footnoteDoc(note string, noteSize float64) idm.Document {
	body := "The wand chooses the wizard, and the wizard learns to listen to it, " +
		"which takes a great deal longer than buying one."
	return document([]idm.Block{
		line(body, 1, 640, 650, 10),
		line(note, 1, 500, 508, noteSize),
		line("The next page continues the story without a note.", 2, 700, 710, 10),
		line("And the page after that ends the chapter.", 3, 700, 710, 10),
	}, 3)
}

func TestFootnoteAtThePageBottomBecomesAnAside(t *testing.T) {
	doc := footnoteDoc("1 Ollivander's shop, Diagon Alley, is described later on.", 8)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 1, Level: 1, Y: 800},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	body := book.Chapters[0].Body
	if !strings.Contains(body, `<aside class="footnote" epub:type="footnote" role="doc-footnote" id="fn-1">`) {
		t.Fatalf("the note is not an aside:\n%s", body)
	}
	if n := strings.Count(body, "Ollivander&#39;s shop"); n != 1 {
		t.Errorf("the note appears %d times, want once inside the aside:\n%s", n, body)
	}
	if i := strings.Index(body, "<aside"); i < 0 || strings.Index(body, "Ollivander") < i {
		t.Errorf("the note text sits outside the aside:\n%s", body)
	}
}

func TestFootnoteReferenceLinksToItsNote(t *testing.T) {
	doc := footnoteDoc("1 Ollivander's shop, Diagon Alley, is described later on.", 8)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 1, Level: 1, Y: 800},
	}

	// The reference is a raised marker on the annotated paragraph.
	text := "The wand chooses the wizard.1"
	doc.Blocks[0].Text = text
	doc.Blocks[0].Spans = []idm.Span{
		{Text: "The wand chooses the wizard."},
		{Text: "1", Super: true},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	body := book.Chapters[0].Body
	if !strings.Contains(body, `epub:type="noteref" role="doc-noteref" id="ref-fn-1" href="#fn-1"`) {
		t.Errorf("no noteref link from the text:\n%s", body)
	}
	if !strings.Contains(body, `<a class="backlink" href="#ref-fn-1"`) {
		t.Errorf("the note has no way back:\n%s", body)
	}
}

func TestBottomParagraphWithoutAMarkerStaysBodyText(t *testing.T) {
	// Small type at the bottom of a page is not enough: a note starts with the
	// mark its reference carries.
	doc := footnoteDoc("This paragraph simply continues onto the next page.", 8)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(book.Chapters[0].Body, "<aside") {
		t.Errorf("a paragraph without a marker became a footnote:\n%s", book.Chapters[0].Body)
	}
}

func TestPageNumberAtTheBottomIsNotAFootnote(t *testing.T) {
	doc := footnoteDoc("42", 8)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(book.Chapters[0].Body, "<aside") {
		t.Errorf("a bare page number became a footnote:\n%s", book.Chapters[0].Body)
	}
}

func TestYearAtTheBottomIsNotAFootnote(t *testing.T) {
	doc := footnoteDoc("1990 was the year the house was rebuilt by hand.", 8)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(book.Chapters[0].Body, "<aside") {
		t.Errorf("a year at the start of a paragraph made it a footnote:\n%s", book.Chapters[0].Body)
	}
}

func TestMarkFootnotesCountsWhatItClassifies(t *testing.T) {
	doc := footnoteDoc("1 Ollivander's shop, Diagon Alley, is described later on.", 8)
	paras := GroupParagraphs(doc.Body())

	if got := MarkFootnotes(paras, doc.Pages); got != 1 {
		t.Fatalf("marked = %d, want 1", got)
	}
	if paras[1].Kind != idm.KindFootnote {
		t.Errorf("paragraph kind = %q, want footnote", paras[1].Kind)
	}
}
