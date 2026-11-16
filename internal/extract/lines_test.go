package extract

import (
	"testing"

	"github.com/klippa-app/go-pdfium/responses"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

func char(text string, left, bottom, right, top float64) *responses.GetPageTextStructuredChar {
	return &responses.GetPageTextStructuredChar{
		Text: text,
		PointPosition: responses.CharPosition{
			Left:   left,
			Bottom: bottom,
			Right:  right,
			Top:    top,
		},
	}
}

// run lays out a string as characters with plausible widths, so tests exercise
// the real grouping code rather than a single blob.
func run(text string, left, bottom, size float64) []*responses.GetPageTextStructuredChar {
	var out []*responses.GetPageTextStructuredChar

	x := left
	for _, r := range text {
		width := size * 0.5
		if r == ' ' {
			width = size * 0.25
		}
		out = append(out, char(string(r), x, bottom, x+width, bottom+size))
		x += width
	}
	return out
}

func TestBuildLinesSingleLine(t *testing.T) {
	lines := buildLines(run("Hello from a PDF.", 17, 763, 10))

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %+v", len(lines), lines)
	}
	if want := "Hello from a PDF."; lines[0].text != want {
		t.Errorf("text = %q, want %q", lines[0].text, want)
	}
	if lines[0].fontSize != 10 {
		t.Errorf("fontSize = %v, want 10", lines[0].fontSize)
	}
	if lines[0].box.Y0 != 763 || lines[0].box.Y1 != 773 {
		t.Errorf("box = %+v, want the run's band", lines[0].box)
	}
}

func TestBuildLinesSplitsOnBaselineChange(t *testing.T) {
	var chars []*responses.GetPageTextStructuredChar
	chars = append(chars, run("first line", 72, 700, 10)...)
	chars = append(chars, run("second line", 72, 680, 10)...)

	lines := buildLines(chars)

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	if lines[0].text != "first line" || lines[1].text != "second line" {
		t.Errorf("lines = %q, %q", lines[0].text, lines[1].text)
	}
}

func TestBuildLinesSplitsOnColumnGutter(t *testing.T) {
	var chars []*responses.GetPageTextStructuredChar
	chars = append(chars, run("left column", 72, 700, 10)...)
	chars = append(chars, run("right column", 320, 700, 10)...)

	lines := buildLines(chars)

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (the gutter is not a word space): %+v", len(lines), lines)
	}
}

func TestBuildLinesKeepsSingleSpaces(t *testing.T) {
	lines := buildLines(run("two words here", 72, 700, 10))

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if want := "two words here"; lines[0].text != want {
		t.Errorf("text = %q, want %q", lines[0].text, want)
	}
}

func TestBuildLinesInsertsMissingSpaces(t *testing.T) {
	// A PDF that positions words without space glyphs.
	var chars []*responses.GetPageTextStructuredChar
	chars = append(chars, char("a", 72, 700, 77, 710))
	chars = append(chars, char("b", 77, 700, 82, 710))
	chars = append(chars, char("c", 88, 700, 93, 710))
	chars = append(chars, char("d", 93, 700, 98, 710))

	lines := buildLines(chars)

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if want := "ab cd"; lines[0].text != want {
		t.Errorf("text = %q, want %q", lines[0].text, want)
	}
}

func TestBuildLinesSplitsOnNewlineMarker(t *testing.T) {
	// PDFium marks line breaks with newline characters; they are the primary
	// signal and must not be treated as text.
	chars := []*responses.GetPageTextStructuredChar{
		char("a", 72, 700, 77, 710),
		char("\r", 77, 700, 77, 700),
		char("\n", 77, 700, 77, 700),
		char("b", 72, 688, 77, 698),
		char("\n", 77, 688, 77, 688),
	}

	lines := buildLines(chars)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	if lines[0].text != "a" || lines[1].text != "b" {
		t.Errorf("lines = %q, %q", lines[0].text, lines[1].text)
	}
}

func TestBuildLinesIgnoresTrailingNewline(t *testing.T) {
	chars := []*responses.GetPageTextStructuredChar{
		char("a", 72, 700, 77, 710),
		char("\n", 77, 700, 77, 700),
	}

	lines := buildLines(chars)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
}

func TestBuildLinesEmptyInput(t *testing.T) {
	if lines := buildLines(nil); len(lines) != 0 {
		t.Fatalf("got %d lines, want 0", len(lines))
	}
	if lines := buildLines([]*responses.GetPageTextStructuredChar{nil}); len(lines) != 0 {
		t.Fatalf("got %d lines for a nil char, want 0", len(lines))
	}
}

func TestBuildLinesUnionBox(t *testing.T) {
	var chars []*responses.GetPageTextStructuredChar
	chars = append(chars, char("a", 50, 700, 60, 710))
	chars = append(chars, char("b", 60, 700, 70, 712)) // slightly taller

	lines := buildLines(chars)

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got, want := lines[0].box, (idm.Rect{X0: 50, Y0: 700, X1: 70, Y1: 712}); got != want {
		t.Errorf("box = %+v, want %+v", got, want)
	}
	if lines[0].fontSize != 12 {
		t.Errorf("fontSize = %v, want the tallest character (12)", lines[0].fontSize)
	}
}

func TestCapHeightIgnoresGlyphsAboveTheBaseline(t *testing.T) {
	// A closing quote sits entirely above the baseline. Taking the highest
	// glyph bottom as the baseline made such a line measure about 4pt instead
	// of 10, which turned most body lines of a real novel into "headings".
	var chars []*responses.GetPageTextStructuredChar
	chars = append(chars, run("word", 72, 700, 10)...)
	chars = append(chars, char("\u201d", 92, 706, 97, 710))

	lines := buildLines(chars)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got := lines[0].fontSize; got < 9 || got > 11 {
		t.Errorf("fontSize = %v, want about 10", got)
	}
}

func TestCapHeightIsStableWithDescendersAndQuotes(t *testing.T) {
	plain := buildLines(run("word", 72, 700, 10))
	withQuote := buildLines(append(run("word", 72, 700, 10), char("\u201d", 92, 706, 97, 710)))
	withDescender := buildLines(append(run("word", 72, 700, 10), char("g", 92, 696, 97, 706)))

	base := plain[0].fontSize
	for name, size := range map[string]float64{
		"quote":     withQuote[0].fontSize,
		"descender": withDescender[0].fontSize,
	} {
		if diff := size - base; diff > 1 || diff < -1 {
			t.Errorf("%s: fontSize = %v, want about %v", name, size, base)
		}
	}
}
