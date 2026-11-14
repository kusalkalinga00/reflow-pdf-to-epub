package normalize

import (
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

func TestTextExpandsLigatures(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ef\ufb01cient", "efficient"},
		{"\ufb02ow", "flow"},
		{"\ufb00ort", "ffort"},
		{"o\ufb03ce", "office"},
		{"ba\ufb04e", "baffle"},
		{"\ufb01ne", "fine"},
	}

	for _, tt := range tests {
		if got := Text(tt.in); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTextRemovesInvisibleCharacters(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hyphen\u00adation", "hyphenation"},
		{"zero\u200bwidth", "zerowidth"},
		{"\ufeffstart", "start"},
	}

	for _, tt := range tests {
		if got := Text(tt.in); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTextNormalizesSpaces(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a\u00a0b", "a b"},
		{"a\u202fb", "a b"},
		{"a\u2003b", "a b"},
		{"a\u3000b", "a b"},
		{"  leading and trailing  ", "leading and trailing"},
		{"many    spaces", "many spaces"},
		{"line\nbreak", "line break"},
		{"tab\tseparated", "tab separated"},
	}

	for _, tt := range tests {
		if got := Text(tt.in); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTextNormalizesUnicodeForm(t *testing.T) {
	// "é" as e + combining acute becomes a single code point.
	if got, want := Text("e\u0301"), "\u00e9"; got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
}

func TestTextCollapsesSpacedCaps(t *testing.T) {
	tests := []struct{ in, want string }{
		{"C O N T E N T S", "CONTENTS"},
		{"A B C D", "ABCD"},
		{"I am a cat", "I am a cat"},
		{"THE ROAD", "THE ROAD"},
		{"A B C", "A B C"}, // too short to be a spaced heading
	}

	for _, tt := range tests {
		if got := Text(tt.in); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTextIsIdempotent(t *testing.T) {
	inputs := []string{
		"ef\ufb01cient  engine",
		"C O N T E N T S",
		"a\u00a0b\u200bc",
		"  padded  ",
	}

	for _, in := range inputs {
		once := Text(in)
		if twice := Text(once); twice != once {
			t.Errorf("Text is not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}

func TestJoinLines(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"plain join", "the road", "bent north", "the road bent north"},
		{"dehyphenate lowercase", "exam-", "ple", "example"},
		{"keep hyphen before a capital", "Anglo-", "Saxon", "Anglo-Saxon"},
		{"em dash", "the end\u2014", "for now", "the end\u2014for now"},
		{"en dash", "pages 1\u2013", "10", "pages 1\u201310"},
		{"closing punctuation", "stalled", ", and stopped", "stalled, and stopped"},
		{"period", "the end", ".", "the end."},
		{"empty left", "", "text", "text"},
		{"empty right", "text", "", "text"},
		{"trims", "word ", " next", "word next"},
		{"unicode hyphen", "exam\u2010", "ple", "example"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinLines(tt.a, tt.b); got != tt.want {
				t.Errorf("JoinLines(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestApplyNormalizesEveryBlock(t *testing.T) {
	doc := idm.Document{
		Meta: idm.Metadata{Title: "Test", Language: "en"},
		Pages: []idm.Page{{
			Number: 1, Width: 595, Height: 842,
			Provenance: idm.ProvenanceDigital, TextCoverage: 0.4,
		}},
		Blocks: []idm.Block{
			{
				Kind: idm.KindParagraph, Text: "ef\ufb01cient  engine",
				Page: 1, BBox: idm.Rect{X0: 72, Y0: 600, X1: 500, Y1: 612},
				Confidence: 1, FontSize: 12,
			},
			{
				Kind: idm.KindHeader, Text: "C O N T E N T S",
				Page: 1, BBox: idm.Rect{X0: 72, Y0: 800, X1: 500, Y1: 812},
				Confidence: 1, FontSize: 12,
			},
		},
	}

	out := Apply(doc)

	if got, want := out.Blocks[0].Text, "efficient engine"; got != want {
		t.Errorf("block 0 text = %q, want %q", got, want)
	}
	if got, want := out.Blocks[1].Text, "CONTENTS"; got != want {
		t.Errorf("block 1 text = %q, want %q", got, want)
	}
	if err := out.Validate(); err != nil {
		t.Errorf("normalized document is invalid: %v", err)
	}

	if doc.Blocks[0].Text != "ef\ufb01cient  engine" {
		t.Error("Apply modified the caller's document")
	}
}

func TestApplyKeepsSpansConsistentWithText(t *testing.T) {
	doc := idm.Document{
		Meta: idm.Metadata{Title: "Test", Language: "en"},
		Pages: []idm.Page{{
			Number: 1, Width: 595, Height: 842,
			Provenance: idm.ProvenanceDigital, TextCoverage: 0.4,
		}},
		Blocks: []idm.Block{{
			Kind: idm.KindParagraph, Text: "an ef\ufb01cient engine",
			Page: 1, BBox: idm.Rect{X0: 72, Y0: 600, X1: 500, Y1: 612},
			Confidence: 1, FontSize: 12,
			Spans: []idm.Span{
				{Text: "an "},
				{Text: "ef\ufb01cient", Italic: true},
				{Text: " engine"},
			},
		}},
	}

	out := Apply(doc)

	if got, want := out.Blocks[0].Text, "an efficient engine"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if err := out.Validate(); err != nil {
		t.Errorf("spans no longer reconstruct the text: %v", err)
	}
}
