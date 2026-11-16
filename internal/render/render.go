// Package render turns the intermediate document model into an EPUB book.
//
// It decides how paragraphs become chapters, writes the XHTML fragments the
// EPUB writer packages, and keeps the page anchors that make a print page list
// work in a reflowed book. It does not know how the text was extracted.
package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/epub"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// ErrNoText means the document has no extractable text: usually a scanned PDF
// that has not been through OCR yet.
var ErrNoText = errors.New("no extractable text")

// Options configures rendering.
type Options struct {
	// DefaultTitle is used when the document has no title of its own.
	DefaultTitle string
	// Now fixes the modification timestamp so output is reproducible. Zero
	// means "use the writer's default".
	Now time.Time
}

// Render produces the book the EPUB writer packages.
func Render(doc idm.Document, opts Options) (epub.Book, error) {
	paras := GroupParagraphs(doc.Body())
	if len(paras) == 0 {
		return epub.Book{}, ErrNoText
	}

	title := titleOf(doc, opts)
	chapters := buildChapters(paras, title, doc.Outline)

	book := epub.Book{
		Metadata: epub.Metadata{
			Title:     title,
			Authors:   doc.Meta.Authors,
			Language:  languageOf(doc),
			Publisher: doc.Meta.Publisher,
			Modified:  opts.Now,
		},
	}

	assets := &assets{}
	for i, ch := range chapters {
		body, pages := chapterXHTML(ch, assets)

		book.Chapters = append(book.Chapters, epub.Chapter{
			Title: ch.title,
			Body:  body,
		})

		// The writer names chapter files by position, so the page list can
		// compute the same path here.
		filename := fmt.Sprintf("text/chapter-%03d.xhtml", i+1)
		for _, page := range pages {
			book.PageList = append(book.PageList, epub.PageTarget{
				Label: strconv.Itoa(page),
				Href:  filename + "#page-" + strconv.Itoa(page),
			})
		}
	}

	book.Images = assets.images
	return book, nil
}

// assets names and deduplicates figures. The same logo or background often
// appears on every page; embedding it once keeps the archive small.
type assets struct {
	names  map[string]string
	images []epub.Image
}

func (a *assets) add(img *idm.Image) string {
	if a.names == nil {
		a.names = map[string]string{}
	}

	hash := sha256.Sum256(img.Data)
	key := hex.EncodeToString(hash[:])
	if name, ok := a.names[key]; ok {
		return name
	}

	name := fmt.Sprintf("fig-%03d%s", len(a.images)+1, imageExtension(img.MediaType))
	a.names[key] = name
	a.images = append(a.images, epub.Image{Name: name, Data: img.Data, MediaType: img.MediaType})
	return name
}

func imageExtension(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".jpg"
	}
}

type chapter struct {
	title     string
	startPage int
	paras     []Paragraph
}

// buildChapters splits at the highest heading level present, falling back to a
// single chapter when the document has no detectable heading structure.
//
// The fallback is one chapter rather than one per page on purpose: a table of
// contents listing "Page 1 … Page 300" is worse than no chapters at all, and
// the page list already provides page-level navigation.
func buildChapters(paras []Paragraph, title string, outline []idm.OutlineEntry) []chapter {
	// The document's own table of contents beats anything inferred from font
	// sizes, so it is used whenever it describes more than a single section.
	if fromOutline := chaptersFromOutline(paras, outline); len(fromOutline) > 0 {
		return fromOutline
	}

	if !hasHeadings(paras) {
		return []chapter{{title: title, paras: paras}}
	}

	top := 6
	for _, p := range paras {
		if p.Kind == idm.KindHeading && p.Level < top {
			top = p.Level
		}
	}

	var chapters []chapter
	for _, p := range paras {
		if p.Kind == idm.KindHeading && p.Level == top {
			chapters = append(chapters, chapter{title: p.Text})
		}
		if len(chapters) == 0 {
			chapters = append(chapters, chapter{title: "Front matter"})
		}
		chapters[len(chapters)-1].paras = append(chapters[len(chapters)-1].paras, p)
	}

	return chapters
}

// chaptersFromOutline splits the document at the top level of its bookmarks.
// It returns nil when the outline does not describe a structure worth using,
// so the size-based fallback can take over.
func chaptersFromOutline(paras []Paragraph, outline []idm.OutlineEntry) []chapter {
	starts := outlineStarts(outline)
	if len(starts) < 2 {
		return nil
	}

	chapters := make([]chapter, len(starts))
	for i, s := range starts {
		chapters[i] = chapter{title: s.Title, startPage: s.Page}
	}

	var frontMatter []Paragraph
	for _, p := range paras {
		index := -1
		for i := len(chapters) - 1; i >= 0; i-- {
			if p.Page >= chapters[i].startPage {
				index = i
				break
			}
		}
		if index < 0 {
			frontMatter = append(frontMatter, p)
			continue
		}
		chapters[index].paras = append(chapters[index].paras, p)
	}

	var out []chapter
	if len(frontMatter) > 0 {
		out = append(out, chapter{title: "Front matter", paras: frontMatter})
	}
	for _, c := range chapters {
		// A bookmark can point at a page with nothing on it; an empty chapter
		// in the table of contents is worse than a missing one.
		if len(c.paras) > 0 {
			out = append(out, c)
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// outlineStarts returns the top-level entries that point at a page, in page
// order, dropping duplicates.
func outlineStarts(outline []idm.OutlineEntry) []idm.OutlineEntry {
	top := 0
	for _, e := range outline {
		if e.Page > 0 && (top == 0 || e.Level < top) {
			top = e.Level
		}
	}
	if top == 0 {
		return nil
	}

	var starts []idm.OutlineEntry
	seen := map[int]bool{}
	for _, e := range outline {
		if e.Level != top || e.Page <= 0 || seen[e.Page] {
			continue
		}
		seen[e.Page] = true
		starts = append(starts, e)
	}

	sort.SliceStable(starts, func(i, j int) bool { return starts[i].Page < starts[j].Page })
	return starts
}

func hasHeadings(paras []Paragraph) bool {
	for _, p := range paras {
		if p.Kind == idm.KindHeading {
			return true
		}
	}
	return false
}

// ParagraphText joins grouped paragraphs the way a reader reads them: in
// order, separated by a single space.
func ParagraphText(paras []Paragraph) string {
	var b strings.Builder
	for _, p := range paras {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// chapterXHTML renders the body fragment and reports which pages got anchors.
func chapterXHTML(ch chapter, assets *assets) (string, []int) {
	var (
		b        strings.Builder
		pages    []int
		lastPage int
	)

	for _, p := range ch.paras {
		if p.Page != lastPage {
			// The anchor is empty so it does not disturb the flow, but it
			// carries the printed page number for the page list.
			fmt.Fprintf(&b, `<span id="page-%d" role="doc-pagebreak" epub:type="pagebreak" aria-label="%d"></span>`+"\n",
				p.Page, p.Page)
			pages = append(pages, p.Page)
			lastPage = p.Page
		}

		switch {
		case p.Kind == idm.KindHeading:
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", p.Level, escape(p.Text), p.Level)
		case p.Kind == idm.KindFigure && p.Image != nil:
			fmt.Fprintf(&b, "<figure><img src=\"../images/%s\" alt=\"\"/></figure>\n", assets.add(p.Image))
		case p.Kind == idm.KindFigure:
			continue
		default:
			fmt.Fprintf(&b, "<p>%s</p>\n", escape(p.Text))
		}
	}

	return b.String(), pages
}

func titleOf(doc idm.Document, opts Options) string {
	switch {
	case doc.Meta.Title != "":
		return doc.Meta.Title
	case opts.DefaultTitle != "":
		return opts.DefaultTitle
	case doc.Meta.SourceName != "":
		return strings.TrimSuffix(doc.Meta.SourceName, ".pdf")
	default:
		return "Untitled"
	}
}

func languageOf(doc idm.Document) string {
	if doc.Meta.Language != "" {
		return doc.Meta.Language
	}
	return "en"
}

// escape makes text safe for XHTML. encoding/xml also replaces invalid runes,
// which PDF text extraction does occasionally produce.
func escape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return ""
	}
	return b.String()
}
