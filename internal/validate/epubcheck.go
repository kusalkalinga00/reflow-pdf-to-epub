// Package validate runs external validators over the finished book.
//
// The unit tests check the structural contract; EPUBCheck checks the
// specification. It is a separate program on purpose: a validator is only
// worth something if it was written by someone else.
package validate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrUnavailable means no EPUBCheck installation was found.
var ErrUnavailable = errors.New("epubcheck is not available")

// Message is one finding from the validator.
type Message struct {
	Severity string `json:"severity"`
	ID       string `json:"id"`
	Message  string `json:"message"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
}

// Report is the verdict for one book.
type Report struct {
	Fatal    int       `json:"fatal"`
	Errors   int       `json:"errors"`
	Warnings int       `json:"warnings"`
	Messages []Message `json:"messages,omitempty"`
}

// OK reports whether the book is free of fatal errors and errors. Warnings do
// not block a book: they are usually style or publisher metadata notes.
func (r Report) OK() bool { return r.Fatal == 0 && r.Errors == 0 }

// Summary is a short, human-readable explanation of a failure.
func (r Report) Summary() string {
	if r.OK() {
		return fmt.Sprintf("epubcheck: %d warnings", r.Warnings)
	}

	var parts []string
	shown := 0
	for _, m := range r.Messages {
		if m.Severity != "ERROR" && m.Severity != "FATAL" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", m.Message, m.ID))
		if shown++; shown == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("epubcheck: %d fatal, %d errors", r.Fatal, r.Errors)
	}
	return fmt.Sprintf("epubcheck: %d fatal, %d errors: %s", r.Fatal, r.Errors, strings.Join(parts, "; "))
}

// EpubCheck runs the reference EPUB validator.
type EpubCheck struct {
	name string
	args []string
}

// NewEpubCheck locates EPUBCheck.
//
// EPUBCHECK_JAR is the supported installation for this project; an `epubcheck`
// on PATH is accepted too, which is what a system package installs.
func NewEpubCheck() (*EpubCheck, error) {
	if jar := os.Getenv("EPUBCHECK_JAR"); jar != "" {
		// #nosec G703 -- the path is operator configuration from the
		// environment, not request data; stat is only a presence check before
		// passing it to the JVM.
		if _, err := os.Stat(jar); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrUnavailable, jar)
		}
		return &EpubCheck{name: "java", args: []string{"-jar", jar}}, nil
	}

	path, err := exec.LookPath("epubcheck")
	if err == nil {
		return &EpubCheck{name: path}, nil
	}

	// The Makefile installs into .epubcheck/ in the working directory; finding
	// it without an exported variable keeps `make build` + `./bin/reflow` a
	// working pair.
	matches, _ := filepath.Glob(filepath.Join(".epubcheck", "*", "epubcheck.jar"))
	if len(matches) > 0 {
		return &EpubCheck{name: "java", args: []string{"-jar", matches[0]}}, nil
	}

	return nil, fmt.Errorf("%w: set EPUBCHECK_JAR or install epubcheck", ErrUnavailable)
}

// Check validates the EPUB at path.
//
// EPUBCheck exits non-zero when it finds problems, so the JSON report is read
// from stdout regardless of the exit status.
func (c *EpubCheck) Check(ctx context.Context, path string) (Report, error) {
	args := append(append([]string{}, c.args...), "-j", "-", path)
	// #nosec G204 -- c.name is "java" or a resolved epubcheck binary, c.args a
	// jar path from the environment, and path is a temporary file this process
	// wrote and validated. Nothing here comes from a request body.
	out, err := exec.CommandContext(ctx, c.name, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return Report{}, fmt.Errorf("epubcheck: %w", err)
		}
	}

	var payload struct {
		Messages []Message `json:"messages"`
		Checker  struct {
			Fatal    int `json:"nFatal"`
			Errors   int `json:"nError"`
			Warnings int `json:"nWarning"`
		} `json:"checker"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Report{}, fmt.Errorf("epubcheck output is not JSON: %w", err)
	}

	return Report{
		Fatal:    payload.Checker.Fatal,
		Errors:   payload.Checker.Errors,
		Warnings: payload.Checker.Warnings,
		Messages: payload.Messages,
	}, nil
}
