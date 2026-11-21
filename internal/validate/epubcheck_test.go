package validate_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kusalkalinga00/reflow/internal/epub"
	"github.com/kusalkalinga00/reflow/internal/validate"
)

func newValidator(t *testing.T) *validate.EpubCheck {
	t.Helper()

	checker, err := validate.NewEpubCheck()
	if err != nil {
		t.Skipf("epubcheck is not installed: %v", err)
	}
	return checker
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sampleBook() epub.Book {
	return epub.Book{
		Metadata: epub.Metadata{
			Title:      "The Long Road",
			Authors:    []string{"A. Author"},
			Language:   "en",
			Identifier: "urn:uuid:11111111-2222-4333-8444-555555555555",
			Modified:   time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC),
		},
		Chapters: []epub.Chapter{
			{Title: "One", Body: `<span id="page-1"></span><h1>One</h1><p>The road bent north.</p>`},
			{Title: "Two", Body: "<h1>Two</h1><p>It never stalled.</p>"},
		},
		PageList: []epub.PageTarget{
			{Label: "1", Href: "text/chapter-001.xhtml#page-1"},
		},
	}
}

func TestEpubCheckAcceptsOurOutput(t *testing.T) {
	checker := newValidator(t)

	data, err := epub.WriteBytes(sampleBook())
	if err != nil {
		t.Fatalf("WriteBytes: %v", err)
	}

	report, err := checker.Check(context.Background(), writeTemp(t, data))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if !report.OK() {
		t.Fatalf("our own output fails validation: %s", report.Summary())
	}
	if report.Fatal != 0 || report.Errors != 0 {
		t.Errorf("report = %+v, want no errors", report)
	}
}

func TestEpubCheckRejectsABrokenArchive(t *testing.T) {
	checker := newValidator(t)

	// A ZIP with the right name and none of the required files.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("mimetype")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("application/epub+zip"))
	zw.Close()

	report, err := checker.Check(context.Background(), writeTemp(t, buf.Bytes()))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if report.OK() {
		t.Fatal("an archive without a container was accepted")
	}
	if report.Summary() == "" {
		t.Error("a failing report has no summary")
	}
}

func TestNewEpubCheckWithoutAnInstallation(t *testing.T) {
	t.Setenv("EPUBCHECK_JAR", "")
	t.Setenv("PATH", "/nonexistent")
	// The working directory may hold a local download; look somewhere empty.
	t.Chdir(t.TempDir())

	if _, err := validate.NewEpubCheck(); err == nil {
		t.Fatal("NewEpubCheck() = nil error without an installation")
	}
}
