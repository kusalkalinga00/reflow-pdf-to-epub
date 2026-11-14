// Package ingest inspects a source PDF before any page is parsed.
//
// This is stage 0 of the pipeline: it enforces size and page limits, rejects
// files that are not PDFs, tells the caller whether a password is needed, and
// reports the metadata the EPUB package will need.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Sentinel errors. Callers branch on these with errors.Is; the wrapped detail
// is for humans and logs.
var (
	// ErrEncrypted means the file needs a password the caller has not supplied.
	// Reflow never removes DRM; this is only for documents the user can
	// already open.
	ErrEncrypted = errors.New("pdf requires a password")

	// ErrTooLarge means the file exceeds the configured byte limit.
	ErrTooLarge = errors.New("pdf exceeds the size limit")

	// ErrTooManyPages means the document exceeds the configured page limit.
	ErrTooManyPages = errors.New("pdf exceeds the page limit")

	// ErrInvalid means the bytes are not a PDF we can read.
	ErrInvalid = errors.New("pdf is not readable")
)

// Limits caps the work a single upload can cause.
type Limits struct {
	MaxBytes int64
	MaxPages int
}

// DefaultLimits are the MVP limits from the proposal: 100 MB and 1,000 pages.
var DefaultLimits = Limits{
	MaxBytes: 100 << 20,
	MaxPages: 1000,
}

// Info describes a source PDF.
type Info struct {
	Pages   int    `json:"pages"`
	Version string `json:"version"`

	Title   string `json:"title,omitempty"`
	Author  string `json:"author,omitempty"`
	Subject string `json:"subject,omitempty"`

	// Encrypted is true when the file is encrypted but readable without a
	// password (the common "permissions only" case).
	Encrypted bool `json:"encrypted"`

	Tagged      bool `json:"tagged"`
	HasOutlines bool `json:"hasOutlines"`
	Linearized  bool `json:"linearized"`

	// PageWidth and PageHeight are the dimensions of the first page, in points.
	PageWidth  float64 `json:"pageWidth"`
	PageHeight float64 `json:"pageHeight"`
	// MixedPageSizes is true when pages do not all share one size.
	MixedPageSizes bool `json:"mixedPageSizes"`
}

// Check reads the structure of src and reports what it found. It never parses
// page content, so it is cheap enough to run before queueing a job.
func Check(ctx context.Context, src io.ReaderAt, size int64, limits Limits) (Info, error) {
	if size <= 0 {
		return Info{}, fmt.Errorf("%w: empty file", ErrInvalid)
	}
	if limits.MaxBytes > 0 && size > limits.MaxBytes {
		return Info{}, fmt.Errorf("%w: %d bytes is over the %d byte limit",
			ErrTooLarge, size, limits.MaxBytes)
	}

	// A PDF must start with its signature. Checking it here cheaply rejects
	// polyglots and renamed archives before a parser sees them.
	header := make([]byte, 5)
	if _, err := src.ReadAt(header, 0); err != nil || string(header) != "%PDF-" {
		return Info{}, fmt.Errorf("%w: missing %%PDF- signature", ErrInvalid)
	}

	// pdfcpu validates while it reads, in its relaxed mode: broken references
	// are reported, but files real readers open still convert.
	info, err := api.PDFInfo(ctx, io.NewSectionReader(src, 0, size), "", nil, false,
		model.NewDefaultConfiguration())
	if err != nil {
		switch {
		case errors.Is(err, pdfcpu.ErrWrongPassword), errors.Is(err, pdfcpu.ErrEncrypted):
			return Info{}, fmt.Errorf("%w: it is encrypted", ErrEncrypted)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return Info{}, err
		default:
			return Info{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}

	if limits.MaxPages > 0 && info.PageCount > limits.MaxPages {
		return Info{}, fmt.Errorf("%w: %d pages is over the %d page limit",
			ErrTooManyPages, info.PageCount, limits.MaxPages)
	}

	out := Info{
		Pages:       info.PageCount,
		Version:     info.Version,
		Title:       strings.TrimSpace(info.Title),
		Author:      strings.TrimSpace(info.Author),
		Subject:     strings.TrimSpace(info.Subject),
		Encrypted:   info.Encrypted,
		Tagged:      info.Tagged,
		HasOutlines: info.Outlines,
		Linearized:  info.Linearized,
	}

	// PageDimensions is the set of distinct page sizes. PageBoundaries is
	// per page and in order, which is where the first page's size comes from.
	out.MixedPageSizes = len(info.PageDimensions) > 1
	if len(info.PageBoundaries) > 0 {
		pb := info.PageBoundaries[0]
		if pb.Media != nil && pb.Media.Rect != nil {
			w, h := pb.Media.Rect.Width(), pb.Media.Rect.Height()
			// A rotated page is read in its display orientation.
			if pb.Rot == 90 || pb.Rot == 270 {
				w, h = h, w
			}
			out.PageWidth, out.PageHeight = w, h
		}
	}

	return out, nil
}

// CheckFile is Check for a path on disk.
func CheckFile(ctx context.Context, name string, limits Limits) (Info, error) {
	f, err := os.Open(name)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return Info{}, err
	}

	return Check(ctx, f, st.Size(), limits)
}
