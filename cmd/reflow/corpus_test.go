package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/corpus"
	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/testpdf"
)

func TestCorpusPasses(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"corpus"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"case", "recall", "single-column", "two-columns", "headings"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr.String(), "0 failed") {
		t.Errorf("stderr = %q, want a zero failure count", stderr.String())
	}
}

func TestCorpusJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"corpus", "-json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr.String())
	}

	var results []corpus.Result
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range results {
		if !r.Pass {
			t.Errorf("%s failed: %s", r.Name, r.Failure)
		}
	}
}

func TestCorpusFailsOnWrongExpectation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "book.pdf"), testpdf.Build(1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "book.txt"), []byte("text that is not in the document\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"corpus", "-dir", dir}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stdout.String(), "FAIL") {
		t.Errorf("output does not report a failure:\n%s", stdout.String())
	}
}

func TestCorpusRequiresExpectedText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "book.pdf"), testpdf.Build(1), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"corpus", "-dir", dir}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), "expected-text") {
		t.Errorf("stderr = %q, want an explanation", stderr.String())
	}
}
