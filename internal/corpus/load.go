package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadOptions control how a directory is turned into cases.
type LoadOptions struct {
	// AllowMissing turns a PDF without expected text into a diagnostic case
	// instead of an error.
	AllowMissing bool
	// Reference extracts expected text with pdftotext when no .txt exists.
	//
	// It is not ground truth: another extractor's output agrees with ours only
	// where both are right. It is still the only measurement available for
	// real books that arrive without a text file, and disagreement is exactly
	// where a human should look.
	Reference bool
}

// Expectations is an optional name.expect.json beside a document's name.txt.
//
// The defaults suit prose, where word order is canonical. A form or résumé
// has no single linear order across its columns, and a producer's text layer
// can disagree with the printed page on a character (the invoice prints
// "Deployment" and its layer says "Deployement"). Such a document states the
// tolerance it judges itself by, and the metrics are still reported in full.
type Expectations struct {
	// MinCharRecall overrides Case.MinCharRecall. Zero keeps the default.
	MinCharRecall float64 `json:"minCharRecall"`
	// MaxWordErrorRate overrides Case.MaxWordErrorRate. Zero keeps the
	// default.
	MaxWordErrorRate float64 `json:"maxWordErrorRate"`
	// WantOrder lists markers that must appear in this order.
	WantOrder []string `json:"wantOrder"`
	// Chapters lists the chapter titles the book must have, in order.
	Chapters []string `json:"chapters"`
}

// LoadDir builds cases from a directory of real documents.
//
// Every name.pdf must sit next to a name.txt holding the text a reader should
// see. An optional name.expect.json narrows or widens the judgement for that
// document. Real PDFs cannot be committed to the repository, so this is how
// the engine is measured against actual books.
func LoadDir(dir string, opts LoadOptions) ([]Case, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var cases []Case
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pdf") {
			continue
		}

		base := strings.TrimSuffix(entry.Name(), ".pdf")
		// #nosec G304 -- the directory is a CLI argument and the name comes
		// from its own listing.
		pdf, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}

		wantPath := filepath.Join(dir, base+".txt")
		// #nosec G304 -- same directory, same listing-derived name.
		want, err := os.ReadFile(wantPath)
		if err != nil {
			switch {
			case opts.Reference:
				text, refErr := ReferenceText(filepath.Join(dir, entry.Name()))
				if refErr != nil {
					return nil, fmt.Errorf("%s: %w", entry.Name(), refErr)
				}
				cases = append(cases, Case{Name: base, PDF: pdf, WantText: text, Reference: true})
				continue
			case opts.AllowMissing:
				cases = append(cases, Case{Name: base, PDF: pdf, NoExpectation: true})
				continue
			default:
				return nil, fmt.Errorf("%s needs an expected-text file at %s: %w", entry.Name(), wantPath, err)
			}
		}

		c := Case{
			Name:     base,
			PDF:      pdf,
			WantText: string(want),
		}
		if err := c.loadExpectations(filepath.Join(dir, base+".expect.json")); err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}

	if len(cases) == 0 {
		return nil, fmt.Errorf("no .pdf files in %s", dir)
	}

	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

// loadExpectations reads an optional sidecar. A missing file leaves the case
// on the defaults; a malformed one is an error, because silently ignoring it
// would judge the document by rules nobody chose.
func (c *Case) loadExpectations(path string) error {
	// #nosec G304 -- the sidecar sits beside a corpus case the operator placed.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%s: %w", path, err)
	}

	var exp Expectations
	if err := json.Unmarshal(data, &exp); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	c.MinCharRecall = exp.MinCharRecall
	c.MaxWordErrorRate = exp.MaxWordErrorRate
	c.WantOrder = exp.WantOrder
	c.WantChapters = exp.Chapters
	return nil
}
