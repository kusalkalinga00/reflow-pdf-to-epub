package corpus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/convert"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ocr"
)

// Thresholds a case must meet to pass. They are deliberately strict: the
// corpus exists to catch regressions, and a loose threshold catches nothing.
const (
	MinCharRecall    = 0.99
	MaxWordErrorRate = 0.02
)

// Case is one PDF and what reading it should produce.
type Case struct {
	Name string
	PDF  []byte

	// WantText is the text a reader should see, with paragraphs separated by a
	// single space.
	WantText string
	// WantOrder are substrings that must appear in this order. It is how
	// reading order is checked without pinning exact wording.
	WantOrder []string
	// WantChapters are the chapter titles the EPUB should have, in order.
	WantChapters []string
	// WantError is the error the pipeline must return, for cases that are
	// expected to fail (a scan with no text layer, for example).
	WantError error
	// NoExpectation marks a diagnostic case with no ground truth: the
	// conversion still runs, but only "did it produce a book" is checked.
	NoExpectation bool
	// Reference marks expected text that came from another extractor rather
	// than from the document's source.
	Reference bool
}

// Result is the outcome of running one case.
type Result struct {
	Name string `json:"name"`

	CharRecall    float64  `json:"charRecall"`
	WordErrorRate float64  `json:"wordErrorRate"`
	OrderOK       bool     `json:"orderOK"`
	ChapterTitles []string `json:"chapters"`

	// Text is the reading text the pipeline produced, kept for diagnosing a
	// failure.
	Text string `json:"text,omitempty"`
	// ExpectedError marks a case that is supposed to fail, such as a scan with
	// no text layer. Metrics are not meaningful for those.
	ExpectedError bool `json:"expectedError,omitempty"`
	// NoExpectation is set for diagnostic cases without expected text.
	NoExpectation bool `json:"noExpectation,omitempty"`
	// Reference is set when the expected text came from another extractor.
	Reference bool `json:"reference,omitempty"`
	Pages     int  `json:"pages,omitempty"`

	Pass    bool   `json:"pass"`
	Failure string `json:"failure,omitempty"`
	Err     string `json:"error,omitempty"`

	Duration time.Duration `json:"durationMs"`
}

// Run converts every case and scores it. One PDFium pool is shared for the
// whole run.
func Run(cases []Case) []Result {
	var recognizer ocr.Recognizer
	if t, err := ocr.NewTesseract(); err == nil {
		recognizer = t
	}

	converter, err := convert.New(convert.Options{OCR: recognizer})
	if err != nil {
		results := make([]Result, len(cases))
		for i, c := range cases {
			results[i] = Result{Name: c.Name, Failure: fmt.Sprintf("start converter: %v", err)}
		}
		return results
	}
	defer converter.Close()

	results := make([]Result, 0, len(cases))
	for _, c := range cases {
		results = append(results, runCase(converter, c))
	}
	return results
}

func runCase(converter *convert.Converter, c Case) (res Result) {
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()
	res.Name = c.Name

	out, err := converter.Run(context.Background(), bytes.NewReader(c.PDF), int64(len(c.PDF)), convert.DocOptions{Name: c.Name + ".pdf"})
	text, chapters := out.Text, out.Chapters
	res.Text = text
	res.ChapterTitles = chapters
	res.Pages = out.Pages

	if c.WantError != nil {
		res.ExpectedError = true
		switch {
		case err == nil:
			res.Failure = fmt.Sprintf("expected %v, got no error", c.WantError)
		case !errors.Is(err, c.WantError):
			res.Failure = fmt.Sprintf("expected %v, got %v", c.WantError, err)
		default:
			res.Pass = true
		}
		return res
	}

	if err != nil {
		res.Err = err.Error()
		res.Failure = err.Error()
		return res
	}

	if c.NoExpectation {
		res.NoExpectation = true
		res.Pass = len(text) > 0
		if !res.Pass {
			res.Failure = "no text was recovered"
		}
		return res
	}

	res.Reference = c.Reference
	res.CharRecall = CharRecall(c.WantText, text)
	res.WordErrorRate = WordErrorRate(c.WantText, text)
	res.OrderOK = InOrder(text, c.WantOrder)

	// Another extractor's output is a measurement, not a specification: it
	// keeps the page furniture we remove, so the thresholds do not apply.
	if c.Reference {
		res.Pass = true
		return res
	}

	var problems []string
	if res.CharRecall < MinCharRecall {
		problems = append(problems, fmt.Sprintf("character recall %.1f%% is below %.0f%%", res.CharRecall*100, MinCharRecall*100))
	}
	if res.WordErrorRate > MaxWordErrorRate {
		problems = append(problems, fmt.Sprintf("word error rate %.1f%% is above %.0f%%", res.WordErrorRate*100, MaxWordErrorRate*100))
	}
	if !res.OrderOK {
		problems = append(problems, "text is not in reading order")
	}
	if len(c.WantChapters) > 0 && !equalStrings(res.ChapterTitles, c.WantChapters) {
		problems = append(problems, fmt.Sprintf("chapters = %v, want %v", res.ChapterTitles, c.WantChapters))
	}

	if len(problems) > 0 {
		problems = append(problems, fmt.Sprintf("text = %q", excerpt(text, 200)))
	}
	res.Pass = len(problems) == 0
	res.Failure = strings.Join(problems, "; ")
	return res
}

func excerpt(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
