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
	"math"
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

		// The writer names chapter files by position, so the page list and
		// the nested section links can compute the same path here.
		filename := fmt.Sprintf("text/chapter-%03d.xhtml", i+1)

		book.Chapters = append(book.Chapters, epub.Chapter{
			Title:    ch.title,
			Body:     body,
			Sections: sectionLinks(ch.sections, filename),
		})

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
	// startY is the top of the chapter within startPage. It is +Inf when the
	// bookmark has no position, meaning the chapter starts at the page top.
	startY float64
	paras  []Paragraph
	// sections are the nested outline entries inside this chapter, in order.
	sections []section
}

// section is a nested outline entry: it points inside the chapter rather than
// owning a file, which is what an EPUB nav allows.
type section struct {
	title     string
	anchor    string
	level     int
	startPage int
	startY    float64
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
		chapters[i] = chapter{title: s.Title, startPage: s.Page, startY: startY(s)}
	}

	locateUnpositioned(chapters, paras)
	assignSections(chapters, outline, starts, paras)

	// A paragraph belongs to the last chapter that starts at or before it. The
	// comparison is by position, not just page, because sections routinely
	// share a page.
	var frontMatter []Paragraph
	for _, p := range paras {
		index := -1
		for i := len(chapters) - 1; i >= 0; i-- {
			c := chapters[i]
			if p.Page > c.startPage {
				index = i
			} else if p.Page == c.startPage && p.Box.Y1 <= c.startY+2 {
				index = i
			}
			if index >= 0 {
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

// assignSections attaches outline entries below the chapter level to the
// chapter they fall inside, as nested sections.
//
// Sections do not own files: they are anchors inside the chapter's XHTML, and
// nesting them in the navigation is how a reader sees the document's real
// depth. The LLM book carries 109 outline entries in 16 chapters; without
// sections, 93 of them were invisible.
func assignSections(chapters []chapter, outline []idm.OutlineEntry, starts []idm.OutlineEntry, paras []Paragraph) {
	chapterLevel := starts[0].Level

	// Chapter positions by title, consumed in outline order. A bookmark with
	// no position — the CV's whole outline is like this — is placed by
	// locating its title in the text, exactly as chapters are. Without that,
	// every positionless section lands at the top of its page and collects
	// under whichever chapter happens to start there.
	available := map[string][]position{}
	for _, c := range chapters {
		available[c.title] = append(available[c.title], position{page: c.startPage, y: c.startY})
	}

	prevPage, prevY := 0, math.Inf(1)

	for _, e := range outline {
		if e.Page <= 0 {
			continue
		}

		located := e
		switch {
		case e.Y > 0:
			// The bookmark carries its own position.
		case e.Level <= chapterLevel:
			if positions := available[e.Title]; len(positions) > 0 {
				located.Page, located.Y = positions[0].page, positions[0].y
				available[e.Title] = positions[1:]
			} else if page, y, ok := findTitle(paras, e.Title, prevPage, prevY); ok {
				located.Page, located.Y = page, y
			} else if prevPage > 0 {
				located.Page, located.Y = prevPage, prevY
			}
		default:
			if page, y, ok := findTitle(paras, e.Title, prevPage, prevY); ok {
				located.Page, located.Y = page, y
			} else if prevPage > 0 {
				// A title that extraction split across lines ("Level 5
				// Pearson BTEC HND in Computing" is wrapped in the CV) cannot
				// be matched as one paragraph. It still belongs with the
				// entry before it, which is where the outline put it.
				located.Page, located.Y = prevPage, prevY
			}
		}

		prevPage, prevY = located.Page, startY(located)

		if e.Level <= chapterLevel {
			continue
		}

		owner := sectionOwner(chapters, located)
		if owner < 0 {
			continue
		}

		chapters[owner].sections = append(chapters[owner].sections, section{
			title:     e.Title,
			anchor:    sectionAnchor(len(chapters[owner].sections) + 1),
			level:     e.Level,
			startPage: located.Page,
			startY:    startY(located),
		})
	}
}

// position is a located place in the text.
type position struct {
	page int
	y    float64
}

// sectionOwner finds the chapter a nested entry belongs to.
//
// An entry with a position belongs to the last chapter that starts at or
// before it. An entry without a position marks its page's top, which is the
// first chapter starting on that page — not the last one, which starts lower
// down. A positionless entry on a page with no chapter of its own belongs to
// the last chapter that started before it.
func sectionOwner(chapters []chapter, e idm.OutlineEntry) int {
	if math.IsInf(startY(e), 1) {
		for i, c := range chapters {
			if c.startPage == e.Page {
				return i
			}
		}
		for i := len(chapters) - 1; i >= 0; i-- {
			if chapters[i].startPage < e.Page {
				return i
			}
		}
		return -1
	}

	for i := len(chapters) - 1; i >= 0; i-- {
		c := chapters[i]
		if e.Page > c.startPage || (e.Page == c.startPage && startY(e) <= c.startY+2) {
			return i
		}
	}
	return -1
}

// sectionAnchor is a stable id for a section within its chapter.
func sectionAnchor(n int) string { return fmt.Sprintf("sec-%02d", n) }

// sectionAnchorHTML emits the anchor element at the position a section begins.
// It is inserted before the first paragraph at or below the section start.
func sectionAnchorHTML(sec section) string {
	return fmt.Sprintf(`<span id="%s"></span>`, sec.anchor)
}

// sectionLinks turns a chapter's sections into navigation entries. The href
// points inside the chapter file, which is where the anchor was emitted.
func sectionLinks(sections []section, filename string) []epub.Section {
	if len(sections) == 0 {
		return nil
	}
	out := make([]epub.Section, len(sections))
	for i, sec := range sections {
		out[i] = epub.Section{
			Title: sec.title,
			Href:  filename + "#" + sec.anchor,
			Level: sec.level,
		}
	}
	return out
}

// outlineStarts picks the level of the outline that describes the document's
// structure.
//
// The shallowest level with at least two usable entries wins. A résumé has a
// single level-1 entry and seven level-2 sections, so using level 1 blindly
// produced one chapter for a document with a real table of contents.
func outlineStarts(outline []idm.OutlineEntry) []idm.OutlineEntry {
	byLevel := map[int][]idm.OutlineEntry{}
	var levels []int

	for _, e := range outline {
		if e.Page <= 0 {
			continue
		}
		if _, seen := byLevel[e.Level]; !seen {
			levels = append(levels, e.Level)
		}
		byLevel[e.Level] = append(byLevel[e.Level], e)
	}
	sort.Ints(levels)

	for _, level := range levels {
		if starts := sortAndDedupe(byLevel[level]); len(starts) >= 2 {
			return starts
		}
	}
	return nil
}

// sortAndDedupe orders entries by page then top-down, dropping entries that
// point at the same position.
func sortAndDedupe(entries []idm.OutlineEntry) []idm.OutlineEntry {
	sorted := append([]idm.OutlineEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Page != sorted[j].Page {
			return sorted[i].Page < sorted[j].Page
		}
		return startY(sorted[i]) > startY(sorted[j])
	})

	// Entries without a position are kept: they are distinct sections that
	// will be located by title later. Dropping them here collapsed a résumé's
	// seven sections into one chapter.
	out := sorted[:0]
	lastPage := -1
	lastY := 0.0
	for _, e := range sorted {
		y := startY(e)
		if e.Page == lastPage && y == lastY && !math.IsInf(y, 1) {
			continue
		}
		lastPage, lastY = e.Page, y
		out = append(out, e)
	}
	return out
}

// locateUnpositioned gives a position to chapters whose bookmark points at a
// page without a vertical destination.
//
// Many producers write /Fit destinations, which carry no y at all — the résumé
// in the corpus has thirteen bookmarks on two pages and not one position. The
// bookmark title is still in the text, so the heading is found by name and its
// position is used instead.
func locateUnpositioned(chapters []chapter, paras []Paragraph) {
	prevPage := 0
	prevY := math.Inf(1)

	for i := range chapters {
		c := &chapters[i]

		if !math.IsInf(c.startY, 1) {
			prevPage, prevY = c.startPage, c.startY
			continue
		}

		page, y, ok := findTitle(paras, c.title, prevPage, prevY)
		if !ok {
			continue
		}
		c.startPage, c.startY = page, y
		prevPage, prevY = page, y
	}
}

// findTitle returns the position of the first paragraph at or after the given
// point whose text matches the bookmark title.
func findTitle(paras []Paragraph, title string, afterPage int, afterY float64) (int, float64, bool) {
	want := normalizeTitle(title)
	if want == "" {
		return 0, 0, false
	}

	for _, p := range paras {
		if p.Page < afterPage {
			continue
		}
		if p.Page == afterPage && p.Box.Y1 > afterY {
			continue
		}

		got := normalizeTitle(p.Text)
		if got == want || strings.HasPrefix(got, want+" ") || strings.HasPrefix(got, want+":") {
			return p.Page, p.Box.Y1, true
		}
	}

	return 0, 0, false
}

func normalizeTitle(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// startY is where a chapter begins on its page. A bookmark without a position
// starts at the top, which sorts first and contains everything on the page.
func startY(entry idm.OutlineEntry) float64 {
	if entry.Y <= 0 {
		return math.Inf(1)
	}
	return entry.Y
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

	// Sections are emitted in reading order: page ascending, and top of page
	// first within a page. A section without a position starts at its page's
	// top, which is the paragraph order's first match.
	sections := append([]section(nil), ch.sections...)
	sort.SliceStable(sections, func(i, j int) bool {
		if sections[i].startPage != sections[j].startPage {
			return sections[i].startPage < sections[j].startPage
		}
		return sections[i].startY > sections[j].startY
	})
	next := 0

	for _, p := range ch.paras {
		for next < len(sections) && sectionStartsAt(sections[next], p) {
			b.WriteString(sectionAnchorHTML(sections[next]))
			b.WriteByte('\n')
			next++
		}

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
		case p.Kind == idm.KindTable && p.Table != nil:
			b.WriteString(tableXHTML(p.Table))
		case p.Kind == idm.KindFigure && p.Image != nil:
			fmt.Fprintf(&b, "<figure><img src=\"../images/%s\" alt=\"\"/></figure>\n", assets.add(p.Image))
		case p.Kind == idm.KindFigure:
			continue
		default:
			fmt.Fprintf(&b, "<p>%s</p>\n", escape(p.Text))
		}
	}

	// A section whose page holds no paragraph still needs its anchor, or the
	// navigation link would dangle and the writer would reject the book.
	for ; next < len(sections); next++ {
		b.WriteString(sectionAnchorHTML(sections[next]))
		b.WriteByte('\n')
	}

	return b.String(), pages
}

// sectionStartsAt reports whether a paragraph is the first one at or after the
// section's position. Positionless sections start at their page's top.
func sectionStartsAt(sec section, p Paragraph) bool {
	if p.Page > sec.startPage {
		return true
	}
	return p.Page == sec.startPage && p.Box.Y1 <= sec.startY+2
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
// tableXHTML renders a recovered grid. Header detection is left to the
// reading system: a plain grid with first-row scope is the honest markup for
// a table whose header row we did not verify.
func tableXHTML(grid *idm.TableGrid) string {
	var b strings.Builder
	b.WriteString("<table>\n")
	for _, row := range grid.Cells {
		b.WriteString("<tr>")
		for _, cell := range row {
			fmt.Fprintf(&b, "<td>%s</td>", escape(cell))
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table>\n")
	return b.String()
}

func escape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return ""
	}
	return b.String()
}
