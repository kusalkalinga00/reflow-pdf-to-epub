package extract

import (
	"testing"

	"github.com/klippa-app/go-pdfium/responses"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

func textRect(text string, left, bottom, right, top float64) *responses.GetPageTextStructuredRect {
	return &responses.GetPageTextStructuredRect{
		Text: text,
		PointPosition: responses.CharPosition{
			Left:   left,
			Bottom: bottom,
			Right:  right,
			Top:    top,
		},
	}
}

func TestBuildLinesSingleRun(t *testing.T) {
	lines := buildLines([]*responses.GetPageTextStructuredRect{
		textRect("Hello from a PDF.", 17, 763, 146, 773),
	})

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].text != "Hello from a PDF." {
		t.Errorf("text = %q", lines[0].text)
	}
	if got, want := lines[0].box, (idm.Rect{X0: 17, Y0: 763, X1: 146, Y1: 773}); got != want {
		t.Errorf("box = %+v, want %+v", got, want)
	}
}

func TestBuildLinesJoinsAdjacentRuns(t *testing.T) {
	// A bold word splits one visual line into three runs. The middle run starts
	// exactly where the first ends, so no space is invented.
	lines := buildLines([]*responses.GetPageTextStructuredRect{
		textRect("The road bent ", 72, 600, 170, 610),
		textRect("north", 170, 600, 210, 610),
		textRect(" and stopped.", 210, 600, 300, 610),
	})

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %+v", len(lines), lines)
	}
	if want := "The road bent north and stopped."; lines[0].text != want {
		t.Errorf("text = %q, want %q", lines[0].text, want)
	}
	if lines[0].fontSize != 10 {
		t.Errorf("fontSize = %v, want 10", lines[0].fontSize)
	}
}

func TestBuildLinesInsertsSpaceAcrossAGap(t *testing.T) {
	lines := buildLines([]*responses.GetPageTextStructuredRect{
		textRect("word", 72, 600, 100, 610),
		textRect("next", 104, 600, 130, 610),
	})

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if want := "word next"; lines[0].text != want {
		t.Errorf("text = %q, want %q", lines[0].text, want)
	}
}

func TestBuildLinesSplitsOnBaselineChange(t *testing.T) {
	lines := buildLines([]*responses.GetPageTextStructuredRect{
		textRect("first line", 72, 600, 200, 610),
		textRect("second line", 72, 580, 200, 590),
	})

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].text != "first line" || lines[1].text != "second line" {
		t.Errorf("lines = %q, %q", lines[0].text, lines[1].text)
	}
}

func TestBuildLinesSplitsOnColumnGutter(t *testing.T) {
	lines := buildLines([]*responses.GetPageTextStructuredRect{
		textRect("left column", 72, 600, 250, 610),
		textRect("right column", 320, 600, 500, 610),
	})

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (the gutter is not a word space)", len(lines))
	}
}

func TestBuildLinesDropsWhitespaceRuns(t *testing.T) {
	lines := buildLines([]*responses.GetPageTextStructuredRect{
		textRect(" ", 16, 751, 23, 751),
		textRect("Hello", 17, 763, 146, 773),
	})

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].text != "Hello" {
		t.Errorf("text = %q, want Hello", lines[0].text)
	}
}

func TestBuildLinesNormalisesWhitespace(t *testing.T) {
	lines := buildLines([]*responses.GetPageTextStructuredRect{
		textRect("spaced   out", 72, 600, 200, 610),
	})

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if want := "spaced out"; lines[0].text != want {
		t.Errorf("text = %q, want %q", lines[0].text, want)
	}
}

func TestBuildLinesSkipsEmptyRects(t *testing.T) {
	if lines := buildLines(nil); len(lines) != 0 {
		t.Fatalf("got %d lines, want 0", len(lines))
	}

	lines := buildLines([]*responses.GetPageTextStructuredRect{
		nil,
		textRect("Degenerate", 10, 10, 10, 10),
	})
	if len(lines) != 0 {
		t.Errorf("got %d lines, want 0 for zero-area runs", len(lines))
	}
}

func TestBuildLinesUsesUnionBox(t *testing.T) {
	lines := buildLines([]*responses.GetPageTextStructuredRect{
		textRect("wide", 50, 600, 300, 610),
		textRect("tall", 300, 595, 330, 615),
	})

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got, want := lines[0].box, (idm.Rect{X0: 50, Y0: 595, X1: 330, Y1: 615}); got != want {
		t.Errorf("box = %+v, want %+v", got, want)
	}
	if lines[0].fontSize != 20 {
		t.Errorf("fontSize = %v, want the tallest run (20)", lines[0].fontSize)
	}
}
