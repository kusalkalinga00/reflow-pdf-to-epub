// Package extract turns a PDF into the intermediate document model.
//
// Extraction is deliberately shallow: it recovers one block per text line, with
// the line's bounding box, effective font size and reading position. Deciding
// which lines form a paragraph, which are headings, and which column they
// belong to is the layout stage's job.
package extract

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"strings"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/single_threaded"
	"github.com/klippa-app/go-pdfium/structs"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ocr"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/tables"
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
	// OCR reads pages that have no text layer. Nil disables it, and the page is
	// left empty as before.
	OCR ocr.Recognizer
	// Rules collects drawn line segments, scaled to page coordinates, for
	// table detection. It is nil unless the caller asks.
	Rules *[]tables.Rule
	// OCRDPI is the resolution pages are rendered at for OCR. Zero means 300.
	OCRDPI int
}

// PDFium extracts text with the PDFium engine.
type PDFium struct {
	pool     pdfium.Pool
	instance pdfium.Pdfium
	opts     Options

	// skippedImages accumulates images that could not be embedded.
	skippedImages int
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
	if opts.OCRDPI <= 0 {
		opts.OCRDPI = 300
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

// ExtractOptions are per-document settings. They exist separately from
// Options because a server reuses one extractor for many documents and must
// not keep one reader's password for the next.
type ExtractOptions struct {
	// Password overrides Options.Password for this document. It is never
	// written to disk or logs.
	Password string
	// Progress reports pages finished. Total is zero until the document is
	// open, then the page count. Done counts pages extracted, so a long book
	// can show real progress instead of a spinner.
	Progress func(done, total int)
}

// Extract reads every page of src.
func (e *PDFium) Extract(ctx context.Context, src io.ReaderAt, size int64) (idm.Document, error) {
	return e.ExtractDoc(ctx, src, size, ExtractOptions{})
}

// ExtractDoc reads every page of src with per-document settings.
func (e *PDFium) ExtractDoc(ctx context.Context, src io.ReaderAt, size int64, opts ExtractOptions) (idm.Document, error) {
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
	password := e.opts.Password
	if opts.Password != "" {
		password = opts.Password
	}
	if password != "" {
		open.Password = &password
	}

	// The extractor is reused across documents; one document's unreadable
	// images must not be counted against the next.
	e.skippedImages = 0

	doc, err := e.instance.OpenDocument(open)
	if err != nil {
		return idm.Document{}, fmt.Errorf("open pdf: %w", err)
	}
	defer e.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	count, err := e.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return idm.Document{}, fmt.Errorf("page count: %w", err)
	}

	out := idm.Document{
		Meta:    e.metadata(doc.Document),
		Outline: e.outline(doc.Document),
	}

	if opts.Progress != nil {
		opts.Progress(0, count.PageCount)
	}
	for i := 0; i < count.PageCount; i++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		page, blocks, err := e.extractPage(ctx, doc.Document, i)
		if err != nil {
			return out, fmt.Errorf("page %d: %w", i+1, err)
		}
		out.Pages = append(out.Pages, page)
		out.Blocks = append(out.Blocks, blocks...)

		if opts.Progress != nil {
			opts.Progress(i+1, count.PageCount)
		}
	}

	out.Features = e.features(doc.Document, count.PageCount)
	out.Features.ImagesSkipped = e.skippedImages

	if err := out.Validate(); err != nil {
		return out, err
	}
	return out, nil
}

func (e *PDFium) extractPage(ctx context.Context, doc references.FPDF_DOCUMENT, index int) (idm.Page, []idm.Block, error) {
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
	provenance := idm.ProvenanceDigital

	if e.opts.Rules != nil {
		for _, rule := range e.rules(doc, index, size.Width, size.Height) {
			rule.Page = index + 1
			*e.opts.Rules = append(*e.opts.Rules, rule)
		}
	}

	// A page with no text layer is either blank or a scan. OCR is the only way
	// to tell, and the only way to recover it.
	figures, skipped := e.images(doc, index)
	e.skippedImages += skipped

	if len(lines) == 0 && e.opts.OCR != nil {
		recognized, err := e.ocrPage(ctx, page, size.Width, size.Height)
		if err != nil {
			return idm.Page{}, nil, fmt.Errorf("ocr: %w", err)
		}
		if len(recognized) > 0 {
			lines = recognized
			provenance = idm.ProvenanceOCR
		}
	}

	blocks := append(blocksFromLines(lines, index+1), figures...)

	out := idm.Page{
		Number:       index + 1,
		Width:        size.Width,
		Height:       size.Height,
		Rotation:     int(rotation.PageRotation) * 90,
		Provenance:   provenance,
		TextCoverage: coverage(lines, size.Width, size.Height),
	}

	return out, blocks, nil
}

// maxStructureElements bounds the walk over the structure tree. Tagged files can
// carry tens of thousands of elements and the walk only produces counts.
const maxStructureElements = 20000

// features counts the structural elements a tagged PDF declares.
//
// It deliberately does not try to recover their content: mapping elements to
// text needs marked content IDs, and many producers do not write them. Knowing
// that a table exists is still worth a lot — it lets the quality report say the
// table was flattened instead of pretending the conversion was perfect.
func (e *PDFium) features(doc references.FPDF_DOCUMENT, pages int) idm.Features {
	var (
		out     idm.Features
		visited int
		walk    func(el references.FPDF_STRUCTELEMENT)
	)

	walk = func(el references.FPDF_STRUCTELEMENT) {
		if visited >= maxStructureElements {
			return
		}
		visited++

		if typ, err := e.instance.FPDF_StructElement_GetType(&requests.FPDF_StructElement_GetType{
			StructElement: el,
		}); err == nil {
			switch {
			case typ.Type == "Table":
				out.Tables++
			case typ.Type == "Figure":
				out.Figures++
			case typ.Type == "H" || (len(typ.Type) == 2 && typ.Type[0] == 'H'):
				out.HeadingElements++
			}
		}

		children, err := e.instance.FPDF_StructElement_CountChildren(&requests.FPDF_StructElement_CountChildren{
			StructElement: el,
		})
		if err != nil {
			return
		}
		for i := 0; i < children.Count; i++ {
			child, err := e.instance.FPDF_StructElement_GetChildAtIndex(&requests.FPDF_StructElement_GetChildAtIndex{
				StructElement: el,
				Index:         i,
			})
			if err == nil && child.StructElement != "" {
				walk(child.StructElement)
			}
		}
	}

	for i := 0; i < pages; i++ {
		tree, err := e.instance.FPDF_StructTree_GetForPage(&requests.FPDF_StructTree_GetForPage{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: i}},
		})
		if err != nil || tree.StructTree == "" {
			continue
		}
		out.Tagged = true

		children, err := e.instance.FPDF_StructTree_CountChildren(&requests.FPDF_StructTree_CountChildren{
			StructTree: tree.StructTree,
		})
		if err == nil {
			for j := 0; j < children.Count; j++ {
				child, err := e.instance.FPDF_StructTree_GetChildAtIndex(&requests.FPDF_StructTree_GetChildAtIndex{
					StructTree: tree.StructTree,
					Index:      j,
				})
				if err == nil && child.StructElement != "" {
					walk(child.StructElement)
				}
			}
		}

		e.instance.FPDF_StructTree_Close(&requests.FPDF_StructTree_Close{})
	}

	return out
}

// outline reads the PDF's own table of contents. Producers that write one are
// telling us the chapter structure directly, which beats inferring it from font
// sizes.
func (e *PDFium) outline(doc references.FPDF_DOCUMENT) []idm.OutlineEntry {
	first, err := e.instance.FPDFBookmark_GetFirstChild(&requests.FPDFBookmark_GetFirstChild{
		Document: doc,
	})
	if err != nil || first.Bookmark == nil {
		return nil
	}

	var entries []idm.OutlineEntry
	e.walkOutline(doc, *first.Bookmark, 1, &entries)
	return entries
}

// maxOutlineDepth stops a malformed file from producing unbounded recursion.
const maxOutlineDepth = 12

func (e *PDFium) walkOutline(doc references.FPDF_DOCUMENT, bookmark references.FPDF_BOOKMARK, level int, entries *[]idm.OutlineEntry) {
	if level > maxOutlineDepth {
		return
	}

	current := bookmark
	for {
		title := ""
		if res, err := e.instance.FPDFBookmark_GetTitle(&requests.FPDFBookmark_GetTitle{
			Bookmark: current,
		}); err == nil {
			title = strings.TrimSpace(res.Title)
		}

		page, y := e.bookmarkPosition(doc, current)
		if title != "" {
			*entries = append(*entries, idm.OutlineEntry{
				Title: title, Page: page, Level: level, Y: y,
			})
		}

		if child, err := e.instance.FPDFBookmark_GetFirstChild(&requests.FPDFBookmark_GetFirstChild{
			Document: doc,
			Bookmark: &current,
		}); err == nil && child.Bookmark != nil {
			e.walkOutline(doc, *child.Bookmark, level+1, entries)
		}

		next, err := e.instance.FPDFBookmark_GetNextSibling(&requests.FPDFBookmark_GetNextSibling{
			Document: doc,
			Bookmark: current,
		})
		if err != nil || next.Bookmark == nil {
			return
		}
		current = *next.Bookmark
	}
}

// bookmarkPosition returns the 1-based page a bookmark points at and its
// vertical destination, or zero when it has none.
func (e *PDFium) bookmarkPosition(doc references.FPDF_DOCUMENT, bookmark references.FPDF_BOOKMARK) (int, float64) {
	if dest, err := e.instance.FPDFBookmark_GetDest(&requests.FPDFBookmark_GetDest{
		Document: doc,
		Bookmark: bookmark,
	}); err == nil && dest.Dest != nil {
		return e.destPosition(doc, *dest.Dest)
	}

	// Some bookmarks carry an action instead of a destination.
	if action, err := e.instance.FPDFBookmark_GetAction(&requests.FPDFBookmark_GetAction{
		Bookmark: bookmark,
	}); err == nil && action.Action != nil {
		if dest, err := e.instance.FPDFAction_GetDest(&requests.FPDFAction_GetDest{
			Document: doc,
			Action:   *action.Action,
		}); err == nil && dest.Dest != nil {
			return e.destPosition(doc, *dest.Dest)
		}
	}

	return 0, 0
}

// destPosition reads the page and vertical position of a destination. Most
// bookmarks in real documents point inside a page rather than at its top, which
// is the only way to recover sections of a one-page document.
func (e *PDFium) destPosition(doc references.FPDF_DOCUMENT, dest references.FPDF_DEST) (int, float64) {
	page := 0
	if index, err := e.instance.FPDFDest_GetDestPageIndex(&requests.FPDFDest_GetDestPageIndex{
		Document: doc,
		Dest:     dest,
	}); err == nil {
		page = index.Index + 1
	}

	y := 0.0
	if location, err := e.instance.FPDFDest_GetLocationInPage(&requests.FPDFDest_GetLocationInPage{
		Dest: dest,
	}); err == nil && location.Y != nil {
		y = float64(*location.Y)
	}

	return page, y
}

// minImagePixels drops images too small to be a figure: bullets, icons and
// rules are drawn as images in some producers.
const minImagePixels = 48

// rules collects the drawn line segments of a page, scaled into page
// coordinates.
//
// PDFium reports path points in the path's own transformation space, which on
// real documents is not user space: on the invoice corpus the points ran to
// 794x1123 on a 595x842 page. The largest observed point extent defines the
// scale, which maps the grid onto the words exactly.
func (e *PDFium) rules(doc references.FPDF_DOCUMENT, index int, pageWidth, pageHeight float64) []tables.Rule {
	page := requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: index}}

	objects, err := e.instance.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: page})
	if err != nil {
		return nil
	}

	type path struct {
		points [][2]float64
	}
	var paths []path

	for i := 0; i < objects.Count; i++ {
		obj, err := e.instance.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: page, Index: i})
		if err != nil {
			continue
		}
		kind, err := e.instance.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj.PageObject})
		if err != nil || kind.Type != enums.FPDF_PAGEOBJ_PATH {
			continue
		}

		segments, err := e.instance.FPDFPath_CountSegments(&requests.FPDFPath_CountSegments{PageObject: obj.PageObject})
		if err != nil {
			continue
		}

		var points [][2]float64
		for s := 0; s < segments.Count; s++ {
			segment, err := e.instance.FPDFPath_GetPathSegment(&requests.FPDFPath_GetPathSegment{
				PageObject: obj.PageObject, Index: s,
			})
			if err != nil {
				continue
			}
			segmentType, err := e.instance.FPDFPathSegment_GetType(&requests.FPDFPathSegment_GetType{
				PathSegment: segment.PathSegment,
			})
			if err != nil {
				continue
			}
			if segmentType.Type != enums.FPDF_SEGMENT_LINETO && segmentType.Type != enums.FPDF_SEGMENT_MOVETO {
				continue
			}
			point, err := e.instance.FPDFPathSegment_GetPoint(&requests.FPDFPathSegment_GetPoint{
				PathSegment: segment.PathSegment,
			})
			if err != nil {
				continue
			}
			points = append(points, [2]float64{float64(point.X), float64(point.Y)})
		}
		if len(points) >= 2 {
			paths = append(paths, path{points: points})
		}
	}

	// The path space extent, to scale from.
	maxX, maxY := 0.0, 0.0
	for _, p := range paths {
		for _, pt := range p.points {
			if pt[0] > maxX {
				maxX = pt[0]
			}
			if pt[1] > maxY {
				maxY = pt[1]
			}
		}
	}
	sx, sy := 1.0, 1.0
	if maxX > pageWidth && maxX > 0 {
		sx = pageWidth / maxX
	}
	if maxY > pageHeight && maxY > 0 {
		sy = pageHeight / maxY
	}

	var rules []tables.Rule
	for _, p := range paths {
		for j := 1; j < len(p.points); j++ {
			x0, y0 := p.points[j-1][0]*sx, p.points[j-1][1]*sy
			x1, y1 := p.points[j][0]*sx, p.points[j][1]*sy

			// Page borders are not table rules; most documents draw one.
			if y0 < 15 || y0 > pageHeight-15 || x0 < 15 || x0 > pageWidth-15 {
				continue
			}
			if y1 < 15 || y1 > pageHeight-15 || x1 < 15 || x1 > pageWidth-15 {
				continue
			}

			switch {
			case abs(y1-y0) < 1 && abs(x1-x0) >= tables.MinHRule:
				rules = append(rules, tables.Rule{
					Horizontal: true,
					Y:          (y0 + y1) / 2,
					X0:         math.Min(x0, x1),
					X1:         math.Max(x0, x1),
				})
			case abs(x1-x0) < 1 && abs(y1-y0) >= tables.MinVRule:
				rules = append(rules, tables.Rule{
					Horizontal: false,
					X0:         (x0 + x1) / 2,
					X1:         (x0 + x1) / 2,
					Y0:         math.Min(y0, y1),
					Y1:         math.Max(y0, y1),
				})
			}
		}
	}

	return rules
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// images collects the figures on a page.
//
// Raw JPEG data can be embedded in an EPUB untouched, which keeps quality and
// costs no re-encoding. Flate images are rebuilt from their samples into a
// PNG. Everything else — indexed palettes, CMYK, JPEG 2000 — goes through the
// image object's own bitmap, which PDFium rasterises for us. Only an object
// PDFium cannot rasterise is counted as skipped: silence would be worse than a
// reported gap.
func (e *PDFium) images(doc references.FPDF_DOCUMENT, index int) ([]idm.Block, int) {
	page := requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: index}}

	objects, err := e.instance.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: page})
	if err != nil {
		return nil, 0
	}

	var (
		blocks  []idm.Block
		skipped int
	)

	for i := 0; i < objects.Count; i++ {
		obj, err := e.instance.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: page, Index: i})
		if err != nil {
			continue
		}

		kind, err := e.instance.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj.PageObject})
		if err != nil || kind.Type != enums.FPDF_PAGEOBJ_IMAGE {
			continue
		}

		// FPDFImageObj_GetImagePixelSize segfaults in this binding version, so
		// the metadata call (which takes the page) is used instead.
		meta, err := e.instance.FPDFImageObj_GetImageMetadata(&requests.FPDFImageObj_GetImageMetadata{
			ImageObject: obj.PageObject,
			Page:        page,
		})
		if err != nil || meta.ImageMetadata.Width < minImagePixels || meta.ImageMetadata.Height < minImagePixels {
			continue
		}

		bounds, err := e.instance.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{
			PageObject: obj.PageObject,
		})
		if err != nil {
			continue
		}

		box := idm.Rect{
			X0: float64(bounds.Left), Y0: float64(bounds.Bottom),
			X1: float64(bounds.Right), Y1: float64(bounds.Top),
		}
		if !box.Valid() || box.Area() <= 0 {
			continue
		}

		image, ok := e.imageObject(obj.PageObject, meta.ImageMetadata)
		if !ok {
			skipped++
			continue
		}

		blocks = append(blocks, idm.Block{
			Kind:       idm.KindFigure,
			Page:       index + 1,
			BBox:       box,
			Confidence: 1,
			Image:      image,
		})
	}

	return blocks, skipped
}

// imageObject returns the figure image, preferring the original JPEG bytes and
// falling back to decoding the samples into a PNG.
func (e *PDFium) imageObject(obj references.FPDF_PAGEOBJECT, meta structs.FPDF_IMAGEOBJ_METADATA) (*idm.Image, bool) {
	if filter, err := e.instance.FPDFImageObj_GetImageFilter(&requests.FPDFImageObj_GetImageFilter{
		ImageObject: obj,
		Index:       0,
	}); err == nil && filter.ImageFilter == "DCTDecode" {
		// Re-embedding the original JPEG keeps the full quality and costs no
		// re-encode.
		if raw, err := e.instance.FPDFImageObj_GetImageDataRaw(&requests.FPDFImageObj_GetImageDataRaw{
			ImageObject: obj,
		}); err == nil && len(raw.Data) > 0 {
			return &idm.Image{
				Data:        raw.Data,
				MediaType:   "image/jpeg",
				PixelWidth:  int(meta.Width),
				PixelHeight: int(meta.Height),
			}, true
		}
	}

	decoded, err := e.instance.FPDFImageObj_GetImageDataDecoded(&requests.FPDFImageObj_GetImageDataDecoded{
		ImageObject: obj,
	})
	if err != nil {
		return nil, false
	}

	img := samplesToImage(decoded.Data, int(meta.Width), int(meta.Height), meta.Colorspace, int(meta.BitsPerPixel))
	if img == nil {
		// Indexed palettes have no samples we can map without the palette,
		// and CMYK/JPEG 2000 are not in Go's image packages. The image
		// object's bitmap is PDFium's own rasterisation of the image, so it
		// works for all of them.
		return e.imageBitmap(obj)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}

	return &idm.Image{
		Data:        buf.Bytes(),
		MediaType:   "image/png",
		PixelWidth:  int(meta.Width),
		PixelHeight: int(meta.Height),
	}, true
}

// imageBitmap rasterises the image object itself and encodes it as a PNG.
//
// FPDFImageObj_GetBitmap renders the image at its own pixel size and decodes
// every colour space PDFium supports, including indexed palettes. It ignores
// an image mask, so a masked image loses transparency; that is still better
// than dropping the figure, and it is why the PNG path is a fallback rather
// than the default.
func (e *PDFium) imageBitmap(obj references.FPDF_PAGEOBJECT) (*idm.Image, bool) {
	bitmap, err := e.instance.FPDFImageObj_GetBitmap(&requests.FPDFImageObj_GetBitmap{
		ImageObject: obj,
	})
	if err != nil {
		return nil, false
	}
	defer e.instance.FPDFBitmap_Destroy(&requests.FPDFBitmap_Destroy{Bitmap: bitmap.Bitmap})

	width, err := e.instance.FPDFBitmap_GetWidth(&requests.FPDFBitmap_GetWidth{Bitmap: bitmap.Bitmap})
	if err != nil || width.Width <= 0 {
		return nil, false
	}
	height, err := e.instance.FPDFBitmap_GetHeight(&requests.FPDFBitmap_GetHeight{Bitmap: bitmap.Bitmap})
	if err != nil || height.Height <= 0 {
		return nil, false
	}
	stride, err := e.instance.FPDFBitmap_GetStride(&requests.FPDFBitmap_GetStride{Bitmap: bitmap.Bitmap})
	if err != nil || stride.Stride <= 0 {
		return nil, false
	}
	format, err := e.instance.FPDFBitmap_GetFormat(&requests.FPDFBitmap_GetFormat{Bitmap: bitmap.Bitmap})
	if err != nil {
		return nil, false
	}
	buffer, err := e.instance.FPDFBitmap_GetBuffer(&requests.FPDFBitmap_GetBuffer{Bitmap: bitmap.Bitmap})
	if err != nil {
		return nil, false
	}

	img := bitmapImage(buffer.Buffer, width.Width, height.Height, stride.Stride, format.Format)
	if img == nil {
		return nil, false
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}

	return &idm.Image{
		Data:        buf.Bytes(),
		MediaType:   "image/png",
		PixelWidth:  width.Width,
		PixelHeight: height.Height,
	}, true
}

// bitmapImage converts PDFium's bitmap formats into a Go image. The formats
// are its own: gray, and blue-green-red with or without alpha, each 8-bit.
func bitmapImage(data []byte, width, height, stride int, format enums.FPDF_BITMAP_FORMAT) image.Image {
	if width <= 0 || height <= 0 || stride <= 0 || len(data) < stride*height {
		return nil
	}

	rect := image.Rect(0, 0, width, height)

	switch format {
	case enums.FPDF_BITMAP_FORMAT_GRAY:
		img := image.NewGray(rect)
		for y := 0; y < height; y++ {
			copy(img.Pix[y*img.Stride:y*img.Stride+width], data[y*stride:y*stride+width])
		}
		return img

	case enums.FPDF_BITMAP_FORMAT_BGR, enums.FPDF_BITMAP_FORMAT_BGRX, enums.FPDF_BITMAP_FORMAT_BGRA:
		img := image.NewRGBA(rect)
		pixel := 4
		if format == enums.FPDF_BITMAP_FORMAT_BGR {
			pixel = 3
		}
		for y := 0; y < height; y++ {
			row := data[y*stride:]
			out := img.Pix[y*img.Stride:]
			for x := 0; x < width; x++ {
				out[x*4+0] = row[x*pixel+2]
				out[x*4+1] = row[x*pixel+1]
				out[x*4+2] = row[x*pixel+0]
				out[x*4+3] = 0xff
			}
		}
		return img
	}

	return nil
}

// samplesToImage turns decoded PDF samples into a Go image for the colour
// spaces and depths we can represent exactly. Anything else returns nil and is
// reported as skipped rather than guessed at.
func samplesToImage(data []byte, width, height int, space enums.FPDF_COLORSPACE, bitsPerPixel int) image.Image {
	if width <= 0 || height <= 0 {
		return nil
	}

	switch {
	case (space == enums.FPDF_COLORSPACE_DEVICERGB || space == enums.FPDF_COLORSPACE_CALRGB) && bitsPerPixel == 24:
		if len(data) < width*height*3 {
			return nil
		}
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for i := 0; i < width*height; i++ {
			img.Pix[i*4+0] = data[i*3+0]
			img.Pix[i*4+1] = data[i*3+1]
			img.Pix[i*4+2] = data[i*3+2]
			img.Pix[i*4+3] = 0xff
		}
		return img

	case (space == enums.FPDF_COLORSPACE_DEVICEGRAY || space == enums.FPDF_COLORSPACE_CALGRAY) && bitsPerPixel == 8:
		if len(data) < width*height {
			return nil
		}
		img := image.NewGray(image.Rect(0, 0, width, height))
		copy(img.Pix, data[:width*height])
		return img
	}

	return nil
}

// ocrPage renders the page and converts the recognized line boxes from image
// pixels (origin top left) to PDF points (origin bottom left).
func (e *PDFium) ocrPage(ctx context.Context, page requests.Page, width, height float64) ([]line, error) {
	rendered, err := e.instance.RenderPageInDPI(&requests.RenderPageInDPI{
		Page: page,
		DPI:  e.opts.OCRDPI,
	})
	if err != nil {
		return nil, fmt.Errorf("render page: %w", err)
	}
	defer rendered.Cleanup()

	var buf bytes.Buffer
	if err := png.Encode(&buf, rendered.Result.RenderedImage); err != nil {
		return nil, fmt.Errorf("encode page image: %w", err)
	}

	recognized, err := e.opts.OCR.Recognize(ctx, buf.Bytes())
	if err != nil {
		return nil, err
	}
	// A cover or a photograph has shapes, not words. Keeping OCR's handful of
	// stray letters from it would put noise in the book; the page then counts
	// as having no text, which is what it is.
	if !ocr.LooksLikeText(recognized) {
		return nil, nil
	}

	imgWidth := float64(rendered.Result.Width)
	imgHeight := float64(rendered.Result.Height)
	if imgWidth <= 0 || imgHeight <= 0 {
		return nil, fmt.Errorf("render page: empty image")
	}

	scaleX := width / imgWidth
	scaleY := height / imgHeight

	lines := make([]line, 0, len(recognized))
	for _, l := range recognized {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		box := idm.Rect{
			X0: l.Left * scaleX,
			Y0: height - l.Bottom*scaleY,
			X1: l.Right * scaleX,
			Y1: height - l.Top*scaleY,
		}
		if !box.Valid() || box.Area() <= 0 {
			continue
		}
		lines = append(lines, line{
			text:       l.Text,
			box:        box,
			fontSize:   box.Height(),
			confidence: l.Confidence,
		})
	}

	return lines, nil
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

func wordTexts(words []word) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = w.text
	}
	return out
}

func blocksFromLines(lines []line, page int) []idm.Block {
	blocks := make([]idm.Block, 0, len(lines))
	for _, l := range lines {
		words := make([]idm.Word, len(l.words))
		for i, w := range l.words {
			words[i] = idm.Word{Text: w.text, Box: w.box}
		}

		// OCR lines carry whole-line text without per-word boxes; digital
		// lines carry words. Both must produce their own text.
		text := l.text
		if len(words) > 0 {
			text = strings.Join(wordTexts(l.words), " ")
		}

		blocks = append(blocks, idm.Block{
			Kind:       idm.KindParagraph,
			Text:       text,
			Page:       page,
			BBox:       l.box,
			Confidence: l.confidence,
			FontSize:   l.fontSize,
			Words:      words,
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
