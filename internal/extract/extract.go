// Package extract turns a PDF into the intermediate document model.
//
// Extraction is deliberately shallow: it recovers one block per text line, with
// the line's bounding box, effective font size and reading position. Deciding
// which lines form a paragraph, which are headings, and which column they
// belong to is the layout stage's job.
package extract

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/single_threaded"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// Extractor converts a PDF into an idm.Document.
type Extractor interface {
	Extract(ctx context.Context, src io.ReaderAt, size int64) (idm.Document, error)
}

// Options configures extraction.
type Options struct {
	// Password unlocks a document the user can already open. It is never
	// written to disk or logs.
	Password string
	// DefaultLanguage is used when the PDF declares no /Lang entry.
	DefaultLanguage string
	// Timeout bounds a single PDFium call, not the whole document.
	Timeout time.Duration
}

// PDFium extracts text with the PDFium engine.
type PDFium struct {
	pool     pdfium.Pool
	instance pdfium.Pdfium
	opts     Options
}

// NewPDFium starts a PDFium pool. Callers must Close it.
//
// The pool is single threaded: PDFium instances are not safe to share between
// goroutines, and one conversion is already CPU bound. Worker-level parallelism
// comes from running several worker processes, not from intra-document
// concurrency.
func NewPDFium(opts Options) (*PDFium, error) {
	if opts.DefaultLanguage == "" {
		opts.DefaultLanguage = "en"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}

	pool := single_threaded.Init(single_threaded.Config{})
	instance, err := pool.GetInstance(opts.Timeout)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("start pdfium: %w", err)
	}

	return &PDFium{pool: pool, instance: instance, opts: opts}, nil
}

// Close releases the PDFium pool.
func (e *PDFium) Close() error { return e.pool.Close() }

// Extract reads every page of src.
func (e *PDFium) Extract(ctx context.Context, src io.ReaderAt, size int64) (idm.Document, error) {
	if err := ctx.Err(); err != nil {
		return idm.Document{}, err
	}
	if size <= 0 {
		return idm.Document{}, fmt.Errorf("extract: empty document")
	}

	open := &requests.OpenDocument{
		FileReader:     io.NewSectionReader(src, 0, size),
		FileReaderSize: size,
	}
	if e.opts.Password != "" {
		open.Password = &e.opts.Password
	}

	doc, err := e.instance.OpenDocument(open)
	if err != nil {
		return idm.Document{}, fmt.Errorf("open pdf: %w", err)
	}
	defer e.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	count, err := e.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return idm.Document{}, fmt.Errorf("page count: %w", err)
	}

	out := idm.Document{Meta: e.metadata(doc.Document)}

	for i := 0; i < count.PageCount; i++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		page, blocks, err := e.extractPage(doc.Document, i)
		if err != nil {
			return out, fmt.Errorf("page %d: %w", i+1, err)
		}
		out.Pages = append(out.Pages, page)
		out.Blocks = append(out.Blocks, blocks...)
	}

	if err := out.Validate(); err != nil {
		return out, err
	}
	return out, nil
}

func (e *PDFium) extractPage(doc references.FPDF_DOCUMENT, index int) (idm.Page, []idm.Block, error) {
	page := requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: index}}

	size, err := e.instance.GetPageSize(&requests.GetPageSize{Page: page})
	if err != nil {
		return idm.Page{}, nil, fmt.Errorf("page size: %w", err)
	}

	rotation, err := e.instance.FPDFPage_GetRotation(&requests.FPDFPage_GetRotation{Page: page})
	if err != nil {
		return idm.Page{}, nil, fmt.Errorf("page rotation: %w", err)
	}

	text, err := e.instance.GetPageTextStructured(&requests.GetPageTextStructured{
		Page: page,
		Mode: requests.GetPageTextStructuredModeChars,
	})
	if err != nil {
		return idm.Page{}, nil, fmt.Errorf("page text: %w", err)
	}

	lines := buildLines(text.Chars)

	out := idm.Page{
		Number:       index + 1,
		Width:        size.Width,
		Height:       size.Height,
		Rotation:     int(rotation.PageRotation) * 90,
		Provenance:   idm.ProvenanceDigital,
		TextCoverage: coverage(lines, size.Width, size.Height),
	}

	return out, blocksFromLines(lines, index+1), nil
}

// metadata reads the document information dictionary and the catalog language.
func (e *PDFium) metadata(doc references.FPDF_DOCUMENT) idm.Metadata {
	meta := idm.Metadata{
		Language: e.opts.DefaultLanguage,
	}

	if lang, err := e.instance.FPDFCatalog_GetLanguage(&requests.FPDFCatalog_GetLanguage{
		Document: doc,
	}); err == nil && strings.TrimSpace(lang.Language) != "" {
		meta.Language = strings.TrimSpace(lang.Language)
	}

	info, err := e.instance.GetMetaData(&requests.GetMetaData{Document: doc})
	if err != nil {
		return meta
	}

	for _, tag := range info.Tags {
		value := strings.TrimSpace(tag.Value)
		switch tag.Tag {
		case "Title":
			meta.Title = value
		case "Author":
			// The Author field is a single free-text string in practice. It is
			// not split on separators because names legitimately contain commas.
			if value != "" {
				meta.Authors = []string{value}
			}
		case "Subject":
			meta.Subject = value
		case "Publisher":
			meta.Publisher = value
		}
	}

	return meta
}

func blocksFromLines(lines []line, page int) []idm.Block {
	blocks := make([]idm.Block, 0, len(lines))
	for _, l := range lines {
		blocks = append(blocks, idm.Block{
			Kind:       idm.KindParagraph,
			Text:       l.text,
			Page:       page,
			BBox:       l.box,
			Confidence: 1,
			FontSize:   l.fontSize,
		})
	}
	return blocks
}

// coverage is the fraction of the page area covered by extracted text. It is
// the signal the classifier uses to tell a digital page from a scanned one.
func coverage(lines []line, width, height float64) float64 {
	if width <= 0 || height <= 0 {
		return 0
	}

	var covered float64
	for _, l := range lines {
		if area := l.box.Area(); area > 0 {
			covered += area
		}
	}

	c := covered / (width * height)
	if c > 1 {
		return 1
	}
	return c
}
