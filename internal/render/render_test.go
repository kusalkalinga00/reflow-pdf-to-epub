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

func TestRenderSplitsWithinAPageAtOutlinePositions(t *testing.T) {
	doc := document([]idm.Block{
		line("Header text at the top.", 1, 760, 770, 10),
		line("Section one body.", 1, 650, 660, 10),
		line("Section two body.", 1, 400, 410, 10),
	}, 1)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Section One", Page: 1, Level: 1, Y: 700},
		{Title: "Section Two", Page: 1, Level: 1, Y: 450},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	want := []string{"Front matter", "Section One", "Section Two"}
	if got := chapterTitlesOf(book); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("chapters = %v, want %v", got, want)
	}
	if !strings.Contains(book.Chapters[1].Body, "Section one body.") {
		t.Errorf("first section body = %q", book.Chapters[1].Body)
	}
	if !strings.Contains(book.Chapters[2].Body, "Section two body.") {
		t.Errorf("second section body = %q", book.Chapters[2].Body)
	}
}

func TestRenderDescendsToALevelWithStructure(t *testing.T) {
	doc := document([]idm.Block{
		line("Part introduction.", 1, 680, 690, 10),
		line("First section body.", 1, 650, 660, 10),
		line("Second section body.", 1, 450, 460, 10),
		line("Third section body.", 1, 300, 310, 10),
	}, 1)
	doc.Outline = []idm.OutlineEntry{
		{Title: "The Whole Document", Page: 1, Level: 1, Y: 780},
		{Title: "First", Page: 1, Level: 2, Y: 700},
		{Title: "Second", Page: 1, Level: 2, Y: 500},
		{Title: "Third", Page: 1, Level: 2, Y: 350},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	titles := chapterTitlesOf(book)
	if len(titles) != 3 {
		t.Fatalf("chapters = %v, want the three level-2 sections", titles)
	}
	if titles[0] != "First" || titles[2] != "Third" {
		t.Errorf("chapters = %v", titles)
	}
}

func TestRenderLocatesBookmarksWithoutPositions(t *testing.T) {
	// Bookmarks that point at a page with no destination, as most PDF printers
	// produce. The titles are in the text, so the headings are found by name.
	doc := document([]idm.Block{
		line("Summary of the document.", 1, 760, 770, 10),
		line("Education", 1, 650, 670, 20),
		line("Some education detail.", 1, 620, 630, 10),
		line("Experience", 1, 500, 520, 20),
		line("Some experience detail.", 1, 470, 480, 10),
	}, 1)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Education", Page: 1, Level: 1},
		{Title: "Experience", Page: 1, Level: 1},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	titles := chapterTitlesOf(book)
	if len(titles) != 3 {
		t.Fatalf("chapters = %v, want front matter plus two sections", titles)
	}
	if titles[1] != "Education" || titles[2] != "Experience" {
		t.Fatalf("chapters = %v", titles)
	}
	if !strings.Contains(book.Chapters[1].Body, "Some education detail.") {
		t.Errorf("education chapter = %q", book.Chapters[1].Body)
	}
	if !strings.Contains(book.Chapters[2].Body, "Some experience detail.") {
		t.Errorf("experience chapter = %q", book.Chapters[2].Body)
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

func TestRenderEmbedsFigures(t *testing.T) {
	doc := document([]idm.Block{
		line("Text before the picture.", 1, 700, 710, 10),
		{
			Kind: idm.KindFigure, Page: 1,
			BBox:       idm.Rect{X0: 100, Y0: 400, X1: 300, Y1: 600},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("jpeg-bytes"), MediaType: "image/jpeg"},
		},
	}, 1)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(book.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(book.Images))
	}
	if book.Images[0].Name != "fig-001.jpg" {
		t.Errorf("image name = %q", book.Images[0].Name)
	}

	body := book.Chapters[0].Body
	if !strings.Contains(body, `<figure><img src="../images/fig-001.jpg" alt=""/>`) {
		t.Errorf("body does not reference the figure:\n%s", body)
	}
}

func TestRenderDeduplicatesIdenticalFigures(t *testing.T) {
	data := []byte("same-bytes")
	doc := document([]idm.Block{
		{Kind: idm.KindFigure, Page: 1, BBox: idm.Rect{X0: 10, Y0: 10, X1: 20, Y1: 20}, Confidence: 1,
			Image: &idm.Image{Data: data, MediaType: "image/jpeg"}},
		{Kind: idm.KindFigure, Page: 1, BBox: idm.Rect{X0: 30, Y0: 30, X1: 40, Y1: 40}, Confidence: 1,
			Image: &idm.Image{Data: data, MediaType: "image/jpeg"}},
	}, 1)

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(book.Images) != 1 {
		t.Errorf("images = %d, want 1 after deduplication", len(book.Images))
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

func TestRenderNestsOutlineSectionsInsideTheirChapter(t *testing.T) {
	doc := document([]idm.Block{
		line("Opening text.", 1, 700, 710, 10),
		line("Section one body.", 1, 620, 630, 10),
		line("Section two body.", 1, 400, 410, 10),
		line("Second chapter body.", 2, 700, 710, 10),
	}, 2)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 1, Level: 1, Y: 720},
		{Title: "Section One", Page: 1, Level: 2, Y: 650},
		{Title: "Section Two", Page: 1, Level: 2, Y: 430},
		{Title: "Chapter Two", Page: 2, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if got, want := chapterTitlesOf(book), []string{"Chapter One", "Chapter Two"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("chapters = %v, want %v", got, want)
	}

	// The sections are navigation entries, not chapters.
	sections := book.Chapters[0].Sections
	if len(sections) != 2 {
		t.Fatalf("first chapter has %d sections, want 2", len(sections))
	}
	if got, want := sections[0].Title, "Section One"; got != want {
		t.Errorf("section 0 title = %q, want %q", got, want)
	}
	if got, want := sections[0].Href, "text/chapter-001.xhtml#sec-01"; got != want {
		t.Errorf("section 0 href = %q, want %q", got, want)
	}
	if len(book.Chapters[1].Sections) != 0 {
		t.Errorf("second chapter has %d sections, want 0", len(book.Chapters[1].Sections))
	}
}

func TestRenderPlacesSectionAnchorsBeforeTheirParagraphs(t *testing.T) {
	doc := document([]idm.Block{
		line("Opening text.", 1, 700, 710, 10),
		line("Section one body.", 1, 620, 630, 10),
		line("Section two body.", 1, 400, 410, 10),
		line("Second chapter body.", 2, 700, 710, 10),
	}, 2)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 1, Level: 1, Y: 720},
		{Title: "Section One", Page: 1, Level: 2, Y: 650},
		{Title: "Section Two", Page: 1, Level: 2, Y: 430},
		{Title: "Chapter Two", Page: 2, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	body := book.Chapters[0].Body
	one := strings.Index(body, `id="sec-01"`)
	two := strings.Index(body, `id="sec-02"`)
	if one < 0 || two < 0 {
		t.Fatalf("section anchors missing:\n%s", body)
	}
	if one > two {
		t.Errorf("section anchors out of reading order: sec-01 at %d, sec-02 at %d", one, two)
	}
	if got, want := strings.Index(body, "Section one body."), one; got < want {
		t.Errorf("sec-01 emitted after its paragraph (anchor %d, paragraph %d)", one, got)
	}
	if got, want := strings.Index(body, "Section two body."), two; got < want {
		t.Errorf("sec-02 emitted after its paragraph (anchor %d, paragraph %d)", two, got)
	}
	if strings.Index(body, "Opening text.") > one {
		t.Error("sec-01 emitted before the chapter's opening paragraph")
	}
}

func TestRenderAnchorsAPositionlessSectionAtItsPageTop(t *testing.T) {
	doc := document([]idm.Block{
		line("First page text.", 1, 700, 710, 10),
		line("Second page text.", 2, 700, 710, 10),
	}, 2)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 1, Level: 1, Y: 720},
		{Title: "Chapter Two", Page: 2, Level: 1, Y: 720},
		{Title: "Bookmarked Subsection", Page: 2, Level: 2},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	body := book.Chapters[1].Body
	anchor := strings.Index(body, `id="sec-01"`)
	text := strings.Index(body, "Second page text.")
	if anchor < 0 || text < 0 {
		t.Fatalf("anchor or paragraph missing:\n%s", body)
	}
	if anchor > text {
		t.Errorf("positionless section anchor emitted after its page's first paragraph (anchor %d, text %d)", anchor, text)
	}
}

func TestPositionlessSectionsFollowTheirOutlineNeighbour(t *testing.T) {
	// The CV's outline carries no positions at all: every bookmark is
	// "page 1, top". Sections must still land under the chapter the outline
	// puts them after, not under whichever chapter starts at the page top.
	doc := document([]idm.Block{
		line("KUSAL KALINGA", 1, 780, 790, 14),
		line("Senior Software Engineer", 1, 750, 760, 12),
		line("PROFESSIONAL SUMMARY", 1, 700, 710, 12),
		line("EDUCATION", 1, 650, 660, 12),
		line("BEng (Hons) in Software Engineering", 1, 620, 630, 10),
		line("SKILLS", 1, 580, 590, 12),
		line("WORK EXPERIENCE", 1, 540, 550, 12),
		line("Software Engineer (Fullstack)", 1, 510, 520, 10),
	}, 1)
	doc.Outline = []idm.OutlineEntry{
		{Title: "KUSAL KALINGA", Page: 1, Level: 1},
		{Title: "Senior Software Engineer", Page: 1, Level: 2},
		{Title: "PROFESSIONAL SUMMARY", Page: 1, Level: 2},
		{Title: "EDUCATION", Page: 1, Level: 2},
		{Title: "BEng (Hons) in Software Engineering", Page: 1, Level: 3},
		{Title: "SKILLS", Page: 1, Level: 2},
		{Title: "WORK EXPERIENCE", Page: 1, Level: 2},
		{Title: "Software Engineer (Fullstack)", Page: 1, Level: 3},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	byTitle := map[string]int{}
	for i, c := range book.Chapters {
		byTitle[c.Title] = i
	}

	education := book.Chapters[byTitle["EDUCATION"]]
	if len(education.Sections) != 1 || education.Sections[0].Title != "BEng (Hons) in Software Engineering" {
		t.Fatalf("EDUCATION sections = %+v, want the BEng entry", education.Sections)
	}

	work := book.Chapters[byTitle["WORK EXPERIENCE"]]
	if len(work.Sections) != 1 || work.Sections[0].Title != "Software Engineer (Fullstack)" {
		t.Fatalf("WORK EXPERIENCE sections = %+v, want the role entry", work.Sections)
	}

	// The anchor must sit inside the chapter body, before the matched text.
	body := education.Body
	anchor := strings.Index(body, `id="sec-01"`)
	text := strings.Index(body, "BEng (Hons) in Software Engineering")
	if anchor < 0 || text < 0 || anchor > text {
		t.Errorf("BEng anchor misplaced (anchor %d, text %d):\n%s", anchor, text, body)
	}
}

func TestParagraphTextIncludesTableCells(t *testing.T) {
	paras := []Paragraph{
		{Kind: idm.KindParagraph, Text: "Before the grid."},
		{Kind: idm.KindTable, Table: &idm.TableGrid{Cells: [][]string{
			{"Date", "Hours"},
			{"8 October, 2026", "1"},
		}}},
		{Kind: idm.KindParagraph, Text: "After the grid."},
	}

	got := ParagraphText(paras)
	want := "Before the grid. Date Hours 8 October, 2026 1 After the grid."
	if got != want {
		t.Errorf("ParagraphText = %q, want %q", got, want)
	}
}

func TestRenderGivesOpeningFiguresToTheChapterBelow(t *testing.T) {
	// A novel's banner and illustration are drawn above the chapter title,
	// and the bookmark points at the title. They must open the chapter, not
	// close the one before it — which for the first chapter is the cover.
	doc := document([]idm.Block{
		line("Cover text.", 1, 700, 710, 10),
		{
			Kind: idm.KindFigure, Page: 2,
			BBox:       idm.Rect{X0: 40, Y0: 420, X1: 320, Y1: 600},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("banner"), MediaType: "image/jpeg"},
		},
		line("Chapter one text.", 2, 300, 360, 10),
		line("Chapter two text.", 4, 700, 710, 10),
	}, 4)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 2, Level: 1, Y: 370},
		{Title: "Chapter Two", Page: 4, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if got := chapterTitlesOf(book); len(got) != 3 || got[0] != "Front matter" || got[1] != "Chapter One" {
		t.Fatalf("chapters = %v", got)
	}
	if strings.Contains(book.Chapters[0].Body, "<figure>") {
		t.Error("the opening figure stayed with the front matter")
	}

	body := book.Chapters[1].Body
	figure := strings.Index(body, "<figure>")
	text := strings.Index(body, "Chapter one text.")
	if figure < 0 || text < 0 || figure > text {
		t.Errorf("figure is not before the chapter text:\n%s", body)
	}
}

func TestRenderKeepsAFigureWithTheChapterItEnds(t *testing.T) {
	// The previous chapter continues onto the page, so a figure above the next
	// heading belongs to the chapter that is ending.
	doc := document([]idm.Block{
		line("Chapter one opening.", 2, 700, 710, 10),
		line("Chapter one ends here.", 3, 620, 630, 10),
		{
			Kind: idm.KindFigure, Page: 3,
			BBox:       idm.Rect{X0: 40, Y0: 400, X1: 320, Y1: 500},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("end-art"), MediaType: "image/jpeg"},
		},
		line("Chapter two text.", 3, 300, 360, 10),
		line("Chapter three text.", 4, 700, 710, 10),
	}, 4)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 2, Level: 1, Y: 720},
		{Title: "Chapter Two", Page: 3, Level: 1, Y: 370},
		{Title: "Chapter Three", Page: 4, Level: 1, Y: 700},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(book.Chapters[0].Body, "<figure>") {
		t.Error("a figure that ends chapter one was moved to the next chapter")
	}
	if strings.Contains(book.Chapters[1].Body, "<figure>") {
		t.Error("chapter two took a figure that belongs to the previous chapter")
	}
}

func TestRenderTreatsALoneFirstPageFigureAsTheCover(t *testing.T) {
	// A cover is page one's only content: one figure, no text. It becomes the
	// book's cover rather than a one-image chapter that some readers render as
	// a blank page before the first chapter.
	doc := document([]idm.Block{
		{Kind: idm.KindFigure, Page: 1,
			BBox:       idm.Rect{X0: 0, Y0: 0, X1: 595, Y1: 842},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("cover"), MediaType: "image/jpeg"}},
		line("Chapter one text.", 2, 700, 710, 10),
		line("Chapter two text.", 3, 700, 710, 10),
	}, 3)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 2, Level: 1, Y: 720},
		{Title: "Chapter Two", Page: 3, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if book.Cover == nil {
		t.Fatal("page one's lone figure was not taken as the cover")
	}
	if string(book.Cover.Data) != "cover" {
		t.Errorf("cover data = %q", book.Cover.Data)
	}
	if titles := chapterTitlesOf(book); len(titles) != 2 || titles[0] != "Chapter One" {
		t.Errorf("chapters = %v, want the two outline chapters", titles)
	}
	if len(book.Images) != 0 {
		t.Errorf("the cover was also embedded as a figure (%d images)", len(book.Images))
	}
	for _, page := range book.PageList {
		if page.Label == "1" {
			t.Error("the cover's page is still in the print page list")
		}
	}
}

func TestRenderKeepsATitlePageThatCarriesText(t *testing.T) {
	doc := document([]idm.Block{
		{Kind: idm.KindFigure, Page: 1,
			BBox:       idm.Rect{X0: 100, Y0: 300, X1: 500, Y1: 700},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("art"), MediaType: "image/jpeg"}},
		line("The Book Title", 1, 100, 140, 24),
		line("Chapter one text.", 2, 700, 710, 10),
		line("Chapter two text.", 3, 700, 710, 10),
	}, 3)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 2, Level: 1, Y: 720},
		{Title: "Chapter Two", Page: 3, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if book.Cover != nil {
		t.Error("a title page with text was taken as a cover")
	}
	if len(book.Images) != 1 {
		t.Errorf("images = %d, want the title-page art kept as a figure", len(book.Images))
	}
}

func TestRenderTakesACoverWhoseTitleIsOnTheArtwork(t *testing.T) {
	// A scanned cover has no text layer, so OCR reads the title printed on the
	// artwork. Those lines sit inside the figure: they are part of the cover,
	// not a separate title page, and must not become one.
	doc := document([]idm.Block{
		{Kind: idm.KindFigure, Page: 1,
			BBox:       idm.Rect{X0: 0, Y0: 0, X1: 595, Y1: 842},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("cover-art"), MediaType: "image/jpeg"}},
		line("HARRY", 1, 700, 740, 36),
		line("POTTER", 1, 600, 640, 36),
		line("Chapter one text.", 2, 700, 710, 10),
		line("Chapter two text.", 3, 700, 710, 10),
	}, 3)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 2, Level: 1, Y: 720},
		{Title: "Chapter Two", Page: 3, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if book.Cover == nil || string(book.Cover.Data) != "cover-art" {
		t.Fatalf("cover = %+v, want the page-one artwork", book.Cover)
	}
	if titles := chapterTitlesOf(book); len(titles) != 2 {
		t.Fatalf("chapters = %v, want the two outline chapters", titles)
	}
	for _, ch := range book.Chapters {
		if strings.Contains(ch.Body, "HARRY") || strings.Contains(ch.Body, "POTTER") {
			t.Errorf("OCR text from the cover became chapter text:\n%s", ch.Body)
		}
	}
}

func TestRenderKeepsATitlePageBeneathAnIllustration(t *testing.T) {
	// Text outside the figure is a title page, not cover artwork.
	doc := document([]idm.Block{
		{Kind: idm.KindFigure, Page: 1,
			BBox:       idm.Rect{X0: 0, Y0: 0, X1: 595, Y1: 600},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("art"), MediaType: "image/jpeg"}},
		line("The Book Title", 1, 620, 660, 24),
		line("Chapter one text.", 2, 700, 710, 10),
		line("Chapter two text.", 3, 700, 710, 10),
	}, 3)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 2, Level: 1, Y: 720},
		{Title: "Chapter Two", Page: 3, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if book.Cover != nil {
		t.Error("a title page with text outside the figure was taken as a cover")
	}
	if !strings.Contains(book.Chapters[0].Body, "The Book Title") {
		t.Error("the title page text was dropped")
	}
}

func TestRenderFindsACoverAfterATitlePage(t *testing.T) {
	// The cover is page two: page one is the title page. The title page stays.
	doc := document([]idm.Block{
		line("The Book Title", 1, 700, 710, 20),
		{Kind: idm.KindFigure, Page: 2,
			BBox:       idm.Rect{X0: 0, Y0: 0, X1: 595, Y1: 842},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("cover"), MediaType: "image/jpeg"}},
		line("Chapter one text.", 3, 700, 710, 10),
		line("Chapter two text.", 4, 700, 710, 10),
	}, 4)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 3, Level: 1, Y: 720},
		{Title: "Chapter Two", Page: 4, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if book.Cover == nil || string(book.Cover.Data) != "cover" {
		t.Fatalf("cover = %+v, want page two's artwork", book.Cover)
	}
	if got := chapterTitlesOf(book); len(got) != 3 || got[0] != "Front matter" {
		t.Fatalf("chapters = %v, want front matter then the two chapters", got)
	}
	front := book.Chapters[0].Body
	if !strings.Contains(front, "The Book Title") {
		t.Error("the title page was removed with the cover")
	}
	if strings.Contains(front, "<figure>") {
		t.Error("the cover is still in the front matter")
	}
}

func TestRenderFindsACoverBesideAPublisherMark(t *testing.T) {
	doc := document([]idm.Block{
		{Kind: idm.KindFigure, Page: 1,
			BBox:       idm.Rect{X0: 0, Y0: 0, X1: 595, Y1: 842},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("cover"), MediaType: "image/jpeg"}},
		{Kind: idm.KindFigure, Page: 1,
			BBox:       idm.Rect{X0: 20, Y0: 20, X1: 60, Y1: 60},
			Confidence: 1,
			Image:      &idm.Image{Data: []byte("logo"), MediaType: "image/png"}},
		line("Chapter one text.", 2, 700, 710, 10),
		line("Chapter two text.", 3, 700, 710, 10),
	}, 3)
	doc.Outline = []idm.OutlineEntry{
		{Title: "Chapter One", Page: 2, Level: 1, Y: 720},
		{Title: "Chapter Two", Page: 3, Level: 1, Y: 720},
	}

	book, err := Render(doc, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if book.Cover == nil || string(book.Cover.Data) != "cover" {
		t.Fatalf("cover = %+v, want the large image, not the publisher mark", book.Cover)
	}
	if got := chapterTitlesOf(book); len(got) != 2 || got[0] != "Chapter One" {
		t.Errorf("chapters = %v, want the two chapters", got)
	}
}

func TestRenderFacsimileUsesTheOutlineAndAcover(t *testing.T) {
	doc := idm.Document{
		Meta: idm.Metadata{Title: "Scanned Book", Language: "en"},
		Pages: []idm.Page{
			{Number: 1, Width: 595, Height: 842},
			{Number: 2, Width: 595, Height: 842},
			{Number: 3, Width: 595, Height: 842},
			{Number: 4, Width: 595, Height: 842},
		},
		Outline: []idm.OutlineEntry{
			{Title: "Chapter One", Page: 2, Level: 1},
			{Title: "Chapter Two", Page: 4, Level: 1},
		},
	}

	pages := []PageImage{
		{Number: 1, Data: []byte("one"), MediaType: "image/jpeg"},
		{Number: 2, Data: []byte("two"), MediaType: "image/jpeg"},
		{Number: 3, Data: []byte("three"), MediaType: "image/jpeg"},
		{Number: 4, Data: []byte("four"), MediaType: "image/jpeg"},
	}

	book, err := RenderFacsimile(doc, pages, Options{})
	if err != nil {
		t.Fatalf("RenderFacsimile: %v", err)
	}

	if book.Cover == nil || string(book.Cover.Data) != "one" {
		t.Fatalf("cover = %+v, want page one", book.Cover)
	}
	if titles := chapterTitlesOf(book); len(titles) != 2 || titles[0] != "Chapter One" || titles[1] != "Chapter Two" {
		t.Fatalf("chapters = %v", titles)
	}
	if len(book.Chapters[0].Body) == 0 || !strings.Contains(book.Chapters[0].Body, "page-002.jpg") {
		t.Errorf("chapter one body = %q", book.Chapters[0].Body)
	}
	if len(book.PageList) != 3 {
		t.Errorf("page list = %d entries, want 3 (the cover is not a reading page)", len(book.PageList))
	}
	for _, want := range []string{"page-002.jpg", "page-003.jpg", "page-004.jpg"} {
		found := false
		for _, img := range book.Images {
			if img.Name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing image %q", want)
		}
	}
}
