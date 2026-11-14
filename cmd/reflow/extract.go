package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/extract"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/ingest"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/layout"
)

// extractDoc is the debugging command behind `reflow extract`. It dumps the
// intermediate document model so extraction can be inspected without a reader.
func extractDoc(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, `usage: reflow extract [-json] [-o file] [-layout] <file.pdf>

Recover text from a PDF and print the intermediate document model.
Without -json, a summary is printed instead.
`)
	}

	asJSON := fs.Bool("json", false, "write the document model as JSON")
	outPath := fs.String("o", "", "write the JSON output to this file")
	applyLayout := fs.Bool("layout", false, "put blocks in reading order before dumping")

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

	if _, err := ingest.CheckFile(ctx, path, ingest.DefaultLimits); err != nil {
		fmt.Fprintln(stderr, "reflow:", describeIngestError(err))
		return exitFailure
	}

	doc, err := runExtraction(ctx, path)
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}

	if *applyLayout {
		doc = layout.Apply(doc)
	}

	writeJSON := *asJSON || *outPath != ""

	if !writeJSON {
		printDocument(stdout, path, doc)
		return exitOK
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}
	data = append(data, '\n')

	if *outPath == "" {
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintln(stderr, "reflow:", err)
			return exitFailure
		}
		return exitOK
	}

	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintln(stderr, "reflow:", err)
		return exitFailure
	}
	fmt.Fprintf(stdout, "wrote %d bytes to %s\n", len(data), *outPath)
	return exitOK
}

func runExtraction(ctx context.Context, path string) (idm.Document, error) {
	e, err := extract.NewPDFium(extract.Options{})
	if err != nil {
		return idm.Document{}, err
	}
	defer e.Close()

	f, err := os.Open(path)
	if err != nil {
		return idm.Document{}, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return idm.Document{}, err
	}

	doc, err := e.Extract(ctx, f, st.Size())
	if err != nil {
		return idm.Document{}, fmt.Errorf("could not read text from the PDF: %w", err)
	}
	return doc, nil
}

func printDocument(w io.Writer, path string, doc idm.Document) {
	fmt.Fprintln(w, path)

	var chars int
	var coverage float64
	for _, block := range doc.Blocks {
		chars += len([]rune(block.Text))
	}
	if doc.PageCount() > 0 {
		for _, page := range doc.Pages {
			coverage += page.TextCoverage
		}
		coverage /= float64(doc.PageCount())
	}

	fmt.Fprintf(w, "  pages       %d\n", doc.PageCount())
	fmt.Fprintf(w, "  blocks      %d\n", len(doc.Blocks))
	fmt.Fprintf(w, "  characters  %d\n", chars)
	fmt.Fprintf(w, "  language    %s\n", doc.Meta.Language)
	fmt.Fprintf(w, "  coverage    %.2f%%\n", coverage*100)
}
