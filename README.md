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
