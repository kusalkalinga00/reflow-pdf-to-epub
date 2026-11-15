// Package normalize cleans up the text extraction produces.
//
// PDF text arrives with the artefacts of typesetting: ligature glyphs, soft
// hyphens, non-breaking spaces, and words broken across lines. None of it is
// visible on the page, but all of it reaches the reader in a reflowed book,
// where search and selection make it obvious.
package normalize

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// Apply returns a copy of doc with every block's text normalized.
func Apply(doc idm.Document) idm.Document {
	out := doc
	out.Blocks = make([]idm.Block, len(doc.Blocks))

	for i, b := range doc.Blocks {
		out.Blocks[i] = b
		out.Blocks[i].Text = Text(b.Text)

		if len(b.Spans) == 0 {
			continue
		}

		// Spans must still concatenate to Text. Runs are normalized with their
		// boundary spaces preserved, and if the result no longer matches the
		// block text, the text wins and styling is dropped rather than
		// producing a block that fails validation.
		spans := make([]idm.Span, len(b.Spans))
		var joined strings.Builder
		for j, s := range b.Spans {
			spans[j] = s
			spans[j].Text = spanText(s.Text)
			joined.WriteString(spans[j].Text)
		}
		if joined.String() != out.Blocks[i].Text {
			continue
		}
		out.Blocks[i].Spans = spans
	}

	return out
}

var ligatures = strings.NewReplacer(
	"\ufb00", "ff",
	"\ufb01", "fi",
	"\ufb02", "fl",
	"\ufb03", "ffi",
	"\ufb04", "ffl",
	"\ufb05", "st",
	"\ufb06", "st",
)

// invisible characters that carry no meaning in extracted text.
var invisible = strings.NewReplacer(
	"\u00ad", "", // soft hyphen: a break opportunity, not a character
	"\u200b", "", // zero-width space
	"\ufeff", "", // byte-order mark that leaked into the text layer
)

// Text normalizes one block of text. It is idempotent.
func Text(s string) string {
	if s == "" {
		return ""
	}

	s = strings.ToValidUTF8(s, "")
	s = ligatures.Replace(s)
	s = invisible.Replace(s)
	s = stripControl(s)
	s = norm.NFC.String(s)
	s = collapseSpaces(s)
	s = collapseSpacedCaps(s)

	return s
}

// stripControl removes control characters.
//
// PDFs whose fonts declare no usable encoding occasionally map punctuation to
// a control code: a hyphen becomes U+0002 in extraction. No book contains C0
// or C1 control characters, so removing them is always safe.
func stripControl(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isControl(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return false
	}
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}

// spanText normalizes a styled run, keeping one space at either edge so the
// runs still reproduce the block's spacing when concatenated.
func spanText(s string) string {
	if s == "" {
		return ""
	}

	leading := spaceAtStart(s)
	trailing := spaceAtEnd(s)
	core := Text(s)

	switch {
	case core == "":
		return " "
	case leading && trailing:
		return " " + core + " "
	case leading:
		return " " + core
	case trailing:
		return core + " "
	default:
		return core
	}
}

func spaceAtStart(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return isSpace(r)
}

func spaceAtEnd(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return isSpace(r)
}

// collapseSpaces turns every kind of typographic space into a single space and
// trims the result.
func collapseSpaces(s string) string {
	if !strings.ContainsFunc(s, isSpace) {
		return s
	}
	return strings.Join(strings.Fields(s), " ")
}

func isSpace(r rune) bool {
	switch r {
	case '\u00a0', // no-break space
		'\u2007', // figure space
		'\u202f', // narrow no-break space
		'\u2002', '\u2003', '\u2004', '\u2005',
		'\u2006', '\u2008', '\u2009', '\u200a',
		'\u3000': // ideographic space
		return true
	}
	return unicode.IsSpace(r)
}

// collapseSpacedCaps turns "C O N T E N T S" into "CONTENTS". Designers space
// out headings, and the extraction faithfully reports every letter as its own
// word.
func collapseSpacedCaps(s string) string {
	fields := strings.Fields(s)
	if len(fields) < 4 {
		return s
	}

	for _, f := range fields {
		if utf8.RuneCountInString(f) != 1 {
			return s
		}
		r, _ := utf8.DecodeRuneInString(f)
		if !unicode.IsUpper(r) && !unicode.IsPunct(r) {
			return s
		}
	}

	return strings.Join(fields, "")
}

// JoinLines joins two lines of the same paragraph, undoing the hyphenation and
// punctuation breaks that the line wrap introduced.
func JoinLines(a, b string) string {
	a = strings.TrimRight(a, " \t")
	b = strings.TrimLeft(b, " \t")

	if a == "" {
		return b
	}
	if b == "" {
		return a
	}

	last, _ := utf8.DecodeLastRuneInString(a)
	first, _ := utf8.DecodeRuneInString(b)

	switch {
	case isHyphen(last) && unicode.IsLower(first):
		// "exam-" + "ple" is one word.
		return a[:len(a)-len(string(last))] + b

	case isHyphen(last):
		// "Anglo-" + "Saxon" keeps its hyphen but loses the line break.
		return a + b

	case last == '\u2014' || last == '\u2013':
		// An em or en dash already separates the words.
		return a + b

	case strings.ContainsRune(",.;:!?)]}»”’", first):
		return a + b
	}

	return a + " " + b
}

func isHyphen(r rune) bool {
	return r == '-' || r == '\u2010' || r == '\u2011'
}
