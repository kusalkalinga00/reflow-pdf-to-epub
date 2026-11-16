package score_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/render"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/score"
)

func document(pages int, coverage float64, blocks ...idm.Block) idm.Document {
	doc := idm.Document{
		Meta:   idm.Metadata{Title: "Test", Language: "en"},
		Blocks: blocks,
	}
	for i := 1; i <= pages; i++ {
		doc.Pages = append(doc.Pages, idm.Page{
			Number: i, Width: 595, Height: 842,
			Provenance: idm.ProvenanceDigital, TextCoverage: coverage,
		})
	}
	return doc
}

func block(kind idm.Kind, confidence float64) idm.Block {
	return idm.Block{
		Kind: kind, Text: "text", Page: 1, Confidence: confidence,
		BBox: idm.Rect{X0: 72, Y0: 600, X1: 500, Y1: 612}, FontSize: 12,
	}
}

func paragraphs() []render.Paragraph {
	return []render.Paragraph{
		{Kind: idm.KindHeading, Level: 1, Text: "Chapter One"},
		{Kind: idm.KindParagraph, Text: "Body text."},
	}
}

func TestStructuredBookScoresFullMarks(t *testing.T) {
	report := score.Evaluate(
		document(3, 0.3, block(idm.KindParagraph, 1)),
		paragraphs(),
		[]string{"Chapter One", "Chapter Two"},
	)

	if report.Score != 100 {
		t.Errorf("score = %d, want 100: %v", report.Score, report.Issues)
	}
	if report.Tier != score.TierStructured {
		t.Errorf("tier = %q, want T1", report.Tier)
	}
	for _, issue := range report.Issues {
		if issue.Severity == score.SeverityWarn {
			t.Errorf("unexpected warning: %s", issue.Message)
		}
	}
}

func TestNoHeadingsIsReflowedWithAWarning(t *testing.T) {
	report := score.Evaluate(
		document(3, 0.3, block(idm.KindParagraph, 1)),
		[]render.Paragraph{{Kind: idm.KindParagraph, Text: "Body."}},
		[]string{"Content"},
	)

	if report.Tier != score.TierReflowed {
		t.Errorf("tier = %q, want T2", report.Tier)
	}
	if report.Score >= 85 {
		t.Errorf("score = %d, want below 85 without headings", report.Score)
	}
	if !hasIssue(report, "NO_HEADINGS") {
		t.Errorf("issues = %v, want NO_HEADINGS", report.Issues)
	}
}

func TestBlankPagesCostPoints(t *testing.T) {
	doc := document(3, 0.3, block(idm.KindParagraph, 1))
	doc.Pages[1].TextCoverage = 0
	doc.Pages[2].TextCoverage = 0

	report := score.Evaluate(doc, paragraphs(), []string{"Chapter One"})

	if report.Signals.BlankPages != 2 {
		t.Fatalf("blank pages = %d, want 2", report.Signals.BlankPages)
	}
	if report.Score != 96 {
		t.Errorf("score = %d, want 96", report.Score)
	}
	if !hasIssue(report, "BLANK_PAGES") {
		t.Errorf("issues = %v, want BLANK_PAGES", report.Issues)
	}
}

func TestGenericChapterTitlesCostPoints(t *testing.T) {
	report := score.Evaluate(
		document(2, 0.3, block(idm.KindParagraph, 1)),
		paragraphs(),
		[]string{"Front matter", "Chapter One"},
	)

	if report.Signals.GenericChapters != 1 {
		t.Errorf("generic chapters = %d, want 1", report.Signals.GenericChapters)
	}
	if report.Score != 95 {
		t.Errorf("score = %d, want 95", report.Score)
	}
	if report.Tier == score.TierStructured {
		t.Error("a placeholder chapter title should not be T1")
	}
}

func TestLowConfidenceBlocksCostPoints(t *testing.T) {
	var blocks []idm.Block
	for i := 0; i < 5; i++ {
		confidence := 1.0
		if i < 2 {
			confidence = 0.3
		}
		blocks = append(blocks, block(idm.KindParagraph, confidence))
	}

	report := score.Evaluate(document(2, 0.3, blocks...), paragraphs(), []string{"Chapter One"})

	if report.Signals.LowConfidence != 2 {
		t.Errorf("low confidence = %d, want 2", report.Signals.LowConfidence)
	}
	if report.Score != 90 {
		t.Errorf("score = %d, want 90", report.Score)
	}
}

func TestScoreNeverGoesNegative(t *testing.T) {
	var blocks []idm.Block
	for i := 0; i < 50; i++ {
		blocks = append(blocks, block(idm.KindParagraph, 0.1))
	}

	doc := document(50, 0, blocks...)
	report := score.Evaluate(doc, []render.Paragraph{{Kind: idm.KindParagraph, Text: "x"}}, []string{"Content"})

	if report.Score < 0 {
		t.Errorf("score = %d, want >= 0", report.Score)
	}
	// T3 is reserved for the page-facsimile path, which does not exist yet: a
	// reflow with poor structure is still a reflow, so it stays at T2.
	if report.Tier != score.TierReflowed {
		t.Errorf("tier = %q, want T2", report.Tier)
	}
}

func TestReportIsDeterministic(t *testing.T) {
	build := func() score.Report {
		return score.Evaluate(
			document(3, 0.3, block(idm.KindParagraph, 1)),
			paragraphs(),
			[]string{"Chapter One"},
		)
	}

	if !reflect.DeepEqual(build(), build()) {
		t.Error("two evaluations of the same input differ")
	}
}

func TestFurnitureIsReportedAsInfo(t *testing.T) {
	report := score.Evaluate(
		document(4, 0.3, block(idm.KindParagraph, 1), block(idm.KindHeader, 1)),
		paragraphs(),
		[]string{"Chapter One"},
	)

	if report.Signals.Furniture != 1 {
		t.Fatalf("furniture = %d, want 1", report.Signals.Furniture)
	}
	if report.Score != 100 {
		t.Errorf("score = %d, want 100: removing furniture is good", report.Score)
	}

	var found bool
	for _, issue := range report.Issues {
		if issue.Code == "FURNITURE_REMOVED" {
			found = true
			if issue.Severity != score.SeverityInfo {
				t.Errorf("severity = %q, want info", issue.Severity)
			}
		}
	}
	if !found {
		t.Errorf("issues = %v, want FURNITURE_REMOVED", report.Issues)
	}
}

func TestOCRedPagesAreReported(t *testing.T) {
	doc := document(2, 0.3, block(idm.KindParagraph, 1))
	doc.Pages[1].Provenance = idm.ProvenanceOCR

	report := score.Evaluate(doc, paragraphs(), []string{"Chapter One"})

	if report.Signals.OCRedPages != 1 {
		t.Fatalf("ocr pages = %d, want 1", report.Signals.OCRedPages)
	}
	if report.Score != 100 {
		t.Errorf("score = %d, want 100: OCR is better than a blank page", report.Score)
	}
	if !hasIssue(report, "OCR_USED") {
		t.Errorf("issues = %v, want OCR_USED", report.Issues)
	}
}

func TestSummaryMatchesTier(t *testing.T) {
	structured := score.Evaluate(document(1, 0.3, block(idm.KindParagraph, 1)), paragraphs(), []string{"Chapter One"})
	if !strings.Contains(structured.Summary, "Structured") {
		t.Errorf("summary = %q", structured.Summary)
	}
}

func hasIssue(r score.Report, code string) bool {
	for _, issue := range r.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
