package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
)

func TestConvertWritesEPUB(t *testing.T) {
	in := writePDF(t, 3)
	out := filepath.Join(t.TempDir(), "book.epub")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"convert", "-o", out, in}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("output is not a ZIP archive: %v", err)
	}
	defer zr.Close()

	if len(zr.File) == 0 || zr.File[0].Name != "mimetype" {
		t.Fatalf("first archive entry is not mimetype: %v", zr.File[0].Name)
	}

	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"mimetype", "META-INF/container.xml", "OEBPS/package.opf", "OEBPS/nav.xhtml"} {
		if !names[want] {
			t.Errorf("archive is missing %q", want)
		}
	}

	if !strings.Contains(stdout.String(), out) {
		t.Errorf("stdout = %q, want it to name the output file", stdout.String())
	}
	if !strings.Contains(stdout.String(), "3 pages") {
		t.Errorf("stdout = %q, want the page count", stdout.String())
	}
	if !strings.Contains(stdout.String(), "/100") {
		t.Errorf("stdout = %q, want a quality score", stdout.String())
	}
}

func TestConvertProgressIsOptional(t *testing.T) {
	in := writePDF(t, 1)

	t.Run("progress by default", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"convert", "-o", filepath.Join(t.TempDir(), "a.epub"), in}, &stdout, &stderr); code != exitOK {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		if stderr.Len() == 0 {
			t.Error("stderr is empty, want progress messages")
		}
	})

	t.Run("quiet", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"convert", "-q", "-o", filepath.Join(t.TempDir(), "b.epub"), in}, &stdout, &stderr); code != exitOK {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want silence", stderr.String())
		}
	})
}

func TestConvertDefaultsToInputName(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "book.pdf")
	if err := os.WriteFile(in, testpdf.Build(1), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"convert", "-q", in}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	out := filepath.Join(dir, "book.epub")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected output at %s: %v", out, err)
	}
}

func TestConvertRequiresOneArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"convert"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "usage: reflow convert") {
		t.Errorf("stderr = %q, want convert usage", stderr.String())
	}
}

func TestConvertPutsColumnsInReadingOrder(t *testing.T) {
	// The source interleaves the columns, which is what a PDF content stream
	// looks like. The output must read the left column first.
	pdf := testpdf.Build(1, testpdf.WithLines(
		testpdf.Line{Text: "Left one.", Size: 12, X: 72, Y: 700},
		testpdf.Line{Text: "Right one.", Size: 12, X: 320, Y: 700},
		testpdf.Line{Text: "Left two.", Size: 12, X: 72, Y: 680},
		testpdf.Line{Text: "Right two.", Size: 12, X: 320, Y: 680},
		testpdf.Line{Text: "Left three.", Size: 12, X: 72, Y: 660},
		testpdf.Line{Text: "Right three.", Size: 12, X: 320, Y: 660},
	))

	dir := t.TempDir()
	in := filepath.Join(dir, "columns.pdf")
	if err := os.WriteFile(in, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "columns.epub")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"convert", "-q", "-o", out, in}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer zr.Close()

	var chapter string
	for _, f := range zr.File {
		if f.Name == "OEBPS/text/chapter-001.xhtml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(rc)
			rc.Close()
			chapter = string(body)
		}
	}
	if chapter == "" {
		t.Fatal("chapter document not found")
	}

	left := strings.Index(chapter, "Left three.")
	right := strings.Index(chapter, "Right one.")
	if left < 0 || right < 0 {
		t.Fatalf("chapter is missing text:\n%s", chapter)
	}
	if left > right {
		t.Errorf("right column text appears before the end of the left column:\n%s", chapter)
	}
}
