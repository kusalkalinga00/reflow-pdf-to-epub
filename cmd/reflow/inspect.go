package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/kusalkalinga00/reflow-pdf-to-epub/internal/ingest"
)

func inspect(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, `usage: reflow inspect [-json] <file.pdf>

Report the structure and metadata of a PDF without converting it.
`)
	}

	asJSON := fs.Bool("json", false, "write the report as JSON")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return exitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	path := fs.Arg(0)
	info, err := ingest.CheckFile(ctx, path, ingest.DefaultLimits)
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", describeIngestError(err))
		return exitFailure
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(info); err != nil {
			fmt.Fprintln(stderr, "reflow:", err)
			return exitFailure
		}
		return exitOK
	}

	printInfo(stdout, path, info)
	return exitOK
}

func describeIngestError(err error) string {
	switch {
	case errors.Is(err, ingest.ErrEncrypted):
		return "this PDF needs a password to open; reflow only converts documents you can already read"
	case errors.Is(err, ingest.ErrTooLarge):
		return fmt.Sprintf("%v (limit %d MB)", err, ingest.DefaultLimits.MaxBytes>>20)
	case errors.Is(err, ingest.ErrTooManyPages):
		return fmt.Sprintf("%v (limit %d pages)", err, ingest.DefaultLimits.MaxPages)
	case errors.Is(err, ingest.ErrInvalid):
		return fmt.Sprintf("not a readable PDF: %v", err)
	default:
		return err.Error()
	}
}

func printInfo(w io.Writer, path string, info ingest.Info) {
	fmt.Fprintln(w, path)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(label, format string, args ...any) {
		fmt.Fprintf(tw, "  %s\t%s\n", label, fmt.Sprintf(format, args...))
	}

	row("pages", "%d", info.Pages)
	row("version", "%s", orDash(info.Version))
	row("size", "%s", pageSize(info))
	row("title", "%s", orDash(info.Title))
	row("author", "%s", orDash(info.Author))
	row("encrypted", "%s", yesNo(info.Encrypted))
	row("tagged", "%s", yesNo(info.Tagged))
	row("outlines", "%s", yesNo(info.HasOutlines))

	tw.Flush()
}

func pageSize(info ingest.Info) string {
	if info.PageWidth == 0 || info.PageHeight == 0 {
		return "unknown"
	}

	out := fmt.Sprintf("%.0f x %.0f pt", info.PageWidth, info.PageHeight)
	if info.MixedPageSizes {
		out += " (varied)"
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
