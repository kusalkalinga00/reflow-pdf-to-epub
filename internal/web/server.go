// Package web serves the local conversion UI.
//
// It is deliberately small: one page, two conversion endpoints, no accounts,
// no disk storage. /api/convert answers the request with the EPUB, which is
// what a script wants. /api/jobs returns an id and reports progress while the
// book is built, which is what a browser needs; a job keeps its source bytes
// and finished EPUB in memory until it expires.
package web

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kusalkalinga00/reflow/internal/convert"
	"github.com/kusalkalinga00/reflow/internal/ingest"
	"github.com/kusalkalinga00/reflow/internal/render"
)

//go:embed static
var staticFiles embed.FS

// Converter is the pipeline this package drives. It is an interface so the
// handlers can be tested without PDFium.
type Converter interface {
	Run(ctx context.Context, src io.ReaderAt, size int64, doc convert.DocOptions) (convert.Result, error)
}

// Options configures a Server.
type Options struct {
	// MaxUpload bounds the request body in bytes. Zero uses the ingest limit.
	MaxUpload int64
	// Concurrency caps simultaneous conversions. Zero means 2.
	Concurrency int
	// ConvertTimeout bounds one conversion. Zero means 10 minutes.
	ConvertTimeout time.Duration
}

// Server serves the UI and the conversion endpoint.
type Server struct {
	converter Converter
	maxUpload int64
	timeout   time.Duration
	slots     chan struct{}
	jobs      *jobStore
	handler   http.Handler
}

// New builds a Server.
func New(converter Converter, opts Options) *Server {
	if opts.MaxUpload <= 0 {
		opts.MaxUpload = ingest.DefaultLimits.MaxBytes
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 2
	}
	if opts.ConvertTimeout <= 0 {
		opts.ConvertTimeout = 10 * time.Minute
	}

	s := &Server{
		converter: converter,
		maxUpload: opts.MaxUpload,
		timeout:   opts.ConvertTimeout,
		slots:     make(chan struct{}, opts.Concurrency),
		jobs:      newJobStore(0, 0),
	}

	mux := http.NewServeMux()
	// Registered without a method so a wrong method gets 405 rather than the
	// static handler's 404.
	mux.HandleFunc("/api/convert", s.handleConvert)
	mux.HandleFunc("POST /api/jobs", s.handleJobStart)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleJobStatus)
	mux.HandleFunc("GET /api/jobs/{id}/epub", s.handleJobEPUB)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleJobCancel)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("/", s.staticHandler())

	s.handler = securityHeaders(mux)
	return s
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// The embed directive guarantees this cannot fail.
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok\n")
}

func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method", "Use POST to convert a file.")
		return
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "busy", "The converter is busy. Try again in a moment.")
		return
	}

	file, header, password, ok := s.readUpload(w, r)
	if !ok {
		return
	}
	defer r.MultipartForm.RemoveAll()
	defer file.Close()

	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload", "The upload could not be read.")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusBadRequest, "upload", "The upload could not be read.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	result, err := s.converter.Run(ctx, file, size, convert.DocOptions{
		Name:     header.Filename,
		Password: password,
	})
	if err != nil {
		message := convert.Explain(err)
		switch {
		case errors.Is(err, ingest.ErrEncrypted) && password != "":
			message = "That password did not open the PDF."
		case errors.Is(err, ingest.ErrEncrypted):
			message = "This PDF is password-protected. Enter its password to continue."
		}
		writeError(w, statusFor(err), errorCode(err), message)
		return
	}

	w.Header().Set("Content-Type", "application/epub+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", epubName(header.Filename)))
	w.Header().Set("X-Reflow-Pages", strconv.Itoa(result.Pages))
	w.Header().Set("X-Reflow-Chapters", strconv.Itoa(len(result.Chapters)))
	w.Header().Set("X-Reflow-Tier", string(result.Quality.Tier))
	w.Header().Set("X-Reflow-Score", strconv.Itoa(result.Quality.Score))
	// The full report rides along as base64 so the page can explain the score
	// without a second request that would convert the book again.
	if report, err := json.Marshal(result.Quality); err == nil {
		w.Header().Set("X-Reflow-Report", base64.StdEncoding.EncodeToString(report))
	}
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(result.EPUB); err != nil {
		// The client went away; there is nobody to tell.
		return
	}
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, ingest.ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ingest.ErrInvalid):
		return http.StatusUnsupportedMediaType
	case errors.Is(err, ingest.ErrEncrypted),
		errors.Is(err, ingest.ErrTooManyPages),
		errors.Is(err, render.ErrNoText):
		return http.StatusUnprocessableEntity
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// maxPasswordBytes bounds the password a request may carry. PDF passwords are
// short; anything longer is a client bug or an attempt to waste memory.
const maxPasswordBytes = 256

// errorCode names the machine-readable reason for a failure. A client keys
// off the code — "password" means "ask for one and retry" — while the message
// is what a person reads.
func errorCode(err error) string {
	switch {
	case errors.Is(err, ingest.ErrEncrypted):
		return "password"
	case errors.Is(err, ingest.ErrTooLarge):
		return "too-large"
	case errors.Is(err, ingest.ErrTooManyPages):
		return "too-many-pages"
	case errors.Is(err, ingest.ErrInvalid):
		return "invalid"
	case errors.Is(err, render.ErrNoText):
		return "no-text"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "internal"
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
		Code  string `json:"code,omitempty"`
	}{Error: message, Code: code})
}

// epubName turns "book.pdf" into "book.epub" and quotes nothing that could
// break the header.
func epubName(name string) string {
	base := name
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".pdf")
	base = strings.TrimSuffix(base, ".PDF")
	if base == "" {
		base = "book"
	}

	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == ' ', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.TrimSpace(b.String()) + ".epub"
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
