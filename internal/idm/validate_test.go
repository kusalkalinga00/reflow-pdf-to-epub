package idm

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

// validDocument returns a document that must pass Validate. Every test either
// uses it unchanged or mutates exactly one thing.
func validDocument() Document {
	return Document{
		Meta: Metadata{
			Title:      "The Long Road",
			Authors:    []string{"A. Author"},
			Language:   "en",
			SourceHash: "9f2c1b",
			SourceName: "book.pdf",
		},
		Pages: []Page{
			{
				Number: 1, Width: 595, Height: 842, Rotation: 0,
				Provenance: ProvenanceDigital, TextCoverage: 0.42, Label: "1",
			},
			{
				Number: 2, Width: 595, Height: 842, Rotation: 0,
				Provenance: ProvenanceDigital, TextCoverage: 0.51, Label: "2",
			},
		},
		Blocks: []Block{
			{
				Kind: KindHeading, Text: "Chapter 1", Level: 1, Page: 1,
				BBox:       Rect{X0: 72, Y0: 700, X1: 523, Y1: 730},
				Confidence: 0.98, Column: 0,
			},
			{
				Kind: KindParagraph, Text: "The road bent north and the engine never stalled.",
				Page:       1,
				BBox:       Rect{X0: 72, Y0: 600, X1: 523, Y1: 660},
				Confidence: 0.95, Column: 0,
				Spans: []Span{
					{Text: "The road bent north and the "},
					{Text: "engine", Italic: true},
					{Text: " never stalled."},
				},
			},
			{
				Kind: KindFootnote, Text: "Rebuilt in the spring of 1892.",
				Page:       2,
				BBox:       Rect{X0: 72, Y0: 80, X1: 523, Y1: 100},
				Confidence: 0.90, Column: 0,
			},
		},
	}
}

func TestValidDocumentPasses(t *testing.T) {
	if err := validDocument().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsSingleMutations(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Document)
		path string
	}{
		{
			name: "missing language",
			mut:  func(d *Document) { d.Meta.Language = "" },
			path: "meta.language",
		},
		{
			name: "no pages",
			mut:  func(d *Document) { d.Pages = nil },
			path: "pages",
		},
		{
			name: "page number does not match index",
			mut:  func(d *Document) { d.Pages[0].Number = 7 },
			path: "pages[0].number",
		},
		{
			name: "zero page width",
			mut:  func(d *Document) { d.Pages[0].Width = 0 },
			path: "pages[0].width",
		},
		{
			name: "not-a-number page height",
			mut:  func(d *Document) { d.Pages[0].Height = math.NaN() },
			path: "pages[0].height",
		},
		{
			name: "unsupported rotation",
			mut:  func(d *Document) { d.Pages[0].Rotation = 45 },
			path: "pages[0].rotation",
		},
		{
			name: "unknown provenance",
			mut:  func(d *Document) { d.Pages[1].Provenance = "guess" },
			path: "pages[1].provenance",
		},
		{
			name: "coverage above one",
			mut:  func(d *Document) { d.Pages[0].TextCoverage = 1.5 },
			path: "pages[0].textCoverage",
		},
		{
			name: "unknown block kind",
			mut:  func(d *Document) { d.Blocks[0].Kind = "sidebar" },
			path: "blocks[0].kind",
		},
		{
			name: "block page out of range",
			mut:  func(d *Document) { d.Blocks[2].Page = 9 },
			path: "blocks[2].page",
		},
		{
			name: "blocks out of reading order",
			mut: func(d *Document) {
				d.Blocks[0].Page = 2
				d.Blocks[1].Page = 1
			},
			path: "blocks[1].page",
		},
		{
			name: "negative column",
			mut:  func(d *Document) { d.Blocks[0].Column = -1 },
			path: "blocks[0].column",
		},
		{
			name: "confidence above one",
			mut:  func(d *Document) { d.Blocks[1].Confidence = 1.01 },
			path: "blocks[1].confidence",
		},
		{
			name: "unbounded bbox",
			mut:  func(d *Document) { d.Blocks[1].BBox.Y1 = math.Inf(1) },
			path: "blocks[1].bbox",
		},
		{
			name: "heading without level",
			mut:  func(d *Document) { d.Blocks[0].Level = 0 },
			path: "blocks[0].level",
		},
		{
			name: "heading level out of range",
			mut:  func(d *Document) { d.Blocks[0].Level = 7 },
			path: "blocks[0].level",
		},
		{
			name: "level on a non-heading",
			mut:  func(d *Document) { d.Blocks[1].Level = 2 },
			path: "blocks[1].level",
		},
		{
			name: "empty paragraph",
			mut:  func(d *Document) { d.Blocks[1].Text = "   " },
			path: "blocks[1].text",
		},
		{
			name: "empty span",
			mut:  func(d *Document) { d.Blocks[1].Spans[1].Text = "" },
			path: "blocks[1].spans[1].text",
		},
		{
			name: "spans do not match text",
			mut:  func(d *Document) { d.Blocks[1].Text = "Something else entirely." },
			path: "blocks[1].spans",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := validDocument()
			tt.mut(&doc)

			err := doc.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() error does not wrap ErrInvalid: %v", err)
			}
			if !strings.Contains(err.Error(), tt.path) {
				t.Errorf("Validate() = %q, want it to name %q", err, tt.path)
			}
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	doc := validDocument()
	doc.Meta.Language = ""
	doc.Pages[0].Width = -1
	doc.Blocks[1].Kind = "sidebar"

	var verr *ValidationError
	if !errors.As(doc.Validate(), &verr) {
		t.Fatal("Validate() did not return a *ValidationError")
	}
	if len(verr.Problems) != 3 {
		t.Fatalf("got %d problems, want 3: %v", len(verr.Problems), verr.Problems)
	}

	wantPaths := []string{"meta.language", "pages[0].width", "blocks[1].kind"}
	for i, want := range wantPaths {
		if got := verr.Problems[i].Path; got != want {
			t.Errorf("problems[%d].Path = %q, want %q", i, got, want)
		}
	}
}

func TestValidationErrorSingular(t *testing.T) {
	err := (&ValidationError{Problems: []Problem{{Path: "pages", Message: "is empty"}}}).Error()
	if !strings.Contains(err, "1 problem)") {
		t.Errorf("Error() = %q, want singular wording", err)
	}
}

func TestBodySkipsHeaderAndFooter(t *testing.T) {
	doc := validDocument()
	doc.Blocks = append(doc.Blocks,
		Block{
			Kind: KindHeader, Text: "The Long Road", Page: 2,
			BBox: Rect{X0: 72, Y0: 800, X1: 523, Y1: 815}, Confidence: 1,
		},
		Block{
			Kind: KindFooter, Text: "42", Page: 2,
			BBox: Rect{X0: 72, Y0: 30, X1: 523, Y1: 45}, Confidence: 1,
		},
	)

	body := doc.Body()
	if len(body) != 3 {
		t.Fatalf("Body() returned %d blocks, want 3", len(body))
	}
	for _, b := range body {
		if b.Kind.IsFurniture() {
			t.Errorf("Body() contains furniture block %q", b.Text)
		}
	}

	if got := doc.Text(); strings.Contains(got, "The Long Road") {
		t.Errorf("Text() = %q, want headers and footers excluded", got)
	}
}

func TestDocumentJSONRoundTrip(t *testing.T) {
	doc := validDocument()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Document
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(doc, got) {
		t.Errorf("round trip changed the document:\n got %+v\nwant %+v", got, doc)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("round-tripped document failed validation: %v", err)
	}
}

func TestKindIsFurniture(t *testing.T) {
	for _, k := range []Kind{KindHeader, KindFooter} {
		if !k.IsFurniture() {
			t.Errorf("%q.IsFurniture() = false, want true", k)
		}
	}
	for _, k := range []Kind{KindParagraph, KindHeading, KindTable, KindFigure} {
		if k.IsFurniture() {
			t.Errorf("%q.IsFurniture() = true, want false", k)
		}
	}
}
