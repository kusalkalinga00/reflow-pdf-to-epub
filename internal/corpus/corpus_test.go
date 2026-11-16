package corpus_test

import (
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/corpus"
)

// TestSyntheticCorpusPasses is the regression gate. Every case in the built-in
// corpus must convert at or above the thresholds; a change that lowers quality
// fails here rather than in someone's reader.
func TestSyntheticCorpusPasses(t *testing.T) {
	cases := corpus.Synthetic()
	results := corpus.Run(cases)
	if len(results) != len(cases) {
		t.Fatalf("got %d results, want %d", len(results), len(cases))
	}

	for _, r := range results {
		if !r.Pass {
			t.Errorf("%s failed: %s", r.Name, r.Failure)
		}
	}
}

func TestSyntheticCorpusCoversEachFailureMode(t *testing.T) {
	names := make(map[string]bool)
	for _, c := range corpus.Synthetic() {
		names[c.Name] = true
	}

	for _, want := range []string{
		"single-column",
		"two-columns",
		"headings",
		"running-heads",
		"spaced-heading",
		"scanned",
	} {
		if !names[want] {
			t.Errorf("corpus has no %q case", want)
		}
	}
}
