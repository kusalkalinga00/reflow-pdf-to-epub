package corpus

import (
	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/render"
	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/testpdf"
)

func line(text string, size, x, y float64) testpdf.Line {
	return testpdf.Line{Text: text, Size: size, X: x, Y: y}
}

// Synthetic returns the built-in corpus.
//
// The PDFs are generated rather than committed: it keeps the repository free of
// binary fixtures, and it means a case can state its expected text next to the
// layout that produces it. Each case targets one failure mode the pipeline has
// a stage for.
func Synthetic() []Case {
	return []Case{
		singleColumn(),
		twoColumns(),
		headingChapters(),
		runningHeads(),
		spacedHeading(),
		scanned(),
	}
}

func singleColumn() Case {
	pdf := testpdf.Build(2, testpdf.WithPages(
		[]testpdf.Line{
			line("First line of the book.", 12, 72, 700),
			line("Second line follows.", 12, 72, 660),
		},
		[]testpdf.Line{
			line("Second page content.", 12, 72, 700),
		},
	))

	return Case{
		Name:      "single-column",
		PDF:       pdf,
		WantText:  "First line of the book. Second line follows. Second page content.",
		WantOrder: []string{"First line", "Second line", "Second page"},
	}
}

func twoColumns() Case {
	pdf := testpdf.Build(1, testpdf.WithPages([]testpdf.Line{
		line("Left column starts here.", 12, 72, 700),
		line("Right column starts here.", 12, 320, 700),
		line("Second left line.", 12, 72, 680),
		line("Second right line.", 12, 320, 680),
	}))

	return Case{
		Name:      "two-columns",
		PDF:       pdf,
		WantText:  "Left column starts here. Second left line. Right column starts here. Second right line.",
		WantOrder: []string{"Second left", "Right column starts"},
	}
}

func headingChapters() Case {
	pdf := testpdf.Build(1, testpdf.WithPages([]testpdf.Line{
		line("Chapter One", 24, 72, 700),
		line("The first body line of the chapter.", 12, 72, 660),
		line("A second body line here.", 12, 72, 640),
		line("Chapter Two", 24, 72, 560),
		line("The second chapter body line.", 12, 72, 520),
		line("A second line for chapter two.", 12, 72, 500),
	}))

	return Case{
		Name:         "headings",
		PDF:          pdf,
		WantText:     "Chapter One The first body line of the chapter. A second body line here. Chapter Two The second chapter body line. A second line for chapter two.",
		WantChapters: []string{"Chapter One", "Chapter Two"},
		WantOrder:    []string{"Chapter One", "Chapter Two"},
	}
}

func runningHeads() Case {
	pages := make([][]testpdf.Line, 0, 3)
	for i := 1; i <= 3; i++ {
		pages = append(pages, []testpdf.Line{
			line("The Long Road", 12, 72, 800),
			line(footerForPage(i), 12, 300, 30),
			line(bodyForPage(i), 12, 72, 600),
		})
	}

	return Case{
		Name:      "running-heads",
		PDF:       testpdf.Build(3, testpdf.WithPages(pages...)),
		WantText:  "Body of page 1. Body of page 2. Body of page 3.",
		WantOrder: []string{"Body of page 1", "Body of page 3"},
	}
}

// Hyphenated line breaks are covered by unit tests in internal/normalize, not
// here: PDFium maps the hyphen of a non-embedded base-14 font to a control
// character, so a generated fixture cannot exercise the real path. Real PDFs
// embed their fonts and get the hyphen; the external corpus is where that shows
// up.
func spacedHeading() Case {
	pdf := testpdf.Build(1, testpdf.WithPages([]testpdf.Line{
		line("C O N T E N T S", 20, 72, 700),
		line("The body of the document.", 12, 72, 660),
	}))

	return Case{
		Name:         "spaced-heading",
		PDF:          pdf,
		WantText:     "CONTENTS The body of the document.",
		WantChapters: []string{"CONTENTS"},
	}
}

// scanned has a page with no text layer at all, which is what a scan looks like
// to extraction. The pipeline must refuse it rather than emit an empty book.
func scanned() Case {
	return Case{
		Name:      "scanned",
		PDF:       testpdf.Build(1, testpdf.WithPages(nil)),
		WantError: render.ErrNoText,
	}
}

func bodyForPage(n int) string {
	return "Body of page " + string(rune('0'+n)) + "."
}

func footerForPage(n int) string {
	return string(rune('0' + n))
}
