package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/epub"
	"github.com/kusalkalinga00/tool-factory-epub-convertor/internal/idm"
)

// PageImage is one rasterised page of a page-image book.
type PageImage struct {
	Number    int
	Data      []byte
	MediaType string
}

// RenderFacsimile builds a book whose pages are images.
//
// It is what a document with no text layer becomes instead of an error: the
// layout is faithful, the text is not selectable, and the verdict says so
// (T3). Chapters come from the outline when it describes the document, so a
// scanned novel still has its chapters in the navigation; otherwise the book
// is one chapter of pages. Every page gets a print page anchor and a page-list
// entry, so "go to page 42" still works.
func RenderFacsimile(doc idm.Document, pages []PageImage, opts Options) (epub.Book, error) {
	if len(pages) == 0 {
		return epub.Book{}, ErrNoText
	}

	book := epub.Book{
		Metadata: epub.Metadata{
			Title:     titleOf(doc, opts),
			Authors:   doc.Meta.Authors,
			Language:  languageOf(doc),
			Publisher: doc.Meta.Publisher,
			Modified:  opts.Now,
		},
	}

	// The first page is the cover when there is a book behind it: readers show
	// it as the cover rather than as page one of the reading flow.
	start := 0
	if len(pages) > 1 && pages[0].Number == 1 {
		book.Cover = &epub.Cover{Data: pages[0].Data, MediaType: pages[0].MediaType}
		start = 1
	}

	for i, group := range facsimileGroups(doc.Outline, pages[start:]) {
		filename := fmt.Sprintf("text/chapter-%03d.xhtml", i+1)

		var body strings.Builder
		for _, page := range group.pages {
			name := fmt.Sprintf("page-%03d%s", page.Number, imageExtension(page.MediaType))
			book.Images = append(book.Images, epub.Image{
				Name:      name,
				Data:      page.Data,
				MediaType: page.MediaType,
			})
			fmt.Fprintf(&body, `<span id="page-%d" role="doc-pagebreak" epub:type="pagebreak" aria-label="%d"></span>`+"\n",
				page.Number, page.Number)
			fmt.Fprintf(&body, `<figure class="page"><img src="../images/%s" alt="Page %d"/></figure>`+"\n",
				name, page.Number)
			book.PageList = append(book.PageList, epub.PageTarget{
				Label: strconv.Itoa(page.Number),
				Href:  filename + "#page-" + strconv.Itoa(page.Number),
			})
		}

		book.Chapters = append(book.Chapters, epub.Chapter{Title: group.title, Body: body.String()})
	}

	return book, nil
}

// facsimileChapter is one chapter of page images.
type facsimileChapter struct {
	title string
	pages []PageImage
}

// facsimileGroups assigns pages to chapters using the outline's chapter level.
func facsimileGroups(outline []idm.OutlineEntry, pages []PageImage) []facsimileChapter {
	if len(pages) == 0 {
		return nil
	}

	starts := outlineStarts(outline)
	if len(starts) < 2 {
		return []facsimileChapter{{title: "Pages", pages: pages}}
	}

	var groups []facsimileChapter

	var front []PageImage
	for _, page := range pages {
		if page.Number < starts[0].Page {
			front = append(front, page)
		}
	}
	if len(front) > 0 {
		groups = append(groups, facsimileChapter{title: "Front matter", pages: front})
	}

	for i, start := range starts {
		limit := math.MaxInt
		if i+1 < len(starts) {
			limit = starts[i+1].Page
		}

		var set []PageImage
		for _, page := range pages {
			if page.Number >= start.Page && page.Number < limit {
				set = append(set, page)
			}
		}
		if len(set) > 0 {
			groups = append(groups, facsimileChapter{title: start.Title, pages: set})
		}
	}

	return groups
}
