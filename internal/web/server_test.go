package web_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/convert"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ingest"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/score"
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
	for _, want := range []string{"Reflow", "Drop a PDF", "Convert", "PDF to EPUB"} {
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

// stubConverter records the document options the handler passes down.
type stubConverter struct {
	got    convert.DocOptions
	result convert.Result
	err    error
}

func (s *stubConverter) Run(_ context.Context, _ io.ReaderAt, _ int64, doc convert.DocOptions) (convert.Result, error) {
	s.got = doc
	return s.result, s.err
}

func uploadWithPassword(t *testing.T, name string, data []byte, password string) (*bytes.Buffer, string) {
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
	if password != "" {
		if err := w.WriteField("password", password); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return &buf, w.FormDataContentType()
}

func TestConvertPassesThePasswordToThePipeline(t *testing.T) {
	stub := &stubConverter{result: convert.Result{EPUB: []byte("epub bytes")}}
	srv := httptest.NewServer(web.New(stub, web.Options{}).Handler())
	defer srv.Close()

	body, contentType := uploadWithPassword(t, "book.pdf", testpdf.Build(1), "open-sesame")
	resp, err := http.Post(srv.URL+"/api/convert", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := stub.got.Password; got != "open-sesame" {
		t.Errorf("password = %q, want open-sesame", got)
	}
}

func TestEncryptedPDFReturnsAPasswordCode(t *testing.T) {
	stub := &stubConverter{err: fmt.Errorf("%w: it is encrypted", ingest.ErrEncrypted)}
	srv := httptest.NewServer(web.New(stub, web.Options{}).Handler())
	defer srv.Close()

	body, contentType := uploadWithPassword(t, "book.pdf", testpdf.Build(1), "")
	resp, err := http.Post(srv.URL+"/api/convert", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}

	var payload struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if payload.Code != "password" {
		t.Errorf("code = %q, want password", payload.Code)
	}
	if payload.Error == "" {
		t.Error("error message is empty")
	}
}

func TestWrongPasswordSaysItDidNotOpen(t *testing.T) {
	stub := &stubConverter{err: fmt.Errorf("%w: it is encrypted", ingest.ErrEncrypted)}
	srv := httptest.NewServer(web.New(stub, web.Options{}).Handler())
	defer srv.Close()

	body, contentType := uploadWithPassword(t, "book.pdf", testpdf.Build(1), "wrong")
	resp, err := http.Post(srv.URL+"/api/convert", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var payload struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if payload.Code != "password" {
		t.Errorf("code = %q, want password", payload.Code)
	}
	if !strings.Contains(payload.Error, "did not open") {
		t.Errorf("error = %q, want it to say the password did not open the file", payload.Error)
	}
}

// progressConverter reports one page and then waits, so a test can observe a
// job while it is running.
type progressConverter struct {
	started chan struct{}
	release chan struct{}
	result  convert.Result
}

func (p *progressConverter) Run(ctx context.Context, _ io.ReaderAt, _ int64, doc convert.DocOptions) (convert.Result, error) {
	if doc.Progress != nil {
		doc.Progress(convert.Progress{Stage: "reading", Done: 1, Total: 2})
	}
	close(p.started)
	select {
	case <-p.release:
		return p.result, nil
	case <-ctx.Done():
		return convert.Result{}, ctx.Err()
	}
}

type jobPoll struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Stage    string `json:"stage"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Pages    int    `json:"pages"`
	Chapters int    `json:"chapters"`
	Error    string `json:"error"`
	Code     string `json:"code"`
	Quality  struct {
		Score int `json:"score"`
	} `json:"quality"`
}

func postJob(t *testing.T, srv *httptest.Server, data []byte, password string) jobPoll {
	t.Helper()

	body, contentType := uploadWithPassword(t, "book.pdf", data, password)
	resp, err := http.Post(srv.URL+"/api/jobs", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	var started jobPoll
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started.ID == "" {
		t.Fatal("job has no id")
	}
	return started
}

func waitForJob(t *testing.T, srv *httptest.Server, id, want string) jobPoll {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(srv.URL + "/api/jobs/" + id)
		if err != nil {
			t.Fatal(err)
		}
		var state jobPoll
		decodeErr := json.NewDecoder(resp.Body).Decode(&state)
		resp.Body.Close()
		if decodeErr != nil {
			t.Fatalf("decode status: %v", decodeErr)
		}
		if state.State == want {
			return state
		}
		if state.State == "error" && want != "error" {
			t.Fatalf("job failed: %s (%s)", state.Error, state.Code)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s never reached state %q", id, want)
	return jobPoll{}
}

func TestJobRunsToCompletionAndServesTheEPUB(t *testing.T) {
	srv := newServer(t, web.Options{})

	started := postJob(t, srv, testpdf.Build(2), "")

	done := waitForJob(t, srv, started.ID, "done")
	if done.Pages != 2 {
		t.Errorf("pages = %d, want 2", done.Pages)
	}
	if done.Chapters == 0 {
		t.Error("no chapters reported")
	}
	if done.Quality.Score <= 0 {
		t.Errorf("score = %d, want a measured score", done.Quality.Score)
	}

	resp, err := http.Get(srv.URL + "/api/jobs/" + started.ID + "/epub")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("epub status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/epub+zip" {
		t.Errorf("content type = %q", got)
	}

	book, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(book, []byte("PK")) {
		t.Errorf("epub does not start with a ZIP signature: %q", book[:min(8, len(book))])
	}
}

func TestJobReportsLiveProgress(t *testing.T) {
	stub := &progressConverter{
		started: make(chan struct{}),
		release: make(chan struct{}),
		result: convert.Result{
			EPUB:     []byte("epub"),
			Pages:    2,
			Chapters: []string{"One"},
			Quality:  score.Report{Score: 90, Tier: score.TierStructured},
		},
	}
	srv := httptest.NewServer(web.New(stub, web.Options{}).Handler())
	defer srv.Close()

	started := postJob(t, srv, testpdf.Build(1), "")

	select {
	case <-stub.started:
	case <-time.After(5 * time.Second):
		t.Fatal("converter never started")
	}

	state := waitForJob(t, srv, started.ID, "running")
	if state.Stage != "reading" || state.Done != 1 || state.Total != 2 {
		t.Errorf("progress = %+v, want reading 1/2", state)
	}

	// The book is not downloadable while the job is still running.
	resp, err := http.Get(srv.URL + "/api/jobs/" + started.ID + "/epub")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("early epub status = %d, want 409", resp.StatusCode)
	}

	close(stub.release)
	waitForJob(t, srv, started.ID, "done")
}

func TestJobCanBeCanceled(t *testing.T) {
	stub := &progressConverter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	srv := httptest.NewServer(web.New(stub, web.Options{}).Handler())
	defer srv.Close()

	started := postJob(t, srv, testpdf.Build(1), "")
	select {
	case <-stub.started:
	case <-time.After(5 * time.Second):
		t.Fatal("converter never started")
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/jobs/"+started.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel status = %d, want 204", resp.StatusCode)
	}

	// The job is gone from the store.
	resp, err = http.Get(srv.URL + "/api/jobs/" + started.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status after cancel = %d, want 404", resp.StatusCode)
	}
}

func TestUnknownJobIsNotFound(t *testing.T) {
	srv := newServer(t, web.Options{})

	resp, err := http.Get(srv.URL + "/api/jobs/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestJobAsksForAPasswordAndSaysWhenItIsWrong(t *testing.T) {
	stub := &stubConverter{err: fmt.Errorf("%w: it is encrypted", ingest.ErrEncrypted)}
	srv := httptest.NewServer(web.New(stub, web.Options{}).Handler())
	defer srv.Close()

	started := postJob(t, srv, testpdf.Build(1), "")
	state := waitForJob(t, srv, started.ID, "error")
	if state.Code != "password" {
		t.Errorf("code = %q, want password", state.Code)
	}
	if !strings.Contains(state.Error, "password-protected") {
		t.Errorf("message = %q, want it to ask for the password", state.Error)
	}

	started = postJob(t, srv, testpdf.Build(1), "wrong")
	state = waitForJob(t, srv, started.ID, "error")
	if !strings.Contains(state.Error, "did not open") {
		t.Errorf("message with a password = %q, want it to say the password was wrong", state.Error)
	}
}

func TestVersionEndpoint(t *testing.T) {
	srv := newServer(t, web.Options{})

	resp, err := http.Get(srv.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Version == "" {
		t.Error("version is empty")
	}
}
