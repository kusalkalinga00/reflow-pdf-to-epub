package epub

import (
	"fmt"

	"github.com/beevik/etree"
)

func (b *prepared) containerDocument() ([]byte, error) {
	doc := newXMLDocument()

	container := doc.CreateElement("container")
	container.CreateAttr("version", "1.0")
	container.CreateAttr("xmlns", "urn:oasis:names:tc:opendocument:xmlns:container")

	rootfiles := container.CreateElement("rootfiles")
	rootfile := rootfiles.CreateElement("rootfile")
	rootfile.CreateAttr("full-path", rootDir+"/"+opfName)
	rootfile.CreateAttr("media-type", mediaTypeOPF)

	doc.Indent(2)
	return doc.WriteToBytes()
}

// packageDocument renders OEBPS/package.opf.
func (b *prepared) packageDocument() ([]byte, error) {
	doc := newXMLDocument()

	pkg := doc.CreateElement("package")
	pkg.CreateAttr("xmlns", "http://www.idpf.org/2007/opf")
	pkg.CreateAttr("version", "3.0")
	pkg.CreateAttr("unique-identifier", "pub-id")
	pkg.CreateAttr("xml:lang", b.meta.Language)

	b.appendMetadata(pkg)
	b.appendManifest(pkg)
	b.appendSpine(pkg)

	doc.Indent(2)
	return doc.WriteToBytes()
}

func (b *prepared) appendMetadata(pkg *etree.Element) {
	md := pkg.CreateElement("metadata")
	md.CreateAttr("xmlns:dc", "http://purl.org/dc/elements/1.1/")

	setAttr(md, "dc:identifier", "id", "pub-id").SetText(b.identifier)
	setText(md, "dc:title", b.meta.Title)
	setText(md, "dc:language", b.meta.Language)

	for i, author := range b.meta.Authors {
		creator := setAttr(md, "dc:creator", "id", fmt.Sprintf("creator-%d", i+1))
		creator.SetText(author)
	}

	if b.meta.Publisher != "" {
		setText(md, "dc:publisher", b.meta.Publisher)
	}

	modified := setAttr(md, "meta", "property", "dcterms:modified")
	modified.SetText(b.modified.Format("2006-01-02T15:04:05Z"))

	b.appendAccessibility(md)

	setAttr(md, "link",
		"rel", "dcterms:conformsTo",
		"href", "https://www.w3.org/TR/epub-a11y-11/#conformance",
	)
}

type metaEntry struct{ property, value string }

// appendAccessibility declares what a reader can expect from the content. The
// values are conservative and true for every book this writer produces: the
// text is the content, navigation is structural, and there is nothing that
// flashes or moves.
func (b *prepared) appendAccessibility(md *etree.Element) {
	metas := []metaEntry{{"schema:accessMode", "textual"}}
	if b.cover != nil {
		// A cover is an image, so the publication is not purely textual.
		metas = append(metas, metaEntry{"schema:accessMode", "visual"})
	}
	metas = append(metas,
		metaEntry{"schema:accessModeSufficient", "textual"},
		metaEntry{"schema:accessibilityFeature", "structuralNavigation"},
		metaEntry{"schema:accessibilityFeature", "tableOfContents"},
		metaEntry{"schema:accessibilityHazard", "none"},
		metaEntry{"schema:accessibilitySummary", "Reflowable text with a navigable table of contents."},
	)

	for _, m := range metas {
		setAttr(md, "meta", "property", m.property).SetText(m.value)
	}
}

func (b *prepared) appendManifest(pkg *etree.Element) {
	manifest := pkg.CreateElement("manifest")

	add := func(id, href, mediaType, properties string) {
		item := setAttr(manifest, "item",
			"id", id,
			"href", href,
			"media-type", mediaType,
		)
		if properties != "" {
			item.CreateAttr("properties", properties)
		}
	}

	add("nav", navName, mediaTypeXHTML, "nav")
	add("ncx", ncxName, mediaTypeNCX, "")
	add("css", cssName, mediaTypeCSS, "")

	if b.cover != nil {
		add("cover-image", b.cover.filename, b.cover.mediaType, "cover-image")
		add("cover", "text/cover.xhtml", mediaTypeXHTML, "")
	}

	for i, ch := range b.chapters {
		add(manifestID(i), ch.filename, mediaTypeXHTML, "")
	}
}

func (b *prepared) appendSpine(pkg *etree.Element) {
	spine := pkg.CreateElement("spine")
	spine.CreateAttr("toc", "ncx")

	if b.cover != nil {
		spine.CreateElement("itemref").CreateAttr("idref", "cover")
	}
	for i := range b.chapters {
		spine.CreateElement("itemref").CreateAttr("idref", manifestID(i))
	}
}

func manifestID(i int) string { return fmt.Sprintf("ch%03d", i+1) }
