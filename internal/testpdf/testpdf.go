// Package testpdf builds small, deterministic PDF files for tests.
//
// It exists so the test suite does not need committed binary fixtures: a test
// can ask for a three-page document and get a valid file that pdfcpu accepts.
package testpdf

import (
	"bytes"
	"fmt"
	"strings"
)

type config struct {
	mediaBox func(page int) string
	lines    []Line
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
	return func(c *config) { c.lines = lines }
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
		if i == 0 && len(cfg.lines) > 0 {
			stream = contentStream(cfg.lines)
		}
		objects[pageNum] = fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R /MediaBox %s /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
			pagesObj, cfg.mediaBox(i+1), font, contentNum,
		)
		objects[contentNum] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)
	}

	objects[catalog] = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObj)
	objects[pagesObj] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages)
	objects[font] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"
	objects[info] = "<< /Title (Test Document) /Author (Test Author) /Producer (testpdf) >>"

	maxObj := firstPage + 2*pages
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
