package render

import (
	"strings"
	"testing"

	"github.com/kusalkalinga00/reflow/internal/idm"
)

func line(text string, page int, y0, y1, size float64) idm.Block {
	return idm.Block{
		Kind:     idm.KindParagraph,
		Text:     text,
		Page:     page,
		BBox:     idm.Rect{X0: 72, Y0: y0, X1: 300, Y1: y1},
		FontSize: size,
	}
}

func heading(text string, page int, y float64, size float64) idm.Block {
	return idm.Block{
		Kind:     idm.KindParagraph,
		Text:     text,
		Page:     page,
		BBox:     idm.Rect{X0: 72, Y0: y, X1: 300, Y1: y + size},
		FontSize: size,
	}
}

func texts(paras []Paragraph) []string {
	out := make([]string, len(paras))
	for i, p := range paras {
		out[i] = p.Text
	}
	return out
}

func TestGroupParagraphsMergesConsecutiveLines(t *testing.T) {
	paras := GroupParagraphs([]idm.Block{
		line("The road bent north", 1, 600, 610, 10),
		line("and the engine never", 1, 588, 598, 10),
		line("stalled.", 1, 576, 586, 10),
	})

	if len(paras) != 1 {
		t.Fatalf("got %d paragraphs, want 1: %v", len(paras), texts(paras))
	}
	if want := "The road bent north and the engine never stalled."; paras[0].Text != want {
		t.Errorf("text = %q, want %q", paras[0].Text, want)
	}
}

func TestGroupParagraphsSplitsOnParagraphGap(t *testing.T) {
	paras := GroupParagraphs([]idm.Block{
		line("First paragraph.", 1, 600, 610, 10),
		line("Second paragraph.", 1, 570, 580, 10), // 20pt gap: a paragraph break
	})

	if len(paras) != 2 {
		t.Fatalf("got %d paragraphs, want 2: %v", len(paras), texts(paras))
	}
}

func TestGroupParagraphsSplitsOnFontSizeChange(t *testing.T) {
	paras := GroupParagraphs([]idm.Block{
		line("Body text at ten points.", 1, 600, 610, 10),
		line("A caption at twelve.", 1, 590, 602, 13),
	})

	if len(paras) != 2 {
		t.Fatalf("got %d paragraphs, want 2: %v", len(paras), texts(paras))
	}
}

func TestGroupParagraphsSplitsAcrossPages(t *testing.T) {
	paras := GroupParagraphs([]idm.Block{
		line("Ends on page one", 1, 100, 110, 10),
		line("starts on page two", 2, 700, 710, 10),
	})

	if len(paras) != 2 {
		t.Fatalf("got %d paragraphs, want 2: %v", len(paras), texts(paras))
	}
}

func TestGroupParagraphsDehyphenatesLineBreaks(t *testing.T) {
	paras := GroupParagraphs([]idm.Block{
		line("an effi-", 1, 600, 610, 10),
		line("cient engine", 1, 588, 598, 10),
	})

	if len(paras) != 1 {
		t.Fatalf("got %d paragraphs, want 1", len(paras))
	}
	if want := "an efficient engine"; paras[0].Text != want {
		t.Errorf("text = %q, want %q", paras[0].Text, want)
	}
}

func TestGroupParagraphsDetectsHeadings(t *testing.T) {
	paras := GroupParagraphs([]idm.Block{
		heading("Chapter One", 1, 700, 20),
		line("Body text at ten points.", 1, 660, 670, 10),
		line("More body text at ten.", 1, 648, 658, 10),
	})

	// The two body lines merge into one paragraph; the heading does not.
	if len(paras) != 2 {
		t.Fatalf("got %d paragraphs, want 2: %v", len(paras), texts(paras))
	}
	if paras[0].Kind != idm.KindHeading {
		t.Fatalf("first paragraph kind = %q, want heading", paras[0].Kind)
	}
	if paras[0].Level != 1 {
		t.Errorf("heading level = %d, want 1", paras[0].Level)
	}
	if paras[1].Kind == idm.KindHeading {
		t.Error("body text was classified as a heading")
	}
	if want := "Body text at ten points. More body text at ten."; paras[1].Text != want {
		t.Errorf("body = %q, want %q", paras[1].Text, want)
	}
}

func TestGroupParagraphsLeavesUniformTextAlone(t *testing.T) {
	paras := GroupParagraphs([]idm.Block{
		line("One line.", 1, 600, 610, 10),
		line("Another line.", 1, 588, 598, 10),
	})

	for _, p := range paras {
		if p.Kind != idm.KindParagraph {
			t.Errorf("%q was classified as %q", p.Text, p.Kind)
		}
	}
}

func TestGroupParagraphsSkipsFurniture(t *testing.T) {
	header := line("The Long Road", 1, 800, 810, 8)
	header.Kind = idm.KindHeader

	footer := line("42", 1, 30, 40, 8)
	footer.Kind = idm.KindFooter

	paras := GroupParagraphs([]idm.Block{header, line("Body.", 1, 600, 610, 10), footer})

	if len(paras) != 1 || paras[0].Text != "Body." {
		t.Fatalf("got %v, want just the body paragraph", texts(paras))
	}
}

func TestBodySizeIsWeightedByText(t *testing.T) {
	tests := []struct {
		name  string
		paras []Paragraph
		want  float64
	}{
		{
			name:  "single size",
			paras: []Paragraph{{Text: "text", FontSize: 10}},
			want:  10,
		},
		{
			name: "one short title among body text",
			paras: []Paragraph{
				{Text: "Chapter One", FontSize: 20},
				{Text: "A long body paragraph that is set in the normal size.", FontSize: 10},
			},
			want: 10,
		},
		{
			name: "body dominates several headings",
			paras: []Paragraph{
				{Text: "Part", FontSize: 30},
				{Text: "Chapter", FontSize: 20},
				{Text: "Body text that goes on for a while, as body text does.", FontSize: 11},
			},
			want: 11,
		},
		{
			name:  "no sizes",
			paras: []Paragraph{{Text: "text"}},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bodySize(tt.paras); got != tt.want {
				t.Errorf("bodySize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupParagraphsEmptyInput(t *testing.T) {
	if got := GroupParagraphs(nil); len(got) != 0 {
		t.Errorf("got %d paragraphs, want 0", len(got))
	}
}

func TestGroupParagraphsSkipsBlankText(t *testing.T) {
	paras := GroupParagraphs([]idm.Block{
		line("   ", 1, 600, 610, 10),
		line("real text", 1, 580, 590, 10),
	})

	if len(paras) != 1 || strings.TrimSpace(paras[0].Text) != "real text" {
		t.Fatalf("got %v", texts(paras))
	}
}
