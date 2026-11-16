// Package epub writes EPUB 3.3 books.
//
// The writer takes fully rendered chapter fragments and produces a ZIP archive
// that satisfies the structural contracts the rest of the system depends on:
// an OCF container, a package document, a navigation document, an NCX for
// older reading systems, accessibility metadata, and a mimetype entry that is
// first, stored, and carries no extra field.
//
// It knows nothing about PDF. Everything above this package hands it content
// that is already in XHTML.
package epub

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// Media types used by the package document.
const (
	mediaTypeEPUB  = "application/epub+zip"
	mediaTypeXHTML = "application/xhtml+xml"
	mediaTypeNCX   = "application/x-dtbncx+xml"
	mediaTypeCSS   = "text/css"
	mediaTypeOPF   = "application/oebps-package+xml"
)

// Layout constants inside the archive. Paths in the package document are
// relative to the package document itself, which is why they omit "OEBPS/".
const (
	mimetypeName  = "mimetype"
	containerName = "META-INF/container.xml"
	rootDir       = "OEBPS"
	opfName       = "package.opf"
	navName       = "nav.xhtml"
	ncxName       = "toc.ncx"
	cssName       = "css/book.css"
)

// ErrInvalidBook is wrapped by every error that means the caller handed the
// writer a book it cannot represent.
var ErrInvalidBook = errors.New("invalid book")

// DefaultStylesheet is used when Book.Stylesheet is empty. It is deliberately
// small: reading systems supply their own typography, and over-styling makes
// user font and spacing settings harder to honour.
const DefaultStylesheet = `body {
  margin: 0 5%;
  line-height: 1.5;
}
h1, h2, h3, h4, h5, h6 {
  line-height: 1.25;
  margin: 1.5em 0 0.5em;
  page-break-after: avoid;
}
p {
  margin: 0 0 1em;
  text-align: justify;
  hyphens: auto;
}
blockquote {
  margin: 1em 2em;
}
pre, code {
  font-family: monospace;
  white-space: pre-wrap;
}
pre {
  margin: 1em 0;
  padding: 0.5em;
  border-left: 2px solid currentColor;
}
img {
  max-width: 100%;
  height: auto;
}
figure {
  margin: 1em 0;
  text-align: center;
}
figcaption {
  font-style: italic;
  font-size: 0.9em;
}
.cover {
  margin: 0;
  padding: 0;
  text-align: center;
}
.cover img {
  max-height: 100vh;
}
`

// Book is everything the writer needs. The zero value is invalid: a book needs
// a title, a language, and at least one chapter.
type Book struct {
	Metadata Metadata
	Cover    *Cover
	Chapters []Chapter
	PageList []PageTarget
	// Images are figures referenced from chapter bodies as "../images/<Name>".
	Images     []Image
	Stylesheet string
}

// Image is one figure file in the archive.
type Image struct {
	// Name is the file name inside images/, for example "fig-001.jpg".
	Name      string
	Data      []byte
	MediaType string
}

// Metadata becomes the package document's metadata section.
type Metadata struct {
	Title     string
	Authors   []string
	Language  string
	Publisher string
	// Identifier is the book's unique identifier, normally a URN. When empty
	// the writer derives a stable one from the content, so the same input
	// always produces the same EPUB.
	Identifier string
	// Modified is the dcterms:modified timestamp. When zero the writer uses
	// the current time; callers that need reproducible output must set it.
	Modified time.Time
}

// Cover is an optional cover image.
type Cover struct {
	Data []byte
	// MediaType is required for reliable output. When empty the writer sniffs
	// the bytes, which does not recognise every format.
	MediaType string
}

// Chapter is one content document.
type Chapter struct {
	// ID is the anchor identifier for the chapter section. It must be a valid
	// XML name and unique within the book. When empty it is derived from the
	// chapter's position.
	ID string
	// Title is the navigation label. When empty it becomes "Chapter N", so a
	// table of contents never has a blank entry.
	Title string
	// Body is an XHTML fragment — the content of the chapter, not a whole
	// document. The writer wraps it, links the stylesheet, and rejects it if
	// it is not well-formed XML.
	Body string
}

// PageTarget maps a printed page label to a location in the book. This is what
// makes "go to page 42" work in a reflowed EPUB.
type PageTarget struct {
	Label string
	Href  string
}

// Write renders the book and writes a complete EPUB archive to dst.
func Write(dst io.Writer, book Book) error {
	b, err := prepare(book)
	if err != nil {
		return err
	}

	files, err := b.files()
	if err != nil {
		return err
	}

	return writeArchive(dst, files, b.modified)
}

// WriteBytes is Write for callers that want the archive in memory.
func WriteBytes(book Book) ([]byte, error) {
	var buf strings.Builder
	if err := Write(&buf, book); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

type prepared struct {
	images     []Image
	meta       Metadata
	identifier string
	modified   time.Time
	chapters   []chapter
	pages      []PageTarget
	cover      *coverAsset
	css        string
}

type chapter struct {
	id       string
	title    string
	filename string // relative to the package document
	body     string
}

type coverAsset struct {
	data      []byte
	mediaType string
	filename  string // relative to the package document
}

// xmlName matches the identifiers the writer will accept as anchor values.
var xmlName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func prepare(book Book) (*prepared, error) {
	if strings.TrimSpace(book.Metadata.Title) == "" {
		return nil, fmt.Errorf("%w: metadata.title is required", ErrInvalidBook)
	}
	if strings.TrimSpace(book.Metadata.Language) == "" {
		return nil, fmt.Errorf("%w: metadata.language is required", ErrInvalidBook)
	}
	if len(book.Chapters) == 0 {
		return nil, fmt.Errorf("%w: at least one chapter is required", ErrInvalidBook)
	}

	b := &prepared{
		meta:     book.Metadata,
		pages:    book.PageList,
		css:      book.Stylesheet,
		modified: book.Metadata.Modified.UTC(),
	}
	if b.css == "" {
		b.css = DefaultStylesheet
	}
	if b.modified.IsZero() {
		b.modified = time.Now().UTC()
	}

	seenID := make(map[string]bool, len(book.Chapters))
	for i, ch := range book.Chapters {
		id := ch.ID
		if id == "" {
			id = fmt.Sprintf("ch%03d", i+1)
		}
		if !xmlName.MatchString(id) {
			return nil, fmt.Errorf("%w: chapter %d has invalid id %q", ErrInvalidBook, i+1, id)
		}
		if seenID[id] {
			return nil, fmt.Errorf("%w: duplicate chapter id %q", ErrInvalidBook, id)
		}
		seenID[id] = true

		title := strings.TrimSpace(ch.Title)
		if title == "" {
			title = fmt.Sprintf("Chapter %d", i+1)
		}

		b.chapters = append(b.chapters, chapter{
			id:       id,
			title:    title,
			filename: fmt.Sprintf("text/chapter-%03d.xhtml", i+1),
			body:     ch.Body,
		})
	}

	for i, pg := range b.pages {
		if strings.TrimSpace(pg.Label) == "" || strings.TrimSpace(pg.Href) == "" {
			return nil, fmt.Errorf("%w: pageList[%d] needs both a label and an href", ErrInvalidBook, i)
		}
	}

	if book.Cover != nil {
		if len(book.Cover.Data) == 0 {
			return nil, fmt.Errorf("%w: cover has no data", ErrInvalidBook)
		}
		mediaType := book.Cover.MediaType
		if mediaType == "" {
			mediaType = http.DetectContentType(book.Cover.Data)
		}
		ext, ok := coverExtensions[mediaType]
		if !ok {
			return nil, fmt.Errorf("%w: unsupported cover media type %q", ErrInvalidBook, mediaType)
		}
		b.cover = &coverAsset{
			data:      book.Cover.Data,
			mediaType: mediaType,
			filename:  path.Join("images", "cover"+ext),
		}
	}

	seenImage := make(map[string]bool, len(book.Images))
	for i, img := range book.Images {
		if img.Name == "" || len(img.Data) == 0 {
			return nil, fmt.Errorf("%w: images[%d] needs a name and data", ErrInvalidBook, i)
		}
		if strings.ContainsAny(img.Name, `/\\`) {
			return nil, fmt.Errorf("%w: images[%d] name %q must be a file name", ErrInvalidBook, i, img.Name)
		}
		if _, ok := coverExtensions[img.MediaType]; !ok {
			return nil, fmt.Errorf("%w: images[%d] has unsupported media type %q", ErrInvalidBook, i, img.MediaType)
		}
		if seenImage[img.Name] {
			return nil, fmt.Errorf("%w: duplicate image name %q", ErrInvalidBook, img.Name)
		}
		seenImage[img.Name] = true
	}
	b.images = book.Images

	b.identifier = strings.TrimSpace(book.Metadata.Identifier)
	if b.identifier == "" {
		b.identifier = deriveIdentifier(book)
	}

	return b, nil
}

var coverExtensions = map[string]string{
	"image/jpeg":    ".jpg",
	"image/png":     ".png",
	"image/gif":     ".gif",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
}

// deriveIdentifier hashes the book's content so that converting the same file
// twice produces the same identifier. That matters for dedupe and for reading
// systems that use the identifier to detect duplicates.
func deriveIdentifier(book Book) string {
	h := sha256.New()
	for _, s := range []string{book.Metadata.Title, book.Metadata.Language} {
		io.WriteString(h, s)
		h.Write([]byte{0})
	}
	for _, author := range book.Metadata.Authors {
		io.WriteString(h, author)
		h.Write([]byte{0})
	}
	for _, ch := range book.Chapters {
		for _, s := range []string{ch.ID, ch.Title, ch.Body} {
			io.WriteString(h, s)
			h.Write([]byte{0})
		}
	}
	return "urn:sha256:" + hex.EncodeToString(h.Sum(nil))
}

// files renders every archive member except the mimetype entry, in the order
// they are written.
func (b *prepared) files() ([]file, error) {
	var files []file

	container, err := b.containerDocument()
	if err != nil {
		return nil, err
	}
	files = append(files, file{name: containerName, data: container})

	opf, err := b.packageDocument()
	if err != nil {
		return nil, err
	}
	files = append(files, file{name: path.Join(rootDir, opfName), data: opf})

	nav, err := b.navDocument()
	if err != nil {
		return nil, err
	}
	files = append(files, file{name: path.Join(rootDir, navName), data: nav})

	ncx, err := b.ncxDocument()
	if err != nil {
		return nil, err
	}
	files = append(files, file{name: path.Join(rootDir, ncxName), data: ncx})

	files = append(files, file{name: path.Join(rootDir, cssName), data: []byte(b.css)})

	for _, img := range b.images {
		files = append(files, file{name: path.Join(rootDir, "images", img.Name), data: img.Data})
	}

	if b.cover != nil {
		files = append(files, file{name: path.Join(rootDir, b.cover.filename), data: b.cover.data})

		coverPage, err := b.coverDocument()
		if err != nil {
			return nil, err
		}
		files = append(files, file{name: path.Join(rootDir, "text", "cover.xhtml"), data: coverPage})
	}

	for _, ch := range b.chapters {
		doc, err := b.chapterDocument(ch)
		if err != nil {
			return nil, err
		}
		files = append(files, file{name: path.Join(rootDir, ch.filename), data: doc})
	}

	return files, nil
}
