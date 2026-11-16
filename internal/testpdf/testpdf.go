// Package testpdf builds small, deterministic PDF files for tests.
//
// It exists so the test suite does not need committed binary fixtures: a test
// can ask for a three-page document and get a valid file that pdfcpu accepts.
package testpdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
)

type config struct {
	mediaBox        func(page int) string
	pages           [][]Line
	hasPages        bool
	outline         []Bookmark
	image           []byte
	imageW          int
	imageH          int
	imageDisplayW   float64
	imageDisplayH   float64
	imageFilter     string
	imageColorspace string
	imageBPC        int
}

// Bookmark is one entry in the PDF outline, pointing at a 1-based page.
type Bookmark struct {
	Title string
	Page  int
}

// Line is one line of text drawn on the first page.
type Line struct {
	Text string
	Size float64
	X, Y float64
}

// Option customises Build.
type Option func(*config)

// WithMediaBox overrides the media box of each page. The page argument is
// 1-based.
func WithMediaBox(fn func(page int) string) Option {
	return func(c *config) { c.mediaBox = fn }
}

// WithLines replaces the first page's content with the given lines, each drawn
// at its own size. It is what makes heading detection testable end to end.
func WithLines(lines ...Line) Option {
	return func(c *config) {
		c.pages = [][]Line{lines}
		c.hasPages = true
	}
}

// WithImage draws a JPEG on the first page. The bytes must already be a JPEG;
// displayWidth and displayHeight are in points.
func WithImage(jpeg []byte, pixelWidth, pixelHeight int, displayWidth, displayHeight float64) Option {
	return func(c *config) {
		c.image = jpeg
		c.imageW = pixelWidth
		c.imageH = pixelHeight
		c.imageDisplayW = displayWidth
		c.imageDisplayH = displayHeight
		c.imageFilter = "DCTDecode"
		c.imageColorspace = "DeviceRGB"
		c.imageBPC = 8
	}
}

// WithCMYKImage draws a four-channel image stored with /FlateDecode, which is
// how print-oriented PDFs store photographs.
func WithCMYKImage(cmyk []byte, pixelWidth, pixelHeight int, displayWidth, displayHeight float64) Option {
	return func(c *config) {
		var compressed bytes.Buffer
		zw := zlib.NewWriter(&compressed)
		zw.Write(cmyk)
		zw.Close()

		c.image = compressed.Bytes()
		c.imageW = pixelWidth
		c.imageH = pixelHeight
		c.imageDisplayW = displayWidth
		c.imageDisplayH = displayHeight
		c.imageFilter = "FlateDecode"
		c.imageColorspace = "DeviceCMYK"
		c.imageBPC = 8
	}
}

// WithFlateImage draws an RGB image stored with /FlateDecode, which is what a
// PNG embedded in a PDF looks like once its samples are compressed.
func WithFlateImage(rgb []byte, pixelWidth, pixelHeight int, displayWidth, displayHeight float64) Option {
	return func(c *config) {
		var compressed bytes.Buffer
		zw := zlib.NewWriter(&compressed)
		zw.Write(rgb)
		zw.Close()

		c.image = compressed.Bytes()
		c.imageW = pixelWidth
		c.imageH = pixelHeight
		c.imageDisplayW = displayWidth
		c.imageDisplayH = displayHeight
		c.imageFilter = "FlateDecode"
		c.imageColorspace = "DeviceRGB"
		c.imageBPC = 8
	}
}

// WithOutline writes a PDF outline (bookmarks) pointing at pages.
func WithOutline(marks ...Bookmark) Option {
	return func(c *config) { c.outline = marks }
}

// WithPages sets the content of every page. Pages beyond the supplied slices
// are left blank.
func WithPages(pages ...[]Line) Option {
	return func(c *config) {
		c.pages = pages
		c.hasPages = true
	}
}

// Build returns a PDF with the given number of pages, each carrying a single
// line of text.
func Build(pages int, opts ...Option) []byte {
	if pages < 1 {
		pages = 1
	}

	cfg := config{
		mediaBox: func(int) string { return "[0 0 595 842]" },
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	const (
		catalog   = 1
		pagesObj  = 2
		font      = 3
		info      = 4
		firstPage = 5
	)

	objects := make(map[int]string, 4+2*pages)

	kids := make([]string, 0, pages)
	for i := 0; i < pages; i++ {
		pageNum := firstPage + 2*i
		contentNum := pageNum + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageNum))

		stream := fmt.Sprintf("BT /F1 24 Tf 72 720 Td (Page %d) Tj ET", i+1)
		if cfg.hasPages {
			stream = ""
			if i < len(cfg.pages) {
				stream = contentStream(cfg.pages[i])
			}
		}
		if i == 0 && len(cfg.image) > 0 {
			stream += fmt.Sprintf("\nq %.2f 0 0 %.2f 150 300 cm /Im1 Do Q\n",
				cfg.imageDisplayW, cfg.imageDisplayH)
		}
		resources := fmt.Sprintf("/Font << /F1 %d 0 R >>", font)
		if len(cfg.image) > 0 {
			resources += fmt.Sprintf(" /XObject << /Im1 %d 0 R >>", firstPage+2*pages)
		}
		objects[pageNum] = fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R /MediaBox %s /Resources << %s >> /Contents %d 0 R >>",
			pagesObj, cfg.mediaBox(i+1), resources, contentNum,
		)
		objects[contentNum] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)
	}

	catalogBody := fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObj)
	objects[pagesObj] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages)
	// WinAnsiEncoding is declared explicitly: without it, text extraction has to
	// guess at the character mapping and gets some punctuation wrong.
	objects[font] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"
	objects[info] = "<< /Title (Test Document) /Author (Test Author) /Producer (testpdf) >>"

	// The last page object is firstPage+2*(pages-1), and its content stream is
	// the object after it; everything else starts at the next free number.
	maxObj := firstPage + 2*pages - 1

	if len(cfg.image) > 0 {
		objects[maxObj+1] = fmt.Sprintf(
			"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /%s /BitsPerComponent %d /Filter /%s /Length %d >>\nstream\n%s\nendstream",
			cfg.imageW, cfg.imageH, cfg.imageColorspace, cfg.imageBPC,
			cfg.imageFilter, len(cfg.image), string(cfg.image),
		)
		maxObj++
	}

	if len(cfg.outline) > 0 {
		outlinesNum := maxObj + 1
		kids := make([]int, 0, len(cfg.outline))

		for i, mark := range cfg.outline {
			num := outlinesNum + 1 + i
			pageRef := firstPage + 2*(mark.Page-1)
			body := fmt.Sprintf("<< /Title (%s) /Parent %d 0 R /Dest [%d 0 R /Fit]",
				escapeText(mark.Title), outlinesNum, pageRef)
			if i > 0 {
				body += fmt.Sprintf(" /Prev %d 0 R", num-1)
			}
			if i < len(cfg.outline)-1 {
				body += fmt.Sprintf(" /Next %d 0 R", num+1)
			}
			body += " >>"
			objects[num] = body
			kids = append(kids, num)
		}

		objects[outlinesNum] = fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count %d >>",
			kids[0], kids[len(kids)-1], len(kids))
		catalogBody = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R /Outlines %d 0 R >>", pagesObj, outlinesNum)
		maxObj = outlinesNum + len(cfg.outline)
	}

	objects[catalog] = catalogBody

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")

	offsets := make([]int, maxObj+1)
	for n := 1; n <= maxObj; n++ {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, objects[n])
	}

	xrefAt := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", maxObj+1)
	buf.WriteString("0000000000 65535 f \n")
	for n := 1; n <= maxObj; n++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		maxObj+1, catalog, info, xrefAt)

	return buf.Bytes()
}

func contentStream(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		size := l.Size
		if size <= 0 {
			size = 12
		}
		fmt.Fprintf(&b, "BT /F1 %.2f Tf %.2f %.2f Td (%s) Tj ET\n", size, l.X, l.Y, escapeText(l.Text))
	}
	return b.String()
}

// escapeText escapes the three characters that are special inside a PDF string.
func escapeText(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(s)
}
