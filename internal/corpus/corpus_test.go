package corpus_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/corpus"
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

func TestExpectationSidecarIsLoaded(t *testing.T) {
	dir := t.TempDir()
	writeCaseFiles(t, dir, "book", `{"maxWordErrorRate": 0.1, "minCharRecall": 0.5, "wantOrder": ["First", "Second"], "chapters": ["One"]}`)

	cases, err := corpus.LoadDir(dir, corpus.LoadOptions{})
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(cases))
	}

	c := cases[0]
	if c.MaxWordErrorRate != 0.1 || c.MinCharRecall != 0.5 {
		t.Errorf("thresholds = %v/%v, want 0.1/0.5", c.MaxWordErrorRate, c.MinCharRecall)
	}
	if len(c.WantOrder) != 2 || c.WantOrder[0] != "First" {
		t.Errorf("WantOrder = %v", c.WantOrder)
	}
	if len(c.WantChapters) != 1 || c.WantChapters[0] != "One" {
		t.Errorf("WantChapters = %v", c.WantChapters)
	}
}

func TestMalformedExpectationSidecarFails(t *testing.T) {
	dir := t.TempDir()
	writeCaseFiles(t, dir, "book", "{not json")

	if _, err := corpus.LoadDir(dir, corpus.LoadOptions{}); err == nil {
		t.Fatal("LoadDir accepted a malformed expect.json")
	}
}

func TestCaseThresholdsOverrideTheDefaults(t *testing.T) {
	base := corpus.Synthetic()[0]
	base.WantText += " Words the extractor does not produce."

	strict := base
	strict.Name = "strict"
	if results := corpus.Run([]corpus.Case{strict}); results[0].Pass {
		t.Errorf("default thresholds passed a case with WER %.1f%%", results[0].WordErrorRate*100)
	}

	lenient := base
	lenient.Name = "lenient"
	lenient.MaxWordErrorRate = 0.5
	lenient.MinCharRecall = 0.5
	if results := corpus.Run([]corpus.Case{lenient}); !results[0].Pass {
		t.Errorf("overridden thresholds still failed: %s", results[0].Failure)
	}
}

func writeCaseFiles(t *testing.T, dir, name, expect string) {
	t.Helper()
	write := func(file, data string) {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(data), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	write(name+".pdf", "%PDF-1.4")
	write(name+".txt", "Some expected text.")
	if expect != "" {
		write(name+".expect.json", expect)
	}
}
