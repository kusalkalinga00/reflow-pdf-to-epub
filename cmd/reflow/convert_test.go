package main

import (
	"archive/zip"
	"bytes"
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
