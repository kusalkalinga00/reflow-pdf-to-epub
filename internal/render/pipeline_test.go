package render_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/epub"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/extract"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/render"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
)

// TestPipelineFindsChapters runs extraction and rendering together on a PDF
// with real heading sizes. It is the end-to-end check that the two stages agree
// on what a heading is, which unit tests with hand-built blocks cannot prove.
func TestPipelineFindsChapters(t *testing.T) {
	pdf := testpdf.Build(1, testpdf.WithLines(
		testpdf.Line{Text: "Chapter One", Size: 24, X: 72, Y: 700},
		testpdf.Line{Text: "The first chapter body is set in a small size and runs long enough to be body text.", Size: 10, X: 72, Y: 660},
		testpdf.Line{Text: "Chapter Two", Size: 24, X: 72, Y: 600},
		testpdf.Line{Text: "The second chapter body is also set in a small size and runs long enough to be body.", Size: 10, X: 72, Y: 560},
	))

	extractor, err := extract.NewPDFium(extract.Options{})
	if err != nil {
		t.Fatalf("NewPDFium: %v", err)
	}
	defer extractor.Close()

	doc, err := extractor.Extract(context.Background(), bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	book, err := render.Render(doc, render.Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(book.Chapters) != 2 {
		t.Fatalf("got %d chapters, want 2: %v", len(book.Chapters), chapterTitles(book.Chapters))
	}
	if book.Chapters[0].Title != "Chapter One" || book.Chapters[1].Title != "Chapter Two" {
		t.Errorf("chapter titles = %v", chapterTitles(book.Chapters))
	}

	first := book.Chapters[0].Body
	if !strings.Contains(first, "<h1>Chapter One</h1>") {
		t.Errorf("first chapter body has no h1:\n%s", first)
	}
	if !strings.Contains(first, "The first chapter body") {
		t.Errorf("first chapter body is missing its text:\n%s", first)
	}
	if strings.Contains(first, "The second chapter body") {
		t.Error("content leaked into the wrong chapter")
	}
}

func TestPipelineProducesNoDecorativeMarkup(t *testing.T) {
	pdf := testpdf.Build(1, testpdf.WithLines(
		testpdf.Line{Text: "One line only.", Size: 12, X: 72, Y: 700},
	))

	extractor, err := extract.NewPDFium(extract.Options{})
	if err != nil {
		t.Fatalf("NewPDFium: %v", err)
	}
	defer extractor.Close()

	doc, err := extractor.Extract(context.Background(), bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	book, err := render.Render(doc, render.Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(book.Chapters) != 1 {
		t.Fatalf("got %d chapters, want 1", len(book.Chapters))
	}
	if !strings.Contains(book.Chapters[0].Body, "<p>One line only.</p>") {
		t.Errorf("body = %q", book.Chapters[0].Body)
	}
}

func chapterTitles(chapters []epub.Chapter) []string {
	titles := make([]string, len(chapters))
	for i, c := range chapters {
		titles[i] = c.Title
	}
	return titles
}
