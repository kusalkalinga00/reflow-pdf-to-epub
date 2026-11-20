package render

import (
	"strings"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

func TestExternalLinkInTheTextBecomesAnAnchor(t *testing.T) {
	doc := document([]idm.Block{
		{
			Kind: idm.KindParagraph, Page: 1, FontSize: 10,
			BBox: idm.Rect{X0: 72, Y0: 700, X1: 400, Y1: 710},
		},
	}, 1)
	doc.Blocks[0].Text = "See the site."
	doc.Blocks[0].Spans = []idm.Span{
		{Text: "See the "},
		{Text: "site", Href: "https://example.com/book"},
		{Text: "."},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(book.Chapters[0].Body, `<a href="https://example.com/book">site</a>`) {
		t.Fatalf("no link in the chapter:\n%s", book.Chapters[0].Body)
	}
}

func TestInternalLinkPointsAtThePageAnchorInItsChapter(t *testing.T) {
	doc := document([]idm.Block{
		line("Chapter one.", 1, 700, 710, 10),
		line("Chapter two.", 2, 700, 710, 10),
		line("Chapter three.", 3, 700, 710, 10),
	}, 3)
	doc.Outline = []idm.OutlineEntry{
		{Title: "One", Page: 1, Level: 1, Y: 720},
		{Title: "Two", Page: 2, Level: 1, Y: 720},
		{Title: "Three", Page: 3, Level: 1, Y: 720},
	}

	// A reference to page 3 from page 1: the anchor lives in chapter 3.
	doc.Blocks[0].Text = "Chapter one.3"
	doc.Blocks[0].Spans = []idm.Span{
		{Text: "Chapter one."},
		{Text: "3", Super: true, Href: "#page-3"},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if got := book.Chapters[0].Body; !strings.Contains(got, `href="chapter-003.xhtml#page-3"`) {
		t.Fatalf("cross-chapter link is wrong:\n%s", got)
	}
}

func TestLinkToAMissingPageStaysPlainText(t *testing.T) {
	doc := document([]idm.Block{line("Chapter one.", 1, 700, 710, 10)}, 1)
	doc.Blocks[0].Text = "Chapter one.9"
	doc.Blocks[0].Spans = []idm.Span{
		{Text: "Chapter one."},
		{Text: "9", Super: true, Href: "#page-9"},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(book.Chapters[0].Body, "<a href=") {
		t.Fatalf("a link to a page that is not in the book was written:\n%s", book.Chapters[0].Body)
	}
	if !strings.Contains(book.Chapters[0].Body, "<sup>9</sup>") {
		t.Errorf("the link text was dropped with the link:\n%s", book.Chapters[0].Body)
	}
}
