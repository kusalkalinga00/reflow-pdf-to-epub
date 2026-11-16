package web_test

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/convert"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/testpdf"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/web"
)

func newServer(t *testing.T, opts web.Options) *httptest.Server {
	t.Helper()

	converter, err := convert.New(convert.Options{})
	if err != nil {
		t.Fatalf("convert.New: %v", err)
	}
	t.Cleanup(func() { _ = converter.Close() })

	srv := httptest.NewServer(web.New(converter, opts).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func uploadBody(t *testing.T, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return &buf, w.FormDataContentType()
}

func TestIndexServesTheUI(t *testing.T) {
	srv := newServer(t, web.Options{})

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content type = %q, want HTML", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"Reflow", "Choose a PDF", "Convert to EPUB"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

func TestHealth(t *testing.T) {
	srv := newServer(t, web.Options{})

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestConvertReturnsAnEPUB(t *testing.T) {
	srv := newServer(t, web.Options{})

	body, contentType := uploadBody(t, "book.pdf", testpdf.Build(2))
	resp, err := http.Post(srv.URL+"/api/convert", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, payload)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/epub+zip" {
		t.Errorf("content type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, `filename="book.epub"`) {
		t.Errorf("content disposition = %q", cd)
	}
	if pages := resp.Header.Get("X-Reflow-Pages"); pages != "2" {
		t.Errorf("pages header = %q, want 2", pages)
	}
	if chapters := resp.Header.Get("X-Reflow-Chapters"); chapters == "" || chapters == "0" {
		t.Errorf("chapters header = %q", chapters)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("response is not a ZIP: %v", err)
	}
	if zr.File[0].Name != "mimetype" {
		t.Errorf("first entry = %q, want mimetype", zr.File[0].Name)
	}
}

func TestConvertReportsQuality(t *testing.T) {
	srv := newServer(t, web.Options{})

	body, contentType := uploadBody(t, "book.pdf", testpdf.Build(2))
	resp, err := http.Post(srv.URL+"/api/convert", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	tier := resp.Header.Get("X-Reflow-Tier")
	if tier != "T1" && tier != "T2" {
		t.Errorf("tier = %q, want T1 or T2", tier)
	}

	scoreHeader := resp.Header.Get("X-Reflow-Score")
	value, err := strconv.Atoi(scoreHeader)
	if err != nil {
		t.Fatalf("score header %q is not a number", scoreHeader)
	}
	if value < 0 || value > 100 {
		t.Errorf("score = %d, want within [0,100]", value)
	}

	encoded := resp.Header.Get("X-Reflow-Report")
	if encoded == "" {
		t.Fatal("no report header")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("report header is not base64: %v", err)
	}
	var report struct {
		Score int
		Tier  string
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	if report.Score != value || report.Tier != tier {
		t.Errorf("report = %+v, want score %d tier %q", report, value, tier)
	}
}

func TestConvertRejectsNonPDF(t *testing.T) {
	srv := newServer(t, web.Options{})

	body, contentType := uploadBody(t, "notes.txt", []byte("this is not a pdf"))
	resp, err := http.Post(srv.URL+"/api/convert", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}

	var payload struct{ Error string }
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if !strings.Contains(payload.Error, "not a readable PDF") {
		t.Errorf("error = %q", payload.Error)
	}
}

func TestConvertRejectsOversizedUpload(t *testing.T) {
	srv := newServer(t, web.Options{MaxUpload: 200})

	body, contentType := uploadBody(t, "book.pdf", testpdf.Build(1))
	resp, err := http.Post(srv.URL+"/api/convert", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestConvertWithoutAFile(t *testing.T) {
	srv := newServer(t, web.Options{})

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("note", "no file here"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(srv.URL+"/api/convert", w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConvertRejectsWrongMethod(t *testing.T) {
	srv := newServer(t, web.Options{})

	resp, err := http.Get(srv.URL + "/api/convert")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv := newServer(t, web.Options{})

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'none'") {
		t.Errorf("CSP = %q", got)
	}
}
