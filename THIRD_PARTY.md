# Third-party software

Reflow itself is Apache-2.0. It is built on other people's work, and the
container image redistributes some of it in binary form. This file lists what
travels where, and under which licence.

## Linked into the binary

| Component | Version | Licence | Notes |
|---|---|---|---|
| [PDFium](https://pdfium.googlesource.com/pdfium/) | pinned by `scripts/pdfium.sh` | BSD-3-Clause | The rendering engine. Distributed as `libpdfium.so` / `libpdfium.dylib` beside the binary; the image copies the full licence set from the PDFium SDK to `/usr/share/doc/reflow/pdfium/`. |
| [pdfium-binaries](https://github.com/bblanchon/pdfium-binaries) | pinned by `scripts/pdfium.sh` | MIT | The prebuilt PDFium builds. |
| [go-pdfium](https://github.com/klippa-app/go-pdfium) | v1.21.4 | MIT | CGO bindings. |
| [pdfcpu](https://github.com/pdfcpu/pdfcpu) | v0.16.4 | Apache-2.0 | Encryption detection and document information. |
| [etree](https://github.com/beevik/etree), [golang.org/x/text](https://pkg.go.dev/golang.org/x/text), [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) | see `go.mod` | BSD-3-Clause | XML, Unicode and image handling. |

## Used at runtime, installed separately

| Component | Licence | Notes |
|---|---|---|
| [Tesseract OCR](https://github.com/tesseract-ocr/tesseract) | Apache-2.0 | Reads pages with no text layer. The image installs `tesseract-ocr` and `tesseract-ocr-eng` from Debian, whose licence files are in `/usr/share/doc/tesseract-ocr/`. |
| [EPUBCheck](https://github.com/w3c/epubcheck) | BSD-3-Clause | Validates every book before it is written. The image carries the jar and its licence in `/opt/epubcheck/`. |
| Java runtime | GPL-2.0 with the Classpath Exception | `default-jre-headless` from Debian, needed only to run EPUBCheck. |

## Development and build tools

Go modules not linked into the binary are listed by `go.sum` with their own
licences; none of them are redistributed.

## Reporting a licensing problem

If you believe something is missing or wrong here, open an issue and it will
be fixed. Attribution matters more than convenience.
