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
	"sync"
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
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/tables"
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
	// Password opens a document the reader can already open. It overrides
	// Options.Password for this document and is never written to disk or
	// logs. A wrong password surfaces as ingest.ErrEncrypted.
	Password string
	// Progress, when set, is called as the conversion advances. It is called
	// from the converting goroutine and must not block.
	Progress func(Progress)
}

// Progress is one step of a conversion. Stage is "reading", "laying out",
// "writing", "validating" or "done"; Done and Total count pages during
// "reading" and are zero elsewhere.
type Progress struct {
	Stage string
	Done  int
	Total int
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
	// rules accumulates the drawn rules of the document being converted.
	rules []tables.Rule
	// mu serializes conversions. The PDFium instance and the rules slice are
	// per-converter state; a second caller waits rather than corrupting them.
	mu sync.Mutex
}

// New starts a Converter. Callers must Close it.
func New(opts Options) (*Converter, error) {
	if opts.Limits == (ingest.Limits{}) {
		opts.Limits = ingest.DefaultLimits
	}

	c := &Converter{opts: opts}
	c.rules = c.rules[:0]

	extractor, err := extract.NewPDFium(extract.Options{
		Password: opts.Password,
		OCR:      opts.OCR,
		Rules:    &c.rules,
	})
	if err != nil {
		return nil, err
	}
	c.extractor = extractor

	return c, nil
}

// Close releases the PDFium pool.
func (c *Converter) Close() error { return c.extractor.Close() }

// Run converts one PDF.
func (c *Converter) Run(ctx context.Context, src io.ReaderAt, size int64, doc DocOptions) (Result, error) {
	start := time.Now()

	password := c.opts.Password
	if doc.Password != "" {
		password = doc.Password
	}
	report := func(p Progress) {
		if doc.Progress != nil {
			doc.Progress(p)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	report(Progress{Stage: "reading"})
	if _, err := ingest.CheckWithPassword(ctx, src, size, c.opts.Limits, password); err != nil {
		return Result{}, err
	}

	c.rules = c.rules[:0]
	document, err := c.extractor.ExtractDoc(ctx, src, size, extract.ExtractOptions{
		Password: password,
		Progress: func(done, total int) {
			report(Progress{Stage: "reading", Done: done, Total: total})
		},
	})
	if err != nil {
		return Result{}, err
	}

	report(Progress{Stage: "laying out"})
	document = normalize.Apply(document)
	document = layout.Apply(document)
	document = c.applyTables(document)

	paras := render.GroupParagraphs(document.Body())
	render.MarkFootnotes(paras, document.Pages)
	text := render.ParagraphText(paras)

	// A document with no text at all but with something to show is a scan: a
	// page-image book is a better answer than an error, and its verdict says
	// what it is.
	if strings.TrimSpace(text) == "" && hasRenderedContent(document) {
		return c.facsimile(ctx, src, size, document, password, doc, start, report)
	}

	report(Progress{Stage: "writing"})
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

	validation, err := c.checkOutput(ctx, buf.Bytes(), report)
	if err != nil {
		return Result{}, err
	}

	report(Progress{Stage: "done", Done: document.PageCount(), Total: document.PageCount()})
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

// facsimile converts a document with no text into a page-image book.
func (c *Converter) facsimile(
	ctx context.Context,
	src io.ReaderAt,
	size int64,
	document idm.Document,
	password string,
	doc DocOptions,
	start time.Time,
	report func(Progress),
) (Result, error) {
	report(Progress{Stage: "rendering"})
	data, err := c.extractor.RenderPages(ctx, src, size, extract.RenderOptions{
		Password: password,
		Progress: func(done, total int) {
			report(Progress{Stage: "rendering", Done: done, Total: total})
		},
	})
	if err != nil {
		return Result{}, err
	}

	pages := make([]render.PageImage, len(data))
	for i, page := range data {
		pages[i] = render.PageImage{Number: i + 1, Data: page, MediaType: "image/jpeg"}
	}

	report(Progress{Stage: "writing"})
	book, err := render.RenderFacsimile(document, pages, render.Options{
		DefaultTitle: titleFallback(doc.Name),
	})
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

	quality := score.Facsimile(document.PageCount(), len(data), len(chapters))

	validation, err := c.checkOutput(ctx, buf.Bytes(), report)
	if err != nil {
		return Result{}, err
	}

	report(Progress{Stage: "done", Done: document.PageCount(), Total: document.PageCount()})
	return Result{
		EPUB:       buf.Bytes(),
		Pages:      document.PageCount(),
		Blocks:     len(document.Blocks),
		Chapters:   chapters,
		Quality:    quality,
		Validation: validation,
		Duration:   time.Since(start),
	}, nil
}

// hasRenderedContent reports whether a page-image book would have anything to
// show: a figure on some page, or an image extraction could not embed but
// PDFium can still rasterise.
func hasRenderedContent(doc idm.Document) bool {
	for _, b := range doc.Blocks {
		if b.Kind == idm.KindFigure {
			return true
		}
	}
	return doc.Features.ImagesSkipped > 0
}

// checkOutput runs the external validator over a finished book and fails
// closed: an artifact the reference validator rejects is not a book.
func (c *Converter) checkOutput(ctx context.Context, data []byte, report func(Progress)) (*validate.Report, error) {
	if c.opts.Validator == nil {
		return nil, nil
	}

	report(Progress{Stage: "validating"})
	result, err := c.validate(ctx, data)
	if err != nil {
		return nil, err
	}
	if !result.OK() {
		return nil, fmt.Errorf("%w: %s", ErrInvalidOutput, result.Summary())
	}
	return &result, nil
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

// applyTables replaces the text inside ruled regions with recovered tables.
//
// Blocks whose words lie inside a detected table are removed and one table
// block is inserted at the position of the first of them, so the reading
// order keeps the table where it appeared on the page.
func (c *Converter) applyTables(doc idm.Document) idm.Document {
	if len(doc.Pages) == 0 || len(c.rules) == 0 {
		return doc
	}

	byPage := make(map[int][]tables.Table, 4)
	wordsByPage := make(map[int][]idm.Word, len(doc.Pages))

	for _, b := range doc.Blocks {
		wordsByPage[b.Page] = append(wordsByPage[b.Page], b.Words...)
	}

	for _, page := range doc.Pages {
		var pageRules []tables.Rule
		for _, r := range c.rules {
			if r.Page == page.Number {
				pageRules = append(pageRules, r)
			}
		}
		if len(pageRules) == 0 {
			continue
		}
		found := tables.BuildTables(pageRules, wordsByPage[page.Number], page)
		if len(found) > 0 {
			byPage[page.Number] = found
		}
	}
	if len(byPage) == 0 {
		return doc
	}

	// Blocks arrive in reading order, so a table is emitted once: at the
	// position of its first contained block.
	emitted := make(map[float64]bool, len(byPage))

	var blocks []idm.Block
	for _, b := range doc.Blocks {
		if b.Kind.IsFurniture() {
			blocks = append(blocks, b)
			continue
		}

		if table, ok := tableAt(byPage, b); ok {
			if emitted[table.Top] {
				// The words of this block are in the grid.
				continue
			}
			emitted[table.Top] = true
			blocks = append(blocks, idm.Block{
				Kind:       idm.KindTable,
				Page:       b.Page,
				BBox:       idm.Rect{X0: table.Left, Y0: table.Bottom, X1: table.Right, Y1: table.Top},
				Confidence: 1,
				Table:      &idm.TableGrid{Cells: table.Cells},
			})
			continue
		}

		blocks = append(blocks, b)
	}

	return doc.WithBlocks(blocks)
}

// tableAt returns the recovered table containing this block, if any. A table
// is identified by its top edge; two tables on one page never share it.
func tableAt(byPage map[int][]tables.Table, b idm.Block) (tables.Table, bool) {
	for _, table := range byPage[b.Page] {
		if insideRect(b, table.Left, table.Bottom, table.Right, table.Top) {
			return table, true
		}
	}
	return tables.Table{}, false
}

// insideTable reports whether the block's words were absorbed by a table.
func insideTable(byPage map[int][]tables.Table, b idm.Block) bool {
	for _, table := range byPage[b.Page] {
		if insideRect(b, table.Left, table.Bottom, table.Right, table.Top) {
			return true
		}
	}
	return false
}

func insideRect(b idm.Block, left, bottom, right, top float64) bool {
	return b.BBox.X0 >= left-5 && b.BBox.X1 <= right+5 &&
		b.BBox.Y0 >= bottom-5 && b.BBox.Y1 <= top+5
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
