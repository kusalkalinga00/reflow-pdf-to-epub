package idm

import "strings"

// Provenance records where a page's text came from. It is per page rather than
// per document because a single PDF can mix digital and scanned pages.
type Provenance string

const (
	// ProvenanceDigital means the text came from the PDF's own text layer.
	ProvenanceDigital Provenance = "digital"
	// ProvenanceOCR means the text was recovered by optical recognition.
	ProvenanceOCR Provenance = "ocr"
)

// Valid reports whether p is a known provenance.
func (p Provenance) Valid() bool {
	switch p {
	case ProvenanceDigital, ProvenanceOCR:
		return true
	}
	return false
}

// Kind classifies a block of content. The set is closed: a block that does not
// fit one of these is a paragraph, so unknown kinds indicate a bug upstream.
type Kind string

const (
	KindParagraph Kind = "para"
	KindHeading   Kind = "heading"
	KindList      Kind = "list"
	KindTable     Kind = "table"
	KindFigure    Kind = "figure"
	KindCaption   Kind = "caption"
	KindCode      Kind = "code"
	KindFootnote  Kind = "footnote"
	KindHeader    Kind = "header"
	KindFooter    Kind = "footer"
)

// Valid reports whether k is a known block kind.
func (k Kind) Valid() bool {
	switch k {
	case KindParagraph, KindHeading, KindList, KindTable, KindFigure,
		KindCaption, KindCode, KindFootnote, KindHeader, KindFooter:
		return true
	}
	return false
}

// IsFurniture reports whether the block is page furniture that must not appear
// in the reflowed body: running headers and footers.
func (k Kind) IsFurniture() bool {
	return k == KindHeader || k == KindFooter
}

// Metadata describes the source document.
type Metadata struct {
	Title      string   `json:"title"`
	Authors    []string `json:"authors,omitempty"`
	Language   string   `json:"language"`
	Publisher  string   `json:"publisher,omitempty"`
	Subject    string   `json:"subject,omitempty"`
	SourceHash string   `json:"sourceHash,omitempty"`
	SourceName string   `json:"sourceName,omitempty"`
}

// Page is one page of the source document.
type Page struct {
	// Number is 1-based and matches the PDF page index.
	Number int `json:"number"`
	// Width and Height are in PDF points (1/72 inch), before rotation.
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	// Rotation is the page's display rotation in degrees.
	Rotation int `json:"rotation"`
	// Provenance says whether the text on this page is digital or OCR.
	Provenance Provenance `json:"provenance"`
	// TextCoverage is a normalised 0..1 estimate of how much of the page holds
	// machine-readable text. 0 means image only; 1 means dense text.
	TextCoverage float64 `json:"textCoverage"`
	// Label is the page number printed on the page, which often differs from
	// Number (front matter is roman, or numbering restarts per chapter). It
	// feeds the EPUB page list.
	Label string `json:"label,omitempty"`
}

// Block is a contiguous run of content in reading order.
type Block struct {
	Kind Kind `json:"kind"`
	// Text is authoritative for content. When Spans is non-empty their
	// concatenation must equal Text; Validate enforces that.
	Text string `json:"text"`
	// Level is the heading level, 1..6. It is zero for every other kind.
	Level int `json:"level,omitempty"`
	// Page is the 1-based source page the block starts on.
	Page int `json:"page"`
	// BBox is the block's bounding box in PDF user space.
	BBox Rect `json:"bbox"`
	// Confidence is 0..1 and records how sure the stage that produced this
	// block is about its classification. It feeds the quality score.
	Confidence float64 `json:"confidence"`
	// FontSize is the effective text size in points. Extraction derives it from
	// the glyph bounding box, not from the PDF's nominal font size, because
	// producers routinely draw 10pt text with a 1pt font and a scaling matrix.
	FontSize float64 `json:"fontSize,omitempty"`
	// Column is the 0-based column index of the block on its page.
	Column int `json:"column"`
	// Spans carries inline styling. It is optional: a block with no spans has
	// no styling beyond its kind.
	Spans []Span `json:"spans,omitempty"`
}

// Span is a styled run of text inside a block.
type Span struct {
	Text   string `json:"text"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
	Mono   bool   `json:"mono,omitempty"`
	Super  bool   `json:"super,omitempty"`
	// Href is set when the span is a link, including footnote references.
	Href string `json:"href,omitempty"`
}

// OutlineEntry is one bookmark from the PDF's own table of contents. Page is
// 1-based, or 0 when the bookmark does not point at a page.
type OutlineEntry struct {
	Title string `json:"title"`
	Page  int    `json:"page"`
	Level int    `json:"level"`
}

// Features records structure the source declares but the conversion cannot
// represent yet. It exists so the quality report can say "a table was here"
// instead of silently flattening it.
type Features struct {
	// Tagged is true when the PDF carries a structure tree.
	Tagged bool `json:"tagged,omitempty"`
	// Tables, Figures and HeadingElements count structure elements.
	Tables          int `json:"tables,omitempty"`
	Figures         int `json:"figures,omitempty"`
	HeadingElements int `json:"headingElements,omitempty"`
}

// Document is a whole source document.
type Document struct {
	Meta     Metadata       `json:"meta"`
	Pages    []Page         `json:"pages"`
	Blocks   []Block        `json:"blocks"`
	Outline  []OutlineEntry `json:"outline,omitempty"`
	Features Features       `json:"features,omitempty"`
}

// WithBlocks returns a copy of d with its blocks replaced and every other field
// carried over.
//
// Pipeline stages that reorder or rewrite blocks must use this rather than
// building a Document field by field: a hand-built document silently drops any
// field added later, which is how the outline was lost once already.
func (d Document) WithBlocks(blocks []Block) Document {
	d.Blocks = blocks
	return d
}

// PageCount returns the number of pages.
func (d Document) PageCount() int { return len(d.Pages) }

// Body returns the blocks that belong in the reflowed body: everything except
// running headers and footers.
func (d Document) Body() []Block {
	body := make([]Block, 0, len(d.Blocks))
	for _, b := range d.Blocks {
		if b.Kind.IsFurniture() {
			continue
		}
		body = append(body, b)
	}
	return body
}

// Text returns the readable text of the body, one block per line. It is the
// input to the corpus metrics, not to EPUB assembly.
func (d Document) Text() string {
	var b strings.Builder
	for _, block := range d.Body() {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(block.Text)
	}
	return b.String()
}
