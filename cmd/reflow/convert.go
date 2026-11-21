package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kusalkalinga00/reflow/internal/convert"
	"github.com/kusalkalinga00/reflow/internal/ocr"
	"github.com/kusalkalinga00/reflow/internal/validate"
)

func convertCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, `usage: reflow convert [-o file.epub] [-password pass] <file.pdf>

Convert a PDF into a reflowable EPUB 3 ebook. -password opens an encrypted
document you can already read; it is used for this run only.
`)
	}

	outPath := fs.String("o", "", "output EPUB path (default: the input name with .epub)")
	quiet := fs.Bool("q", false, "suppress progress messages")
	useOCR := fs.Bool("ocr", true, "read pages with no text layer using tesseract")
	useValidation := fs.Bool("validate", true, "reject the book if EPUBCheck finds errors")
	password := fs.String("password", "", "password for an encrypted PDF you can already open")

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

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}

	progress("%s: %d bytes", path, info.Size())

	var recognizer ocr.Recognizer
	if *useOCR {
		recognizer, err = ocr.NewTesseract()
		if err != nil {
			progress("OCR disabled: %v", err)
		}
	}

	var validator *validate.EpubCheck
	if *useValidation {
		validator, err = validate.NewEpubCheck()
		if err != nil {
			progress("validation skipped: %v", err)
		}
	}

	result, err := convert.Convert(ctx, f, info.Size(), convert.Options{
		OCR:       recognizer,
		Validator: validator,
	}, convert.DocOptions{Name: filepath.Base(path), Password: *password})
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", convert.Explain(err))
		return exitFailure
	}

	progress("%d pages, %d blocks, %d chapters", result.Pages, result.Blocks, len(result.Chapters))
	if result.Validation != nil {
		progress("epubcheck: %d errors, %d warnings", result.Validation.Errors, result.Validation.Warnings)
	}
	for _, issue := range result.Quality.Issues {
		progress("  %s: %s", strings.ToUpper(string(issue.Severity)), issue.Message)
	}

	if err := writeFileAtomic(dest, result.EPUB); err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}

	fmt.Fprintf(stdout, "wrote %s (%d pages, %d chapter%s, %s %d/100)\n",
		dest, result.Pages, len(result.Chapters), plural(len(result.Chapters)),
		result.Quality.Tier, result.Quality.Score)
	return exitOK
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// writeFileAtomic writes through a temporary file so a failure never leaves a
// truncated book behind.
func writeFileAtomic(dest string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".reflow-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
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
