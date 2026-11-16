package extract

import (
	"bytes"
	"context"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ocr"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
)

// fakeRecognizer stands in for Tesseract so the wiring and the coordinate
// conversion can be tested without an OCR engine.
type fakeRecognizer struct {
	lines []ocr.Line
	calls int
}

func (f *fakeRecognizer) Recognize(context.Context, []byte) ([]ocr.Line, error) {
	f.calls++
	return f.lines, nil
}

func extractWithOCR(t *testing.T, recognizer ocr.Recognizer, pdf []byte) idm.Document {
	t.Helper()

	extractor, err := NewPDFium(Options{OCR: recognizer})
	if err != nil {
		t.Fatalf("NewPDFium: %v", err)
	}
	t.Cleanup(func() { extractor.Close() })

	doc, err := extractor.Extract(context.Background(), bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return doc
}

func TestOCRRecoversPagesWithoutText(t *testing.T) {
	recognizer := &fakeRecognizer{lines: []ocr.Line{{
		Text: "Recovered from the page image",
		Left: 100, Top: 100, Right: 900, Bottom: 160,
		Confidence: 0.87,
	}}}

	// A page with no text layer at all, which is what a scan looks like.
	doc := extractWithOCR(t, recognizer, testpdf.Build(1, testpdf.WithPages(nil)))

	if recognizer.calls != 1 {
		t.Errorf("recognizer called %d times, want once", recognizer.calls)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("pages = %d, want 1", doc.PageCount())
	}
	if doc.Pages[0].Provenance != idm.ProvenanceOCR {
		t.Errorf("provenance = %q, want ocr", doc.Pages[0].Provenance)
	}
	if doc.Pages[0].TextCoverage <= 0 {
		t.Errorf("coverage = %v, want the recognized area", doc.Pages[0].TextCoverage)
	}

	if len(doc.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(doc.Blocks))
	}
	block := doc.Blocks[0]
	if block.Text != "Recovered from the page image" {
		t.Errorf("text = %q", block.Text)
	}
	if block.Confidence != 0.87 {
		t.Errorf("confidence = %v, want the OCR value", block.Confidence)
	}
	if !block.BBox.Valid() {
		t.Errorf("box = %+v, want a valid rectangle", block.BBox)
	}

	page := doc.Pages[0]
	if block.BBox.Y0 < 0 || block.BBox.Y1 > page.Height {
		t.Errorf("box %+v is outside the page (%v high); pixel-to-point conversion is wrong",
			block.BBox, page.Height)
	}
	// The box was near the top of the image, which is near the top of the page.
	if block.BBox.Y0 < page.Height/2 {
		t.Errorf("box %+v should sit in the upper half of the page", block.BBox)
	}
}

func TestOCRIsSkippedWhenTextExists(t *testing.T) {
	recognizer := &fakeRecognizer{lines: []ocr.Line{{
		Text: "should not appear", Left: 10, Top: 10, Right: 100, Bottom: 40, Confidence: 0.9,
	}}}

	doc := extractWithOCR(t, recognizer, testpdf.Build(1))

	if recognizer.calls != 0 {
		t.Errorf("recognizer was called %d times for a page with text", recognizer.calls)
	}
	if doc.Pages[0].Provenance != idm.ProvenanceDigital {
		t.Errorf("provenance = %q, want digital", doc.Pages[0].Provenance)
	}
	for _, b := range doc.Blocks {
		if b.Text == "should not appear" {
			t.Error("OCR text replaced the text layer")
		}
	}
}

func TestOCRDisabledLeavesBlankPages(t *testing.T) {
	doc := extractWithOCR(t, nil, testpdf.Build(1, testpdf.WithPages(nil)))

	if doc.PageCount() != 1 {
		t.Fatalf("pages = %d, want 1", doc.PageCount())
	}
	if len(doc.Blocks) != 0 {
		t.Errorf("blocks = %d, want none without OCR", len(doc.Blocks))
	}
	if doc.Pages[0].Provenance != idm.ProvenanceDigital {
		t.Errorf("provenance = %q, want digital", doc.Pages[0].Provenance)
	}
}
