# Reflow — PDF to EPUB

A web app that converts PDF files into reflowable EPUB 3 ebooks.

**Status:** proposal stage. No implementation yet.

- [Project proposal](./PROPOSAL.md) — problem, goals, conversion design,
  architecture, quality strategy, risks, timeline.

## The short version

PDF stores positioned glyphs, not documents, so converting it to a reflowable ebook
is an inference problem with no single correct answer. Reflow's approach:

1. Classify the PDF (digital / scanned / mixed) and route it down the right pipeline.
2. Measure the result and report an honest fidelity tier with reasons.
3. Fall back to a page-faithful EPUB with accessible text rather than emitting a
   mangled book.
4. Offer a fast review editor so a user can fix chapter breaks in a minute.

## Stack

Go (API, worker, engine) · Next.js + TypeScript (web) · Postgres (state + queue via
River) · S3/R2 (files) · PDFium (extraction) · Tesseract (OCR) · epubcheck + ace
(validation). Details and rejected alternatives in the proposal, §10.

## Non-goals

No DRM circumvention, no layout preservation, no math OCR, no hosting of books.

## Development

Requires Go 1.27+, a C toolchain, and `pkg-config` (`brew install pkgconf`,
`apt-get install pkg-config`).

```sh
make deps       # download the pinned PDFium SDK into .pdfium/ (gitignored)
make build      # compile to bin/reflow
make run -- version
make run -- inspect book.pdf
make run -- extract book.pdf
make run -- convert book.pdf -o book.epub
make run -- corpus
make check      # gofmt, go.mod tidy, vet, tests with -race
```

Text extraction goes through PDFium, which is a native library, so the build
needs CGO. `make deps` fetches a pinned build from `pdfium-binaries` and writes
the `pkg-config` file that points at it; `build`, `test`, and `check` run it
automatically. The rpath is baked in, so binaries and test suites run without
setting `DYLD_LIBRARY_PATH`.

The CLI is a thin shell around `internal/` packages; every pipeline stage is a
library call so it can be tested without a running service.

```
cmd/reflow/          CLI entry point
internal/version/    build metadata
internal/ingest/     PDF preflight: size and page limits, validity, encryption
internal/extract/    PDFium text, line geometry, and coverage
internal/corpus/     quality corpus: character recall, word error rate, reading order
internal/normalize/  ligatures, Unicode, whitespace, and de-hyphenation
internal/layout/     reading order for columns, running header and footer removal
internal/render/     paragraphs, headings, chapters, page anchors
internal/idm/        intermediate document model (extraction → assembly)
internal/epub/       EPUB 3 writer: container, package, nav, NCX, archive
internal/testpdf/    deterministic PDF fixtures for tests
```

The EPUB writer enforces the parts readers and validators actually check: `mimetype`
first and stored with no extra field, a package document that lists every archive
member, a nav document with a table of contents, and accessibility metadata.
