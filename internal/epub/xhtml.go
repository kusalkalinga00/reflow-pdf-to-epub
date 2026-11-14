package epub

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/beevik/etree"
)

// chapterTemplate wraps a rendered fragment in a complete XHTML document. The
// fragment is inserted verbatim; the result is parsed before it is written, so
// a malformed fragment fails here rather than in a reader.
const chapterTemplate = `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="{{lang}}" lang="{{lang}}">
<head>
<meta charset="utf-8"/>
<title>{{title}}</title>
<link rel="stylesheet" type="text/css" href="../{{css}}"/>
</head>
<body>
<section epub:type="chapter" role="doc-chapter" id="{{id}}">
{{body}}
</section>
</body>
</html>
`

func (b *prepared) chapterDocument(ch chapter) ([]byte, error) {
	doc := strings.NewReplacer(
		"{{lang}}", xmlEscape(b.meta.Language),
		"{{title}}", xmlEscape(ch.title),
		"{{css}}", cssName,
		"{{id}}", xmlEscape(ch.id),
		"{{body}}", ch.body,
	).Replace(chapterTemplate)

	if err := checkWellFormed([]byte(doc)); err != nil {
		return nil, fmt.Errorf("%w: chapter %q is not well-formed XHTML: %w", ErrInvalidBook, ch.title, err)
	}
	return []byte(doc), nil
}

func (b *prepared) coverDocument() ([]byte, error) {
	doc, body := newXHTMLDocument(b.meta.Language, "Cover", "../"+cssName)

	section := body.CreateElement("section")
	section.CreateAttr("epub:type", "cover")
	section.CreateAttr("role", "doc-cover")

	div := section.CreateElement("div")
	div.CreateAttr("class", "cover")

	img := div.CreateElement("img")
	img.CreateAttr("src", "../"+b.cover.filename)
	img.CreateAttr("alt", "Cover")

	doc.Indent(2)
	return doc.WriteToBytes()
}

// newXHTMLDocument builds the envelope every content document shares: an XML
// declaration, the HTML5 doctype, the XHTML and EPUB namespaces, a language
// declaration, and a stylesheet link.
func newXHTMLDocument(lang, title, cssHref string) (*etree.Document, *etree.Element) {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="utf-8"`)
	doc.CreateDirective("DOCTYPE html")

	root := doc.CreateElement("html")
	root.CreateAttr("xmlns", "http://www.w3.org/1999/xhtml")
	root.CreateAttr("xmlns:epub", "http://www.idpf.org/2007/ops")
	root.CreateAttr("xml:lang", lang)
	root.CreateAttr("lang", lang)

	head := root.CreateElement("head")
	charset := head.CreateElement("meta")
	charset.CreateAttr("charset", "utf-8")

	titleEl := head.CreateElement("title")
	titleEl.SetText(title)

	link := head.CreateElement("link")
	link.CreateAttr("rel", "stylesheet")
	link.CreateAttr("type", "text/css")
	link.CreateAttr("href", cssHref)

	body := root.CreateElement("body")
	return doc, body
}

// newXMLDocument builds an XML declaration for a non-XHTML document (OPF, NCX,
// container).
func newXMLDocument() *etree.Document {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="utf-8"`)
	return doc
}

// checkWellFormed parses data as XML and discards the result. It is the gate
// that keeps a broken content document out of the archive.
func checkWellFormed(data []byte) error {
	doc := etree.NewDocument()
	return doc.ReadFromBytes(data)
}

func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		// EscapeText only fails when the writer fails; strings.Builder never does.
		return s
	}
	return b.String()
}

// setText creates a child element and gives it text content.
func setText(parent *etree.Element, tag, text string) *etree.Element {
	el := parent.CreateElement(tag)
	el.SetText(text)
	return el
}

// setAttr creates an element and applies key/value attributes in order.
func setAttr(parent *etree.Element, tag string, attrs ...string) *etree.Element {
	el := parent.CreateElement(tag)
	for i := 0; i+1 < len(attrs); i += 2 {
		el.CreateAttr(attrs[i], attrs[i+1])
	}
	return el
}
