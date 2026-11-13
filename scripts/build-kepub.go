// Build standard EPUB and Kobo editions of the English reading companion.
// Run from the repository root:
// go run scripts/build-kepub.go
// Needs Pandoc and kepubify. Never packages the Russian novel.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const finalPath = "web/snegov-dictator-kepub.kepub"
const standardPath = "web/snegov-dictator.epub"
const buildDir = "tmp/kobo"
const title = "Dictator — English Reading Companion"
const modified = "2026-11-13T00:00:00Z"

type document struct {
	API    []int          `json:"pandoc-api-version"`
	Meta   map[string]any `json:"meta"`
	Blocks []any          `json:"blocks"`
}
type chapter struct {
	ID, Path, Source          string
	Part, Chapter, Paragraphs int
	Files                     map[string]int `json:"file_counts"`
}
type sourceMap struct {
	Chapters []chapter
	Parts    []struct {
		Part     int
		Chapters []chapter
	}
}
type page struct {
	ID, Kind, Title, Source, Output string
	Part, Chapter                   int
	Doc                             document
	OriginalParagraphs              []string
	OriginalRecords                 []string
}
type locator struct {
	ID, Target string
	Chapters   []string
}
type builder struct {
	book                                 sourceMap
	pages, chapters, catalog             []*page
	bySource, summaryAnchors             map[string]*page
	parts                                map[int]*page
	locators                             map[string]*locator
	files                                map[string][]byte
	inputs                               map[string]string
	versions                             map[string]string
	tables, records, narrativeParagraphs int
}
type link struct{ Target, Attribute string }
type xmlInfo struct {
	IDs                       map[string]bool
	Links                     []link
	Body, Text                string
	Paragraphs                []string
	KoboSpans, Columns, Inner int
	Tags                      map[string]int
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func check(ok bool, format string, args ...any) {
	if !ok {
		panic(fmt.Sprintf(format, args...))
	}
}
func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func esc(s string) string     { return html.EscapeString(s) }
func norm(s string) string    { return strings.Join(strings.Fields(s), " ") }
func arr(v any) []any         { a, ok := v.([]any); check(ok, "expected array, got %T", v); return a }
func str(v any) string        { s, ok := v.(string); check(ok, "expected string, got %T", v); return s }
func node(v any) (string, []any) {
	n, ok := v.(map[string]any)
	if !ok {
		return "", nil
	}
	t, _ := n["t"].(string)
	c, _ := n["c"].([]any)
	return t, c
}
func walk(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(x)
		for _, c := range x {
			walk(c, fn)
		}
	case []any:
		for _, c := range x {
			walk(c, fn)
		}
	}
}
func inline(v any) string {
	var s strings.Builder
	var collect func(any)
	collect = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, c := range x {
				collect(c)
			}
		case map[string]any:
			switch x["t"] {
			case "Str":
				s.WriteString(str(x["c"]))
			case "Space", "SoftBreak", "LineBreak":
				s.WriteByte(' ')
			case "Code":
				s.WriteString(str(arr(x["c"])[1]))
			case "Link", "Image":
				collect(arr(x["c"])[1])
			case "RawInline", "RawBlock":
			default:
				collect(x["c"])
			}
		}
	}
	collect(v)
	return norm(s.String())
}
func command(input []byte, program string, args ...string) []byte {
	c := exec.Command(program, args...)
	c.Stdin = bytes.NewReader(input)
	var errors bytes.Buffer
	c.Stderr = &errors
	out, err := c.Output()
	check(err == nil, "%s: %v\n%s", program, err, errors.String())
	return out
}
func pandoc(data []byte, from, to string) []byte {
	return command(data, "pandoc", "--from="+from, "--to="+to, "--wrap=none", "--eol=lf")
}
func parse(data []byte) document {
	var d document
	must(json.Unmarshal(pandoc(data, "markdown-smart-blank_before_header", "json"), &d))
	return d
}
func render(d document) string {
	data, err := json.Marshal(d)
	must(err)
	return string(pandoc(data, "json", "html5"))
}
func (b *builder) read(name string) []byte {
	check(!filepath.IsAbs(name) && !strings.HasPrefix(filepath.Clean(name), ".."), "unsafe input: %s", name)
	info, err := os.Lstat(name)
	must(err)
	check(info.Mode().IsRegular(), "input is not a regular file: %s", name)
	data, err := os.ReadFile(name)
	must(err)
	b.inputs[name] = hash(data)
	return data
}
func write(name string, data []byte) {
	must(os.MkdirAll(filepath.Dir(name), 0755))
	must(os.WriteFile(name, data, 0644))
}
func relative(from, to string) string {
	s, err := filepath.Rel(path.Dir(from), to)
	must(err)
	return filepath.ToSlash(s)
}
func raw(v any) string {
	t, c := node(v)
	if (t == "RawBlock" || t == "RawInline") && c[0] == "html" {
		return str(c[1])
	}
	return ""
}
func headingID(v any) string {
	t, c := node(v)
	if t == "Header" {
		return str(arr(c[1])[0])
	}
	return ""
}

var anchorRE = regexp.MustCompile(`\bid="([^"]+)"`)

func anchor(id string) any {
	return map[string]any{"t": "RawBlock", "c": []any{"html", `<span id="` + esc(id) + `"></span>`}}
}
func newDoc(api []int) document { return document{API: api, Meta: map[string]any{}} }
func textNode(s string) any     { return map[string]any{"t": "Str", "c": s} }

func (b *builder) load() {
	must(json.Unmarshal(b.read("docs/knowledge/source-map.json"), &b.book))
	check(len(b.book.Chapters) == 73 && len(b.book.Parts) == 6, "source map must have 73 chapters and six parts")
	counts := []int{10, 9, 13, 15, 11, 15}
	expected := map[string]chapter{}
	for i, p := range b.book.Parts {
		check(p.Part == i+1 && len(p.Chapters) == counts[i], "unexpected part inventory")
	}
	for _, c := range b.book.Chapters {
		id := strings.ToLower(c.ID)
		check(expected[id].ID == "", "duplicate source chapter %s", id)
		expected[id] = c
	}
	full := parse(b.read("DICTATOR_SUMMARY.md"))
	intro := &page{ID: "introduction", Kind: "introduction", Title: "Introduction", Source: "DICTATOR_SUMMARY.md", Output: "text/introduction.xhtml", Doc: newDoc(full.API)}
	b.pages = append(b.pages, intro)
	b.bySource[intro.Source] = intro
	current := intro
	for _, block := range full.Blocks {
		t, c := node(block)
		if t == "Header" && c[0] == float64(2) && strings.HasPrefix(inline(c[2]), "Part ") {
			var n int
			_, err := fmt.Sscanf(inline(c[2]), "Part %d", &n)
			must(err)
			check(n >= 1 && n <= 6 && b.parts[n] == nil, "invalid or duplicate part %d", n)
			p := &page{ID: fmt.Sprintf("part-%d", n), Kind: "part", Title: inline(c[2]), Part: n, Source: "DICTATOR_SUMMARY.md", Output: fmt.Sprintf("text/part-%d.xhtml", n), Doc: newDoc(full.API)}
			c[0] = float64(1)
			p.Doc.Blocks = append(p.Doc.Blocks, block)
			b.parts[n] = p
			b.pages = append(b.pages, p)
			b.summaryAnchors[headingID(block)] = p
			current = p
			continue
		}
		walk(block, func(n map[string]any) {
			for _, m := range anchorRE.FindAllStringSubmatch(raw(n), -1) {
				if r, ok := expected[m[1]]; ok {
					check(b.summaryAnchors[m[1]] == nil, "duplicate chapter %s", m[1])
					current = &page{ID: m[1], Kind: "chapter", Part: r.Part, Chapter: r.Chapter, Source: "DICTATOR_SUMMARY.md", Output: "text/" + m[1] + ".xhtml", Doc: newDoc(full.API)}
					b.pages = append(b.pages, current)
					b.chapters = append(b.chapters, current)
					b.summaryAnchors[m[1]] = current
				}
			}
		})
		if t == "Header" && c[0] == float64(3) && current.Kind == "chapter" {
			current.Title = inline(c[2])
			c[0] = float64(1)
		}
		current.Doc.Blocks = append(current.Doc.Blocks, block)
		if id := headingID(block); id != "" {
			b.summaryAnchors[id] = current
		}
	}
	check(len(b.chapters) == 73 && len(b.parts) == 6, "summary inventory differs")
	for i, p := range b.chapters {
		check(p.ID == strings.ToLower(b.book.Chapters[i].ID) && p.Title != "", "chapter identity/order differs at %d", i)
	}
	// Retain every reference page and six fact ledgers, with document-scoped anchors.
	references := []string{"index", "characters", "places", "timeline", "world", "continuity", "chapter-reviews", "source-map"}
	for i := 1; i <= 6; i++ {
		references = append(references, fmt.Sprintf("parts/part-%d-ledger", i))
	}
	for _, name := range references {
		source := "docs/knowledge/" + name + ".md"
		d := parse(b.read(source))
		var nameTitle string
		for _, block := range d.Blocks {
			t, c := node(block)
			if t == "Header" && c[0] == float64(1) {
				nameTitle = inline(c[2])
				break
			}
		}
		check(nameTitle != "", "missing reference title: %s", source)
		p := &page{ID: "catalog-" + strings.ReplaceAll(name, "/", "-"), Kind: "catalog", Title: nameTitle, Source: source, Output: "catalog/" + name + ".xhtml", Doc: d}
		b.catalog = append(b.catalog, p)
		b.pages = append(b.pages, p)
		b.bySource[source] = p
	}
}

// Convert tables to one labeled record per row. All original cells are retained.
func (b *builder) tableRecords(blocks []any) []any {
	var out []any
	for _, block := range blocks {
		t, c := node(block)
		if t != "Table" {
			out = append(out, block)
			continue
		}
		b.tables++
		headRows := arr(arr(c[3])[1])
		check(len(headRows) == 1, "expected a single table header row")
		heads := arr(arr(headRows[0])[1])
		var rows []any
		for _, body := range arr(c[4]) {
			v := arr(body)
			rows = append(rows, arr(v[2])...)
			rows = append(rows, arr(v[3])...)
		}
		rows = append(rows, arr(arr(c[5])[1])...)
		for _, row := range rows {
			cells := arr(arr(row)[1])
			check(len(cells) == len(heads), "table width differs")
			var record []any
			for j, cell := range cells {
				v := arr(cell)
				check(v[2] == float64(1) && v[3] == float64(1), "spanning table cells unsupported")
				cellBlocks := arr(v[4])
				label := inline(arr(heads[j])[4])
				prefix := []any{map[string]any{"t": "Strong", "c": []any{textNode(label + ":")}}, map[string]any{"t": "Space"}}
				if len(cellBlocks) > 0 {
					typ, content := node(cellBlocks[0])
					if typ == "Plain" || typ == "Para" {
						copyNode := map[string]any{"t": "Para", "c": append(prefix, content...)}
						record = append(record, copyNode)
						record = append(record, cellBlocks[1:]...)
						continue
					}
				}
				record = append(record, map[string]any{"t": "Para", "c": prefix})
				record = append(record, cellBlocks...)
			}
			out = append(out, map[string]any{"t": "Div", "c": []any{[]any{"", []any{"record"}, []any{}}, record}})
			b.records++
		}
	}
	return out
}

func (b *builder) sourceLocator(target string) *locator {
	if l := b.locators[target]; l != nil {
		return l
	}
	l := &locator{ID: "source-" + hash([]byte(target))[:16], Target: target}
	file, _, _ := strings.Cut(target, "#")
	for _, c := range b.book.Chapters {
		matched := target == c.Source || file == c.Path
		if strings.HasPrefix(file, "src/text/") {
			_, matchedFile := c.Files[strings.TrimPrefix(file, "src/")]
			matched = matched || matchedFile
		}
		if matched {
			l.Chapters = append(l.Chapters, strings.ToLower(c.ID))
		}
	}
	b.locators[target] = l
	return l
}
func (b *builder) rewrite(p *page, target string) (string, bool) {
	u, err := url.Parse(target)
	must(err)
	check(!u.IsAbs() && u.Host == "", "external link in source: %s", target)
	check(!strings.HasPrefix(u.Path, "/"), "root-relative content link: %s", target)
	source := p.Source
	if u.Path != "" {
		source = path.Clean(path.Join(path.Dir(p.Source), u.Path))
	}
	var output string
	locatorLink := false
	if source == "DICTATOR_SUMMARY.md" {
		q := b.bySource[source]
		if u.Fragment != "" {
			q = b.summaryAnchors[u.Fragment]
		}
		check(q != nil, "unknown summary anchor: %s", target)
		output = q.Output
	} else if q := b.bySource[source]; q != nil {
		output = q.Output
	} else if source == "src" || strings.HasPrefix(source, "src/") || strings.HasPrefix(source, "tmp/chapter-text/") || source == "docs/knowledge/source-map.json" || source == "docs/knowledge/source-coverage.tsv" {
		key := source
		if u.Fragment != "" {
			key += "#" + u.Fragment
		}
		l := b.sourceLocator(key)
		output = "text/source-guide.xhtml"
		u.Fragment = l.ID
		locatorLink = true
	} else {
		panic(fmt.Sprintf("unmapped link %q from %s", target, p.Source))
	}
	u.Path = relative(p.Output, output)
	u.RawPath = ""
	return u.String(), locatorLink
}
func (b *builder) prepare(p *page) {
	// Capture exact narrative paragraphs before any presentation changes.
	if p.Kind == "chapter" || p.Kind == "introduction" {
		for _, block := range p.Doc.Blocks {
			t, c := node(block)
			if t == "Para" {
				s := inline(c)
				if s != "" && !strings.HasPrefix(s, "Source") {
					p.OriginalParagraphs = append(p.OriginalParagraphs, s)
					b.narrativeParagraphs++
				}
			}
		}
	}
	// Independent text inventory of all reference leaf blocks and table cells.
	if p.Kind == "catalog" {
		walk(p.Doc.Blocks, func(n map[string]any) {
			t, c := node(n)
			if t == "Para" || t == "Plain" {
				if s := inline(c); s != "" {
					p.OriginalRecords = append(p.OriginalRecords, s)
				}
			}
		})
	}
	p.Doc.Blocks = b.tableRecords(p.Doc.Blocks)
	explicit := map[string]bool{}
	walk(p.Doc.Blocks, func(n map[string]any) {
		for _, m := range anchorRE.FindAllStringSubmatch(raw(n), -1) {
			explicit[m[1]] = true
		}
	})
	walk(p.Doc.Blocks, func(n map[string]any) {
		t, c := node(n)
		if t == "Header" {
			a := arr(c[1])
			if explicit[str(a[0])] {
				a[0] = ""
			}
		}
		if t == "Link" || t == "Image" {
			target := arr(c[2])
			rewritten, source := b.rewrite(p, str(target[0]))
			target[0] = rewritten
			if source {
				check(t == "Link", "source image cannot be embedded")
				c[1] = append(arr(c[1]), map[string]any{"t": "Space"}, textNode("(locator only)"))
			}
		}
	})
}
func xhtml(title, css, body string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<!DOCTYPE html>` + "\n" + `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" lang="en" xml:lang="en"><head><meta charset="utf-8"/><title>` + esc(title) + `</title><link rel="stylesheet" type="text/css" href="` + esc(css) + `"/></head><body>` + body + `</body></html>` + "\n")
}
func (b *builder) guide() *page {
	var body strings.Builder
	body.WriteString(`<h1 id="source-guide">Source guide</h1><p>This file contains an English reading companion: the introduction, all 73 chapter summaries, and the linked reference catalog. The original Russian novel and its paragraph caches are not included.</p><p>Links marked “locator only” lead here. The paths and anchors below identify evidence in the source repository. They are provenance locators, not broken download links. No internet connection is needed to read this companion or follow its catalog links.</p><p>For the original evidence, use the supplied Russian EPUB or the repository source files. Chapter identifiers have the form P1-C01. Paragraph identifiers such as P0001 are local to the chapter cache named in the source map. Different chapters can share an HTML file, and one chapter can cross file boundaries. The source map and part ledgers retain these distinctions.</p>`)
	keys := make([]string, 0, len(b.locators))
	for key := range b.locators {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		l := b.locators[key]
		body.WriteString(`<section class="locator" id="` + l.ID + `"><h2>Evidence locator</h2><p><code>` + esc(key) + `</code></p>`)
		if strings.HasPrefix(key, "src/") || key == "src" {
			body.WriteString(`<p>Original Russian source; text is not embedded in this companion.</p>`)
		} else if strings.HasPrefix(key, "tmp/chapter-text/") {
			body.WriteString(`<p>Russian paragraph cache; text is not embedded in this companion.</p>`)
		} else {
			body.WriteString(`<p>Machine-readable source record in the repository. The readable source map is included in this companion.</p>`)
		}
		if len(l.Chapters) > 0 {
			body.WriteString(`<p>Related summaries: `)
			for i, id := range l.Chapters {
				if i > 0 {
					body.WriteString(`; `)
				}
				body.WriteString(`<a href="` + id + `.xhtml#` + id + `">` + strings.ToUpper(id) + `</a>`)
			}
			body.WriteString(`.</p>`)
		}
		body.WriteString(`<p><a href="../catalog/source-map.xhtml">Source map</a> · <a href="../catalog/index.xhtml">Reference catalog</a></p></section>`)
	}
	p := &page{ID: "source-guide", Kind: "guide", Title: "Source guide", Output: "text/source-guide.xhtml"}
	b.files["EPUB/"+p.Output] = xhtml(p.Title, "../styles/reader.css", body.String())
	return p
}
func (b *builder) assemble() {
	b.files["mimetype"] = []byte("application/epub+zip")
	b.files["META-INF/container.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="EPUB/package.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)
	b.files["EPUB/styles/reader.css"] = b.read("web/kobo/reader.css")
	for _, p := range b.pages {
		b.prepare(p)
		body := render(p.Doc)
		if p.Kind == "introduction" {
			body = `<p><em>English reading companion to the novel by Sergei Snegov. Full spoilers. Russian source links are <a href="source-guide.xhtml">provenance locators</a>; the original novel is not embedded.</em></p>` + body
		}
		b.files["EPUB/"+p.Output] = xhtml(p.Title, relative(p.Output, "styles/reader.css"), body)
	}
	guide := b.guide()
	b.pages = append(b.pages, guide)
	titlepage := &page{ID: "titlepage", Kind: "titlepage", Title: title, Output: "text/titlepage.xhtml"}
	b.files["EPUB/"+titlepage.Output] = xhtml(title, "../styles/reader.css", `<div class="titlepage"><h1>`+esc(title)+`</h1><p>To the novel by Sergei Snegov</p><p>Complete English chapter summaries and reference catalog</p><p>Six parts · 73 chapters</p><p><strong>Full spoilers, including the ending.</strong></p><p>This companion is a summary and study aid. It does not contain the complete novel or a translation of its full text.</p><p><a href="introduction.xhtml">Start reading</a> · <a href="../nav.xhtml">Contents</a> · <a href="source-guide.xhtml">Source guide</a></p></div>`)
	b.pages = append([]*page{titlepage}, b.pages...)
	var toc strings.Builder
	toc.WriteString(`<nav epub:type="toc" id="toc"><h1>Contents</h1><ol><li><a href="text/introduction.xhtml">Introduction</a></li>`)
	for i := 1; i <= 6; i++ {
		p := b.parts[i]
		toc.WriteString(`<li><a href="` + p.Output + `">` + esc(p.Title) + `</a><ol>`)
		for _, c := range b.chapters {
			if c.Part == i {
				toc.WriteString(`<li><a href="` + c.Output + `#` + c.ID + `">` + esc(c.Title) + `</a></li>`)
			}
		}
		toc.WriteString(`</ol></li>`)
	}
	toc.WriteString(`<li><a href="catalog/index.xhtml">Reference catalog</a><ol>`)
	for _, p := range b.catalog {
		toc.WriteString(`<li><a href="` + p.Output + `">` + esc(p.Title) + `</a></li>`)
	}
	toc.WriteString(`</ol></li><li><a href="text/source-guide.xhtml">Source guide</a></li></ol></nav><nav epub:type="landmarks" hidden="hidden"><h2>Landmarks</h2><ol><li><a epub:type="bodymatter" href="text/introduction.xhtml">Start reading</a></li><li><a epub:type="bibliography" href="catalog/index.xhtml">References</a></li></ol></nav>`)
	b.files["EPUB/nav.xhtml"] = xhtml("Contents", "styles/reader.css", toc.String())
	var opf strings.Builder
	opf.WriteString(`<?xml version="1.0" encoding="UTF-8"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="book-id" xml:lang="en"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="book-id">urn:uuid:792ce4a3-dbd7-4b78-93e3-e83a9f23d75c</dc:identifier><dc:title>` + esc(title) + `</dc:title><dc:language>en</dc:language><dc:creator id="creator">Snegov Dictator reading companion project</dc:creator><meta refines="#creator" property="role" scheme="marc:relators">com</meta><dc:description>English reading companion to Sergei Snegov's Dictator: introduction, all 73 chapter summaries in six parts, and an offline reference catalog. Full spoilers. This is not the full novel or a full translation.</dc:description><dc:source>Companion to the supplied Russian EPUB of Sergei Snegov's Dictator.</dc:source><meta property="dcterms:modified">` + modified + `</meta><meta property="rendition:layout">reflowable</meta></metadata><manifest><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="css" href="styles/reader.css" media-type="text/css"/>`)
	for _, p := range b.pages {
		opf.WriteString(`<item id="` + p.ID + `" href="` + p.Output + `" media-type="application/xhtml+xml"/>`)
	}
	opf.WriteString(`</manifest><spine page-progression-direction="ltr">`)
	for _, p := range b.pages {
		opf.WriteString(`<itemref idref="` + p.ID + `"/>`)
	}
	opf.WriteString(`</spine></package>`)
	b.files["EPUB/package.opf"] = []byte(opf.String())
}

func parseXML(name string, data []byte) xmlInfo {
	check(utf8.Valid(data), "invalid UTF-8 in %s", name)
	d := xml.NewDecoder(bytes.NewReader(data))
	info := xmlInfo{IDs: map[string]bool{}, Tags: map[string]int{}}
	var all, body, para strings.Builder
	depth, inBody, paraDepth := 0, 0, 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		check(err == nil, "invalid XML in %s: %v", name, err)
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			info.Tags[t.Name.Local]++
			check(t.Name.Local != "script" && t.Name.Local != "iframe", "active content in %s", name)
			if t.Name.Local == "html" {
				check(t.Name.Space == "http://www.w3.org/1999/xhtml", "missing XHTML namespace: %s", name)
			}
			if t.Name.Local == "body" {
				inBody = depth
			}
			if t.Name.Local == "p" {
				paraDepth = depth
				para.Reset()
			}
			if blockTag(t.Name.Local) {
				all.WriteByte(' ')
				if inBody > 0 {
					body.WriteByte(' ')
				}
			}
			for _, a := range t.Attr {
				check(!strings.HasPrefix(strings.ToLower(a.Name.Local), "on"), "event attribute in %s", name)
				if a.Name.Local == "id" {
					check(!info.IDs[a.Value], "duplicate ID %s in %s", a.Value, name)
					info.IDs[a.Value] = true
					if a.Value == "book-columns" {
						info.Columns++
					}
					if a.Value == "book-inner" {
						info.Inner++
					}
				}
				if a.Name.Local == "class" && strings.Contains(" "+a.Value+" ", " koboSpan ") {
					info.KoboSpans++
				}
				if a.Name.Local == "href" || a.Name.Local == "src" {
					info.Links = append(info.Links, link{a.Value, a.Name.Local})
				}
			}
		case xml.CharData:
			all.Write(t)
			if inBody > 0 {
				body.Write(t)
			}
			if paraDepth > 0 {
				para.Write(t)
			}
		case xml.EndElement:
			if paraDepth == depth {
				info.Paragraphs = append(info.Paragraphs, norm(para.String()))
				paraDepth = 0
			}
			if inBody == depth {
				inBody = 0
			}
			depth--
			if blockTag(t.Name.Local) {
				all.WriteByte(' ')
				if inBody > 0 {
					body.WriteByte(' ')
				}
			}
		}
	}
	check(depth == 0, "unclosed XML in %s", name)
	info.Text = norm(all.String())
	info.Body = norm(body.String())
	return info
}
func blockTag(tag string) bool {
	switch tag {
	case "p", "div", "section", "li", "h1", "h2", "h3", "h4", "h5", "h6", "td", "th", "tr", "ol", "ul", "nav", "br", "blockquote", "dt", "dd":
		return true
	}
	return false
}
func zipFiles(name string) (map[string][]byte, []*zip.File) {
	z, err := zip.OpenReader(name)
	must(err)
	defer z.Close()
	files := map[string][]byte{}
	for _, f := range z.File {
		check(files[f.Name] == nil, "duplicate ZIP entry %s", f.Name)
		check(path.Clean(f.Name) == f.Name && !strings.HasPrefix(f.Name, "../") && !strings.HasPrefix(f.Name, "/"), "unsafe ZIP entry %s", f.Name)
		r, err := f.Open()
		must(err)
		data, err := io.ReadAll(r)
		must(err)
		must(r.Close())
		files[f.Name] = data
	}
	return files, z.File
}
func makeZIP(name string, files map[string][]byte) {
	f, err := os.Create(name)
	must(err)
	z := zip.NewWriter(f)
	keys := []string{"mimetype"}
	for key := range files {
		if key != "mimetype" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys[1:])
	for _, key := range keys {
		method := uint16(zip.Deflate)
		if key == "mimetype" {
			method = zip.Store
		}
		h := &zip.FileHeader{Name: key, Method: method}
		h.SetModTime(time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC))
		if key == "mimetype" {
			// EPUB forbids an extra field on the first mimetype local header.
			// Keep the DOS timestamp but suppress Go's extended timestamp field.
			h.Modified = time.Time{}
		}
		w, err := z.CreateHeader(h)
		must(err)
		_, err = w.Write(files[key])
		must(err)
	}
	must(z.Close())
	must(f.Close())
}

func (b *builder) validate(name string, kobo bool) map[string]any {
	files, entries := zipFiles(name)
	check(len(entries) > 0 && entries[0].Name == "mimetype" && entries[0].Method == zip.Store && len(entries[0].Extra) == 0 && string(files["mimetype"]) == "application/epub+zip", "invalid EPUB mimetype ordering/storage/extra field")
	var container struct {
		Roots []struct {
			Path string `xml:"full-path,attr"`
			Type string `xml:"media-type,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	must(xml.Unmarshal(files["META-INF/container.xml"], &container))
	check(len(container.Roots) == 1 && container.Roots[0].Path == "EPUB/package.opf" && container.Roots[0].Type == "application/oebps-package+xml", "invalid container rootfile")
	type item struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		Type       string `xml:"media-type,attr"`
		Properties string `xml:"properties,attr"`
	}
	var opf struct {
		Version  string `xml:"version,attr"`
		UID      string `xml:"unique-identifier,attr"`
		Metadata struct {
			Title      string `xml:"title"`
			Language   string `xml:"language"`
			Identifier struct {
				ID    string `xml:"id,attr"`
				Value string `xml:",chardata"`
			} `xml:"identifier"`
			Meta []struct {
				Property string `xml:"property,attr"`
				Value    string `xml:",chardata"`
			} `xml:"meta"`
		} `xml:"metadata"`
		Items []item `xml:"manifest>item"`
		Spine []struct {
			Ref string `xml:"idref,attr"`
		} `xml:"spine>itemref"`
	}
	must(xml.Unmarshal(files["EPUB/package.opf"], &opf))
	check(opf.Version == "3.0" && opf.Metadata.Language == "en" && opf.Metadata.Title == title && opf.UID == opf.Metadata.Identifier.ID && opf.Metadata.Identifier.Value != "", "invalid EPUB metadata")
	items := map[string]item{}
	listed := map[string]bool{}
	navs, covers := 0, 0
	for _, i := range opf.Items {
		check(items[i.ID].ID == "" && i.ID != "", "duplicate/empty manifest ID %s", i.ID)
		items[i.ID] = i
		target := path.Join("EPUB", i.Href)
		check(files[target] != nil, "missing manifest resource %s", target)
		check(!listed[target], "duplicate manifest resource %s", target)
		listed[target] = true
		if i.Properties == "nav" {
			navs++
		}
		if i.Properties == "cover-image" {
			covers++
			check(i.Type == "image/png", "cover must be PNG")
		}
	}
	check(navs == 1 && covers == 0, "invalid nav or unexpected cover image")
	var chapterOrder []string
	seenSpine := map[string]bool{}
	for _, s := range opf.Spine {
		i, ok := items[s.Ref]
		check(ok && i.Type == "application/xhtml+xml" && !seenSpine[s.Ref], "invalid/duplicate spine item %s", s.Ref)
		seenSpine[s.Ref] = true
		if strings.HasPrefix(i.Href, "text/p") && regexp.MustCompile(`text/p[1-6]-c[0-9]{2}\.xhtml`).MatchString(i.Href) {
			chapterOrder = append(chapterOrder, strings.TrimSuffix(path.Base(i.Href), ".xhtml"))
		}
	}
	check(len(chapterOrder) == 73, "spine chapter count differs")
	for i, c := range b.book.Chapters {
		check(chapterOrder[i] == strings.ToLower(c.ID), "spine chapter order differs")
	}
	infos := map[string]xmlInfo{}
	links, spans, xhtmls := 0, 0, 0
	for key, data := range files {
		check(!strings.HasPrefix(key, "src/") && !strings.Contains(key, "chapter-text") && !strings.HasSuffix(key, ".js"), "forbidden bundled source/script: %s", key)
		if strings.HasSuffix(key, ".xhtml") || strings.HasSuffix(key, ".opf") || strings.HasSuffix(key, ".xml") || strings.HasSuffix(key, ".ncx") {
			infos[key] = parseXML(key, data)
		}
		if strings.HasSuffix(key, ".css") {
			check(!bytes.Contains(data, []byte("url(")) && !bytes.Contains(data, []byte("@import")), "CSS resource dependency in %s", key)
		}
		if strings.HasPrefix(key, "EPUB/") && key != "EPUB/package.opf" {
			check(listed[key], "unmanifested resource %s", key)
		}
	}
	for key, info := range infos {
		if strings.HasSuffix(key, ".xhtml") {
			xhtmls++
			check(info.Tags["html"] == 1 && info.Tags["head"] == 1 && info.Tags["body"] == 1, "invalid XHTML structure: %s", key)
			for _, r := range info.Body {
				check(!unicode.In(r, unicode.Cyrillic), "Russian reading text found in %s", key)
			}
			if kobo && key != "EPUB/text/cover.xhtml" {
				check(info.Columns == 1 && info.Inner == 1 && info.KoboSpans > 0, "missing Kobo wrappers/spans in %s", key)
			}
			spans += info.KoboSpans
		}
		for _, l := range info.Links {
			u, err := url.Parse(l.Target)
			must(err)
			check(!u.IsAbs() && u.Host == "" && !strings.HasPrefix(u.Path, "/"), "remote/absolute dependency %s in %s", l.Target, key)
			target := key
			if u.Path != "" {
				target = path.Clean(path.Join(path.Dir(key), u.Path))
			}
			check(files[target] != nil, "broken path %s in %s", l.Target, key)
			if u.Fragment != "" {
				check(infos[target].IDs[u.Fragment], "broken anchor %s in %s", l.Target, key)
			}
			links++
		}
	}
	for _, p := range b.pages {
		key := "EPUB/" + p.Output
		info := infos[key]
		if p.Kind == "chapter" {
			check(info.IDs[p.ID], "missing chapter identity %s", p.ID)
		}
		if len(p.OriginalParagraphs) > 0 {
			var actual []string
			for _, s := range info.Paragraphs {
				if s == "" || strings.HasPrefix(s, "Source") || strings.HasPrefix(s, "English reading companion to the novel by Sergei Snegov.") {
					continue
				}
				actual = append(actual, s)
			}
			check(len(actual) == len(p.OriginalParagraphs), "paragraph count differs in %s: %d vs %d", p.Output, len(actual), len(p.OriginalParagraphs))
			for i, s := range actual {
				check(s == p.OriginalParagraphs[i], "narrative text differs in %s paragraph %d\nActual: %s\nExpected: %s", p.Output, i+1, s, p.OriginalParagraphs[i])
			}
		}
		for _, record := range p.OriginalRecords {
			check(strings.Contains(strings.ReplaceAll(info.Body, " (locator only)", ""), record), "reference text missing in %s: %.100s", p.Output, record)
		}
		if kobo {
			before := parseXML(key, b.files[key])
			check(before.Body == info.Body, "conversion changed visible text in %s", key)
		}
	}
	return map[string]any{"passed": true, "archive_entries": len(entries), "manifest_items": len(opf.Items), "spine_items": len(opf.Spine), "xhtml_documents": xhtmls, "chapters": 73, "parts": 6, "catalog_documents": 14, "internal_links_checked": links, "kobo_spans": spans, "narrative_paragraphs_compared": b.narrativeParagraphs, "zip_mimetype_first_and_stored": true, "all_xml_well_formed": true, "all_paths_and_fragments_resolve": true, "english_reading_text_only": true, "no_scripts_or_remote_resources": true, "all_catalog_record_text_preserved": true, "conversion_visible_text_unchanged": kobo}
}

func main() {
	reuseKepub := flag.Bool("reuse-kepub", false, "Validate and retain the existing Kobo edition while publishing standard EPUB")
	flag.Parse()
	check(flag.NArg() == 0, "unexpected command arguments")
	b := &builder{bySource: map[string]*page{}, summaryAnchors: map[string]*page{}, parts: map[int]*page{}, locators: map[string]*locator{}, files: map[string][]byte{}, inputs: map[string]string{}, versions: map[string]string{}}
	for _, p := range []string{"pandoc", "kepubify"} {
		out := command(nil, p, "--version")
		b.versions[p] = strings.Split(string(out), "\n")[0]
	}
	b.versions["go"] = strings.TrimSpace(string(command(nil, "go", "version")))
	b.read("scripts/build-kepub.go")
	b.load()
	b.assemble()
	must(os.MkdirAll(buildDir, 0755))
	intermediate := buildDir + "/snegov-dictator-intermediate.epub"
	makeZIP(intermediate, b.files)
	before := b.validate(intermediate, false)
	// Write the converter to a unique output, validate, then atomically publish locally.
	stage, err := os.MkdirTemp(buildDir, "convert-")
	must(err)
	defer os.RemoveAll(stage)
	converted := filepath.Join(stage, "snegov-dictator-kepub.kepub")
	conversionCommand := []string{"kepubify", "--calibre", "--output", "<temporary .kepub>", intermediate}
	var conversion []byte
	if *reuseKepub {
		converted = finalPath
		conversionCommand = nil
		conversion = []byte("Existing kepubify output validated and retained; no conversion performed in this run.")
	} else {
		conversion = command(nil, "kepubify", "--calibre", "--output", converted, intermediate)
	}
	after := b.validate(converted, true)
	finalBytes, err := os.ReadFile(converted)
	must(err)
	if !*reuseKepub {
		must(os.Rename(converted, finalPath))
	}
	standardBytes, err := os.ReadFile(intermediate)
	must(err)
	standardStage := filepath.Join(stage, "snegov-dictator.epub")
	write(standardStage, standardBytes)
	must(os.Rename(standardStage, standardPath))
	archive, _ := zipFiles(finalPath)
	for name, data := range archive {
		write(filepath.Join(buildDir, "preview", name), data)
	}
	var chapterInventory []map[string]any
	for _, p := range b.chapters {
		chapterInventory = append(chapterInventory, map[string]any{"id": strings.ToUpper(p.ID), "title": p.Title, "path": "EPUB/" + p.Output, "narrative_paragraphs": len(p.OriginalParagraphs), "narrative_sha256": hash([]byte(strings.Join(p.OriginalParagraphs, "\n")))})
	}
	report := map[string]any{"schema": "snegov-kepub-build-v1", "output": finalPath, "bytes": len(finalBytes), "sha256": hash(finalBytes), "source_hashes_sha256": b.inputs, "tool_versions": b.versions, "intermediate": intermediate, "conversion_command": conversionCommand, "conversion_output": strings.TrimSpace(string(conversion)), "kepub_reused": *reuseKepub, "intermediate_checks": before, "kepub_checks": after, "tables_converted_to_records": b.tables, "table_records": b.records, "source_locators": len(b.locators), "chapter_inventory": chapterInventory, "limits": []string{"Formal EPUBCheck was not run; Java/EPUBCheck is unavailable.", "No physical Kobo device was available; this is structural and content validation.", "The source guide retains provenance paths; the original Russian novel and caches are not embedded."}}
	report["artifacts"] = []map[string]any{
		{"format": "EPUB 3", "path": standardPath, "bytes": len(standardBytes), "sha256": hash(standardBytes), "checks": before},
		{"format": "Kobo KEPUB", "path": finalPath, "bytes": len(finalBytes), "sha256": hash(finalBytes), "checks": after},
	}
	data, err := json.MarshalIndent(report, "", "  ")
	must(err)
	write(buildDir+"/build-report.json", append(data, '\n'))
	fmt.Printf("Built %s\n%d bytes\nSHA-256 %s\n73 chapters, 14 catalog documents, %d source locators\nChecks: %s\n", finalPath, len(finalBytes), hash(finalBytes), len(b.locators), buildDir+"/build-report.json")
	fmt.Printf("Built %s\n%d bytes\nSHA-256 %s\n", standardPath, len(standardBytes), hash(standardBytes))
}
