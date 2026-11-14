package extract_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/extract"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
)

func newExtractor(t *testing.T, opts extract.Options) *extract.PDFium {
	t.Helper()

	e, err := extract.NewPDFium(opts)
	if err != nil {
		t.Fatalf("NewPDFium: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func extractBytes(t *testing.T, e extract.Extractor, data []byte) (idm.Document, error) {
	t.Helper()
	return e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)))
}

func TestExtractRecoversText(t *testing.T) {
	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), testpdf.Build(2))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if doc.PageCount() != 2 {
		t.Fatalf("pages = %d, want 2", doc.PageCount())
	}
	if len(doc.Blocks) < 2 {
		t.Fatalf("blocks = %d, want at least one per page", len(doc.Blocks))
	}

	text := doc.Text()
	for _, want := range []string{"Page 1", "Page 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("text is missing %q:\n%s", want, text)
		}
	}

	if doc.Meta.Language != "en" {
		t.Errorf("language = %q, want the default en", doc.Meta.Language)
	}
}

func TestExtractPageGeometry(t *testing.T) {
	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), testpdf.Build(1))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	page := doc.Pages[0]
	if page.Width != 595 || page.Height != 842 {
		t.Errorf("page size = %vx%v, want 595x842", page.Width, page.Height)
	}
	if page.Provenance != idm.ProvenanceDigital {
		t.Errorf("provenance = %q, want digital", page.Provenance)
	}
	if page.TextCoverage <= 0 || page.TextCoverage > 1 {
		t.Errorf("coverage = %v, want a fraction in (0,1]", page.TextCoverage)
	}

	for _, b := range doc.Blocks {
		if b.FontSize <= 0 {
			t.Errorf("block %q has font size %v", b.Text, b.FontSize)
		}
		if b.BBox.X0 < 0 || b.BBox.Y0 < 0 || b.BBox.X1 > page.Width || b.BBox.Y1 > page.Height {
			t.Errorf("block %q has box %+v outside the page", b.Text, b.BBox)
		}
	}
}

func TestExtractRejectsCorruptInput(t *testing.T) {
	_, err := extractBytes(t, newExtractor(t, extract.Options{}), []byte("this is not a pdf"))
	if err == nil {
		t.Fatal("Extract() = nil error, want a rejection")
	}
}

func TestExtractHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	e := newExtractor(t, extract.Options{})
	data := testpdf.Build(1)

	_, err := e.Extract(ctx, bytes.NewReader(data), int64(len(data)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestExtractEncryptedDocument(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.pdf")
	locked := filepath.Join(dir, "locked.pdf")

	if err := os.WriteFile(plain, testpdf.Build(1), 0o600); err != nil {
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

	t.Run("without a password", func(t *testing.T) {
		if _, err := extractBytes(t, newExtractor(t, extract.Options{}), data); err == nil {
			t.Fatal("Extract() = nil error for an encrypted document")
		}
	})

	t.Run("with the password", func(t *testing.T) {
		doc, err := extractBytes(t, newExtractor(t, extract.Options{Password: "open-sesame"}), data)
		if err != nil {
			t.Fatalf("Extract() error: %v", err)
		}
		if doc.PageCount() != 1 {
			t.Errorf("pages = %d, want 1", doc.PageCount())
		}
	})
}
