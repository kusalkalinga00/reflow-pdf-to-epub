package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadDir builds cases from a directory of real documents.
//
// Every name.pdf must sit next to a name.txt holding the text a reader should
// see. Real PDFs cannot be committed to the repository, so this is how the
// engine is measured against actual books. With allowMissing, a PDF without a
// .txt becomes a diagnostic case instead of an error.
func LoadDir(dir string, allowMissing bool) ([]Case, error) {
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
			if !allowMissing {
				return nil, fmt.Errorf("%s needs an expected-text file at %s: %w", entry.Name(), wantPath, err)
			}
			cases = append(cases, Case{Name: base, PDF: pdf, NoExpectation: true})
			continue
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
