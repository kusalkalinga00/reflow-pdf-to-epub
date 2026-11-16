// Package convert runs the whole pipeline: PDF in, EPUB out.
//
// It exists so the CLI, the HTTP server, and the corpus all execute the same
// sequence of stages. A stage added here reaches every caller at once; a
// pipeline duplicated in three places drifts within a week.
package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/epub"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/extract"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ingest"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/layout"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/normalize"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ocr"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/render"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/score"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/validate"
)

// ErrInvalidOutput means the book was built but the reference validator
// rejected it. It is never handed to the caller.
var ErrInvalidOutput = errors.New("the generated EPUB failed validation")

// Options configures a Converter.
type Options struct {
	// Password unlocks a document the user can already open.
	Password string
	// Limits caps upload size and page count. The zero value uses
	// ingest.DefaultLimits.
	Limits ingest.Limits
	// Now fixes the EPUB modification timestamp for reproducible output.
	Now time.Time
	// OCR reads pages that have no text layer. Nil disables it.
	OCR ocr.Recognizer
	// Validator runs EPUBCheck over the finished book. Nil skips validation.
	Validator *validate.EpubCheck
}

// DocOptions describe one document within a Converter.
type DocOptions struct {
	// Name is the source file name, used as a title fallback.
	Name string
}

// Result is a completed conversion.
type Result struct {
	EPUB []byte

	// Pages, Blocks and Chapters describe what was recovered.
	Pages    int
	Blocks   int
	Chapters []string

	// Text is the reading text, with paragraphs separated by a space. It is
	// what the corpus scores; callers that only want the book can ignore it.
	Text string

	// Quality is the measured verdict: score, tier, and what was noticed.
	Quality score.Report
	// Validation is the external validator's verdict, when one ran.
	Validation *validate.Report

	Duration time.Duration
}

// Converter holds the resources a conversion needs. A PDFium pool is not
// goroutine-safe, so one Converter serializes conversions; run several
// processes for parallel work.
type Converter struct {
	opts      Options
	extractor *extract.PDFium
}

// New starts a Converter. Callers must Close it.
func New(opts Options) (*Converter, error) {
	if opts.Limits == (ingest.Limits{}) {
		opts.Limits = ingest.DefaultLimits
	}

	extractor, err := extract.NewPDFium(extract.Options{
		Password: opts.Password,
		OCR:      opts.OCR,
	})
	if err != nil {
		return nil, err
	}

	return &Converter{opts: opts, extractor: extractor}, nil
}

// Close releases the PDFium pool.
func (c *Converter) Close() error { return c.extractor.Close() }

// Run converts one PDF.
func (c *Converter) Run(ctx context.Context, src io.ReaderAt, size int64, doc DocOptions) (Result, error) {
	start := time.Now()

	if _, err := ingest.Check(ctx, src, size, c.opts.Limits); err != nil {
		return Result{}, err
	}

	document, err := c.extractor.Extract(ctx, src, size)
	if err != nil {
		return Result{}, err
	}

	document = normalize.Apply(document)
	document = layout.Apply(document)

	paras := render.GroupParagraphs(document.Body())
	text := render.ParagraphText(paras)

	book, err := render.Render(document, render.Options{DefaultTitle: titleFallback(doc.Name)})
	if err != nil {
		return Result{}, err
	}

	var buf bytes.Buffer
	if err := epub.Write(&buf, book); err != nil {
		return Result{}, err
	}

	chapters := make([]string, len(book.Chapters))
	for i, ch := range book.Chapters {
		chapters[i] = ch.Title
	}

	quality := score.Evaluate(document, paras, chapters)

	var validation *validate.Report
	if c.opts.Validator != nil {
		report, err := c.validate(ctx, buf.Bytes())
		if err != nil {
			return Result{}, err
		}
		validation = &report
		if !report.OK() {
			// Fail closed: an artifact the reference validator rejects is not
			// a book, and handing it over would waste the reader's time.
			return Result{}, fmt.Errorf("%w: %s", ErrInvalidOutput, report.Summary())
		}
	}

	return Result{
		EPUB:       buf.Bytes(),
		Pages:      document.PageCount(),
		Blocks:     len(document.Blocks),
		Chapters:   chapters,
		Text:       text,
		Quality:    quality,
		Validation: validation,
		Duration:   time.Since(start),
	}, nil
}

// Convert is a one-shot convenience wrapper for callers without a Converter.
func Convert(ctx context.Context, src io.ReaderAt, size int64, opts Options, doc DocOptions) (Result, error) {
	c, err := New(opts)
	if err != nil {
		return Result{}, err
	}
	defer c.Close()

	return c.Run(ctx, src, size, doc)
}

// validate runs the external validator over the finished bytes.
func (c *Converter) validate(ctx context.Context, data []byte) (validate.Report, error) {
	tmp, err := os.CreateTemp("", "reflow-validate-*.epub")
	if err != nil {
		return validate.Report{}, err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return validate.Report{}, err
	}
	if err := tmp.Close(); err != nil {
		return validate.Report{}, err
	}

	return c.opts.Validator.Check(ctx, tmp.Name())
}

// Explain turns a pipeline error into a sentence a user can act on.
func Explain(err error) string {
	switch {
	case errors.Is(err, ingest.ErrEncrypted):
		return "This PDF needs a password to open. Reflow only converts documents you can already read."
	case errors.Is(err, ingest.ErrTooLarge):
		return fmt.Sprintf("This PDF is larger than the %d MB limit.", ingest.DefaultLimits.MaxBytes>>20)
	case errors.Is(err, ingest.ErrTooManyPages):
		return fmt.Sprintf("This PDF has more than the %d page limit.", ingest.DefaultLimits.MaxPages)
	case errors.Is(err, render.ErrNoText):
		return "This PDF has no extractable text. It looks like a scan, and OCR is not available yet."
	case errors.Is(err, ingest.ErrInvalid):
		return "This file is not a readable PDF."
	case errors.Is(err, ErrInvalidOutput):
		return "The book was built but failed EPUB validation, so it was not written. " + err.Error()
	default:
		return err.Error()
	}
}

func titleFallback(name string) string {
	base := filepath.Base(name)
	if base == "" || base == "." {
		return ""
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Raw returns the document as extraction produced it, before normalization and
// layout. It is what the debugging commands show.
func (c *Converter) Raw(ctx context.Context, src io.ReaderAt, size int64) (idm.Document, error) {
	if _, err := ingest.Check(ctx, src, size, c.opts.Limits); err != nil {
		return idm.Document{}, err
	}
	return c.extractor.Extract(ctx, src, size)
}

// Document returns the intermediate model after normalization and layout, for
// callers that want the structure rather than the finished book.
func (c *Converter) Document(ctx context.Context, src io.ReaderAt, size int64) (idm.Document, error) {
	if _, err := ingest.Check(ctx, src, size, c.opts.Limits); err != nil {
		return idm.Document{}, err
	}

	doc, err := c.extractor.Extract(ctx, src, size)
	if err != nil {
		return idm.Document{}, err
	}

	return layout.Apply(normalize.Apply(doc)), nil
}
