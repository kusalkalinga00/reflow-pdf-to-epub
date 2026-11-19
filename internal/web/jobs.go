package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"time"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/convert"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/score"
)

// A job is one conversion that outlives its HTTP request.
//
// The synchronous endpoint is still there for scripts, but a browser cannot
// show where a three-minute conversion has got to when the answer is the file
// itself. A job carries the source bytes, reports progress as pages are read,
// and hands the finished EPUB over when it is ready.
type job struct {
	id       string
	name     string
	password string
	source   []byte

	mu       sync.Mutex
	state    string // queued, running, done, error, canceled
	progress convert.Progress
	pages    int
	chapters int
	quality  score.Report
	epub     []byte
	err      error

	cancel   context.CancelFunc
	finished time.Time
}

// Job states. They are part of the JSON contract with the page.
const (
	jobQueued   = "queued"
	jobRunning  = "running"
	jobDone     = "done"
	jobError    = "error"
	jobCanceled = "canceled"
)

// jobStatus is what the page polls.
type jobStatus struct {
	ID       string        `json:"id"`
	State    string        `json:"state"`
	Stage    string        `json:"stage,omitempty"`
	Done     int           `json:"done"`
	Total    int           `json:"total"`
	Pages    int           `json:"pages,omitempty"`
	Chapters int           `json:"chapters,omitempty"`
	Quality  *score.Report `json:"quality,omitempty"`
	Error    string        `json:"error,omitempty"`
	Code     string        `json:"code,omitempty"`
}

func (j *job) status() jobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()

	s := jobStatus{
		ID:       j.id,
		State:    j.state,
		Stage:    j.progress.Stage,
		Done:     j.progress.Done,
		Total:    j.progress.Total,
		Pages:    j.pages,
		Chapters: j.chapters,
	}
	if j.state == jobDone {
		quality := j.quality
		s.Quality = &quality
	}
	if j.err != nil {
		s.Error = convert.Explain(j.err)
		s.Code = errorCode(j.err)
	}
	return s
}

func (j *job) onProgress(p convert.Progress) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state == jobCanceled {
		return
	}
	j.state = jobRunning
	j.progress = p
	j.pages = p.Total
}

func (j *job) finish(result convert.Result) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state == jobCanceled {
		return
	}
	j.state = jobDone
	j.pages = result.Pages
	j.chapters = len(result.Chapters)
	j.quality = result.Quality
	j.epub = result.EPUB
	j.finished = time.Now()
	j.progress = convert.Progress{Stage: "done", Done: result.Pages, Total: result.Pages}
}

func (j *job) fail(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state == jobCanceled {
		return
	}
	j.state = jobError
	j.err = err
	j.finished = time.Now()
}

// finishedAt reports when the job stopped running, if it has.
func (j *job) finishedAt() (time.Time, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.finished, !j.finished.IsZero()
}

func (j *job) markCanceled() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.state = jobCanceled
	j.err = nil
	j.finished = time.Now()
}

// jobStore keeps jobs in memory. Nothing is written to disk, and a completed
// job is dropped after ttl or when a newer one needs its slot.
type jobStore struct {
	mu   sync.Mutex
	jobs map[string]*job
	ttl  time.Duration
	max  int
}

func newJobStore(ttl time.Duration, max int) *jobStore {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if max <= 0 {
		max = 16
	}
	return &jobStore{jobs: make(map[string]*job), ttl: ttl, max: max}
}

// add stores a job, dropping finished jobs that are past their ttl and, if
// still at capacity, the oldest finished job. It returns false when every
// slot holds a live conversion.
func (s *jobStore) add(j *job) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, old := range s.jobs {
		if finished, ok := old.finishedAt(); ok && now.Sub(finished) > s.ttl {
			delete(s.jobs, id)
		}
	}

	if len(s.jobs) >= s.max {
		var oldest *job
		var oldestAt time.Time
		for _, old := range s.jobs {
			finished, ok := old.finishedAt()
			if !ok {
				continue
			}
			if oldest == nil || finished.Before(oldestAt) {
				oldest, oldestAt = old, finished
			}
		}
		if oldest == nil {
			return false
		}
		delete(s.jobs, oldest.id)
	}

	s.jobs[j.id] = j
	return true
}

func (s *jobStore) get(id string) (*job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *jobStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
}

// handleJobStart accepts a conversion and returns its id immediately.
func (s *Server) handleJobStart(w http.ResponseWriter, r *http.Request) {
	file, header, password, ok := s.readUpload(w, r)
	if !ok {
		return
	}
	defer r.MultipartForm.RemoveAll()
	defer file.Close()

	source, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload", "The upload could not be read.")
		return
	}

	id, err := newJobID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "The job could not be created.")
		return
	}

	j := &job{
		id:       id,
		name:     header.Filename,
		password: password,
		source:   source,
		state:    jobQueued,
	}
	if !s.jobs.add(j) {
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, "busy", "The converter is busy. Try again in a moment.")
		return
	}

	go s.runJob(j)

	writeJSON(w, http.StatusAccepted, j.status())
}

// runJob waits for a conversion slot, converts, and records the outcome.
func (s *Server) runJob(j *job) {
	ctx, cancel := context.WithCancel(context.Background())
	j.mu.Lock()
	j.cancel = cancel
	j.mu.Unlock()
	defer cancel()

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		j.markCanceled()
		return
	}

	j.mu.Lock()
	if j.state == jobCanceled {
		j.mu.Unlock()
		return
	}
	j.state = jobRunning
	j.mu.Unlock()

	runCtx, timeout := context.WithTimeout(ctx, s.timeout)
	defer timeout()

	result, err := s.converter.Run(runCtx, bytes.NewReader(j.source), int64(len(j.source)), convert.DocOptions{
		Name:     j.name,
		Password: j.password,
		Progress: j.onProgress,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			j.markCanceled()
			return
		}
		j.fail(err)
		return
	}
	j.finish(result)
}

// handleJobStatus reports one job's state and progress.
func (s *Server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not-found", "No such conversion.")
		return
	}
	writeJSON(w, http.StatusOK, j.status())
}

// handleJobEPUB returns the finished book. It does not delete the job; the
// page may retry a download and the job expires on its own.
func (s *Server) handleJobEPUB(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not-found", "No such conversion.")
		return
	}

	j.mu.Lock()
	state, epub := j.state, j.epub
	j.mu.Unlock()

	if state != jobDone {
		writeError(w, http.StatusConflict, "not-ready", "That conversion is not finished yet.")
		return
	}

	w.Header().Set("Content-Type", "application/epub+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", epubName(j.name)))
	w.Header().Set("Content-Length", fmt.Sprint(len(epub)))
	w.WriteHeader(http.StatusOK)
	w.Write(epub)
}

// handleJobCancel stops a running job and forgets it.
func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not-found", "No such conversion.")
		return
	}

	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	j.markCanceled()
	s.jobs.remove(j.id)

	w.WriteHeader(http.StatusNoContent)
}

// readUpload parses the multipart body shared by both conversion endpoints.
// It writes the error response itself and returns ok=false when the request
// cannot be converted.
func (s *Server) readUpload(w http.ResponseWriter, r *http.Request) (multipart.File, *multipart.FileHeader, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUpload)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too-large",
				fmt.Sprintf("That file is larger than the %d MB limit.", s.maxUpload>>20))
			return nil, nil, "", false
		}
		writeError(w, http.StatusBadRequest, "upload", "The upload could not be read.")
		return nil, nil, "", false
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "no-file", "No file was uploaded.")
		return nil, nil, "", false
	}

	password := r.FormValue("password")
	if len(password) > maxPasswordBytes {
		file.Close()
		writeError(w, http.StatusBadRequest, "password-too-long", "That password is too long.")
		return nil, nil, "", false
	}

	return file, header, password, true
}

// newJobID returns an unguessable id: the endpoint is local, but an id is a
// capability to read someone's book.
func newJobID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// writeJSON writes a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
