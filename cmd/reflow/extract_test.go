package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/idm"
)

func TestExtractPrintsSummary(t *testing.T) {
	path := writePDF(t, 2)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"extract", path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"pages", "blocks", "characters", "language"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "2") {
		t.Errorf("output does not mention the page count:\n%s", out)
	}
}

func TestExtractJSON(t *testing.T) {
	path := writePDF(t, 2)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"extract", "-json", path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	var doc idm.Document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if doc.PageCount() != 2 {
		t.Errorf("pages = %d, want 2", doc.PageCount())
	}
	if len(doc.Blocks) == 0 {
		t.Error("no blocks extracted")
	}
	if !strings.Contains(doc.Text(), "Page 1") {
		t.Errorf("text does not contain the fixture content:\n%s", doc.Text())
	}
}

func TestExtractWritesFile(t *testing.T) {
	path := writePDF(t, 1)
	out := filepath.Join(t.TempDir(), "doc.json")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"extract", "-o", out, path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output file: %v", err)
	}

	var doc idm.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output file is not valid JSON: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Errorf("pages = %d, want 1", doc.PageCount())
	}
	if !strings.Contains(stdout.String(), out) {
		t.Errorf("stdout = %q, want it to name the written file", stdout.String())
	}
}

func TestExtractRequiresOneArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"extract"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "usage: reflow extract") {
		t.Errorf("stderr = %q, want extract usage", stderr.String())
	}
}
