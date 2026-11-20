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
	RecoveredTables int     `json:"recoveredTables"`
	Footnotes       int     `json:"footnotes"`
	Tables          int     `json:"tables"`
	Figures         int     `json:"figures"`
	FiguresEmbedded int     `json:"figuresEmbedded"`
	ImagesSkipped   int     `json:"imagesSkipped"`
	Links           int     `json:"links"`
	LinkAnnotations int     `json:"linkAnnotations"`
	ExternalLinks   int     `json:"externalLinks"`
	LinksSkipped    int     `json:"linksSkipped"`
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

	if missing := s.Figures - s.FiguresEmbedded; missing > 0 {
		deduct(min(10, 2*missing), "FIGURES_DROPPED",
			"Left %d figure%s in the PDF; they could not be extracted.",
			missing, plural(missing))
	}

	if s.ImagesSkipped > 0 {
		deduct(min(10, 2*s.ImagesSkipped), "IMAGES_SKIPPED",
			"Skipped %d image%s in a format EPUB cannot embed.",
			s.ImagesSkipped, plural(s.ImagesSkipped))
	}

	if s.Footnotes > 0 {
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityInfo,
			Code:     "FOOTNOTES",
			Message:  fmt.Sprintf("Recovered %d footnote%s.", s.Footnotes, plural(s.Footnotes)),
		})
	}

	if s.RecoveredTables > 0 {
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityInfo,
			Code:     "TABLES_RECOVERED",
			Message:  fmt.Sprintf("Recovered %d ruled table%s as real grids.", s.RecoveredTables, plural(s.RecoveredTables)),
		})
	}

	if s.Links > 0 {
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityInfo,
			Code:     "LINKS",
			Message: fmt.Sprintf("Carried %d link%s into the text, %d of them to the web.",
				s.Links, plural(s.Links), s.ExternalLinks),
		})
	}

	if s.LinksSkipped > 0 {
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityInfo,
			Code:     "LINKS_SKIPPED",
			Message: fmt.Sprintf("Left %d link%s unlinked: their target is a script, a local file, or another document.",
				s.LinksSkipped, plural(s.LinksSkipped)),
		})
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
		switch p.Kind {
		case idm.KindHeading:
			s.Headings++
		case idm.KindFootnote:
			s.Footnotes++
		}
	}

	for _, b := range doc.Blocks {
		switch {
		case b.Kind == idm.KindTable:
			s.RecoveredTables++
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

	// A page whose text could not be read is a loss only if it had nothing
	// else to recover. A cover or an illustration page has no text by design,
	// and its figure is embedded, so counting it would report a loss that does
	// not exist.
	figurePages := make(map[int]bool, len(paras))
	for _, p := range paras {
		if p.Kind == idm.KindFigure {
			figurePages[p.Page] = true
		}
	}

	var coverage float64
	for _, page := range doc.Pages {
		if page.TextCoverage <= 0 && !figurePages[page.Number] {
			s.BlankPages++
		}
		coverage += page.TextCoverage
	}
	if len(doc.Pages) > 0 {
		s.MeanCoverage = coverage / float64(len(doc.Pages))
	}

	// A link counts when it sits on text the reader can click. Annotations
	// over a picture or over white space carry no text, so they are counted
	// separately rather than reported as links that were lost.
	for _, p := range paras {
		for _, span := range p.Spans {
			if span.Href == "" {
				continue
			}
			s.Links++
			if strings.HasPrefix(span.Href, "http://") || strings.HasPrefix(span.Href, "https://") {
				s.ExternalLinks++
			}
		}
	}
	s.LinkAnnotations = len(doc.Links)
	s.LinksSkipped = doc.Features.LinksSkipped

	s.OutlineEntries = len(doc.Outline)
	s.Tables = doc.Features.Tables
	s.Figures = doc.Features.Figures
	s.ImagesSkipped = doc.Features.ImagesSkipped

	for _, b := range doc.Blocks {
		if b.Image != nil {
			s.FiguresEmbedded++
		}
	}

	for _, title := range chapters {
		if isGenericTitle(title) {
			s.GenericChapters++
		}
	}

	return s
}

// Facsimile scores a page-image book.
//
// The layout is faithful — every page is the page the producer drew — and the
// loss is that none of it is text: not selectable, not searchable, and not
// reflowable. That loss is named and costs a fixed 35 points; pages that could
// not be rendered cost more.
func Facsimile(pages, rendered, chapters int) Report {
	s := Signals{
		Pages:           pages,
		Chapters:        chapters,
		Figures:         rendered,
		FiguresEmbedded: rendered,
	}
	r := Report{Signals: s, Tier: TierFacsimile}

	score := 65
	r.Issues = append(r.Issues, Issue{
		Severity: SeverityWarn,
		Code:     "PAGE_IMAGES",
		Message:  fmt.Sprintf("Page-image book: %d pages are images. The text is not selectable or searchable.", rendered),
	})

	if missing := pages - rendered; missing > 0 {
		score -= min(30, 5*missing)
		r.Issues = append(r.Issues, Issue{
			Severity: SeverityWarn,
			Code:     "PAGES_MISSING",
			Message:  fmt.Sprintf("%d page%s could not be rendered.", missing, plural(missing)),
		})
	}

	r.Score = clamp(score, 1, 100)
	r.Summary = fmt.Sprintf("Page images: %d pages rendered. The layout is faithful; the text is not selectable.", rendered)
	return r
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
