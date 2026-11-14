package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/epub"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ingest"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/render"
)

func convert(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, `usage: reflow convert [-o file.epub] <file.pdf>

Convert a PDF into a reflowable EPUB 3 ebook.
`)
	}

	outPath := fs.String("o", "", "output EPUB path (default: the input name with .epub)")
	quiet := fs.Bool("q", false, "suppress progress messages")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return exitUsage
	}

	path := fs.Arg(0)
	dest := *outPath
	if dest == "" {
		dest = strings.TrimSuffix(path, filepath.Ext(path)) + ".epub"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	progress := func(format string, args ...any) {
		if !*quiet {
			fmt.Fprintf(stderr, format+"\n", args...)
		}
	}

	info, err := ingest.CheckFile(ctx, path, ingest.DefaultLimits)
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", describeIngestError(err))
		return exitFailure
	}
	progress("%s: %d pages", path, info.Pages)

	doc, err := runExtraction(ctx, path)
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}

	doc.Meta.SourceName = filepath.Base(path)
	if doc.Meta.Title == "" {
		doc.Meta.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	progress("recovered %d blocks", len(doc.Blocks))

	book, err := render.Render(doc, render.Options{})
	if err != nil {
		if errors.Is(err, render.ErrNoText) {
			fmt.Fprintln(stderr, "reflow: this PDF has no extractable text. It looks like a scan, and OCR is not implemented yet.")
			return exitFailure
		}
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}
	progress("%d chapters", len(book.Chapters))

	if err := writeBook(dest, book); err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}

	fmt.Fprintf(stdout, "wrote %s (%d pages, %d chapters)\n", dest, doc.PageCount(), len(book.Chapters))
	return exitOK
}

// writeBook writes to a temporary file and renames it, so a failed conversion
// never leaves a truncated EPUB behind.
func writeBook(dest string, book epub.Book) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".reflow-*.epub")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := epub.Write(tmp, book); err != nil {
		tmp.Close()
		return err
	}
	// CreateTemp uses 0600; an ebook should be readable like any other file.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), dest)
}
