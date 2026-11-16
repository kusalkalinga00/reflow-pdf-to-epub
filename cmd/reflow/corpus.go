package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/corpus"
)

// corpusCmd runs the quality corpus: the built-in synthetic cases, or a
// directory of real PDFs with expected text beside them.
func corpusCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("corpus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, `usage: reflow corpus [-dir folder] [-json]

Run the conversion corpus and report quality metrics.
With no -dir, the built-in synthetic cases are used. With -dir, every
name.pdf needs a name.txt holding the text a reader should see.
`)
	}

	dir := fs.String("dir", "", "directory of .pdf files with matching .txt expected text")
	asJSON := fs.Bool("json", false, "write the results as JSON")
	noExpect := fs.Bool("no-expect", false, "convert PDFs without expected .txt files and report diagnostics")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return exitUsage
	}

	cases := corpus.Synthetic()
	if *dir != "" {
		loaded, err := corpus.LoadDir(*dir, *noExpect)
		if err != nil {
			fmt.Fprintln(stderr, "reflow:", err)
			return exitFailure
		}
		cases = loaded
	}

	results := corpus.Run(cases)

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintln(stderr, "reflow:", err)
			return exitFailure
		}
	} else {
		printResults(stdout, results)
	}

	failed := 0
	for _, r := range results {
		if !r.Pass {
			failed++
		}
	}
	fmt.Fprintf(stderr, "%d cases, %d failed\n", len(results), failed)

	if failed > 0 {
		return exitFailure
	}
	return exitOK
}

func printResults(w io.Writer, results []corpus.Result) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "case\trecall\twer\torder\tchapters\tresult")

	for _, r := range results {
		recall, wer, order := "—", "—", "—"
		if !r.ExpectedError && !r.NoExpectation {
			recall = fmt.Sprintf("%.1f%%", r.CharRecall*100)
			wer = fmt.Sprintf("%.1f%%", r.WordErrorRate*100)
			order = "no"
			if r.OrderOK {
				order = "yes"
			}
		}

		result := "pass"
		if !r.Pass {
			result = "FAIL"
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
			r.Name, recall, wer, order, len(r.ChapterTitles), result)
	}
	tw.Flush()
}
