package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
)

func sampleBook() Book {
	return Book{
		Metadata: Metadata{
			Title:      "The Long Road",
			Authors:    []string{"A. Author", "B. Writer"},
			Language:   "en",
			Publisher:  "Tool Factory",
			Identifier: "urn:uuid:0b9f0f2e-0000-4000-8000-000000000000",
			Modified:   time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
		},
		Chapters: []Chapter{
			{
				ID:    "ch-one",
				Title: "The Long Road",
				Body:  `<span id="page-1"></span><h1>Chapter 1</h1><p>The road bent north and the engine never stalled.</p>`,
			},
			{
				Title: "The Efficient Engine",
				Body:  `<span id="page-2"></span><h1>Chapter 2</h1><p>It was rebuilt in the spring of 1892.</p>`,
			},
		},
		PageList: []PageTarget{
			{Label: "1", Href: "text/chapter-001.xhtml#page-1"},
			{Label: "2", Href: "text/chapter-002.xhtml#page-2"},
		},
	}
}

func mustWrite(t *testing.T, book Book) []byte {
	t.Helper()
	data, err := WriteBytes(book)
	if err != nil {
		t.Fatalf("WriteBytes() error: %v", err)
	}
	return data
}

type archive struct {
	order []*zip.File
	files map[string][]byte
}

func openArchive(t *testing.T, data []byte) *archive {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	a := &archive{order: zr.File, files: make(map[string][]byte, len(zr.File))}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		a.files[f.Name] = body
	}
	return a
}

func (a *archive) mustGet(t *testing.T, name string) []byte {
	t.Helper()
	data, ok := a.files[name]
	if !ok {
		t.Fatalf("archive has no entry %q; it has %v", name, a.names())
	}
	return data
}

func (a *archive) names() []string {
	names := make([]string, 0, len(a.order))
	for _, f := range a.order {
		names = append(names, f.Name)
	}
	return names
}

func mustParseXML(t *testing.T, data []byte) *etree.Document {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		t.Fatalf("parse XML: %v\n%s", err, data)
	}
	return doc
}

// findAll returns every element with the given name. etree splits namespaced
// names into Space and Tag, so "dc:title" is compared against the full key.
func findAll(el *etree.Element, tag string) []*etree.Element {
	var found []*etree.Element
	var walk func(*etree.Element)
	walk = func(e *etree.Element) {
		if e.FullTag() == tag {
			found = append(found, e)
		}
		for _, child := range e.ChildElements() {
			walk(child)
		}
	}
	walk(el)
	return found
}

func allElements(doc *etree.Document) []*etree.Element {
	var out []*etree.Element
	var walk func(*etree.Element)
	walk = func(e *etree.Element) {
		out = append(out, e)
		for _, child := range e.ChildElements() {
			walk(child)
		}
	}
	if doc.Root() != nil {
		walk(doc.Root())
	}
	return out
}

func firstOf(t *testing.T, doc *etree.Document, tag string) *etree.Element {
	t.Helper()
	found := findAll(doc.Root(), tag)
	if len(found) == 0 {
		t.Fatalf("document has no <%s> element", tag)
	}
	return found[0]
}

func TestMimetypeEntryIsFirstStoredAndClean(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))

	first := a.order[0]
	if first.Name != "mimetype" {
		t.Fatalf("first entry is %q, want %q", first.Name, "mimetype")
	}
	if first.Method != zip.Store {
		t.Errorf("mimetype method = %d, want store (%d)", first.Method, zip.Store)
	}
	if len(first.Extra) != 0 {
		t.Errorf("mimetype has %d bytes of extra field, want none", len(first.Extra))
	}
	if first.Flags&0x8 != 0 {
		t.Error("mimetype uses a data descriptor; the OCF spec forbids it")
	}
	if got := a.mustGet(t, "mimetype"); string(got) != "application/epub+zip" {
		t.Errorf("mimetype content = %q, want %q", got, "application/epub+zip")
	}
}

func TestMinimumRequiredFilesExist(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))

	for _, name := range []string{
		"mimetype",
		"META-INF/container.xml",
		"OEBPS/package.opf",
		"OEBPS/nav.xhtml",
		"OEBPS/toc.ncx",
		"OEBPS/css/book.css",
		"OEBPS/text/chapter-001.xhtml",
		"OEBPS/text/chapter-002.xhtml",
	} {
		a.mustGet(t, name)
	}
}

func TestContainerPointsAtPackageDocument(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))
	doc := mustParseXML(t, a.mustGet(t, "META-INF/container.xml"))

	rootfile := firstOf(t, doc, "rootfile")
	if got := rootfile.SelectAttrValue("full-path", ""); got != "OEBPS/package.opf" {
		t.Errorf("full-path = %q, want %q", got, "OEBPS/package.opf")
	}
	if got := rootfile.SelectAttrValue("media-type", ""); got != mediaTypeOPF {
		t.Errorf("media-type = %q, want %q", got, mediaTypeOPF)
	}
}

func TestPackageMetadata(t *testing.T) {
	book := sampleBook()
	a := openArchive(t, mustWrite(t, book))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))

	pkg := doc.Root()
	if got := pkg.SelectAttrValue("version", ""); got != "3.0" {
		t.Errorf("package version = %q, want 3.0", got)
	}
	if got := pkg.SelectAttrValue("unique-identifier", ""); got != "pub-id" {
		t.Errorf("unique-identifier = %q, want pub-id", got)
	}

	if got := firstOf(t, doc, "dc:identifier").Text(); got != book.Metadata.Identifier {
		t.Errorf("dc:identifier = %q, want %q", got, book.Metadata.Identifier)
	}
	if got := firstOf(t, doc, "dc:title").Text(); got != book.Metadata.Title {
		t.Errorf("dc:title = %q, want %q", got, book.Metadata.Title)
	}
	if got := firstOf(t, doc, "dc:language").Text(); got != "en" {
		t.Errorf("dc:language = %q, want en", got)
	}
	if got := firstOf(t, doc, "dc:publisher").Text(); got != "Tool Factory" {
		t.Errorf("dc:publisher = %q, want %q", got, "Tool Factory")
	}

	creators := findAll(pkg, "dc:creator")
	if len(creators) != 2 {
		t.Fatalf("got %d dc:creator elements, want 2", len(creators))
	}
	if creators[1].Text() != "B. Writer" {
		t.Errorf("second creator = %q, want %q", creators[1].Text(), "B. Writer")
	}

	var modified string
	for _, m := range findAll(pkg, "meta") {
		if m.SelectAttrValue("property", "") == "dcterms:modified" {
			modified = m.Text()
		}
	}
	if modified != "2026-11-14T10:00:00Z" {
		t.Errorf("dcterms:modified = %q, want %q", modified, "2026-11-14T10:00:00Z")
	}
}

func TestAccessibilityMetadata(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))

	got := map[string]bool{}
	for _, m := range findAll(doc.Root(), "meta") {
		property := m.SelectAttrValue("property", "")
		if strings.HasPrefix(property, "schema:") {
			got[property+"="+m.Text()] = true
		}
	}

	for _, want := range []string{
		"schema:accessMode=textual",
		"schema:accessModeSufficient=textual",
		"schema:accessibilityFeature=structuralNavigation",
		"schema:accessibilityFeature=tableOfContents",
		"schema:accessibilityHazard=none",
	} {
		if !got[want] {
			t.Errorf("missing accessibility metadata %q", want)
		}
	}

	var conforms bool
	for _, l := range findAll(doc.Root(), "link") {
		if l.SelectAttrValue("rel", "") == "dcterms:conformsTo" {
			conforms = true
		}
	}
	if !conforms {
		t.Error("missing dcterms:conformsTo link")
	}
}

func TestManifestAndSpineAgree(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))

	manifestIDs := map[string]string{} // id -> href
	for _, item := range findAll(firstOf(t, doc, "manifest"), "item") {
		manifestIDs[item.SelectAttrValue("id", "")] = item.SelectAttrValue("href", "")
	}

	if _, ok := manifestIDs["nav"]; !ok {
		t.Error("manifest has no nav item")
	}
	for _, item := range findAll(firstOf(t, doc, "manifest"), "item") {
		if item.SelectAttrValue("id", "") == "nav" &&
			item.SelectAttrValue("properties", "") != "nav" {
			t.Error("nav item is missing properties=\"nav\"")
		}
	}

	spine := firstOf(t, doc, "spine")
	refs := findAll(spine, "itemref")
	if len(refs) != 2 {
		t.Fatalf("spine has %d itemrefs, want 2", len(refs))
	}

	for i, ref := range refs {
		idref := ref.SelectAttrValue("idref", "")
		href, ok := manifestIDs[idref]
		if !ok {
			t.Errorf("spine itemref %q has no manifest item", idref)
			continue
		}
		want := fmt.Sprintf("text/chapter-%03d.xhtml", i+1)
		if href != want {
			t.Errorf("spine[%d] points at %q, want %q", i, href, want)
		}
	}
}

func TestEveryManifestItemExistsInArchive(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))

	for _, item := range findAll(firstOf(t, doc, "manifest"), "item") {
		href := item.SelectAttrValue("href", "")
		if _, ok := a.files["OEBPS/"+href]; !ok {
			t.Errorf("manifest item %q points at missing file %q", item.SelectAttrValue("id", ""), "OEBPS/"+href)
		}
	}
}

func TestEveryArchiveFileIsInManifest(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))

	listed := map[string]bool{
		"mimetype":               true,
		"META-INF/container.xml": true,
		"OEBPS/package.opf":      true,
	}
	for _, item := range findAll(firstOf(t, doc, "manifest"), "item") {
		listed["OEBPS/"+item.SelectAttrValue("href", "")] = true
	}

	for _, name := range a.names() {
		if !listed[name] {
			t.Errorf("archive entry %q is not declared in the manifest", name)
		}
	}
}

func TestNavigationDocument(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/nav.xhtml"))

	var toc *etree.Element
	for _, nav := range findAll(doc.Root(), "nav") {
		if nav.SelectAttrValue("epub:type", "") == "toc" {
			toc = nav
		}
	}
	if toc == nil {
		t.Fatal("nav.xhtml has no navigation document with epub:type=\"toc\"")
	}

	links := findAll(toc, "a")
	if len(links) != 2 {
		t.Fatalf("toc has %d links, want 2", len(links))
	}
	if got := links[0].SelectAttrValue("href", ""); got != "text/chapter-001.xhtml" {
		t.Errorf("first toc link = %q, want text/chapter-001.xhtml", got)
	}
	if got := links[0].Text(); got != "The Long Road" {
		t.Errorf("first toc label = %q, want %q", got, "The Long Road")
	}
}

func TestPageListAppearsOnlyWhenPageTargetsExist(t *testing.T) {
	t.Run("with page targets", func(t *testing.T) {
		a := openArchive(t, mustWrite(t, sampleBook()))
		doc := mustParseXML(t, a.mustGet(t, "OEBPS/nav.xhtml"))

		var found bool
		for _, nav := range findAll(doc.Root(), "nav") {
			if nav.SelectAttrValue("epub:type", "") == "page-list" {
				found = true
				if links := findAll(nav, "a"); len(links) != 2 {
					t.Errorf("page list has %d entries, want 2", len(links))
				}
			}
		}
		if !found {
			t.Error("page list nav is missing")
		}

		ncx := mustParseXML(t, a.mustGet(t, "OEBPS/toc.ncx"))
		if targets := findAll(ncx.Root(), "pageTarget"); len(targets) != 2 {
			t.Errorf("ncx has %d page targets, want 2", len(targets))
		}
	})

	t.Run("without page targets", func(t *testing.T) {
		book := sampleBook()
		book.PageList = nil
		a := openArchive(t, mustWrite(t, book))
		doc := mustParseXML(t, a.mustGet(t, "OEBPS/nav.xhtml"))

		for _, nav := range findAll(doc.Root(), "nav") {
			if nav.SelectAttrValue("epub:type", "") == "page-list" {
				t.Error("page list nav present for a book without page targets")
			}
		}
	})
}

func TestPageListFragmentsExist(t *testing.T) {
	book := sampleBook()
	book.PageList = []PageTarget{
		{Label: "1", Href: "text/chapter-001.xhtml#page-1"},
	}

	a := openArchive(t, mustWrite(t, book))

	for _, target := range book.PageList {
		file, fragment, ok := strings.Cut(target.Href, "#")
		if !ok {
			t.Errorf("page target %q has no fragment", target.Href)
			continue
		}

		doc := mustParseXML(t, a.mustGet(t, "OEBPS/"+file))
		found := false
		for _, el := range allElements(doc) {
			if el.SelectAttrValue("id", "") == fragment {
				found = true
				break
			}
		}
		if !found {
			// epubcheck reports this as RSC-012: a link into a document that
			// does not define the anchor.
			t.Errorf("page target %q points at a missing id", target.Href)
		}
	}
}

func TestChapterDocuments(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))

	for i, name := range []string{"OEBPS/text/chapter-001.xhtml", "OEBPS/text/chapter-002.xhtml"} {
		doc := mustParseXML(t, a.mustGet(t, name))
		root := doc.Root()

		if root.Tag != "html" {
			t.Errorf("%s: root is <%s>, want <html>", name, root.Tag)
		}
		if got := root.SelectAttrValue("xmlns", ""); got != "http://www.w3.org/1999/xhtml" {
			t.Errorf("%s: xmlns = %q", name, got)
		}
		if got := root.SelectAttrValue("xml:lang", ""); got != "en" {
			t.Errorf("%s: xml:lang = %q, want en", name, got)
		}

		link := firstOf(t, doc, "link")
		if got := link.SelectAttrValue("href", ""); got != "../css/book.css" {
			t.Errorf("%s: stylesheet href = %q, want ../css/book.css", name, got)
		}

		section := firstOf(t, doc, "section")
		if got := section.SelectAttrValue("epub:type", ""); got != "chapter" {
			t.Errorf("%s: section epub:type = %q, want chapter", name, got)
		}

		wantID := []string{"ch-one", "ch002"}[i]
		if got := section.SelectAttrValue("id", ""); got != wantID {
			t.Errorf("%s: section id = %q, want %q", name, got, wantID)
		}
	}
}

func TestChaptersKeepRenderedMarkup(t *testing.T) {
	a := openArchive(t, mustWrite(t, sampleBook()))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/text/chapter-001.xhtml"))

	if got := firstOf(t, doc, "h1").Text(); got != "Chapter 1" {
		t.Errorf("h1 = %q, want Chapter 1", got)
	}
	if got := firstOf(t, doc, "p").Text(); !strings.Contains(got, "never stalled") {
		t.Errorf("paragraph = %q, want it to contain the chapter text", got)
	}
}

func TestCoverIsPackaged(t *testing.T) {
	book := sampleBook()
	book.Cover = &Cover{
		Data:      []byte("\x89PNG\r\n\x1a\nfake cover bytes"),
		MediaType: "image/png",
	}
	a := openArchive(t, mustWrite(t, book))

	a.mustGet(t, "OEBPS/images/cover.png")
	coverPage := mustParseXML(t, a.mustGet(t, "OEBPS/text/cover.xhtml"))

	img := firstOf(t, coverPage, "img")
	if got := img.SelectAttrValue("src", ""); got != "../images/cover.png" {
		t.Errorf("cover img src = %q, want ../images/cover.png", got)
	}
	if img.SelectAttrValue("alt", "") == "" {
		t.Error("cover img has no alt text")
	}

	doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))
	var coverItem *etree.Element
	for _, item := range findAll(firstOf(t, doc, "manifest"), "item") {
		if item.SelectAttrValue("properties", "") == "cover-image" {
			coverItem = item
		}
	}
	if coverItem == nil {
		t.Fatal("manifest has no cover-image item")
	}
	if got := coverItem.SelectAttrValue("media-type", ""); got != "image/png" {
		t.Errorf("cover media-type = %q, want image/png", got)
	}

	refs := findAll(firstOf(t, doc, "spine"), "itemref")
	if got := refs[0].SelectAttrValue("idref", ""); got != "cover" {
		t.Errorf("first spine item = %q, want the cover", got)
	}
}

func TestImagesArePackaged(t *testing.T) {
	book := sampleBook()
	book.Images = []Image{{
		Name: "fig-001.jpg", Data: []byte("jpeg-bytes"), MediaType: "image/jpeg",
	}}

	a := openArchive(t, mustWrite(t, book))
	a.mustGet(t, "OEBPS/images/fig-001.jpg")

	doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))
	var found *etree.Element
	for _, item := range findAll(firstOf(t, doc, "manifest"), "item") {
		if item.SelectAttrValue("href", "") == "images/fig-001.jpg" {
			found = item
		}
	}
	if found == nil {
		t.Fatal("image is not in the manifest")
	}
	if got := found.SelectAttrValue("media-type", ""); got != "image/jpeg" {
		t.Errorf("media type = %q", got)
	}
}

func TestInvalidImagesAreRejected(t *testing.T) {
	tests := []struct {
		name  string
		image Image
		want  string
	}{
		{"no data", Image{Name: "a.jpg", MediaType: "image/jpeg"}, "needs a name and data"},
		{"no name", Image{Data: []byte("x"), MediaType: "image/jpeg"}, "needs a name and data"},
		{"path in name", Image{Name: "../x.jpg", Data: []byte("x"), MediaType: "image/jpeg"}, "must be a file name"},
		{"unknown type", Image{Name: "a.tiff", Data: []byte("x"), MediaType: "image/tiff"}, "unsupported media type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			book := sampleBook()
			book.Images = []Image{tt.image}

			_, err := WriteBytes(book)
			if err == nil {
				t.Fatal("WriteBytes() = nil error, want a rejection")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestDuplicateImageNamesAreRejected(t *testing.T) {
	book := sampleBook()
	book.Images = []Image{
		{Name: "fig-001.jpg", Data: []byte("a"), MediaType: "image/jpeg"},
		{Name: "fig-001.jpg", Data: []byte("b"), MediaType: "image/jpeg"},
	}

	if _, err := WriteBytes(book); err == nil {
		t.Fatal("duplicate image names were accepted")
	}
}

func TestImagesWithoutAltTextAreRejected(t *testing.T) {
	book := sampleBook()
	book.Chapters[0].Body = `<span id="page-1"></span><p>Text</p><img src="../images/fig-001.jpg"/>`

	_, err := WriteBytes(book)
	if err == nil {
		t.Fatal("an image without alt text was accepted")
	}
	if !strings.Contains(err.Error(), "alt") {
		t.Errorf("error = %q, want it to mention alt text", err)
	}
}

func TestEmptyAltTextIsAccepted(t *testing.T) {
	book := sampleBook()
	book.Chapters[0].Body = `<span id="page-1"></span><p>Text</p><img src="../images/fig-001.jpg" alt=""/>`

	if _, err := WriteBytes(book); err != nil {
		t.Fatalf("an image with empty alt text is valid for a decorative figure: %v", err)
	}
}

func TestCoverMediaTypeIsRequiredToBeKnown(t *testing.T) {
	book := sampleBook()
	book.Cover = &Cover{Data: []byte("%PDF-1.7"), MediaType: "application/pdf"}

	_, err := WriteBytes(book)
	if err == nil {
		t.Fatal("WriteBytes() = nil error, want a rejection")
	}
}

func TestInvalidBooksAreRejected(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Book)
		want string
	}{
		{"missing title", func(b *Book) { b.Metadata.Title = "" }, "title"},
		{"missing language", func(b *Book) { b.Metadata.Language = "" }, "language"},
		{"no chapters", func(b *Book) { b.Chapters = nil }, "at least one chapter"},
		{
			"duplicate chapter ids",
			func(b *Book) { b.Chapters[1].ID = b.Chapters[0].ID },
			"duplicate chapter id",
		},
		{
			"chapter id is not an XML name",
			func(b *Book) { b.Chapters[0].ID = "chapter one" },
			"invalid id",
		},
		{
			"malformed chapter body",
			func(b *Book) { b.Chapters[0].Body = `<span id="page-1"></span><p>unclosed` },
			"not well-formed",
		},
		{
			"page target without label",
			func(b *Book) { b.PageList[0].Label = "" },
			"pageList[0]",
		},
		{
			"cover without data",
			func(b *Book) { b.Cover = &Cover{MediaType: "image/png"} },
			"cover has no data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			book := sampleBook()
			tt.mut(&book)

			_, err := WriteBytes(book)
			if err == nil {
				t.Fatal("WriteBytes() = nil error, want a rejection")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestUntitledChapterGetsANumberedLabel(t *testing.T) {
	book := sampleBook()
	book.Chapters[0].Title = ""

	a := openArchive(t, mustWrite(t, book))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/nav.xhtml"))

	links := findAll(firstOf(t, doc, "nav"), "a")
	if got := links[0].Text(); got != "Chapter 1" {
		t.Errorf("toc label = %q, want %q", got, "Chapter 1")
	}
}

func TestDerivedIdentifierIsStableAndContentSensitive(t *testing.T) {
	book := sampleBook()
	book.Metadata.Identifier = ""

	read := func(b Book) string {
		a := openArchive(t, mustWrite(t, b))
		doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))
		return firstOf(t, doc, "dc:identifier").Text()
	}

	first := read(book)
	second := read(book)
	if first != second {
		t.Errorf("identifier is not stable: %q then %q", first, second)
	}
	if !strings.HasPrefix(first, "urn:sha256:") {
		t.Errorf("identifier = %q, want a urn:sha256 URN", first)
	}

	changed := sampleBook()
	changed.Metadata.Identifier = ""
	changed.Chapters[0].Body += "<p>An extra paragraph.</p>"
	if third := read(changed); third == first {
		t.Error("identifier did not change when the content changed")
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	book := sampleBook()

	first := mustWrite(t, book)
	second := mustWrite(t, book)

	if !bytes.Equal(first, second) {
		t.Error("two writes of the same book differ; output is not reproducible")
	}
}

func TestZeroModifiedTimeFallsBackToNow(t *testing.T) {
	book := sampleBook()
	book.Metadata.Modified = time.Time{}

	a := openArchive(t, mustWrite(t, book))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/package.opf"))

	var modified string
	for _, m := range findAll(doc.Root(), "meta") {
		if m.SelectAttrValue("property", "") == "dcterms:modified" {
			modified = m.Text()
		}
	}

	parsed, err := time.Parse(time.RFC3339, modified)
	if err != nil {
		t.Fatalf("dcterms:modified = %q is not RFC 3339: %v", modified, err)
	}
	if time.Since(parsed) > time.Minute {
		t.Errorf("dcterms:modified = %v, want roughly now", parsed)
	}
}

func TestEveryXMLDocumentIsWellFormed(t *testing.T) {
	book := sampleBook()
	book.Cover = &Cover{Data: []byte("\x89PNG\r\n\x1a\nfake"), MediaType: "image/png"}
	a := openArchive(t, mustWrite(t, book))

	for name, data := range a.files {
		if !strings.HasSuffix(name, ".xml") &&
			!strings.HasSuffix(name, ".opf") &&
			!strings.HasSuffix(name, ".xhtml") &&
			!strings.HasSuffix(name, ".ncx") {
			continue
		}

		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			_, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("%s is not well-formed XML: %v", name, err)
				break
			}
		}
	}
}

func TestSectionsNestInsideTheirChapterInNavigation(t *testing.T) {
	book := sampleBook()
	book.Chapters[0].Body = `<span id="page-1"></span><h1>Chapter 1</h1><p>Opening.</p><span id="sec-01"></span><h2>First step</h2><p>Step one.</p>
<span id="sec-02"></span><h2>Second step</h2><p>Step two.</p>`
	book.Chapters[0].Sections = []Section{
		{Title: "First step", Href: "text/chapter-001.xhtml#sec-01"},
		{Title: "Second step", Href: "text/chapter-001.xhtml#sec-02"},
	}

	a := openArchive(t, mustWrite(t, book))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/nav.xhtml"))

	var toc *etree.Element
	for _, nav := range findAll(doc.Root(), "nav") {
		if nav.SelectAttrValue("epub:type", "") == "toc" {
			toc = nav
		}
	}
	if toc == nil {
		t.Fatal("no toc nav")
	}

	links := findAll(toc, "a")
	if len(links) != 4 {
		t.Fatalf("toc has %d links, want 4 (2 chapters + 2 sections)", len(links))
	}
	if got, want := links[1].SelectAttrValue("href", ""), "text/chapter-001.xhtml#sec-01"; got != want {
		t.Errorf("section link = %q, want %q", got, want)
	}

	// The sections must sit inside the first chapter's list item, not beside
	// it: a flat list would hide the document's depth.
	var items []*etree.Element
	for _, ol := range toc.ChildElements() {
		if ol.FullTag() == "ol" {
			items = ol.ChildElements()
		}
	}
	if len(items) != 2 {
		t.Fatalf("toc has %d top-level items, want 2", len(items))
	}
	var nested []*etree.Element
	for _, child := range items[0].ChildElements() {
		if child.FullTag() == "ol" {
			nested = child.ChildElements()
		}
	}
	if len(nested) != 2 {
		t.Fatalf("first chapter nests %d items, want 2", len(nested))
	}

	ncx := mustParseXML(t, a.mustGet(t, "OEBPS/toc.ncx"))

	var roots []*etree.Element
	for _, navMap := range findAll(ncx.Root(), "navMap") {
		for _, child := range navMap.ChildElements() {
			if child.FullTag() == "navPoint" {
				roots = append(roots, child)
			}
		}
	}
	if len(roots) != 2 {
		t.Fatalf("ncx navMap has %d top-level navPoints, want 2", len(roots))
	}

	var sectionPoints []*etree.Element
	for _, child := range roots[0].ChildElements() {
		if child.FullTag() == "navPoint" {
			sectionPoints = append(sectionPoints, child)
		}
	}
	if len(sectionPoints) != 2 {
		t.Fatalf("first chapter nests %d navPoints, want 2 sections", len(sectionPoints))
	}

	// playOrder must be unique across every navPoint and pageTarget.
	points := findAll(ncx.Root(), "navPoint")
	targets := findAll(ncx.Root(), "pageTarget")
	seen := map[string]bool{}
	for _, el := range append(points, targets...) {
		order := el.SelectAttrValue("playOrder", "")
		if order == "" {
			t.Errorf("%s has no playOrder", el.FullTag())
			continue
		}
		if seen[order] {
			t.Errorf("duplicate playOrder %s", order)
		}
		seen[order] = true
	}
	if len(seen) != len(points)+len(targets) {
		t.Errorf("%d playOrders for %d navPoints and %d pageTargets", len(seen), len(points), len(targets))
	}
}

func TestSectionWithMissingAnchorIsRejected(t *testing.T) {
	book := sampleBook()
	book.Chapters[0].Sections = []Section{
		{Title: "Missing", Href: "text/chapter-001.xhtml#sec-99"},
	}

	if _, err := WriteBytes(book); err == nil {
		t.Fatal("WriteBytes accepted a section pointing at a missing anchor")
	}
}

func TestSectionOutsideItsChapterIsRejected(t *testing.T) {
	book := sampleBook()
	book.Chapters[0].Sections = []Section{
		{Title: "Elsewhere", Href: "text/chapter-002.xhtml#page-2"},
	}

	if _, err := WriteBytes(book); err == nil {
		t.Fatal("WriteBytes accepted a section pointing at another chapter")
	}
}

func TestDeeperSectionsNestUnderTheirParent(t *testing.T) {
	book := sampleBook()
	book.Chapters[0].Body = `<span id="page-1"></span><h1>Chapter 1</h1><p>Opening.</p>
<span id="sec-01"></span><h2>Part</h2><p>Part text.</p>
<span id="sec-02"></span><h3>Child</h3><p>Child text.</p>
<span id="sec-03"></span><h2>Sibling</h2><p>Sibling text.</p>`
	book.Chapters[0].Sections = []Section{
		{Title: "Part", Href: "text/chapter-001.xhtml#sec-01", Level: 2},
		{Title: "Child", Href: "text/chapter-001.xhtml#sec-02", Level: 3},
		{Title: "Sibling", Href: "text/chapter-001.xhtml#sec-03", Level: 2},
	}

	a := openArchive(t, mustWrite(t, book))
	doc := mustParseXML(t, a.mustGet(t, "OEBPS/nav.xhtml"))

	var toc *etree.Element
	for _, nav := range findAll(doc.Root(), "nav") {
		if nav.SelectAttrValue("epub:type", "") == "toc" {
			toc = nav
		}
	}
	if toc == nil {
		t.Fatal("no toc nav")
	}

	var chapterLI *etree.Element
	for _, ol := range toc.ChildElements() {
		if ol.FullTag() == "ol" {
			chapterLI = ol.ChildElements()[0]
		}
	}
	if chapterLI == nil {
		t.Fatal("no chapter item")
	}

	// Chapter > ol (2 items); the first item holds the child's ol (1 item).
	var sections []*etree.Element
	for _, child := range chapterLI.ChildElements() {
		if child.FullTag() == "ol" {
			sections = child.ChildElements()
		}
	}
	if len(sections) != 2 {
		t.Fatalf("chapter nests %d sections, want 2", len(sections))
	}

	var children []*etree.Element
	for _, child := range sections[0].ChildElements() {
		if child.FullTag() == "ol" {
			children = child.ChildElements()
		}
	}
	if len(children) != 1 {
		t.Fatalf("first section nests %d children, want 1", len(children))
	}
	if got, want := findAll(children[0], "a")[0].Text(), "Child"; got != want {
		t.Errorf("child label = %q, want %q", got, want)
	}

	ncx := mustParseXML(t, a.mustGet(t, "OEBPS/toc.ncx"))
	for _, head := range findAll(ncx.Root(), "head") {
		metas := findAll(head, "meta")
		for _, meta := range metas {
			if meta.SelectAttrValue("name", "") == "dtb:depth" {
				if got, want := meta.SelectAttrValue("content", ""), "3"; got != want {
					t.Errorf("dtb:depth = %q, want %q", got, want)
				}
			}
		}
	}
}

func TestCoverImageBecomesACoverDocument(t *testing.T) {
	book := sampleBook()
	book.Cover = &Cover{Data: []byte("jpeg-bytes"), MediaType: "image/jpeg"}

	a := openArchive(t, mustWrite(t, book))

	page, ok := a.files["OEBPS/text/cover.xhtml"]
	if !ok {
		t.Fatal("no cover document was written")
	}
	if !strings.Contains(string(page), `alt="Cover"`) {
		t.Errorf("cover image has no alt text:\n%s", page)
	}
	// doc-cover is not an ARIA role; EPUBCheck rejects it.
	if strings.Contains(string(page), "doc-cover") {
		t.Errorf("cover document uses an invalid role:\n%s", page)
	}

	opf := string(a.mustGet(t, "OEBPS/package.opf"))
	if !strings.Contains(opf, `properties="cover-image"`) {
		t.Error("the package document does not mark the cover image")
	}

	nav := string(a.mustGet(t, "OEBPS/nav.xhtml"))
	if !strings.Contains(nav, `epub:type="cover"`) {
		t.Error("the landmarks do not point at the cover")
	}
}
