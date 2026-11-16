// Package score turns measured signals into a fidelity verdict.
//
// The point is honesty: the pipeline always produces a file, so the reader has
// to be told how much of the source it actually understood. Every number here
// comes from something that was measured — page coverage, chapter detection,
// block confidence — not from a model's opinion of its own output.
package score

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/render"
)

// Tier describes how much structure the conversion recovered.
type Tier string

const (
	// TierStructured has chapters and headings that were detected, not guessed.
	TierStructured Tier = "T1"
	// TierReflowed has clean text but uncertain structure.
	TierReflowed Tier = "T2"
	// TierFacsimile is reserved for the OCR and page-image path, which does
	// not exist yet. Nothing currently produces it.
	TierFacsimile Tier = "T3"
)

// Severity ranks an issue's effect on the reader.
type Severity string

const (
	SeverityInfo Severity = "info"
	SeverityWarn Severity = "warn"
)

// Issue is one thing the reader should know about.
type Issue struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
}

// Signals are the measurements the score is computed from. They are reported
// alongside the score so a number can always be traced to a cause.
type Signals struct {
	Pages           int     `json:"pages"`
	Blocks          int     `json:"blocks"`
	Headings        int     `json:"headings"`
	Chapters        int     `json:"chapters"`
	GenericChapters int     `json:"genericChapters"`
	BlankPages      int     `json:"blankPages"`
	Furniture       int     `json:"furnitureRemoved"`
	LowConfidence   int     `json:"lowConfidenceBlocks"`
	OCRedPages      int     `json:"ocrPages"`
	OutlineEntries  int     `json:"outlineEntries"`
	Tables          int     `json:"tables"`
	Figures         int     `json:"figures"`
	MeanCoverage    float64 `json:"meanCoverage"`
}

// Report is the verdict for one conversion.
type Report struct {
	Score   int     `json:"score"`
	Tier    Tier    `json:"tier"`
	Summary string  `json:"summary"`
	Issues  []Issue `json:"issues,omitempty"`
	Signals Signals `json:"signals"`
}

// Evaluate scores a converted document.
//
// paras and chapters must come from the same render pass that produced the
// book, so the verdict describes what the reader will actually open.
func Evaluate(doc idm.Document, paras []render.Paragraph, chapters []string) Report {
	s := gather(doc, paras, chapters)
	r := Report{Signals: s}

	score := 100

	deduct := func(points int, code, format string, args ...any) {
		score -= points
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityWarn,
			Code:     code,
			Message:  fmt.Sprintf(format, args...),
		})
	}

	if s.BlankPages > 0 {
		deduct(min(20, 2*s.BlankPages), "BLANK_PAGES",
			"%d page%s had no extractable text, so any images on them are missing.",
			s.BlankPages, plural(s.BlankPages))
	}

	if s.Headings == 0 {
		deduct(15, "NO_HEADINGS",
			"No headings were found, so the whole book is one chapter.")
	}

	if s.GenericChapters > 0 {
		deduct(min(10, 5*s.GenericChapters), "GENERIC_CHAPTERS",
			"A placeholder title was used for %d chapter%s.",
			s.GenericChapters, plural(s.GenericChapters))
	}

	if s.Blocks > 0 && s.LowConfidence > s.Blocks/5 {
		deduct(10, "LOW_CONFIDENCE",
			"%d%% of blocks were classified with low confidence.",
			100*s.LowConfidence/s.Blocks)
	}

	if s.OutlineEntries > 0 {
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityInfo,
			Code:     "OUTLINE_USED",
			Message:  fmt.Sprintf("Chapters follow the PDF's own outline (%d entries).", s.OutlineEntries),
		})
	}

	if s.Tables > 0 {
		deduct(min(10, 5*s.Tables), "TABLES_FLATTENED",
			"Flattened %d table%s into running text; the column layout is lost.",
			s.Tables, plural(s.Tables))
	}

	if s.Figures > 0 {
		deduct(min(10, 2*s.Figures), "FIGURES_DROPPED",
			"Left %d figure%s in the PDF; images are not extracted yet.",
			s.Figures, plural(s.Figures))
	}

	if s.OCRedPages > 0 {
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityInfo,
			Code:     "OCR_USED",
			Message: fmt.Sprintf("%d page%s read with OCR; recognition errors are possible.",
				s.OCRedPages, plural(s.OCRedPages)),
		})
	}

	if s.Furniture > 0 {
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityInfo,
			Code:     "FURNITURE_REMOVED",
			Message:  fmt.Sprintf("Removed %d running header%s and footer%s.", s.Furniture, plural(s.Furniture), plural(s.Furniture)),
		})
	}

	r.Score = clamp(score, 0, 100)
	r.Tier = tierFor(r.Score, s)
	r.Summary = summarize(r.Tier, s)

	sort.SliceStable(r.Issues, func(i, j int) bool {
		if r.Issues[i].Severity != r.Issues[j].Severity {
			return r.Issues[i].Severity == SeverityWarn
		}
		return r.Issues[i].Code < r.Issues[j].Code
	})

	return r
}

func gather(doc idm.Document, paras []render.Paragraph, chapters []string) Signals {
	s := Signals{
		Pages:    doc.PageCount(),
		Blocks:   len(doc.Blocks),
		Chapters: len(chapters),
	}

	for _, p := range paras {
		if p.Kind == idm.KindHeading {
			s.Headings++
		}
	}

	for _, b := range doc.Blocks {
		switch {
		case b.Kind.IsFurniture():
			s.Furniture++
		case b.Confidence < 0.6:
			s.LowConfidence++
		}
	}

	for _, page := range doc.Pages {
		if page.Provenance == idm.ProvenanceOCR {
			s.OCRedPages++
		}
	}

	var coverage float64
	for _, page := range doc.Pages {
		if page.TextCoverage <= 0 {
			s.BlankPages++
		}
		coverage += page.TextCoverage
	}
	if len(doc.Pages) > 0 {
		s.MeanCoverage = coverage / float64(len(doc.Pages))
	}

	s.OutlineEntries = len(doc.Outline)
	s.Tables = doc.Features.Tables
	s.Figures = doc.Features.Figures

	for _, title := range chapters {
		if isGenericTitle(title) {
			s.GenericChapters++
		}
	}

	return s
}

func isGenericTitle(title string) bool {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "", "content", "untitled", "body":
		return true
	}
	return false
}

func tierFor(score int, s Signals) Tier {
	hasStructure := s.Headings > 0 || s.OutlineEntries > 0
	if score >= 85 && hasStructure && s.GenericChapters == 0 {
		return TierStructured
	}
	if score >= 50 {
		return TierReflowed
	}
	return TierFacsimile
}

func summarize(tier Tier, s Signals) string {
	switch tier {
	case TierStructured:
		return fmt.Sprintf("Structured ebook: %d chapters with headings detected.", s.Chapters)
	case TierReflowed:
		return "Clean text, but the structure is uncertain. Check the chapter breaks before reading."
	default:
		return "Low confidence: too little structure was recovered to trust the layout."
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
