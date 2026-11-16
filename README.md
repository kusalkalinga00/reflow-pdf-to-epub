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

## Web UI

```sh
make run -- serve
# reflow is listening on http://127.0.0.1:8080
```

A single page: drop a PDF, get an EPUB. Conversion happens in memory inside the
request, so nothing is written to disk or kept afterwards. The server binds to
localhost and has no accounts, no storage, and no queue — it runs the same
`internal/convert` pipeline as the CLI, and shows the quality report with the
download.

## Quality

Every conversion is scored from measured signals — page coverage, chapter and
heading detection, running-header removal — and labelled with a fidelity tier.

| Tier | Meaning |
|---|---|
| T1 Structured | chapters and headings were detected |
| T2 Reflowed | clean text, but the structure is uncertain |
| T3 | reserved for the page-image path, which does not exist yet |

`reflow convert` prints the tier and score, the web UI shows them with the
reasons, and the HTTP API carries them in `X-Reflow-Tier` and `X-Reflow-Score`
with the full report in `X-Reflow-Report`.

Every book is also checked by **EPUBCheck** before it is written; a book that
fails is never handed over. Accessibility is enforced where it can be: a
document language is required, navigation landmarks are always emitted, and an
image without `alt` is rejected. DAISY's `ace` checker is not wired in — its
Electron runner crashes on current Node and its Puppeteer runner needs a Chrome
that is not present — so the contract is checked rather than claimed.

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
make run -- serve
make check      # gofmt, go.mod tidy, vet, tests with -race
```

Text extraction goes through PDFium, which is a native library, so the build
needs CGO. Scanned pages are read with Tesseract when it is installed
(`apt-get install tesseract-ocr`, `brew install tesseract`); without it those
pages stay empty and the quality report says so. `convert -ocr=false` turns the
fallback off. `make deps` fetches a pinned build from `pdfium-binaries` and writes
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
internal/convert/    the pipeline: PDF in, EPUB out
internal/web/        local conversion server and upload page
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
