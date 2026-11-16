package render

import (
	"errors"
	"strings"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/epub"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

func document(blocks []idm.Block, pages int) idm.Document {
	doc := idm.Document{
		Meta: idm.Metadata{
			Title:    "The Long Road",
			Authors:  []string{"A. Author"},
			Language: "en",
		},
	}
	for i := 1; i <= pages; i++ {
		doc.Pages = append(doc.Pages, idm.Page{
			Number: i, Width: 595, Height: 842,
			Provenance: idm.ProvenanceDigital, TextCoverage: 0.4,
		})
	}
	doc.Blocks = blocks
	return doc
}

func TestRenderMetadata(t *testing.T) {
	doc := document([]idm.Block{line("Body.", 1, 600, 610, 10)}, 1)
	doc.Meta.Publisher = "Tool Factory"

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if book.Metadata.Title != "The Long Road" {
		t.Errorf("title = %q", book.Metadata.Title)
	}
	if book.Metadata.Language != "en" {
		t.Errorf("language = %q", book.Metadata.Language)
	}
	if book.Metadata.Publisher != "Tool Factory" {
		t.Errorf("publisher = %q", book.Metadata.Publisher)
	}
	if len(book.Metadata.Authors) != 1 || book.Metadata.Authors[0] != "A. Author" {
		t.Errorf("authors = %v", book.Metadata.Authors)
	}
}

func TestRenderTitleFallbacks(t *testing.T) {
	tests := []struct {
		name string
		meta idm.Metadata
		opts Options
		want string
	}{
		{
			name: "document title wins",
			meta: idm.Metadata{Title: "From the PDF", Language: "en"},
			opts: Options{DefaultTitle: "From options"},
			want: "From the PDF",
		},
		{
			name: "default title",
			meta: idm.Metadata{Language: "en"},
			opts: Options{DefaultTitle: "From options"},
			want: "From options",
		},
		{
			name: "source name without extension",
			meta: idm.Metadata{Language: "en", SourceName: "book.pdf"},
			want: "book",
		},
		{
			name: "last resort",
			meta: idm.Metadata{Language: "en"},
			want: "Untitled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := document([]idm.Block{line("Body.", 1, 600, 610, 10)}, 1)
			doc.Meta = tt.meta

			book, err := Render(doc, tt.opts)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if book.Metadata.Title != tt.want {
				t.Errorf("title = %q, want %q", book.Metadata.Title, tt.want)
			}
		})
	}
}

func TestRenderSplitsChaptersAtHeadings(t *testing.T) {
	doc := document([]idm.Block{
		heading("Chapter One", 1, 700, 20),
		line("First chapter body.", 1, 660, 670, 10),
		heading("Chapter Two", 2, 700, 20),
		line("Second chapter body.", 2, 660, 670, 10),
	}, 2)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(book.Chapters) != 2 {
		t.Fatalf("got %d chapters, want 2", len(book.Chapters))
	}
	if book.Chapters[0].Title != "Chapter One" || book.Chapters[1].Title != "Chapter Two" {
		t.Errorf("titles = %q, %q", book.Chapters[0].Title, book.Chapters[1].Title)
	}
	if !strings.Contains(book.Chapters[1].Body, "Second chapter body.") {
		t.Errorf("second chapter body = %q", book.Chapters[1].Body)
	}
	if strings.Contains(book.Chapters[1].Body, "First chapter body.") {
		t.Error("content leaked into the wrong chapter")
	}
}

func TestRenderFrontMatterBeforeFirstHeading(t *testing.T) {
	doc := document([]idm.Block{
		line("A title page line.", 1, 700, 710, 10),
		heading("Chapter One", 1, 660, 20),
		line("Body.", 1, 620, 630, 10),
	}, 1)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(book.Chapters) != 2 {
		t.Fatalf("got %d chapters, want 2", len(book.Chapters))
	}
	if book.Chapters[0].Title != "Front matter" {
		t.Errorf("first chapter = %q, want Front matter", book.Chapters[0].Title)
	}
}

func TestRenderUsesTheOutlineForChapters(t *testing.T) {
	doc := document([]idm.Block{
		line("Cover and copyright.", 1, 700, 710, 10),
		line("First chapter body.", 2, 700, 710, 10),
		line("More of the first chapter.", 3, 700, 710, 10),
		line("Second chapter body.", 5, 700, 710, 10),
	}, 5)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 2, Level: 1},
		{Title: "Chapter Two", Page: 5, Level: 1},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	titles := chapterTitlesOf(book)
	want := []string{"Front matter", "Chapter One", "Chapter Two"}
	if len(titles) != len(want) {
		t.Fatalf("chapters = %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("chapters = %v, want %v", titles, want)
		}
	}

	// Pages 2 and 3 belong to the first outline chapter, not the second.
	if !strings.Contains(book.Chapters[1].Body, "More of the first chapter.") {
		t.Errorf("first chapter is missing its second page:\n%s", book.Chapters[1].Body)
	}
	if strings.Contains(book.Chapters[1].Body, "Second chapter body.") {
		t.Error("content leaked across outline chapters")
	}
}

func TestRenderIgnoresAThinOutline(t *testing.T) {
	doc := document([]idm.Block{
		line("Body on page one.", 1, 600, 610, 10),
		line("Body on page two.", 2, 600, 610, 10),
	}, 2)
	doc.Outline = []idm.OutlineEntry{{Title: "Only Entry", Page: 1, Level: 1}}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// One bookmark does not describe a structure; the fallback is used.
	if len(book.Chapters) != 1 {
		t.Fatalf("got %d chapters, want 1: %v", len(book.Chapters), chapterTitlesOf(book))
	}
	if book.Chapters[0].Title == "Only Entry" {
		t.Error("a single bookmark was used to split the book")
	}
}

func chapterTitlesOf(book epub.Book) []string {
	titles := make([]string, len(book.Chapters))
	for i, c := range book.Chapters {
		titles[i] = c.Title
	}
	return titles
}

func TestRenderWithoutHeadingsIsOneChapter(t *testing.T) {
	doc := document([]idm.Block{
		line("Page one text.", 1, 600, 610, 10),
		line("Page two text.", 2, 600, 610, 10),
	}, 2)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(book.Chapters) != 1 {
		t.Fatalf("got %d chapters, want 1", len(book.Chapters))
	}
	if book.Chapters[0].Title != "The Long Road" {
		t.Errorf("chapter title = %q, want the document title", book.Chapters[0].Title)
	}
}

func TestRenderPageAnchorsAndList(t *testing.T) {
	doc := document([]idm.Block{
		line("Page one text.", 1, 600, 610, 10),
		line("Page two text.", 2, 600, 610, 10),
	}, 2)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	body := book.Chapters[0].Body
	for _, want := range []string{`id="page-1"`, `id="page-2"`, `epub:type="pagebreak"`, `aria-label="2"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}

	if len(book.PageList) != 2 {
		t.Fatalf("page list has %d entries, want 2", len(book.PageList))
	}
	if got := book.PageList[1]; got.Label != "2" || got.Href != "text/chapter-001.xhtml#page-2" {
		t.Errorf("page target = %+v", got)
	}
}

func TestRenderPageListPointsAtTheContainingChapter(t *testing.T) {
	doc := document([]idm.Block{
		heading("Chapter One", 1, 700, 20),
		line("The first chapter is set in a normal size and runs long enough to read as body text.", 1, 660, 670, 10),
		heading("Chapter Two", 5, 700, 20),
		line("The second chapter is also set in a normal size and runs long enough to read as body text.", 5, 660, 670, 10),
	}, 5)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	want := map[string]string{
		"1": "text/chapter-001.xhtml#page-1",
		"5": "text/chapter-002.xhtml#page-5",
	}
	for _, pt := range book.PageList {
		if expected, ok := want[pt.Label]; ok && pt.Href != expected {
			t.Errorf("page %s href = %q, want %q", pt.Label, pt.Href, expected)
		}
	}
}

func TestRenderEscapesMarkup(t *testing.T) {
	doc := document([]idm.Block{
		line(`Tom & Jerry <3 "quotes"`, 1, 600, 610, 10),
	}, 1)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	body := book.Chapters[0].Body
	if !strings.Contains(body, "Tom &amp; Jerry &lt;3") {
		t.Errorf("body is not escaped:\n%s", body)
	}
}

func TestRenderRejectsDocumentWithoutText(t *testing.T) {
	doc := document(nil, 1)

	_, err := Render(doc, Options{})
	if !errors.Is(err, ErrNoText) {
		t.Fatalf("error = %v, want ErrNoText", err)
	}
}

func TestRenderDefaultsLanguage(t *testing.T) {
	doc := document([]idm.Block{line("Body.", 1, 600, 610, 10)}, 1)
	doc.Meta.Language = ""

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if book.Metadata.Language != "en" {
		t.Errorf("language = %q, want en", book.Metadata.Language)
	}
}
