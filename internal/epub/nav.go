package epub

import (
	"fmt"
	"strconv"

	"github.com/beevik/etree"
)

// navDocument renders OEBPS/nav.xhtml: the EPUB 3 navigation document. It
// carries the table of contents, the print page list, and landmarks.
func (b *prepared) navDocument() ([]byte, error) {
	doc, body := newXHTMLDocument(b.meta.Language, "Contents", cssName)

	toc := setAttr(body, "nav",
		"epub:type", "toc",
		"id", "toc",
		"role", "doc-toc",
	)
	setText(toc, "h1", "Contents")

	ol := toc.CreateElement("ol")
	for _, ch := range b.chapters {
		li := ol.CreateElement("li")
		link := setAttr(li, "a", "href", ch.filename)
		link.SetText(ch.title)

		// A chapter's subsections are anchors inside the same file. They nest
		// under the chapter so the reader sees the document's real depth
		// without a file per subsection.
		writeTOCSections(li, ch.sections)
	}

	if len(b.pages) > 0 {
		pageList := setAttr(body, "nav",
			"epub:type", "page-list",
			"id", "page-list",
			"hidden", "hidden",
		)
		pages := pageList.CreateElement("ol")
		for _, pg := range b.pages {
			li := pages.CreateElement("li")
			link := setAttr(li, "a", "href", pg.Href)
			link.SetText(pg.Label)
		}
	}

	landmarks := setAttr(body, "nav",
		"epub:type", "landmarks",
		"hidden", "hidden",
	)
	marks := landmarks.CreateElement("ol")
	start := marks.CreateElement("li")
	startLink := setAttr(start, "a",
		"epub:type", "bodymatter",
		"href", b.chapters[0].filename,
	)
	startLink.SetText("Start")
	if b.cover != nil {
		cover := marks.CreateElement("li")
		coverLink := setAttr(cover, "a",
			"epub:type", "cover",
			"href", "text/cover.xhtml",
		)
		coverLink.SetText("Cover")
	}

	doc.Indent(2)
	return doc.WriteToBytes()
}

// writeTOCSections nests a chapter's sections under its list item.
//
// Depth is relative to the chapter's first section, so a book whose chapters
// are level 1 and sections level 2 and 3 nests two deep. A level that is
// missing from the outline is clamped to the deepest list that exists, which
// keeps a malformed outline from creating an empty <ol>.
func writeTOCSections(chapterLI *etree.Element, sections []Section) {
	if len(sections) == 0 {
		return
	}

	// lists[d] is the <ol> holding level d, where d is relative to the
	// chapter's own sections.
	lists := make(map[int]*etree.Element)
	var lastLI []*etree.Element

	for i, sec := range sections {
		depth := sectionDepth(sections, i)
		if depth > len(lastLI) {
			depth = len(lastLI)
		}

		var list *etree.Element
		if depth == 0 {
			if lists[0] == nil {
				lists[0] = chapterLI.CreateElement("ol")
			}
			list = lists[0]
		} else {
			parent := lastLI[depth-1]
			if lists[depth] == nil {
				lists[depth] = parent.CreateElement("ol")
			}
			list = lists[depth]
		}

		item := list.CreateElement("li")
		link := setAttr(item, "a", "href", sec.Href)
		link.SetText(sec.Title)

		// Entering a shallower level closes every deeper one.
		for d := range lists {
			if d > depth {
				delete(lists, d)
			}
		}
		lastLI = append(lastLI[:depth], item)
	}
}

// sectionDepth is a section's level relative to its chapter's first section.
// Zero is the chapter's first section level.
func sectionDepth(sections []Section, i int) int {
	depth := sections[i].Level - sections[0].Level
	if depth < 0 {
		return 0
	}
	return depth
}

// navDepth is the deepest level of the navigation tree: chapters are the
// first level, and a section adds its depth within its chapter.
func navDepth(chapters []chapter) int {
	depth := 1
	for _, ch := range chapters {
		for i := range ch.sections {
			// A chapter is the first level; its first section is the second.
			if d := 2 + sectionDepth(ch.sections, i); d > depth {
				depth = d
			}
		}
	}
	return depth
}

// navPointCount is the number of NCX navPoints, parents and sections alike.
func navPointCount(chapters []chapter) int {
	n := len(chapters)
	for _, ch := range chapters {
		n += len(ch.sections)
	}
	return n
}

// ncxDocument renders OEBPS/toc.ncx for reading systems that predate EPUB 3.
// It duplicates the navigation on purpose: EPUB 2 compatibility is cheap and
// a surprising number of devices still rely on it.
func (b *prepared) ncxDocument() ([]byte, error) {
	doc := newXMLDocument()

	ncx := doc.CreateElement("ncx")
	ncx.CreateAttr("xmlns", "http://www.daisy.org/z3986/2005/ncx/")
	ncx.CreateAttr("version", "2005-1")
	ncx.CreateAttr("xml:lang", b.meta.Language)

	head := ncx.CreateElement("head")
	setAttr(head, "meta", "name", "dtb:uid", "content", b.identifier)
	setAttr(head, "meta", "name", "dtb:depth", "content", strconv.Itoa(navDepth(b.chapters)))
	setAttr(head, "meta", "name", "dtb:totalPageCount", "content", strconv.Itoa(len(b.pages)))
	setAttr(head, "meta", "name", "dtb:maxPageNumber", "content", strconv.Itoa(len(b.pages)))

	docTitle := ncx.CreateElement("docTitle")
	setText(docTitle, "text", b.meta.Title)

	navMap := ncx.CreateElement("navMap")

	// playOrder counts every navPoint in document order, parents before their
	// children, so the page list continues after the last section.
	order := 0
	for i, ch := range b.chapters {
		order++
		point := setAttr(navMap, "navPoint",
			"id", fmt.Sprintf("navPoint-%d", i+1),
			"playOrder", strconv.Itoa(order),
		)
		label := point.CreateElement("navLabel")
		setText(label, "text", ch.title)
		setAttr(point, "content", "src", ch.filename)

		points := []*etree.Element{point}
		for j, sec := range ch.sections {
			depth := sectionDepth(ch.sections, j)
			if depth >= len(points) {
				depth = len(points) - 1
			}
			order++

			parent := points[depth]
			secPoint := setAttr(parent, "navPoint",
				"id", fmt.Sprintf("navPoint-%d-%d", i+1, j+1),
				"playOrder", strconv.Itoa(order),
			)
			secLabel := secPoint.CreateElement("navLabel")
			setText(secLabel, "text", sec.Title)
			setAttr(secPoint, "content", "src", sec.Href)

			// This entry's children go under it, not under the previous
			// branch's deeper entries.
			points = append(points[:depth+1], secPoint)
		}
	}

	if len(b.pages) > 0 {
		// The NCX requires pageTarget value+type pairs to be unique; a source
		// whose pages map to duplicate labels (two bookmarks on one page)
		// would produce a book epubcheck rejects.
		seen := make(map[string]bool, len(b.pages))
		order := 0

		pageList := ncx.CreateElement("pageList")
		for _, pg := range b.pages {
			key := pg.Label + "\x00normal"
			if seen[key] {
				continue
			}
			seen[key] = true
			order++

			target := setAttr(pageList, "pageTarget",
				"id", fmt.Sprintf("pageTarget-%d", order),
				"value", pg.Label,
				"type", "normal",
				"playOrder", strconv.Itoa(navPointCount(b.chapters)+order),
			)
			label := target.CreateElement("navLabel")
			setText(label, "text", pg.Label)
			setAttr(target, "content", "src", pg.Href)
		}
	}

	doc.Indent(2)
	return doc.WriteToBytes()
}
