# Project Proposal — Reflow

## A web app that converts PDF to EPUB

| | |
|---|---|
| **Repository** | `tool-factory-epub-converter` |
| **Working title** | Reflow |
| **Status** | Draft for review |
| **Date** | 14 November 2026 |
| **Author** | Kusal |
| **Target release** | MVP in 8 weeks (public beta, mid-January 2027) |

---

## 1. Summary

Reflow is a web application that turns a PDF into a reflowable EPUB 3 ebook: real
text, real chapters, a working table of contents, and a file that passes `epubcheck`
and opens cleanly on Kindle, Apple Books, Kobo, and Calibre.

The hard part is not packaging an EPUB. The hard part is that **PDF has no
structure to recover** — it is a page-description format, not a document format. Text
is drawn glyph-by-glyph at coordinates. There are no paragraphs, no headings, no
reading order, and often no correct character encoding. Every PDF-to-EPUB tool in
existence is therefore making *inferences*, and most of them make those inferences
badly or silently.

Reflow's product bet is honesty plus control:

1. **Classify first.** Detect whether the PDF is digital, scanned, or mixed, and route
   it down the right pipeline instead of pretending one pipeline fits all.
2. **Score the output.** Every conversion produces a measured fidelity score and a
   plain-language explanation of what worked and what did not.
3. **Never fail silently.** If semantic reconstruction is unreliable, fall back to a
   page-faithful EPUB that still has selectable, searchable, screen-reader-accessible
   text rather than emitting a mangled book.
4. **Let the user fix it.** A lightweight review editor for chapter splits, headings,
   and page ranges turns an 85% conversion into a 98% one in about two minutes.

The initial build is a single-developer project, self-hosted, queue-based, and
designed so the conversion engine can later be extracted as an API or an open-source
library.

---

## 2. Problem and opportunity

### 2.1 Why PDF → EPUB is genuinely difficult

These are the failure modes we must design against. They are not hypothetical; they
are the reasons the existing tools produce poor output.

| Problem | Consequence for naive tools |
|---|---|
| Text is positioned glyph-by-glyph, not stored as paragraphs | Line breaks appear mid-sentence; paragraphs merge or shatter |
| Content-stream order ≠ reading order | Multi-column pages interleave into nonsense |
| Fonts may lack a `ToUnicode` CMap or use custom encodings | Extracted text is garbled, or maps to the wrong characters |
| Ligatures (`ﬁ`, `ﬂ`, `ﬃ`) are single glyphs | "efﬁcient" survives into the EPUB and breaks search |
| Hyphenation at line ends | "hyphen-ation" and false word joins |
| Headers, footers, watermarks, page numbers repeat on every page | Interleaved into body text or dumped into every chapter |
| Headings have no semantic markup, only font size | No chapters, no TOC, one giant HTML file |
| Footnote markers are superscripts | Footnotes stay inline as stray digits |
| Tables are drawn with lines or whitespace | Flattened into unreadable word runs |
| Math is glyph positioning | Destroyed entirely; needs MathML or image fallback |
| Scanned pages have no text at all | Zero output without OCR |
| Encrypted / permission-restricted PDFs | Crash with an opaque error |

### 2.2 Who this is for

| Persona | Job to be done | What they care about |
|---|---|---|
| **The sideloader** | Move legally owned PDFs onto a Kindle/Kobo for comfortable reading | Speed, no signup friction, works on first try |
| **The accessibility user** | Get a screen-reader-usable version of a document they are entitled to read | Valid structure, correct alt text, real reading order |
| **The researcher / student** | Convert lecture notes, papers, and reports into a notes-friendly format | Searchable text, tables that survive, page numbers preserved |
| **The indie author / publisher** | Convert a print-ready PDF into an ebook listing | Chapter control, metadata, cover, professional output |
| **The internal team** | Convert a documentation set as a batch | API access, determinism, no data leakage |

### 2.3 Market gap

Existing options cluster at two extremes: free tools that produce one giant malformed
XHTML file, and desktop suites (Calibre, Acrobat) that are powerful but opaque and
manual. Almost nothing offers a **measured quality signal** or a **fast correction
loop** in the browser. That is the wedge.

---

## 3. Goals and non-goals

### 3.1 Goals

- **G1** Convert digital-text PDFs into semantically structured, reflowable EPUB 3.3
  with chapters, heading hierarchy, TOC, and preserved page numbers.
- **G2** Produce output that passes `epubcheck` with zero errors and `ace` with zero
  critical accessibility issues.
- **G3** Report an honest, reproducible per-book quality score with actionable reasons.
- **G4** Handle the four most common real-world inputs: single-column text, two-column
  academic papers, scanned books, and mixed digital/scan documents.
- **G5** Keep the whole journey under 5 minutes for a 300-page book, including review.
- **G6** Keep user documents private: encrypted in transit and at rest, auto-deleted,
  never used for training without explicit opt-in.
- **G7** Ship a conversion engine that is testable in isolation and reusable behind an API.

### 3.2 Non-goals (explicit, for MVP)

- **No DRM circumvention.** Encrypted or DRM-protected files are refused with a clear
  message. This is a legal and ethical line, not a technical limitation.
- **No pixel-perfect layout preservation.** EPUB is reflowable; fidelity to page
  geometry is not the point.
- **No math OCR.** Equations fall back to cropped images with alt text in MVP.
- **No handwriting recognition.**
- **No PDF editing or annotation.**
- **No hosting or distribution** of converted books. We hand over a file.
- **No `.mobi`, `.azw3`, or `.kepub`** in MVP (see §17, open questions).

---

## 4. Success metrics

Measured on a versioned internal corpus (§12) plus beta telemetry.

| # | Metric | Target (MVP) | Stretch (v1.1) |
|---|---|---|---|
| M1 | Character recall vs. ground truth, digital PDFs | ≥ 99.0% | ≥ 99.7% |
| M2 | Word error rate after normalization, digital PDFs | ≤ 1.5% | ≤ 0.5% |
| M3 | Reading-order accuracy, single column | ≥ 97% | ≥ 99% |
| M4 | Reading-order accuracy, two column | ≥ 88% | ≥ 95% |
| M5 | `epubcheck` pass rate (zero errors) | 100% | 100% |
| M6 | `ace` critical-issue count | 0 | 0 |
| M7 | Conversion crash rate | ≤ 1% | ≤ 0.2% |
| M8 | p50 conversion time, 200-page digital PDF | ≤ 60 s | ≤ 30 s |
| M9 | OCR throughput, scanned page | ≤ 2.5 s/page | ≤ 1.2 s/page |
| M10 | Beta users rating output "readable without edits" | ≥ 80% | ≥ 90% |
| M11 | Uploads requiring retry after failure | ≤ 10% | ≤ 5% |
| M12 | Median time-to-first-successful-download, new user | ≤ 4 min | ≤ 2 min |

---

## 5. Scope

### 5.1 MVP (weeks 1–8)

- Drag-and-drop upload, single file, up to 100 MB / 1,000 pages
- Password prompt for encrypted PDFs (password held in memory only)
- Automatic classification: digital / scanned / mixed
- Layout analysis: column detection, reading order, header/footer stripping
- Text normalization: ligatures, hyphenation, whitespace, Unicode NFC
- Chapter detection from font statistics and TOC/bookmarks when present
- EPUB 3.3 assembly with nav doc, page list, metadata, cover extraction
- Validation gate (`epubcheck` + `ace`) before any file is released
- Quality score with per-issue explanations
- Progress UI with per-stage status
- Signed download link, files deleted after 24 hours
- Job history for signed-in users
- Admin: corpus runner, conversion diagnostics, queue health

### 5.2 v1.1 (weeks 9–14)

- Review editor: drag chapter boundaries, promote/demote headings, reassign pages
- Table reconstruction to HTML tables (line-detection first, ML fallback)
- Footnote linking (`noteref` → `footnote`)
- Batch upload (ZIP of PDFs) and API keys
- Optional LLM assist: heading inference, alt-text generation, running-header removal
- Kindle-oriented output profile, `.kepub` for Kobo
- Shareable read-only previews

### 5.3 Later

- Public REST API and webhooks
- Self-hostable Docker image / open-source engine
- Reference-manager export (BibTeX sidecar for papers)
- Multi-language OCR model selection
- Team accounts, SSO, retention policies

---

## 6. Conversion design (the core of this project)

The engine is a deterministic pipeline of small stages over an intermediate
document model (IDM). Every stage is individually testable, and every stage records
diagnostics that feed the quality score.

### 6.1 Pipeline

```
 PDF
  │
  ├─[0] Ingest & guard ......... size, page count, encryption, malware, MIME sniff
  │
  ├─[1] Classify ............... text coverage per page → digital | scanned | mixed
  │
  ├─[2] Extract ................ text + glyphs + fonts + images + drawings
  │                              (OCR path for pages below coverage threshold)
  │
  ├─[3] Layout analysis ....... blocks → columns → reading order
  │
  ├─[4] Structure ............. headings, paragraphs, lists, tables, footnotes,
  │                              page-break anchors, chapter boundaries
  │
  ├─[5] Normalize ............. ligatures, hyphenation, spaces, NFC, quotes,
  │                              header/footer/watermark removal
  │
  ├─[6] Assemble .............. IDM → XHTML + CSS + OPF + nav + images
  │
  ├─[7] Validate .............. epubcheck, ace, structural invariants
  │
  ├─[8] Score ................. metrics → fidelity tier + user-facing report
  │
  └─[9] Publish ............... ZIP to object storage, signed URL, TTL
```

**Stage 2 detail.** Digital pages come from the PDF text layer; if a page's
extractable characters per unit area fall below a threshold, that page is routed to
OCR and later merged. A mixed document keeps per-page provenance so we know which
pages are OCR-derived (relevant for both scoring and user trust).

**Stage 3 detail.** Glyphs are clustered into lines by baseline, lines into blocks by
proximity and alignment, blocks into columns by vertical gutter detection. Reading
order is assigned by a column-aware sort, then corrected using font-continuity and
sentence-completion heuristics across column breaks. This is where most tools fail and
where we will spend the most tuning effort.

**Stage 5 detail.** Normalization is a sequence of reversible, individually tested
transforms:

```
ﬁ → fi, ﬂ → fl, ﬀ → ff, ﬃ → ffi, ﬄ → ffl
"exam- ple" → "example"   (de-hyphenate, only across a line break,
                           only when the tail is not a real word prefix)
"C O N T E N T S" → "CONTENTS"  (character-spaced heading detection)
U+00A0 / U+202F / multiple spaces → single space
NFD → NFC, smart quotes preserved but normalized to typographic form
Repeated running headers/footers (≥ 60% of pages) → removed
Footnote superscripts → extracted into <aside epub:type="footnote">
```

### 6.2 Fidelity tiers

Every job is assigned a tier, shown to the user with a reason. This is the product's
core honesty mechanism.

| Tier | Name | Condition | Output |
|---|---|---|---|
| **T1** | Structured | High text coverage, clean layout, confident structure | Full semantic EPUB: chapters, headings, lists, linked footnotes, page list |
| **T2** | Reflowed | Good text, uncertain structure | Reflowable EPUB with conservative structure; headings marked as uncertain in the report |
| **T3** | Facsimile | Low text confidence, complex layout, or math/tables dominate | Page-image EPUB with a selectable, accessible text layer and page list |

The UI states the tier plainly: *"We produced a structured ebook. 2 chapters were
detected with low confidence — you may want to review the chapter breaks."* No silent
quality loss.

### 6.3 EPUB output contract

```
mimetype                     (application/epub+zip, first entry, stored, no extra field)
META-INF/
  container.xml
  encryption.xml            (only when a font must be obfuscated)
OEBPS/
  package.opf               metadata, manifest, spine
  nav.xhtml                 epub:type="toc" + "page-list" + "landmarks"
  toc.ncx                   EPUB 2 compatibility
  css/book.css
  images/cover.jpg
  images/fig-001.png
  text/chapter-001.xhtml
  text/chapter-002.xhtml
  ...
```

Guarantees we enforce before release:

- `mimetype` is the first ZIP entry, stored uncompressed, exactly `application/epub+zip`
- OPF declares `dc:identifier`, `dc:title`, `dc:language`, `dcterms:modified`
- Unique `dc:identifier` derived from content hash when the source has none
- Nav document contains `<nav epub:type="toc">`; page list emitted when the source has
  page numbers (a genuine advantage over most converters)
- Every XHTML file is well-formed XML with a correct doctype and `xml:lang`
- No empty spine entries, no orphan manifest items, no `file://` references
- Images have dimensions, sensible DPI, and alt text (generated or `alt=""` for decoration)
- Accessibility metadata emitted (`schema:accessMode`, `accessModeSufficient`,
  `accessibilityFeature`, `accessibilityHazard`, `accessibilitySummary`) and
  `dcterms:conformsTo` → EPUB Accessibility 1.1
- Total uncompressed size and per-file size limits respected

### 6.4 Quality score

A 0–100 score, a tier, and up to five ranked, human-readable issues:

```
91 · Structured
   ✓ Text recovered from all 312 pages
   ✓ Reading order verified on all pages
   ✓ 24 chapters detected, 23 high confidence
   ⚠ 3 tables converted to text
   ⚠ Running header on page 88 may have leaked into body text
```

The score is computed from measured signals (coverage, order confidence, structure
confidence, validation results), not from a model's self-report. It must be
reproducible: same input, same score.

---

## 7. Architecture

```
                      ┌──────────────────┐
     browser ────────▶│  Next.js (web)   │
                      │  uploader, jobs, │
                      │  reader preview  │
                      └────────┬─────────┘
                               │ JSON / SSE
                      ┌────────▼─────────┐
                      │    API  (Go)     │
                      │  auth, quotas,   │
                      │  presign, jobs   │
                      └───┬────┬────┬────┘
                          │    │    │
               ┌──────────┘    │    └──────────────┐
               │               │                   │
        ┌──────▼──────┐  ┌─────▼──────┐   ┌────────▼────────┐
        │  Postgres   │  │ River job  │   │  S3-compatible  │
        │  metadata   │  │ queue +    │   │  object store   │
        │  jobs       │  │ NOTIFY     │   │  uploads/out    │
        └──────┬──────┘  └─────┬──────┘   └────────┬────────┘
               └───────┬───────┘                   │
                       │                           │
                ┌──────▼───────────────────────────▼──────┐
                │    Worker pool (one Go binary, N pods)  │
                │  A: extract + layout   B: OCR           │
                │  C: assemble + validate                 │
                └──────┬────────┬──────────────┬──────────┘
                       │        │              │
             ┌─────────▼──┐ ┌───▼────────┐ ┌───▼─────────────┐
             │  pdfium    │ │ tesseract  │ │ epubcheck / ace │
             │ (sandbox)  │ │ (subproc)  │ │ (subprocess)    │
             └────────────┘ └────────────┘ └─────────────────┘
```

**Principles**

- **Stateless workers.** Any worker can pick up any job; state lives in Postgres +
  object storage. Horizontal scaling is trivial and crashes are survivable.
- **One binary, one language.** The API and the worker share the same Go module and
  the same engine packages; the worker is the same binary with a different subcommand.
  No RPC contract to keep in sync between the web tier and the converter.
- **Suspicious input never shares a process.** PDFium runs in a sandboxed host process
  and Tesseract/`epubcheck`/`ace` run as subprocesses, so a parser exploit or a
  pathological file does not touch the worker's memory space.
- **Separate queues per stage class.** OCR jobs must not starve fast text jobs.
- **Stream, don't buffer.** Files go browser → object store via presigned URL; the API
  never proxies large bodies.
- **One transaction per state change.** The job row and its queue entry are written
  together, so a crash can never leave a job that is queued but does not exist, or
  exists but is never picked up.
- **Deterministic engine version.** Every artifact records the engine version and the
  config hash, so a regression can be reproduced exactly.
- **Fail closed.** An artifact that fails validation is never downloadable; the user
  gets a diagnosable error instead of a broken book.

### 7.1 Request flow

```
1. POST /v1/uploads        → { uploadId, presigned PUT url }
2. PUT  <presigned url>    → file lands in object storage
3. POST /v1/jobs           → { jobId } (dedupe by content hash)
4. GET  /v1/jobs/{id}/events (SSE) → stage progress
5. GET  /v1/jobs/{id}      → { tier, score, issues, downloadUrl }
6. GET  /v1/download/{tok} → 302 to short-lived signed URL
```

### 7.2 Job state machine

```
queued ─▶ ingesting ─▶ analyzing ─▶ extracting ─▶ structuring
   │                                    │
   │                                    └─▶ ocr_queued ─▶ ocr_running
   ▼
failed        assembling ─▶ validating ─▶ scoring ─▶ ready
                  │             │
                  └─────────────┴──▶ failed_validation
needs_password ─▶ (resume on password submit)
```

Any terminal failure carries a machine-readable `errorCode` plus a user-facing
explanation and a suggested next action.

---

## 8. Data model

```sql
users            (id, email, plan, created_at, deleted_at)
documents        (id, user_id, filename, size_bytes, page_count,
                  content_sha256, pdf_class, encrypted, created_at)
jobs             (id, document_id, status, tier, score,
                  engine_version, config_hash, error_code,
                  queued_at, started_at, finished_at, attempts)
job_events       (id, job_id, stage, level, message, payload, created_at)
artifacts        (id, job_id, kind, storage_key, bytes, sha256, expires_at)
issues           (id, job_id, severity, code, message, page_ref)
usage_counters   (user_id, period, conversions, ocr_pages, llm_tokens)
```

Notes:

- `content_sha256` enables instant dedupe: the same file converted twice costs nothing.
- Passwords for encrypted PDFs are never stored — not in Postgres, not in logs, not in
  queue payloads. They are passed to the worker out-of-band and discarded after use.
- Artifacts carry `expires_at`; a sweeper enforces the retention policy.

---

## 9. API sketch

```http
POST /v1/uploads
{ "filename": "book.pdf", "sizeBytes": 4123987, "contentSha256": "9f2c…" }
→ 201 { "uploadId": "up_…", "url": "https://bucket/…", "expiresIn": 900 }
```

```http
POST /v1/jobs
{ "uploadId": "up_…", "options": { "profile": "generic", "ocr": "auto",
  "llmAssist": false, "pageRange": "1-320" } }
→ 202 { "jobId": "job_…", "status": "queued", "deduped": false }
```

```http
GET /v1/jobs/job_… 
→ 200 {
  "status": "ready",
  "tier": "T1",
  "score": 91,
  "issues": [
    { "severity": "warn", "code": "TABLE_FLATTENED",
      "message": "3 tables were converted to text", "page": 88 }
  ],
  "artifacts": [
    { "kind": "epub", "bytes": 2718344,
      "downloadUrl": "/v1/download/tok_…", "expiresAt": "2026-11-14T18:00:00Z" }
  ],
  "timing": { "extractMs": 8421, "ocrMs": 0, "assembleMs": 1290 }
}
```

```http
POST /v1/jobs/job_…/password   { "password": "…" }   → 202
GET  /v1/jobs/job_…/events                            → text/event-stream
DELETE /v1/jobs/job_…                                 → 204 (hard delete artifacts)
```

Errors are RFC 9457 problem+json with a stable `code`, a human `detail`, and a
`retryable` flag.

---

## 10. Tech stack

**Language decision: Go.** The API, the worker, and the conversion engine are one Go
module. TypeScript handles the browser. Nothing in the request path is interpreted at
runtime.

| Layer | Choice | Why |
|---|---|---|
| Web | Next.js (App Router) + TypeScript + Tailwind + shadcn/ui | Fast to build; good upload and streaming story; static hosting |
| API | Go 1.24, stdlib `net/http` (method+wildcard routing) + `chi` middleware | No framework needed; typed contracts; one binary |
| Engine | Go packages over an IDM: `internal/{extract,layout,struct,normalize,epub,score}` | Deterministic, testable stage-by-stage, same language as the API |
| PDF text + rendering | `klippa-app/go-pdfium` (pdfium, BSD-3-Clause) | Per-character boxes, font info, and page rasterisation; permissive licence |
| PDF structure, encryption, page ops | `pdfcpu/pdfcpu` (Apache-2.0) | Encryption detection, validation, page counts, embedded image extraction |
| Layout analysis | Own geometry over glyph boxes (`internal/layout`) | Columns and blocks are clustering over rectangles; no ML needed for MVP |
| OCR | Tesseract 5 as a subprocess, fanned out with `errgroup` | Crash isolation, no CGO in the worker, no per-page cost |
| XHTML DOM | `beevik/etree` | Real XML tree with escaping handled; no string concatenation |
| EPUB packaging | stdlib `archive/zip` with an explicit `Store` header | Full control: `mimetype` first, uncompressed, no extra field |
| DB | Postgres 17 + `pgx/v5` + `sqlc` + `goose` | Typed queries generated from SQL; migrations in version control; no ORM |
| Queue | `riverqueue/river` (Postgres-backed) | Enqueue lands in the same transaction as the job row; removes Redis entirely |
| Progress fan-out | Postgres `LISTEN/NOTIFY` → SSE | No pub/sub service to operate at this scale |
| Object storage | S3/R2 via `aws-sdk-go-v2`; MinIO locally | Presigned uploads, lifecycle expiry |
| Auth | `alexedwards/scs` sessions + `golang.org/x/oauth2` | Server-side sessions, revocable, no JWT invalidation problem |
| Logging / metrics | stdlib `log/slog` + OpenTelemetry + Prometheus | Structured logs with job IDs only, never document text |
| Validation | `epubcheck` (Java) + `@daisy/ace` (Node), subprocess | Reference implementations; do not rewrite a validator |
| Tests | stdlib `testing`, native fuzzing for the normalization transforms, golden files | Fuzzing is built into the toolchain |
| Deploy | Multi-stage Docker build → ~30 MB image; Compose on one VM | Fast cold start, low memory per worker, trivial rollback |

### 10.1 Why Go is viable for the engine

This deserves justification, because most PDF tooling in this problem space is Python.

- **PDFium gives us what the heuristics actually need.** Per-character bounding boxes,
  font name/size/weight per span, and page rasterisation. Column detection, line
  clustering, heading inference and reading order are all geometry and string work.
- **The work is CPU-bound and needs fan-out.** Pages, OCR tiles and image encoding are
  embarrassingly parallel; `errgroup` + a weighted semaphore + `context` deadlines is
  the natural shape of the pipeline, and `pprof` makes tuning cheap.
- **Operational weight matters for a conversion service.** One static binary, a ~30 MB
  image, and tens of megabytes of RSS per worker, versus a 1–2 GB Python image with
  PyTorch and a 10-second cold start.
- **The polyglot cost already exists.** `epubcheck` is Java, `ace` is Node, Tesseract is
  native. Containing all three behind subprocess calls is the same work in any language.
- **Compile-time contracts.** The IDM is the interface between seven pipeline stages;
  changing a struct without updating the stages is a build failure, not a 3 a.m. runtime
  surprise.

### 10.2 What we give up

| Loss | Impact | Mitigation |
|---|---|---|
| No Docling/marker equivalent in Go | Hard layouts (magazines, dense tables) score worse than an ML pipeline | Heuristics beat generic models on clean digital PDFs; IDM is serialisable so an ML stage can be added out-of-process (below) |
| No `pdfplumber`-grade table detection | Tables flatten to text in MVP | Tables are already v1.1 scope; emit a `TABLE_FLATTENED` issue so the score is honest |
| Thinner PDF test tooling | Fewer off-the-shelf fixtures | We were building our own corpus and harness anyway (§12) |
| Smaller talent pool for Go+PDF | Slower ramp for a collaborator | The engine is plain Go with no esoteric dependencies |

**The escape hatch.** `layout-ml` is designed as an optional out-of-process stage: a
Python sidecar running Docling, speaking gRPC/HTTP, feature-flagged, invoked only for
books landing on the T2/T3 boundary. The IDM crosses the boundary as JSON. We build it
only if the week-3 corpus run shows heuristics falling short, and we measure it before
believing it. If heuristics hold at ≥ 95% on single-column, the sidecar never ships.

### 10.3 Libraries considered and rejected

| Option | Why not |
|---|---|
| **`unipdf` (UniDoc)** | The most capable pure-Go PDF library, but AGPL-3.0 without a paid licence — a network service using it must publish its source. |
| **`go-fitz` (MuPDF bindings)** | MuPDF is AGPL-3.0. Same problem, plus CGO. |
| **`ledongthuc/pdf`** | Extracts text from simple files, but no font information, no glyph boxes, no rendering — cannot support layout analysis. |
| **`gosseract`** | Works, but CGO Tesseract in-process means an OCR crash takes the worker down with it. Subprocess gives the same result with isolation. |
| **`encoding/xml` marshalling for XHTML** | `xmlns:epub` and mixed-content handling get awkward; `etree` builds the tree explicitly. |
| **`gorm` / `ent`** | The schema is six tables. `sqlc` generates typed code from real SQL with no runtime reflection. |
| **Calibre `ebook-convert`** | Black box, GPL-3, no per-stage diagnostics. Kept as a corpus baseline for comparison, never as the engine. |
| **Redis + asynq** | Good queue, but it is a second stateful service and a second source of truth. River keeps enqueue transactional. Revisit only if Postgres queue contention becomes real. |
| **Pandoc** | Great for structured sources, but no PDF layout analysis. |

> **Licensing is a first-class constraint.** This product is a network service; any
> AGPL dependency in the request path obliges us to publish the corresponding source.
> That rules out the two best Go PDF libraries. The dependency allowlist (MIT/BSD/
> Apache-2.0, with copyleft permitted only across a process boundary) is enforced in
> CI from week 1.

---

## 11. Security, privacy, and legal

### 11.1 Security

- Presigned upload/download URLs, short TTL, scoped to one object
- MIME sniffing and structural PDF validation before parsing; reject polyglots
- PDFium parses in a separate host process; every external tool runs as an
  unprivileged subprocess with a CPU/memory/time budget and no network
- ClamAV scan on ingest
- OWASP ASVS L2 for the web app; strict CSP; no inline scripts
- Zip-bomb and recursion limits on PDF object graphs and image streams
- Per-IP and per-account rate limits; conversion quotas by plan

### 11.2 Privacy

- Encryption in transit (TLS 1.3) and at rest (bucket + volume encryption)
- Files auto-deleted 24 hours after completion (configurable, shorter by default for
  anonymous users); hard delete with partial-artifact cleanup on failure
- No human review of user documents by default
- No use of user content for model training or corpus building without explicit,
  revocable opt-in
- Logs never contain document text, only IDs, sizes, timings, and error codes
- Documented retention policy and a one-click "delete everything" account action
- GDPR: data export and erasure paths; clear data-processing statement

### 11.3 Legal and ethical boundaries

- **DRM is out of scope.** Encrypted PDFs are supported only with a user-supplied
  password for files the user can already open. Circumvention is never implemented.
- **Copyright.** The tool converts files the user supplies; we do not host, index, or
  share books. Terms of service make the user responsible for their right to convert.
- **Licence hygiene.** A dependency allowlist (MIT/BSD/Apache-2.0) is enforced in CI.
  Copyleft dependencies are permitted only where they run as separate processes.
- **Accessibility is a feature, not a checkbox.** Valid structure and alt text are
  part of the output contract because the alternative excludes readers.

---

## 12. Quality strategy

### 12.1 Golden corpus

A versioned set of ~80 PDFs with hand-verified expectations. It is the single most
valuable asset in this project and gets built from week 1, not week 6.

| Bucket | Count | Examples | What it catches |
|---|---|---|---|
| Clean single-column novels | 15 | Public-domain fiction, Project Gutenberg print editions | Baseline text quality, chapter detection |
| Two-column papers | 15 | arXiv preprints, journal articles | Reading order, footnotes, figures |
| Technical books | 10 | Programming books with code blocks, tables | Monospace, tables, callouts |
| Scanned books | 10 | Old scans with OCR already present or absent | OCR path, skew, noise |
| Mixed / adversarial | 10 | Cover pages, blank pages, rotated pages, watermarks | Robustness |
| Malformed | 10 | Fonts without `ToUnicode`, encrypted, huge pages | Graceful failure |
| Other languages | 10 | Sinhala, Tamil, German, Arabic | Unicode, RTL, hyphenation |

Ground truth is stored as normalized plain text plus structural annotations (chapter
boundaries, heading levels, table locations), so metrics are computed automatically.

### 12.2 Metrics and gates

- **Character recall** and **word error rate** against ground truth text
- **Reading-order accuracy** via longest-common-subsequence against annotated order
- **Structure F1** for chapter and heading detection
- **Validation**: `epubcheck` error count, `ace` violation count
- **Regression gate**: a PR that drops any metric by more than 0.5% on the corpus fails CI
- **Baseline comparison**: every report includes Calibre and a naive extractor for context

### 12.3 Manual review protocol

Each corpus run produces a browsable HTML report: page image beside extracted text,
with diffs and highlighted issues. A 10-book stratified sample is read end-to-end on a
real e-reader each release. Automated metrics cannot catch "technically valid but
miserable to read," so this step is mandatory.

---

## 13. Non-functional requirements

| Area | Requirement |
|---|---|
| Performance | 200-page digital PDF ≤ 60 s p50; onboarding (upload → first byte) ≤ 5 s |
| Capacity | 50 concurrent conversions on 4 vCPU / 8 GB workers; queue absorbs bursts |
| Availability | 99.5% API uptime; jobs survive worker restarts |
| Limits | 100 MB, 1,000 pages, 400 MB uncompressed output per job |
| Accessibility (our site) | WCAG 2.2 AA; keyboard-complete; announcements for job progress |
| Compatibility | Chrome/Edge/Firefox/Safari last 2 versions; mobile upload from iOS/Android |
| Output compatibility | Kindle (via Send to Kindle), Apple Books, Kobo, Google Play Books, Calibre, Thorium |
| Internationalization | Unicode-correct, RTL-safe XHTML, language tag from PDF metadata with user override |
| Observability | Structured logs, per-stage latency histograms, queue depth, failure-code dashboard |

---

## 14. Cost model (indicative)

Per 300-page conversion, single-tenant VM.

| Item | Assumption | Cost per 1,000 conversions |
|---|---|---|
| CPU conversion (digital) | ~45 s on 2 vCPU | ≈ $4 |
| OCR (scanned, 300 pp) | ~2 s/page on 4 vCPU | ≈ $40 |
| Storage (24 h TTL, 3 MB avg) | negligible | < $1 |
| Egress (3 MB files) | | ≈ $3 |
| Validation (epubcheck/ace) | ~4 s CPU | ≈ $1 |
| LLM assist (opt-in only) | ~$0.05/book | ≈ $50 |
| **Total (digital, no LLM)** | | **≈ $9 / 1,000** |
| **Total (scanned)** | | **≈ $45 / 1,000** |

Implication: digital conversions are cheap enough for a generous free tier; OCR and
LLM assist are the features that need metering.

---

## 15. Risks

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | Layout analysis underperforms on real-world messy PDFs | High | High | Ship tiered output (T1/T2/T3) instead of over-promising; corpus-driven tuning; review editor |
| R2 | AGPL/GPL dependency contaminates a network service | Medium | High | Licence allowlist in CI (rules out `unipdf` and `go-fitz`); copyleft tools only across a process boundary |
| R3 | OCR cost or latency blows the unit economics | Medium | Medium | Per-plan page quotas; pre-flight page-count estimate shown before conversion |
| R4 | Parser crashes or resource exhaustion on hostile PDFs | Medium | High | PDFium in its own host process; container limits; CPU/time budgets; fuzzing; fail-closed validation |
| R5 | Copyright / DRM misuse by users | Medium | High | Explicit refusal of DRM files, ToS, abuse reporting, no hosting or sharing |
| R6 | Scope creep into editing, hosting, formats | High | Medium | Written non-goals; MVP checklist is the release gate |
| R7 | Single-developer bus factor | Medium | Medium | Documented pipeline, corpus + CI as the contract, no tribal knowledge |
| R8 | Output "valid but unpleasant" | Medium | High | Mandatory human read-through each release; beta feedback loop |
| R9 | Table and math fidelity disappoint | High | Low | Explicit limitation messaging; image fallback with alt text |
| R10 | Upload abuse / storage exhaustion | Low | Medium | Quotas, rate limits, TTL sweeper, per-account caps |
| R11 | No ML layout model in the Go ecosystem | Medium | Medium | Heuristics-first, measured on the corpus at W3; `layout-ml` sidecar exists as a pre-designed escape hatch (§10.2) |

---

## 16. Timeline

Single developer, part-time (≈25 h/week). Eight weeks to public beta.

| Week | Milestone | Deliverables | Exit criteria |
|---|---|---|---|
| **W1** | Foundations | Go module layout, CI (build/vet/test/race/fuzz), Compose, corpus v0 (20 PDFs), IDM spec, metric harness, licence allowlist | `go run ./cmd/reflow convert` emits a technically valid EPUB end-to-end (any quality) |
| **W2** | Extract & classify | Ingest guard, classification, pdfium text/glyph/font extraction, coverage metrics | M1 measurable on corpus; encrypted files fail cleanly |
| **W3** | Layout | Line/block/column detection, reading order, header/footer removal; corpus decides on `layout-ml` sidecar | M3 met on single column; two-column prototype works |
| **W4** | Structure & normalization | Headings, paragraphs, chapter splitting, ligature/hyphen/Unicode pipeline, page anchors | Structure F1 baseline recorded; TOC generated for clean books |
| **W5** | Assembly & validation | EPUB writer (`archive/zip` + `etree`), OPF/nav/page-list, CSS, cover, `epubcheck` + `ace` in CI | 100% validation pass on corpus; accessibility metadata present |
| **W6** | Web app | Go API + Next.js upload, River jobs, SSE progress, download, auth, retention sweeper | A stranger can convert a book unaided |
| **W7** | OCR & scoring | Tesseract subprocess pool, mixed-document merge, quality score, tier logic, issue copy | Scanned book converts to T3 with accessible text layer |
| **W8** | Hardening & beta | Rate limits, quotas, dashboards, error copy, docs, beta deploy | M5–M12 green; 10 external testers onboarded |

**v1.1 window (weeks 9–14):** review editor, tables, footnote linking, batch/API,
optional LLM assist, Kobo/Kindle profiles.

### 16.1 Milestone framing

Each milestone is defined by a demo, not by a task list: *"by Friday I can show a
two-column paper converted with correct reading order and a TOC."* If the demo cannot
be shown, scope is cut, not the date — with the cut recorded in this document.

---

## 17. Open questions (decisions needed before W1 ends)

1. **Business model.** Free anonymous tier + paid OCR/page packs? Subscription? Or a
   free public good with donations? This changes the quota and retention design.
2. **Language priority.** English-first, or do we support Sinhala/Tamil from day one?
   The corpus and hyphenation rules change significantly.
3. **LLM assist default.** Off by default (privacy, cost, determinism) is my
   recommendation. Is a "best quality" toggle acceptable if it sends excerpts to a
   third-party model?
4. **Open-source strategy.** Publish the engine as a library from day one, or keep the
   differentiator closed? Affects architecture (clean public interface) and community.
5. **Extra formats.** `.kepub`, `.azw3`, and `.docx` output — in scope for v1.1 or later?
6. **Hosting region and vendor.** Single EU region? Which object store — R2, S3, B2?
7. **Branding.** "Reflow" is a placeholder; the repo name suggests a family of small
   tools, which may argue for a shared shell and a distinct sub-brand.

---

## 18. Definition of done (MVP)

- [ ] A user with no account converts a 300-page digital PDF and downloads a
      structurally correct EPUB in under 3 minutes
- [ ] A scanned book converts to a T3 EPUB that is navigable and screen-reader usable
- [ ] 100% of the golden corpus passes `epubcheck` with zero errors
- [ ] `ace` reports zero critical issues on all T1 and T3 outputs
- [ ] Quality score is reproducible and matches the corpus report
- [ ] Files are deleted on schedule; the sweeper is verified by an automated test
- [ ] DRM and password-protected files are handled with clear, correct messaging
- [ ] Errors are diagnosable from a job ID alone (no document content in logs)
- [ ] The engine runs outside the web app:
      `engine.Convert(ctx, file, out, opts) (Artifact, Report, error)`

### 18.1 Engine entry point

The whole pipeline must be reachable from one call, with no HTTP or database in the
dependency graph. This is what keeps the engine testable and extractable as a library
or a CLI (`reflow convert book.pdf -o book.epub`).

```go
type Options struct {
    Profile   Profile   // generic | kindle | kobo
    OCR       OCRMode   // auto | force | off
    PageRange [2]int    // optional 1-based inclusive range
    LLMAssist bool
}

// Convert streams a PDF through the pipeline and writes an EPUB.
// It returns a machine-readable Report even when err != nil, so callers can
// surface partial diagnostics.
func Convert(ctx context.Context, src io.ReaderAt, dst io.Writer,
    opts Options) (Report, error)
```
- [ ] Retention, privacy, and licence statements are published
- [ ] 10 external beta users complete the flow unaided

---

## Appendix A — Intermediate Document Model (sketch)

```go
package idm

type Document struct {
	Meta      Metadata  // title, author, language, source hash
	Pages     []Page    // geometry, provenance: digital | ocr
	Blocks    []Block   // ordered, column-tagged
	Structure []Node    // chapters → headings → paragraphs
}

type BlockKind uint8

const (
	Para BlockKind = iota
	Heading
	List
	Table
	Figure
	Caption
	Code
	Footnote
	Header
	Footer
)

type Block struct {
	Kind       BlockKind
	Text       string
	Level      int      // heading level; 0 when not a heading
	Page       int      // 1-based source page
	BBox       Rect     // points, PDF user space
	Confidence float64  // 0..1, feeds the quality score
	Column     int      // 0-based column index on the page
	Spans      []Span   // style runs: bold, italic, mono, superscript, link
}

type Rect struct{ X0, Y0, X1, Y1 float64 }
```

The IDM is JSON-serialisable on purpose: the optional `layout-ml` sidecar (§10.2) can
receive and return it without a shared library or a shared language.

Every block keeps `Confidence` and its source page, so the report can point at a real
page and the review editor can highlight the exact region.

## Appendix B — Example XHTML output

```xhtml
<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml"
      xmlns:epub="http://www.idpf.org/2007/ops"
      xml:lang="en" lang="en">
<head>
  <meta charset="utf-8"/>
  <title>Chapter 3 — The Long Road</title>
  <link rel="stylesheet" type="text/css" href="../css/book.css"/>
</head>
<body epub:type="bodymatter">
  <section epub:type="chapter" role="doc-chapter" id="ch03">
    <h1>Chapter 3 — The Long Road</h1>
    <p><span id="page-042" role="doc-pagebreak" epub:type="pagebreak"
             aria-label="42"></span>The road bent north...</p>
    <p>...and the efficient engine<a epub:type="noteref" role="doc-noteref"
       href="#fn-3-1" id="fnref-3-1">1</a> never stalled.</p>
    <aside epub:type="footnote" role="doc-footnote" id="fn-3-1">
      <p><a href="#fnref-3-1">1.</a> Rebuilt in the spring of 1892.</p>
    </aside>
  </section>
</body>
</html>
```

## Appendix C — Glossary

| Term | Meaning |
|---|---|
| **IDM** | Intermediate Document Model — our structured representation between PDF and EPUB |
| **Fidelity tier** | T1 structured / T2 reflowed / T3 facsimile, describing how much structure we trust |
| **Reflowable** | Content reflows to the reader's screen size and font settings (the opposite of fixed layout) |
| **Page list** | EPUB navigation listing print page numbers; lets a reader jump to "page 42" |
| **`epubcheck`** | The reference EPUB validator (W3C/DAISY) |
| **`ace`** | Accessibility checker for EPUB by DAISY |
| **`ToUnicode` CMap** | The table that maps PDF glyph codes to real Unicode characters; when missing, text extraction breaks |
