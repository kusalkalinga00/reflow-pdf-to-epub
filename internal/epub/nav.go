package epub

import (
	"fmt"
	"strconv"
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
	setAttr(head, "meta", "name", "dtb:depth", "content", "1")
	setAttr(head, "meta", "name", "dtb:totalPageCount", "content", strconv.Itoa(len(b.pages)))
	setAttr(head, "meta", "name", "dtb:maxPageNumber", "content", strconv.Itoa(len(b.pages)))

	docTitle := ncx.CreateElement("docTitle")
	setText(docTitle, "text", b.meta.Title)

	navMap := ncx.CreateElement("navMap")
	for i, ch := range b.chapters {
		point := setAttr(navMap, "navPoint",
			"id", fmt.Sprintf("navPoint-%d", i+1),
			"playOrder", strconv.Itoa(i+1),
		)
		label := point.CreateElement("navLabel")
		setText(label, "text", ch.title)
		setAttr(point, "content", "src", ch.filename)
	}

	if len(b.pages) > 0 {
		pageList := ncx.CreateElement("pageList")
		for i, pg := range b.pages {
			target := setAttr(pageList, "pageTarget",
				"id", fmt.Sprintf("pageTarget-%d", i+1),
				"value", pg.Label,
				"type", "normal",
				"playOrder", strconv.Itoa(len(b.chapters)+i+1),
			)
			label := target.CreateElement("navLabel")
			setText(label, "text", pg.Label)
			setAttr(target, "content", "src", pg.Href)
		}
	}

	doc.Indent(2)
	return doc.WriteToBytes()
}
