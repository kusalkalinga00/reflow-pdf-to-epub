package extract

import (
	"testing"

	"github.com/klippa-app/go-pdfium/responses"

	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/idm"
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

// glyph is one piece of a line under construction.
func glyph(text string, x, bottom, top float64) piece {
	return piece{text: text, box: idm.Rect{X0: x, Y0: bottom, X1: x + 4, Y1: top}}
}

func TestLineMarksARaisedMarkerAsSuperscript(t *testing.T) {
	pieces := []piece{
		glyph("w", 100, 700, 710), glyph("a", 104, 700, 710),
		glyph("n", 108, 700, 710), glyph("d", 112, 700, 710),
		glyph(".", 116, 700, 710),
		glyph("1", 120, 703, 709), glyph("2", 122, 703, 709),
		glyph(" ", 126, 700, 710),
		glyph("x", 130, 700, 710),
	}

	l := joinPieces(pieces)
	if l.text != "wand.12 x" {
		t.Fatalf("text = %q", l.text)
	}

	spans := lineSpans(l.styled, nil)
	var supers []string
	for _, s := range spans {
		if s.Super {
			supers = append(supers, s.Text)
		}
	}
	if len(supers) != 1 || supers[0] != "12" {
		t.Fatalf("superscript spans = %v, want [12]", supers)
	}
	if got := joinedSpans(spans); got != l.text {
		t.Errorf("spans describe %q, text is %q", got, l.text)
	}
}

func TestLineDoesNotLiftPunctuation(t *testing.T) {
	// Quotes and apostrophes have small ink boxes sitting above the baseline.
	// Lifting them would lift punctuation throughout the book.
	pieces := []piece{
		glyph("d", 100, 700, 710), glyph("o", 104, 700, 710),
		glyph("n", 108, 700, 710),
		glyph("'", 112, 706, 709),
		glyph("t", 114, 700, 710),
	}

	l := joinPieces(pieces)
	for _, s := range lineSpans(l.styled, nil) {
		if s.Super {
			t.Fatalf("punctuation was lifted: %+v", s)
		}
	}
}

func TestLineDoesNotLiftAnAsteriskInsideAWord(t *testing.T) {
	// In code, an asterisk is multiplied by the next token. A note mark ends a
	// word; this one does not.
	pieces := []piece{
		glyph("a", 100, 700, 710), glyph("*", 104, 703, 707),
		glyph("b", 106, 700, 710),
	}

	l := joinPieces(pieces)
	for _, s := range lineSpans(l.styled, nil) {
		if s.Super {
			t.Fatalf("an asterisk inside a word was lifted: %+v", s)
		}
	}
}

func TestLineMarksAnOrdinalSuffix(t *testing.T) {
	pieces := []piece{
		glyph("2", 100, 700, 710),
		glyph("n", 104, 703, 707), glyph("d", 106, 703, 707),
		glyph(" ", 110, 700, 710),
		glyph("f", 114, 700, 710), glyph("l", 118, 700, 710),
		glyph("o", 122, 700, 710), glyph("o", 126, 700, 710), glyph("r", 130, 700, 710),
	}

	l := joinPieces(pieces)
	var supers []string
	for _, s := range lineSpans(l.styled, nil) {
		if s.Super {
			supers = append(supers, s.Text)
		}
	}
	if len(supers) != 1 || supers[0] != "nd" {
		t.Fatalf("superscript spans = %v, want [nd]", supers)
	}
}

func TestLineSpansSplitAWordWithoutMovingASpace(t *testing.T) {
	// "wand1" with the marker attached: the runs split inside the word, so the
	// text must come back exactly, with no space invented at the boundary.
	styled := []styledPiece{
		{text: "w", box: idm.Rect{X0: 100, Y0: 700, X1: 104, Y1: 710}},
		{text: "a", box: idm.Rect{X0: 104, Y0: 700, X1: 108, Y1: 710}},
		{text: "n", box: idm.Rect{X0: 108, Y0: 700, X1: 112, Y1: 710}},
		{text: "d", box: idm.Rect{X0: 112, Y0: 700, X1: 116, Y1: 710}},
		{text: "1", box: idm.Rect{X0: 116, Y0: 703, X1: 119, Y1: 709}, super: true},
	}

	spans := lineSpans(styled, nil)
	if got := joinedSpans(spans); got != "wand1" {
		t.Fatalf("spans describe %q, want %q", got, "wand1")
	}
	if len(spans) != 2 || !spans[1].Super {
		t.Fatalf("spans = %+v, want the marker split off", spans)
	}
}

func TestLineSpansCarryTheLinkUnderTheText(t *testing.T) {
	styled := []styledPiece{
		{text: "S", box: idm.Rect{X0: 100, Y0: 700, X1: 104, Y1: 710}},
		{text: "e", box: idm.Rect{X0: 104, Y0: 700, X1: 108, Y1: 710}},
		{text: "e", box: idm.Rect{X0: 108, Y0: 700, X1: 112, Y1: 710}},
		{text: " ", box: idm.Rect{X0: 112, Y0: 700, X1: 115, Y1: 710}},
		{text: "u", box: idm.Rect{X0: 115, Y0: 700, X1: 119, Y1: 710}},
		{text: "s", box: idm.Rect{X0: 119, Y0: 700, X1: 123, Y1: 710}},
	}
	links := []idm.Link{{
		Page: 1,
		Rect: idm.Rect{X0: 114, Y0: 698, X1: 124, Y1: 712},
		Href: "https://example.com/us",
	}}

	spans := lineSpans(styled, links)
	if got := joinedSpans(spans); got != "See us" {
		t.Fatalf("spans describe %q", got)
	}
	if len(spans) != 2 || spans[0].Href != "" || spans[1].Href != "https://example.com/us" {
		t.Fatalf("spans = %+v, want the link on the word only", spans)
	}
}

func TestLineSpansTurnAnInternalLinkIntoAPageReference(t *testing.T) {
	styled := []styledPiece{
		{text: "p", box: idm.Rect{X0: 100, Y0: 700, X1: 104, Y1: 710}},
		{text: " ", box: idm.Rect{X0: 104, Y0: 700, X1: 107, Y1: 710}},
		{text: "4", box: idm.Rect{X0: 107, Y0: 700, X1: 111, Y1: 710}},
		{text: "2", box: idm.Rect{X0: 111, Y0: 700, X1: 115, Y1: 710}},
	}
	links := []idm.Link{{
		Page:       1,
		Rect:       idm.Rect{X0: 106, Y0: 698, X1: 116, Y1: 712},
		TargetPage: 42,
	}}

	spans := lineSpans(styled, links)
	if got := joinedSpans(spans); got != "p 42" {
		t.Fatalf("spans describe %q", got)
	}
	if len(spans) != 2 || spans[1].Href != "#page-42" {
		t.Fatalf("spans = %+v, want the page reference", spans)
	}
}

func TestSafeHrefKeepsOnlySchemesABookCanOffer(t *testing.T) {
	good := map[string]string{
		"https://example.com":     "https://example.com",
		"http://example.com":      "http://example.com",
		"mailto:me@example.com":   "mailto:me@example.com",
		"  https://example.com  ": "https://example.com",
	}
	for in, want := range good {
		if got, ok := safeHref(in); !ok || got != want {
			t.Errorf("safeHref(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}

	bad := []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"file:///etc/passwd",
		"#page-12",
		"chapter-002.xhtml",
		"",
	}
	for _, in := range bad {
		if got, ok := safeHref(in); ok {
			t.Errorf("safeHref(%q) = %q, want rejected", in, got)
		}
	}
}
