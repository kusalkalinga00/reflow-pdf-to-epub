package extract_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/extract"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
)

func newExtractor(t *testing.T, opts extract.Options) *extract.PDFium {
	t.Helper()

	e, err := extract.NewPDFium(opts)
	if err != nil {
		t.Fatalf("NewPDFium: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func extractBytes(t *testing.T, e extract.Extractor, data []byte) (idm.Document, error) {
	t.Helper()
	return e.Extract(context.Background(), bytes.NewReader(data), int64(len(data)))
}

func TestExtractRecoversText(t *testing.T) {
	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), testpdf.Build(2))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if doc.PageCount() != 2 {
		t.Fatalf("pages = %d, want 2", doc.PageCount())
	}
	if len(doc.Blocks) < 2 {
		t.Fatalf("blocks = %d, want at least one per page", len(doc.Blocks))
	}

	text := doc.Text()
	for _, want := range []string{"Page 1", "Page 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("text is missing %q:\n%s", want, text)
		}
	}

	if doc.Meta.Language != "en" {
		t.Errorf("language = %q, want the default en", doc.Meta.Language)
	}
}

func TestExtractReadsTheOutline(t *testing.T) {
	pdf := testpdf.Build(3, testpdf.WithOutline(
		testpdf.Bookmark{Title: "Chapter One", Page: 1},
		testpdf.Bookmark{Title: "Chapter Two", Page: 3},
	))

	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), pdf)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(doc.Outline) != 2 {
		t.Fatalf("outline has %d entries, want 2: %+v", len(doc.Outline), doc.Outline)
	}
	if doc.Outline[0].Title != "Chapter One" || doc.Outline[0].Page != 1 {
		t.Errorf("entry 0 = %+v", doc.Outline[0])
	}
	if doc.Outline[1].Title != "Chapter Two" || doc.Outline[1].Page != 3 {
		t.Errorf("entry 1 = %+v", doc.Outline[1])
	}
	for _, e := range doc.Outline {
		if e.Level != 1 {
			t.Errorf("%q has level %d, want 1", e.Title, e.Level)
		}
	}

	if err := doc.Validate(); err != nil {
		t.Errorf("document with an outline is invalid: %v", err)
	}
}

func TestExtractReadsOutlinePositions(t *testing.T) {
	pdf := testpdf.Build(1, testpdf.WithOutline(
		testpdf.Bookmark{Title: "Top", Page: 1, Y: 760},
		testpdf.Bookmark{Title: "Middle", Page: 1, Y: 400},
	))

	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), pdf)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(doc.Outline) != 2 {
		t.Fatalf("outline has %d entries, want 2", len(doc.Outline))
	}
	if got := doc.Outline[0].Y; got < 759 || got > 761 {
		t.Errorf("first entry Y = %v, want about 760", got)
	}
	if got := doc.Outline[1].Y; got < 399 || got > 401 {
		t.Errorf("second entry Y = %v, want about 400", got)
	}
}

func TestExtractWithoutOutline(t *testing.T) {
	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), testpdf.Build(1))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(doc.Outline) != 0 {
		t.Errorf("outline = %+v, want none", doc.Outline)
	}
}

func TestExtractPageGeometry(t *testing.T) {
	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), testpdf.Build(1))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	page := doc.Pages[0]
	if page.Width != 595 || page.Height != 842 {
		t.Errorf("page size = %vx%v, want 595x842", page.Width, page.Height)
	}
	if page.Provenance != idm.ProvenanceDigital {
		t.Errorf("provenance = %q, want digital", page.Provenance)
	}
	if page.TextCoverage <= 0 || page.TextCoverage > 1 {
		t.Errorf("coverage = %v, want a fraction in (0,1]", page.TextCoverage)
	}

	for _, b := range doc.Blocks {
		if b.FontSize <= 0 {
			t.Errorf("block %q has font size %v", b.Text, b.FontSize)
		}
		if b.BBox.X0 < 0 || b.BBox.Y0 < 0 || b.BBox.X1 > page.Width || b.BBox.Y1 > page.Height {
			t.Errorf("block %q has box %+v outside the page", b.Text, b.BBox)
		}
	}
}

func makeJPEG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 120, 120))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 200, G: 30, B: 30, A: 255}}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestExtractRecoversJpegFigures(t *testing.T) {
	pdf := testpdf.Build(1, testpdf.WithImage(makeJPEG(t), 120, 120, 200, 200))

	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), pdf)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var figures []idm.Block
	for _, b := range doc.Blocks {
		if b.Image != nil {
			figures = append(figures, b)
		}
	}
	if len(figures) != 1 {
		t.Fatalf("got %d figures, want 1", len(figures))
	}

	fig := figures[0]
	if fig.Kind != idm.KindFigure {
		t.Errorf("kind = %q, want figure", fig.Kind)
	}
	if fig.Image.MediaType != "image/jpeg" {
		t.Errorf("media type = %q", fig.Image.MediaType)
	}
	if len(fig.Image.Data) == 0 {
		t.Error("no image data")
	}
	if fig.Image.PixelWidth != 120 || fig.Image.PixelHeight != 120 {
		t.Errorf("pixel size = %dx%d, want 120x120", fig.Image.PixelWidth, fig.Image.PixelHeight)
	}
	if fig.BBox.Area() <= 0 {
		t.Errorf("box = %+v, want a placement rectangle", fig.BBox)
	}
	if err := doc.Validate(); err != nil {
		t.Errorf("document with a figure is invalid: %v", err)
	}
}

func TestExtractDecodesFlateImages(t *testing.T) {
	const size = 120
	rgb := make([]byte, size*size*3)
	for i := range rgb {
		rgb[i] = byte(i % 251)
	}

	pdf := testpdf.Build(1, testpdf.WithFlateImage(rgb, size, size, 200, 200))

	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), pdf)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var figure *idm.Block
	for i := range doc.Blocks {
		if doc.Blocks[i].Image != nil {
			figure = &doc.Blocks[i]
		}
	}
	if figure == nil {
		t.Fatal("no figure was recovered")
	}

	if figure.Image.MediaType != "image/png" {
		t.Errorf("media type = %q, want image/png", figure.Image.MediaType)
	}

	img, err := png.Decode(bytes.NewReader(figure.Image.Data))
	if err != nil {
		t.Fatalf("recovered image is not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
		t.Errorf("decoded size = %dx%d, want %dx%d", b.Dx(), b.Dy(), size, size)
	}
}

func TestExtractDecodesIndexedImages(t *testing.T) {
	// A palette image: the samples are indices, so without the palette they
	// are meaningless. PDFium rasterises the object for us, and the result
	// must carry the palette's colours.
	const size = 120
	palette := [][3]byte{
		{255, 0, 0},
		{0, 255, 0},
		{0, 0, 255},
		{255, 255, 255},
	}
	indices := make([]byte, size*size)
	for i := range indices {
		indices[i] = byte(i % len(palette))
	}

	pdf := testpdf.Build(1, testpdf.WithIndexedImage(indices, palette, size, size, 200, 200))

	doc, err := extractBytes(t, newExtractor(t, extract.Options{}), pdf)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var figure *idm.Block
	for i := range doc.Blocks {
		if doc.Blocks[i].Image != nil {
			figure = &doc.Blocks[i]
		}
	}
	if figure == nil {
		t.Fatal("indexed image was skipped")
	}

	if figure.Image.MediaType != "image/png" {
		t.Errorf("media type = %q, want image/png", figure.Image.MediaType)
	}

	img, err := png.Decode(bytes.NewReader(figure.Image.Data))
	if err != nil {
		t.Fatalf("recovered image is not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
		t.Fatalf("decoded size = %dx%d, want %dx%d", b.Dx(), b.Dy(), size, size)
	}

	// Pixel n carries palette entry n, laid out left to right.
	for i, want := range palette {
		r, g, b, _ := img.At(i, 0).RGBA()
		got := [3]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8)}
		if got != want {
			t.Errorf("pixel %d = %v, want %v", i, got, want)
		}
	}
}

func TestExtractRejectsCorruptInput(t *testing.T) {
	_, err := extractBytes(t, newExtractor(t, extract.Options{}), []byte("this is not a pdf"))
	if err == nil {
		t.Fatal("Extract() = nil error, want a rejection")
	}
}

func TestExtractHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	e := newExtractor(t, extract.Options{})
	data := testpdf.Build(1)

	_, err := e.Extract(ctx, bytes.NewReader(data), int64(len(data)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestExtractEncryptedDocument(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.pdf")
	locked := filepath.Join(dir, "locked.pdf")

	if err := os.WriteFile(plain, testpdf.Build(1), 0o600); err != nil {
		t.Fatal(err)
	}

	conf := model.NewDefaultConfiguration()
	conf.UserPW = "open-sesame"
	conf.OwnerPW = "open-sesame"
	if err := api.EncryptFile(context.Background(), plain, locked, conf); err != nil {
		t.Fatalf("encrypt fixture: %v", err)
	}

	data, err := os.ReadFile(locked)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("without a password", func(t *testing.T) {
		if _, err := extractBytes(t, newExtractor(t, extract.Options{}), data); err == nil {
			t.Fatal("Extract() = nil error for an encrypted document")
		}
	})

	t.Run("with the password", func(t *testing.T) {
		doc, err := extractBytes(t, newExtractor(t, extract.Options{Password: "open-sesame"}), data)
		if err != nil {
			t.Fatalf("Extract() error: %v", err)
		}
		if doc.PageCount() != 1 {
			t.Errorf("pages = %d, want 1", doc.PageCount())
		}
	})
}
