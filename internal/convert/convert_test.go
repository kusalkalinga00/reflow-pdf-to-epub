package convert_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/convert"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ingest"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/score"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
)

func simplePDF() []byte {
	return testpdf.Build(1, testpdf.WithPages([]testpdf.Line{
		{Text: "A short page of text.", Size: 12, X: 72, Y: 700},
	}))
}

func TestConverterSerializesConcurrentRuns(t *testing.T) {
	converter, err := convert.New(convert.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer converter.Close()

	data := simplePDF()

	// One Converter owns one PDFium instance and a shared rules slice. Two
	// callers must both get a book, and the race detector must stay quiet.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := converter.Run(context.Background(), bytes.NewReader(data), int64(len(data)),
				convert.DocOptions{Name: "book.pdf"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Run: %v", err)
		}
	}
}

func TestPoolRunsConversionsConcurrently(t *testing.T) {
	pool, err := convert.NewPool(2, convert.Options{})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	if got := pool.Size(); got != 2 {
		t.Fatalf("Size = %d, want 2", got)
	}

	data := simplePDF()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := pool.Run(context.Background(), bytes.NewReader(data), int64(len(data)),
				convert.DocOptions{Name: "book.pdf"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("pool Run: %v", err)
		}
	}
}

func TestProgressReportsPagesAndStages(t *testing.T) {
	converter, err := convert.New(convert.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer converter.Close()

	data := testpdf.Build(2, testpdf.WithPages(
		[]testpdf.Line{{Text: "First page.", Size: 12, X: 72, Y: 700}},
		[]testpdf.Line{{Text: "Second page.", Size: 12, X: 72, Y: 700}},
	))

	var stages []convert.Progress
	_, err = converter.Run(context.Background(), bytes.NewReader(data), int64(len(data)),
		convert.DocOptions{
			Name: "book.pdf",
			Progress: func(p convert.Progress) {
				stages = append(stages, p)
			},
		})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawPages, sawDone bool
	for _, p := range stages {
		if p.Stage == "reading" && p.Total == 2 && p.Done == 2 {
			sawPages = true
		}
	}
	if !sawPages {
		t.Errorf("no page progress for 2 pages: %+v", stages)
	}
	if len(stages) == 0 || stages[len(stages)-1].Stage != "done" {
		t.Errorf("last stage = %+v, want done", stages)
	}
	for _, p := range stages {
		if p.Stage == "done" {
			sawDone = true
		}
	}
	if !sawDone {
		t.Error("done stage was never reported")
	}
}

func TestPasswordOpensAnEncryptedPDF(t *testing.T) {
	const password = "open-sesame"
	data := encryptedPDF(t, password)

	converter, err := convert.New(convert.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer converter.Close()

	_, err = converter.Run(context.Background(), bytes.NewReader(data), int64(len(data)),
		convert.DocOptions{Name: "locked.pdf"})
	if !errors.Is(err, ingest.ErrEncrypted) {
		t.Fatalf("without a password: err = %v, want ErrEncrypted", err)
	}

	result, err := converter.Run(context.Background(), bytes.NewReader(data), int64(len(data)),
		convert.DocOptions{Name: "locked.pdf", Password: password})
	if err != nil {
		t.Fatalf("with the password: %v", err)
	}
	if result.Pages != 1 {
		t.Errorf("pages = %d, want 1", result.Pages)
	}
	if len(result.EPUB) == 0 {
		t.Error("no EPUB was produced")
	}

	_, err = converter.Run(context.Background(), bytes.NewReader(data), int64(len(data)),
		convert.DocOptions{Name: "locked.pdf", Password: "wrong"})
	if !errors.Is(err, ingest.ErrEncrypted) {
		t.Errorf("with a wrong password: err = %v, want ErrEncrypted", err)
	}
}

// encryptedPDF encrypts a one-page fixture with a user password, so the
// password path is tested against a real encrypted file rather than a stub.
func encryptedPDF(t *testing.T, password string) []byte {
	t.Helper()

	plain := simplePDF()
	conf := model.NewDefaultConfiguration()
	conf.UserPW = password
	conf.OwnerPW = "owner-secret"
	conf.EncryptUsingAES = true
	conf.EncryptKeyLength = 256

	var out bytes.Buffer
	if err := api.Encrypt(context.Background(), bytes.NewReader(plain), &out, conf); err != nil {
		t.Fatalf("encrypt fixture: %v", err)
	}
	return out.Bytes()
}
func TestFootnoteAtThePageBottomBecomesAnAside(t *testing.T) {
	pdf := testpdf.Build(3, testpdf.WithPages(
		[]testpdf.Line{
			{Text: "The passage above is worth a note, and the note sits at the foot of the page.", Size: 10, X: 72, Y: 700},
			{Text: "1 The note itself, set smaller than the body text it annotates.", Size: 8, X: 72, Y: 60},
		},
		[]testpdf.Line{{Text: "The next page carries the argument forward.", Size: 10, X: 72, Y: 700}},
		[]testpdf.Line{{Text: "The last page closes the chapter.", Size: 10, X: 72, Y: 700}},
	))

	converter, err := convert.New(convert.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer converter.Close()

	result, err := converter.Run(context.Background(), bytes.NewReader(pdf), int64(len(pdf)), convert.DocOptions{Name: "notes.pdf"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(result.EPUB), int64(len(result.EPUB)))
	if err != nil {
		t.Fatalf("epub is not a zip: %v", err)
	}

	var body string
	for _, f := range zr.File {
		if f.Name == "OEBPS/text/chapter-001.xhtml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open chapter: %v", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read chapter: %v", err)
			}
			body = string(data)
		}
	}
	if !strings.Contains(body, `epub:type="footnote" role="doc-footnote"`) {
		t.Fatalf("the note is not marked as a footnote:\n%s", body)
	}

	if result.Quality.Signals.Footnotes != 1 {
		t.Errorf("footnote signal = %d, want 1", result.Quality.Signals.Footnotes)
	}
	if !hasIssue(result.Quality, "FOOTNOTES") {
		t.Errorf("issues = %v, want FOOTNOTES", result.Quality.Issues)
	}
}

// hasIssue reports whether the report carries an issue with this code.
func hasIssue(report score.Report, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestScannedDocumentBecomesAPageImageBook(t *testing.T) {
	// A page with a picture and no text at all is a scan, not a failure.
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 200, 200, 255
	}
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}

	pdf := testpdf.Build(1, testpdf.WithPages(nil), testpdf.WithImage(jpegBytes.Bytes(), 64, 64, 400, 400))

	converter, err := convert.New(convert.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer converter.Close()

	result, err := converter.Run(context.Background(), bytes.NewReader(pdf), int64(len(pdf)), convert.DocOptions{Name: "scan.pdf"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Quality.Tier != score.TierFacsimile {
		t.Errorf("tier = %q, want T3", result.Quality.Tier)
	}
	if result.Quality.Score != 65 {
		t.Errorf("score = %d, want 65", result.Quality.Score)
	}
	if len(result.Chapters) != 1 || result.Chapters[0] != "Pages" {
		t.Errorf("chapters = %v, want [Pages]", result.Chapters)
	}

	zr, err := zip.NewReader(bytes.NewReader(result.EPUB), int64(len(result.EPUB)))
	if err != nil {
		t.Fatalf("epub is not a zip: %v", err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "OEBPS/images/page-001.jpg" {
			found = true
		}
	}
	if !found {
		t.Error("the rendered page is not in the book")
	}
}
