package ingest_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ingest"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
)

func check(data []byte, limits ingest.Limits) (ingest.Info, error) {
	return ingest.Check(context.Background(), bytes.NewReader(data), int64(len(data)), limits)
}

func TestCheckReadsDocumentStructure(t *testing.T) {
	info, err := check(testpdf.Build(3), ingest.DefaultLimits)
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}

	if info.Pages != 3 {
		t.Errorf("Pages = %d, want 3", info.Pages)
	}
	if info.Version != "1.7" {
		t.Errorf("Version = %q, want 1.7", info.Version)
	}
	if info.Title != "Test Document" {
		t.Errorf("Title = %q, want %q", info.Title, "Test Document")
	}
	if info.Author != "Test Author" {
		t.Errorf("Author = %q, want %q", info.Author, "Test Author")
	}
	if info.PageWidth != 595 || info.PageHeight != 842 {
		t.Errorf("page size = %vx%v, want 595x842", info.PageWidth, info.PageHeight)
	}
	if info.MixedPageSizes {
		t.Error("MixedPageSizes = true for a uniform document")
	}
	if info.Encrypted {
		t.Error("Encrypted = true for a plain document")
	}
}

func TestCheckReportsMixedPageSizes(t *testing.T) {
	data := testpdf.Build(2, testpdf.WithMediaBox(func(page int) string {
		if page == 1 {
			return "[0 0 595 842]"
		}
		return "[0 0 420 595]"
	}))

	info, err := check(data, ingest.DefaultLimits)
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if !info.MixedPageSizes {
		t.Error("MixedPageSizes = false, want true")
	}
}

func TestCheckEnforcesLimits(t *testing.T) {
	data := testpdf.Build(3)

	t.Run("size", func(t *testing.T) {
		limits := ingest.Limits{MaxBytes: 16, MaxPages: 100}
		_, err := check(data, limits)
		if !errors.Is(err, ingest.ErrTooLarge) {
			t.Fatalf("error = %v, want ErrTooLarge", err)
		}
	})

	t.Run("pages", func(t *testing.T) {
		limits := ingest.Limits{MaxBytes: 1 << 20, MaxPages: 2}
		_, err := check(data, limits)
		if !errors.Is(err, ingest.ErrTooManyPages) {
			t.Fatalf("error = %v, want ErrTooManyPages", err)
		}
	})
}

func TestCheckRejectsNonPDF(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"plain text", []byte("this is not a pdf at all")},
		{"empty", nil},
		{"truncated header", []byte("%PDF")},
		{"corrupt body", append([]byte("%PDF-1.7\n"), []byte("garbage without objects")...)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := check(tt.data, ingest.DefaultLimits)
			if err == nil {
				t.Fatal("Check() = nil error, want a rejection")
			}
			if !errors.Is(err, ingest.ErrInvalid) && !errors.Is(err, ingest.ErrTooLarge) {
				t.Errorf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCheckDetectsPasswordProtectedPDF(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.pdf")
	locked := filepath.Join(dir, "locked.pdf")

	if err := os.WriteFile(plain, testpdf.Build(2), 0o600); err != nil {
		t.Fatal(err)
	}

	conf := model.NewDefaultConfiguration()
	conf.UserPW = "open-sesame"
	conf.OwnerPW = "open-sesame"
	if err := api.EncryptFile(context.Background(), plain, locked, conf); err != nil {
		t.Fatalf("encrypt fixture: %v", err)
	}

	data, err := os.ReadFile(locked)
	if err != nil {
		t.Fatal(err)
	}

	_, err = check(data, ingest.DefaultLimits)
	if !errors.Is(err, ingest.ErrEncrypted) {
		t.Fatalf("error = %v, want ErrEncrypted", err)
	}
}

func TestCheckFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, testpdf.Build(5), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := ingest.CheckFile(context.Background(), path, ingest.DefaultLimits)
	if err != nil {
		t.Fatalf("CheckFile() error: %v", err)
	}
	if info.Pages != 5 {
		t.Errorf("Pages = %d, want 5", info.Pages)
	}
}

func TestCheckFileMissing(t *testing.T) {
	_, err := ingest.CheckFile(context.Background(), filepath.Join(t.TempDir(), "nope.pdf"), ingest.DefaultLimits)
	if err == nil {
		t.Fatal("CheckFile() = nil error for a missing file")
	}
}
