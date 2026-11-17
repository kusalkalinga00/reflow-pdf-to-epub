package corpus

import (
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

// LoadDir builds cases from a directory of real documents.
//
// Every name.pdf must sit next to a name.txt holding the text a reader should
// see. Real PDFs cannot be committed to the repository, so this is how the
// engine is measured against actual books.
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
		pdf, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}

		wantPath := filepath.Join(dir, base+".txt")
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

		cases = append(cases, Case{
			Name:     base,
			PDF:      pdf,
			WantText: string(want),
		})
	}

	if len(cases) == 0 {
		return nil, fmt.Errorf("no .pdf files in %s", dir)
	}

	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}
