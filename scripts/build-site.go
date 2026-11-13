// Build the complete static reader. Run from the repository root:
// go run scripts/build-site.go
// Pandoc is a build dependency; hosting needs only the generated site files.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type document struct {
	API    []int          `json:"pandoc-api-version"`
	Meta   map[string]any `json:"meta"`
	Blocks []any          `json:"blocks"`
}
type chapterRecord struct {
	ID, Path      string
	Part, Chapter int
}
type bookMap struct {
	Chapters []chapterRecord `json:"chapters"`
	Parts    []struct {
		Part     int
		Title    string
		Chapters []chapterRecord
	} `json:"parts"`
	Front     struct{ Path string } `json:"front_matter"`
	Inventory struct {
		Hashes map[string]string `json:"source_hashes_sha256"`
	} `json:"inventory"`
}
type page struct {
	ID, Kind, Title, Label, Description, Source, Output string
	Part, Chapter                                       int
	Doc                                                 document
}
type manifest struct {
	Format string            `json:"format"`
	Files  map[string]string `json:"files"`
}
type builder struct {
	root           string
	book           bookMap
	pages          []*page
	chapters       []*page
	catalog        []*page
	parts          map[int]string
	pageBySource   map[string]*page
	summaryAnchors map[string]*page
	files          map[string][]byte
	inputs         map[string]string
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func require(ok bool, message string, args ...any) {
	if !ok {
		panic(fmt.Sprintf(message, args...))
	}
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func escape(s string) string    { return html.EscapeString(s) }
func list(v any) []any          { a, ok := v.([]any); require(ok, "unexpected Pandoc array: %T", v); return a }
func textValue(v any) string {
	s, ok := v.(string)
	require(ok, "unexpected Pandoc text: %T", v)
	return s
}
func node(v any) (string, []any) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", nil
	}
	t, _ := m["t"].(string)
	c, _ := m["c"].([]any)
	return t, c
}
func walk(v any, visit func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		visit(x)
		for _, child := range x {
			walk(child, visit)
		}
	case []any:
		for _, child := range x {
			walk(child, visit)
		}
	}
}
func inlineText(v any) string {
	var out strings.Builder
	var collect func(any)
	collect = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				collect(child)
			}
		case map[string]any:
			switch x["t"] {
			case "Str":
				out.WriteString(textValue(x["c"]))
			case "Space", "SoftBreak", "LineBreak":
				out.WriteByte(' ')
			case "Code":
				out.WriteString(textValue(list(x["c"])[1]))
			case "Link", "Image":
				collect(list(x["c"])[1])
			default:
				collect(x["c"])
			}
		}
	}
	collect(v)
	return out.String()
}
func pandoc(input []byte, from, to string) []byte {
	cmd := exec.Command("pandoc", "--from="+from, "--to="+to, "--wrap=none", "--eol=lf")
	cmd.Stdin = bytes.NewReader(input)
	var errors bytes.Buffer
	cmd.Stderr = &errors
	result, err := cmd.Output()
	require(err == nil, "pandoc %s → %s: %v\n%s", from, to, err, errors.String())
	return result
}
func parseMarkdown(data []byte) document {
	var d document
	must(json.Unmarshal(pandoc(data, "markdown-smart-blank_before_header", "json"), &d))
	return d
}
func (b *builder) read(name string) []byte {
	name = filepath.ToSlash(filepath.Clean(name))
	require(name != "." && !strings.HasPrefix(name, "../") && !filepath.IsAbs(name), "unsafe input path %q", name)
	full := filepath.Join(b.root, name)
	info, err := os.Lstat(full)
	must(err)
	require(info.Mode().IsRegular(), "input must be a regular file: %s", name)
	resolved, err := filepath.EvalSymlinks(full)
	must(err)
	require(resolved == full, "symlink input is not allowed: %s", name)
	data, err := os.ReadFile(full)
	must(err)
	b.inputs[name] = digest(data)
	return data
}
func rawHTML(n map[string]any) string {
	if n["t"] != "RawBlock" && n["t"] != "RawInline" {
		return ""
	}
	c := list(n["c"])
	if c[0] != "html" {
		return ""
	}
	return textValue(c[1])
}
func htmlStarts(s string, visit func(xml.StartElement)) {
	d := xml.NewDecoder(strings.NewReader(s))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	for {
		token, err := d.RawToken()
		if err == io.EOF {
			return
		}
		must(err)
		if start, ok := token.(xml.StartElement); ok {
			visit(start)
		}
	}
}
func idsInRaw(s string) []string {
	var ids []string
	htmlStarts(s, func(start xml.StartElement) {
		for _, attr := range start.Attr {
			if attr.Name.Local == "id" || (start.Name.Local == "a" && attr.Name.Local == "name") {
				ids = append(ids, attr.Value)
			}
		}
	})
	return ids
}
func anchorBlock(id string) any {
	return map[string]any{"t": "RawBlock", "c": []any{"html", `<span id="` + escape(id) + `"></span>`}}
}
func headingID(block any) string {
	t, c := node(block)
	if t == "Header" {
		return textValue(list(c[1])[0])
	}
	return ""
}
func stripTopHeading(d *document) string {
	for i, block := range d.Blocks {
		t, c := node(block)
		if t == "Header" && c[0] == float64(1) {
			title := inlineText(c[2])
			id := headingID(block)
			if id == "" {
				d.Blocks = append(d.Blocks[:i], d.Blocks[i+1:]...)
			} else {
				d.Blocks[i] = anchorBlock(id)
			}
			return title
		}
	}
	panic("document has no level-one heading")
}
func roman(n int) string { return []string{"", "I", "II", "III", "IV", "V", "VI"}[n] }

func (b *builder) loadPages() {
	must(json.Unmarshal(b.read("docs/knowledge/source-map.json"), &b.book))
	require(len(b.book.Chapters) == 73 && len(b.book.Parts) == 6, "expected 73 chapters in six parts")
	counts := []int{10, 9, 13, 15, 11, 15}
	expected := map[string]chapterRecord{}
	for _, record := range b.book.Chapters {
		id := strings.ToLower(record.ID)
		_, exists := expected[id]
		require(!exists, "duplicate source chapter %s", id)
		expected[id] = record
	}
	for i, part := range b.book.Parts {
		require(part.Part == i+1 && len(part.Chapters) == counts[i], "invalid part inventory %d", i+1)
	}
	full := parseMarkdown(b.read("DICTATOR_SUMMARY.md"))
	overview := &page{ID: "overview", Kind: "overview", Title: "A world at war. A promise of peace.", Label: "Dictator · Sergei Snegov", Description: "A complete English reading companion to Sergei Snegov’s Dictator: 73 chapter summaries, characters, places, and source-linked references.", Source: "DICTATOR_SUMMARY.md", Output: "index.html", Doc: document{API: full.API, Meta: map[string]any{}}}
	current := overview
	seen := map[string]bool{}
	partIDs := map[int]string{}
	for _, block := range full.Blocks {
		t, c := node(block)
		if t == "Header" && c[0] == float64(1) {
			id := headingID(block)
			current.Doc.Blocks = append(current.Doc.Blocks, anchorBlock(id))
			b.summaryAnchors[id] = overview
			continue
		}
		if t == "Header" && c[0] == float64(2) && strings.HasPrefix(inlineText(c[2]), "Part ") {
			var part int
			_, err := fmt.Sscanf(inlineText(c[2]), "Part %d", &part)
			must(err)
			require(part >= 1 && part <= 6, "unexpected summary part %d", part)
			_, title, found := strings.Cut(inlineText(c[2]), " — ")
			require(found, "part heading has no title")
			b.parts[part] = title
			partIDs[part] = headingID(block)
			continue
		}
		var blockAnchors []string
		walk(block, func(n map[string]any) {
			if raw := rawHTML(n); raw != "" {
				blockAnchors = append(blockAnchors, idsInRaw(raw)...)
			}
		})
		if len(blockAnchors) > 0 {
			for _, id := range blockAnchors {
				if record, ok := expected[id]; ok {
					require(!seen[id], "duplicate summary chapter %s", id)
					seen[id] = true
					current = &page{ID: id, Kind: "chapter", Part: record.Part, Chapter: record.Chapter, Label: fmt.Sprintf("Part %s · Chapter %d", roman(record.Part), record.Chapter), Source: "DICTATOR_SUMMARY.md", Output: "chapters/" + id + ".html", Doc: document{API: full.API, Meta: map[string]any{}}}
					b.chapters = append(b.chapters, current)
					b.summaryAnchors[id] = current
					if record.Chapter == 1 {
						partID := partIDs[record.Part]
						require(partID != "", "missing part heading %d", record.Part)
						current.Doc.Blocks = append(current.Doc.Blocks, anchorBlock(partID))
						b.summaryAnchors[partID] = current
					}
				}
			}
		}
		if t == "Header" && c[0] == float64(3) && current.Kind == "chapter" && current.Title == "" {
			_, title, found := strings.Cut(inlineText(c[2]), " — ")
			require(found, "chapter heading has no title")
			current.Title = title
			current.Description = current.Label + " of Sergei Snegov’s Dictator: " + title + ". Complete English summary with source links."
			id := headingID(block)
			current.Doc.Blocks = append(current.Doc.Blocks, anchorBlock(id))
			b.summaryAnchors[id] = current
			continue
		}
		current.Doc.Blocks = append(current.Doc.Blocks, block)
		if id := headingID(block); id != "" {
			b.summaryAnchors[id] = current
		}
	}
	require(len(seen) == len(expected), "summary chapter set does not match source map")
	for i, record := range b.book.Chapters {
		require(b.chapters[i].ID == strings.ToLower(record.ID), "summary order differs at chapter %d", i+1)
		require(b.chapters[i].Title != "", "chapter has no title: %s", record.ID)
	}
	b.pages = append([]*page{overview}, b.chapters...)
	b.pageBySource[overview.Source] = overview
	paths, err := filepath.Glob(filepath.Join(b.root, "docs/knowledge/*.md"))
	must(err)
	ledgers, err := filepath.Glob(filepath.Join(b.root, "docs/knowledge/parts/*.md"))
	must(err)
	paths = append(paths, ledgers...)
	sort.Strings(paths)
	for _, fullPath := range paths {
		source, err := filepath.Rel(b.root, fullPath)
		must(err)
		source = filepath.ToSlash(source)
		rel := strings.TrimPrefix(source, "docs/knowledge/")
		output := "catalog/" + strings.TrimSuffix(rel, ".md") + ".html"
		doc := parseMarkdown(b.read(source))
		title := stripTopHeading(&doc)
		p := &page{ID: "catalog-" + strings.TrimSuffix(strings.ReplaceAll(rel, "/", "-"), ".md"), Kind: "catalog", Title: title, Label: "Reference library", Description: title + " for Sergei Snegov’s Dictator, with chapter and source references.", Source: source, Output: output, Doc: doc}
		if rel == "index.md" {
			p.Title = "Explore the novel"
			p.Description = "Characters, places, chronology, technology, and source-linked evidence for Dictator."
			p.Doc = parseMarkdown([]byte(catalogLanding))
			stripTopHeading(&p.Doc)
		}
		if rel == "source-map.md" {
			downloads := parseMarkdown([]byte("## Download the source records\n\n[Source map (JSON)](source-map.json) · [Source coverage (TSV)](source-coverage.tsv)\n"))
			p.Doc.Blocks = append(p.Doc.Blocks, downloads.Blocks...)
		}
		b.catalog = append(b.catalog, p)
		b.pages = append(b.pages, p)
		b.pageBySource[source] = p
	}
	require(len(b.catalog) == 14, "expected eight reference pages and six part ledgers, got %d", len(b.catalog))
}

func relative(from, to string) string {
	result, err := filepath.Rel(filepath.FromSlash(path.Dir(from)), filepath.FromSlash(to))
	must(err)
	return filepath.ToSlash(result)
}
func (b *builder) rewriteLink(p *page, target string) (string, bool) {
	u, err := url.Parse(target)
	must(err)
	if u.IsAbs() || u.Host != "" {
		return target, true
	}
	if strings.HasPrefix(u.Path, "/") {
		panic("root-relative content link: " + target)
	}
	source := p.Source
	if u.Path != "" {
		source = path.Clean(path.Join(path.Dir(p.Source), u.Path))
	}
	var output string
	raw := false
	if source == "DICTATOR_SUMMARY.md" {
		output = "index.html"
		if u.Fragment != "" {
			destination, ok := b.summaryAnchors[u.Fragment]
			require(ok, "unknown summary anchor %q in %s", u.Fragment, p.Source)
			output = destination.Output
		}
	} else if destination, ok := b.pageBySource[source]; ok {
		output = destination.Output
	} else if source == "src" || source == "src/" {
		output = "source/titlepage.xhtml"
		raw = true
	} else if strings.HasPrefix(source, "src/") {
		output = "source/" + strings.TrimPrefix(source, "src/")
		raw = true
	} else if strings.HasPrefix(source, "tmp/chapter-text/") {
		output = "evidence/chapter-text/" + strings.TrimPrefix(source, "tmp/chapter-text/")
		raw = true
	} else if source == "docs/knowledge/source-map.json" || source == "docs/knowledge/source-coverage.tsv" {
		output = "evidence/" + path.Base(source)
		raw = true
	} else {
		panic(fmt.Sprintf("unmapped content link %q (%s from %s)", target, source, p.Source))
	}
	u.Path = relative(p.Output, output)
	u.RawPath = ""
	return u.String(), raw
}
func addAttr(attr []any, key, value string) {
	pairs := list(attr[2])
	attr[2] = append(pairs, []any{key, value})
}
func (b *builder) renderBody(p *page) string {
	explicit := map[string]bool{}
	walk(p.Doc.Blocks, func(n map[string]any) {
		if raw := rawHTML(n); raw != "" {
			for _, id := range idsInRaw(raw) {
				explicit[id] = true
			}
		}
	})
	walk(p.Doc.Blocks, func(n map[string]any) {
		switch n["t"] {
		case "Link", "Image":
			c := list(n["c"])
			target := list(c[2])
			rewritten, raw := b.rewriteLink(p, textValue(target[0]))
			target[0] = rewritten
			if n["t"] == "Link" && raw {
				addAttr(list(c[0]), "hx-boost", "false")
			}
		case "Header":
			attr := list(list(n["c"])[1])
			if explicit[textValue(attr[0])] {
				attr[0] = ""
			}
		}
	})
	data, err := json.Marshal(p.Doc)
	must(err)
	return string(pandoc(data, "json", "html5"))
}
func (b *builder) navigation(p *page) string {
	var out strings.Builder
	link := func(destination *page, label, class string) {
		current := ""
		if p.ID == destination.ID {
			current = ` aria-current="page"`
		}
		fmt.Fprintf(&out, `<a class="%s" data-page-id="%s" href="%s"%s>%s</a>`+"\n", class, escape(destination.ID), escape(relative(p.Output, destination.Output)), current, escape(label))
	}
	link(b.pages[0], "Overview", "contents-link")
	out.WriteString(`<p class="contents-section-label">The chapter summaries</p>` + "\n")
	for part := 1; part <= 6; part++ {
		open := ""
		if p.Part == part || (p.Kind == "overview" && part == 1) {
			open = " open"
		}
		fmt.Fprintf(&out, `<details class="contents-group" data-part="%d"%s><summary>Part %s · %s</summary><ol class="chapter-list">`+"\n", part, open, roman(part), escape(b.parts[part]))
		for _, chapter := range b.chapters {
			if chapter.Part != part {
				continue
			}
			current := ""
			if p.ID == chapter.ID {
				current = ` aria-current="page"`
			}
			fmt.Fprintf(&out, `<li><a data-page-id="%s" data-search="%s" href="%s"%s><span class="chapter-number">%02d</span> %s</a></li>`+"\n", chapter.ID, escape(chapter.Label+" "+chapter.Title), escape(relative(p.Output, chapter.Output)), current, chapter.Chapter, escape(chapter.Title))
		}
		out.WriteString("</ol></details>\n")
	}
	out.WriteString(`<p class="contents-section-label">The reference library</p>` + "\n")
	for _, name := range []string{"index", "characters", "places", "timeline", "world", "continuity", "source-map", "chapter-reviews"} {
		entry := b.pageBySource["docs/knowledge/"+name+".md"]
		label := entry.Title
		if name == "index" {
			label = "Explore the novel"
		}
		if name == "world" {
			label = "World and technology"
		}
		if name == "continuity" {
			label = "Continuity and open questions"
		}
		if name == "chapter-reviews" {
			label = "Chapter evidence"
		}
		link(entry, label, "contents-link")
	}
	return out.String()
}
func (b *builder) paging(p *page) string {
	var previous, next *page
	if p.Kind == "overview" {
		next = b.chapters[0]
	}
	if p.Kind == "chapter" {
		for i, c := range b.chapters {
			if c != p {
				continue
			}
			if i == 0 {
				previous = b.pages[0]
			} else {
				previous = b.chapters[i-1]
			}
			if i+1 < len(b.chapters) {
				next = b.chapters[i+1]
			} else {
				next = b.pageBySource["docs/knowledge/index.md"]
			}
		}
	}
	if p.Kind == "catalog" {
		previous = b.pages[0]
		if p.ID != "catalog-index" {
			next = b.pageBySource["docs/knowledge/index.md"]
		}
	}
	var out strings.Builder
	for _, item := range []struct {
		destination      *page
		class, direction string
	}{{previous, "previous", "Previous"}, {next, "next", "Next"}} {
		if item.destination == nil {
			continue
		}
		label := item.destination.Title
		if item.destination.Kind == "chapter" {
			label = item.destination.Label + " · " + label
		}
		if item.destination.Kind == "overview" {
			label = "Overview"
		}
		fmt.Fprintf(&out, `<a class="%s" href="%s"><span class="page-direction">%s</span><span class="page-name">%s</span></a>`+"\n", item.class, escape(relative(p.Output, item.destination.Output)), item.direction, escape(label))
	}
	return out.String()
}
func (b *builder) renderPages() {
	template := string(b.read("web/template.html"))
	for _, p := range b.pages {
		root := relative(p.Output, "index.html")
		root = strings.TrimSuffix(root, "index.html")
		if root == "" {
			root = "./"
		}
		download := ""
		if p.Kind == "overview" {
			data := b.read("web/" + koboDownload)
			epub := b.read("web/" + epubDownload)
			require(bytes.HasPrefix(data, []byte("PK\x03\x04")), "Kobo download is not a ZIP publication")
			require(bytes.HasPrefix(epub, []byte("PK\x03\x04")), "EPUB download is not a ZIP publication")
			download = fmt.Sprintf(`<section class="ebook-downloads" aria-labelledby="offline-title"><h2 id="offline-title">Read offline</h2><p class="download-details">Take all 73 chapter summaries and the reference catalog with you.</p><div class="download-options"><a class="download-button" data-format="epub" href="%s" download="%s" type="application/epub+zip" hx-boost="false">Download EPUB <span aria-hidden="true">↓</span></a><a class="download-button" data-format="kepub" href="%s" download="%s" type="application/epub+zip" hx-boost="false">Download Kobo EPUB <span aria-hidden="true">↓</span></a></div><p class="download-details">EPUB: %d KB. Kobo EPUB: %d KB. Choose Kobo EPUB for Kobo eReaders.</p></section>`, escape(relative(p.Output, epubDownload)), epubDownload, escape(relative(p.Output, koboDownload)), koboDownload, (len(epub)+1023)/1024, (len(data)+1023)/1024)
		}
		values := map[string]string{"SITE_ROOT": escape(root), "PAGE_ID": escape(p.ID), "PAGE_KIND": escape(p.Kind), "PAGE_TITLE": escape(p.Title), "PAGE_LABEL": escape(p.Label), "PAGE_DESCRIPTION": escape(p.Description), "NAV_HTML": b.navigation(p), "CONTENT_HTML": b.renderBody(p), "PREV_NEXT_HTML": b.paging(p), "DOWNLOAD_HTML": download}
		var replacements []string
		for _, key := range []string{"SITE_ROOT", "PAGE_ID", "PAGE_KIND", "PAGE_TITLE", "PAGE_LABEL", "PAGE_DESCRIPTION", "NAV_HTML", "CONTENT_HTML", "PREV_NEXT_HTML", "DOWNLOAD_HTML"} {
			require(strings.Contains(template, "{{"+key+"}}"), "template missing %s", key)
			replacements = append(replacements, "{{"+key+"}}", values[key])
		}
		result := strings.NewReplacer(replacements...).Replace(template)
		require(!strings.Contains(result, "{{"), "unresolved template placeholder in %s", p.Output)
		b.files[p.Output] = []byte(result)
	}
}
func (b *builder) copyTree(input, output string, allowed func(string) bool) {
	must(filepath.WalkDir(filepath.Join(b.root, input), func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		require(entry.Type()&os.ModeSymlink == 0, "symlink in public input: %s", full)
		rel, err := filepath.Rel(filepath.Join(b.root, input), full)
		must(err)
		rel = filepath.ToSlash(rel)
		if allowed(rel) {
			b.files[output+"/"+rel] = b.read(input + "/" + rel)
		}
		return nil
	}))
}

const koboDownload = "snegov-dictator-kepub.kepub"
const epubDownload = "snegov-dictator.epub"

func (b *builder) bundleEvidence() {
	b.copyTree("web/assets", "assets", func(name string) bool { return !strings.HasPrefix(path.Base(name), ".") })
	b.files[koboDownload] = b.read("web/" + koboDownload)
	b.files[epubDownload] = b.read("web/" + epubDownload)
	allowed := map[string]bool{".html": true, ".xhtml": true, ".css": true, ".opf": true, ".ncx": true, ".xml": true, ".jpeg": true}
	b.copyTree("src", "source", func(name string) bool { return allowed[path.Ext(name)] || name == "mimetype" })
	for name, want := range b.book.Inventory.Hashes {
		data := b.read(name)
		require(digest(data) == want, "source hash mismatch: %s", name)
	}
	for _, record := range b.book.Chapters {
		require(record.Path == "tmp/chapter-text/"+record.ID+".txt", "unexpected cache path %s", record.Path)
		b.files["evidence/chapter-text/"+record.ID+".txt"] = b.read(record.Path)
	}
	require(b.book.Front.Path == "tmp/chapter-text/front-matter.txt", "unexpected front matter path")
	b.files["evidence/chapter-text/front-matter.txt"] = b.read(b.book.Front.Path)
	for _, name := range []string{"source-map.json", "source-coverage.tsv"} {
		b.files["evidence/"+name] = b.read("docs/knowledge/" + name)
	}
	// Keep only files reached by the reader, plus the HTMX distribution license.
	keep := map[string]bool{}
	queue := []string{"assets/HTMX-LICENSE.txt"}
	for _, p := range b.pages {
		queue = append(queue, p.Output)
	}
	cssURL := regexp.MustCompile(`url\(\s*["']?([^"')\s]+)["']?\s*\)`)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if keep[name] {
			continue
		}
		data, exists := b.files[name]
		require(exists, "public dependency is missing: %s", name)
		keep[name] = true
		var links []string
		switch path.Ext(name) {
		case ".html", ".xhtml", ".xml", ".ncx", ".opf", ".svg":
			links = inspectHTML(data).links
		case ".css":
			for _, match := range cssURL.FindAllSubmatch(data, -1) {
				links = append(links, string(match[1]))
			}
		}
		for _, link := range links {
			u, err := url.Parse(link)
			must(err)
			if u.IsAbs() || u.Host != "" || u.Path == "" {
				continue
			}
			require(!strings.HasPrefix(u.Path, "/"), "root-relative public dependency: %s", link)
			queue = append(queue, path.Clean(path.Join(path.Dir(name), u.Path)))
		}
	}
	for name := range b.files {
		if !keep[name] {
			delete(b.files, name)
		}
	}
	b.files["README.md"] = []byte(siteReadme)
}

type htmlInfo struct {
	ids   map[string]bool
	links []string
}

func inspectHTML(data []byte) htmlInfo {
	info := htmlInfo{ids: map[string]bool{}}
	htmlStarts(string(data), func(start xml.StartElement) {
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "id":
				require(!info.ids[attr.Value], "duplicate HTML id %q", attr.Value)
				info.ids[attr.Value] = true
			case "name":
				if start.Name.Local == "a" {
					info.ids[attr.Value] = true
				}
			case "href", "src":
				info.links = append(info.links, attr.Value)
			}
		}
	})
	return info
}
func (b *builder) verify() int {
	parsed := map[string]htmlInfo{}
	for name, data := range b.files {
		switch path.Ext(name) {
		case ".html", ".xhtml", ".xml", ".ncx", ".opf", ".svg":
			parsed[name] = inspectHTML(data)
		}
	}
	checked := 0
	for name, info := range parsed {
		for _, target := range info.links {
			u, err := url.Parse(target)
			must(err)
			if u.IsAbs() || u.Host != "" {
				continue
			}
			require(!strings.HasPrefix(u.Path, "/"), "root-relative URL %s in %s", target, name)
			destination := name
			if u.Path != "" {
				destination = path.Clean(path.Join(path.Dir(name), u.Path))
			}
			_, exists := b.files[destination]
			require(exists, "missing link target: %s → %s", name, target)
			if u.Fragment != "" {
				other, ok := parsed[destination]
				require(ok && other.ids[u.Fragment], "missing anchor: %s → %s", name, target)
			}
			checked++
		}
	}
	for _, p := range b.chapters {
		require(parsed[p.Output].ids[p.ID], "chapter anchor missing: %s", p.ID)
	}
	for name, want := range b.inputs {
		data, err := os.ReadFile(filepath.Join(b.root, name))
		must(err)
		require(digest(data) == want, "input changed during build: %s", name)
	}
	return checked
}

func ensureSafePath(root, name string) {
	require(name != "." && name != ".." && path.Clean(name) == name && !path.IsAbs(name) && !strings.HasPrefix(name, "../"), "unsafe generated path %q", name)
	current := root
	for _, part := range strings.Split(name, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		must(err)
		require(info.Mode()&os.ModeSymlink == 0, "refusing symlink output %s", current)
	}
}
func writeAtomic(name string, data []byte) {
	must(os.MkdirAll(filepath.Dir(name), 0755))
	file, err := os.CreateTemp(filepath.Dir(name), ".site-build-*")
	must(err)
	temporary := file.Name()
	defer os.Remove(temporary)
	_, err = file.Write(data)
	must(err)
	must(file.Chmod(0644))
	must(file.Close())
	must(os.Rename(temporary, name))
}
func (b *builder) save(checkOnly bool) {
	output := filepath.Join(b.root, "site")
	ensureSafePath(b.root, "site")
	old := manifest{Format: "dictator-static-site-v1", Files: map[string]string{}}
	manifestPath := filepath.Join(output, ".generated-manifest.json")
	ensureSafePath(output, ".generated-manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		must(json.Unmarshal(data, &old))
		require(old.Format == "dictator-static-site-v1", "unrecognized site manifest")
	} else {
		require(os.IsNotExist(err), "read site manifest: %v", err)
	}
	next := manifest{Format: "dictator-static-site-v1", Files: map[string]string{}}
	for name, data := range b.files {
		next.Files[name] = digest(data)
	}
	for name, want := range next.Files {
		ensureSafePath(output, name)
		existing, err := os.ReadFile(filepath.Join(output, name))
		if os.IsNotExist(err) {
			require(!checkOnly, "generated file missing: %s", name)
			continue
		}
		must(err)
		got := digest(existing)
		require(!checkOnly || got == want, "generated file is stale: %s", name)
		prior, managed := old.Files[name]
		require(got == want || (managed && got == prior), "refusing to replace modified or unmanaged file: site/%s", name)
	}
	for name, prior := range old.Files {
		ensureSafePath(output, name)
		if _, keep := next.Files[name]; keep {
			continue
		}
		existing, err := os.ReadFile(filepath.Join(output, name))
		if os.IsNotExist(err) {
			continue
		}
		must(err)
		require(!checkOnly, "stale managed file: %s", name)
		require(digest(existing) == prior, "refusing to remove modified file: site/%s", name)
	}
	if checkOnly {
		return
	}
	names := make([]string, 0, len(b.files))
	for name := range b.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writeAtomic(filepath.Join(output, name), b.files[name])
	}
	for name := range old.Files {
		if _, keep := next.Files[name]; !keep {
			err := os.Remove(filepath.Join(output, name))
			if !os.IsNotExist(err) {
				must(err)
			}
		}
	}
	data, err := json.MarshalIndent(next, "", "  ")
	must(err)
	writeAtomic(manifestPath, append(data, '\n'))
}
func (b *builder) publishList(checkOnly bool) {
	var names []string
	for name := range b.files {
		if name != "README.md" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	data := []byte(strings.Join(names, "\n") + "\n")
	name := "tmp/site/public-files.txt"
	ensureSafePath(b.root, name)
	full := filepath.Join(b.root, name)
	if checkOnly {
		actual, err := os.ReadFile(full)
		must(err)
		require(bytes.Equal(data, actual), "publication file list is stale")
	} else {
		writeAtomic(full, data)
	}
}

func main() {
	defer func() {
		if err := recover(); err != nil {
			fmt.Fprintln(os.Stderr, "build-site:", err)
			os.Exit(1)
		}
	}()
	checkOnly := flag.Bool("check", false, "verify generated files without writing them")
	root := flag.String("root", ".", "repository root (output is always its site/ directory)")
	flag.Parse()
	require(flag.NArg() == 0, "unexpected arguments")
	absolute, err := filepath.Abs(*root)
	must(err)
	resolved, err := filepath.EvalSymlinks(absolute)
	must(err)
	require(resolved != "/" && resolved != os.Getenv("HOME"), "unsafe repository root")
	b := &builder{root: resolved, parts: map[int]string{}, pageBySource: map[string]*page{}, summaryAnchors: map[string]*page{}, files: map[string][]byte{}, inputs: map[string]string{}}
	b.loadPages()
	b.renderPages()
	b.bundleEvidence()
	links := b.verify()
	b.save(*checkOnly)
	b.publishList(*checkOnly)
	action := "Built"
	if *checkOnly {
		action = "Verified"
	}
	fmt.Printf("%s %d complete reader pages (73 chapters, 14 reference pages, 1 overview), %d public files; %d local links and anchors checked. Source hashes unchanged. Publication list: tmp/site/public-files.txt\n", action, len(b.pages), len(b.files)-1, links)
}

const catalogLanding = `# Explore the novel

Follow the people, places, and ideas that connect all six parts of Sergei Snegov’s *Dictator*. This reference covers the full novel and contains spoilers.

## People, places, and ideas

- [Characters and relationships](characters.md): identities, alliances, changing offices, and private loyalties.
- [Places and political geography](places.md): countries, cities, battlefields, and the movement of the war.
- [Timeline and events](timeline.md): the sequence of events, recollections, and later revelations.
- [World and technology](world.md): institutions, energy-water, weather warfare, and the rules that shape the conflict.
- [Continuity and open questions](continuity.md): apparent contradictions, deception, later explanations, and unresolved evidence.

## Read the evidence

The [source and chapter map](source-map.md) connects every chapter to the supplied Russian text and a numbered paragraph copy. The [chapter evidence](chapter-reviews.md) follows the book’s discoveries in order. These references distinguish observed events, testimony, interpretation, and uncertainty.

- [Part I evidence](parts/part-1-ledger.md)
- [Part II evidence](parts/part-2-ledger.md)
- [Part III evidence](parts/part-3-ledger.md)
- [Part IV evidence](parts/part-4-ledger.md)
- [Part V evidence](parts/part-5-ledger.md)
- [Part VI evidence](parts/part-6-ledger.md)

Return to the [introduction](../../DICTATOR_SUMMARY.md) or begin with [Part I, Chapter 1](../../DICTATOR_SUMMARY.md#p1-c01).
`

const siteReadme = `# Dictator static reading companion

This directory is a self-contained static website. It contains the introduction,
all 73 chapter summaries, 14 reference pages, and the public book evidence.
Every page works with normal links and without JavaScript. HTMX adds partial
page navigation when a web server is available.

## Build

From the repository root, with Go and Pandoc installed:

    go run scripts/build-kepub.go
    go run scripts/build-site.go
    go run scripts/build-site.go -check

The builder reads DICTATOR_SUMMARY.md, docs/knowledge/*.md, the six part ledgers,
docs/knowledge/source-map.json, web/template.html, and web/assets/. It copies
the public Russian EPUB source from src/, the named paragraph caches from
tmp/chapter-text/, and source-map JSON and TSV evidence. It never copies the
repository metadata or operational records. The canonical Markdown and source
files remain unchanged. A reader-facing reference landing replaces the catalog
maintenance instructions only in this generated website.

The build checks the exact chapter order, source hashes, local link targets,
fragment anchors, and generated duplicate IDs. The manifest tracks only files
owned by the builder. Modified or conflicting output files cause a failure;
unrelated files are preserved. Output is restricted to the repository's site/
directory. Do not edit generated files; update their source and rebuild.

## Preview

From the repository root:

    go run scripts/serve-site.go

Open http://127.0.0.1:4173/. Use -root site or -addr 127.0.0.1:4174 to select
another folder or local port. Press Ctrl-C to stop the preview server.

## Static hosting

Use the sorted paths in tmp/site/public-files.txt (relative to this directory)
to copy only public files to a static host. The list excludes this README and
the internal build manifest. It includes every reader page, each linked asset
or evidence file, and the HTMX distribution license. No runtime backend,
package manager, JavaScript build tool, or external asset service is required.
Relative URLs support hosting at either a domain root or a directory prefix.
The overview has EPUB and Kobo KEPUB downloads near the end of the page. Their
canonical files are in web/ and are copied into this directory by the builder.
The required Kobo download extension for this project is .kepub. The filename
is snegov-dictator-kepub.kepub; kepubify uses --calibre to retain that extension.
Do not add a single-page-app rewrite rule: each HTML address is a real file.
Opening index.html directly also supports normal reading and navigation;
HTMX enhancement needs HTTP. This build does not publish or deploy the site.

The local HTMX license is in assets/HTMX-LICENSE.txt. Its version record remains
in the repository at web/assets/HTMX-VERSION.txt. Original book source files are bundled unchanged in
source/; paragraph caches and machine-readable source evidence are in evidence/.
`
