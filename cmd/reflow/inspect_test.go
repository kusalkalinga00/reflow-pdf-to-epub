package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ingest"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
)

func writePDF(t *testing.T, pages int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, testpdf.Build(pages), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectPrintsHumanReport(t *testing.T) {
	path := writePDF(t, 3)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inspect", path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"book.pdf", "pages", "Test Document", "Test Author", "encrypted"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "3") {
		t.Errorf("output does not mention the page count:\n%s", out)
	}
}

func TestInspectJSON(t *testing.T) {
	path := writePDF(t, 2)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inspect", "-json", path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	var info ingest.Info
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if info.Pages != 2 {
		t.Errorf("pages = %d, want 2", info.Pages)
	}
	if info.Title != "Test Document" {
		t.Errorf("title = %q, want %q", info.Title, "Test Document")
	}
}

func TestInspectRejectsNonPDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a.pdf")
	if err := os.WriteFile(path, []byte("just some text"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inspect", path}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), "not a readable PDF") {
		t.Errorf("stderr = %q, want a readable explanation", stderr.String())
	}
}

func TestInspectRequiresOneArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"inspect"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "usage: reflow inspect") {
		t.Errorf("stderr = %q, want inspect usage", stderr.String())
	}
}
