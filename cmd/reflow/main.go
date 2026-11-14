// Command reflow converts PDF files into reflowable EPUB 3 ebooks.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/version"
)

// Exit codes are part of the CLI contract: scripts branch on them.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

const usageText = `reflow converts PDF files into reflowable EPUB 3 ebooks.

usage:
  reflow <command> [arguments]

commands:
  inspect   report the structure and metadata of a PDF
  version   print build version information
  help      print this message

Conversion commands are added as the engine lands. See PROPOSAL.md.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is separated from main so the command surface is testable without
// spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}

	switch args[0] {
	case "inspect":
		return inspect(args[1:], stdout, stderr)
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, version.String())
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return exitOK
	default:
		fmt.Fprintf(stderr, "reflow: unknown command %q\n\n%s", args[0], usageText)
		return exitUsage
	}
}
