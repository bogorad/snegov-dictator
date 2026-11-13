// Independent audit of the EPUB source map and extracted caches.
// Run from the repository root: go run scripts/audit-book-source.go
// Uses encoding/xml, not the regular expressions in scripts/book-source.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type nav struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []nav `xml:"navPoint"`
}
type chapter struct {
	ID          string         `json:"id"`
	Part        int            `json:"part"`
	Chapter     int            `json:"chapter"`
	Label       string         `json:"label"`
	Source      string         `json:"source"`
	Href        string         `json:"href"`
	Anchor      string         `json:"anchor"`
	FI          int            `json:"fi"`
	Offset      int            `json:"offset"`
	Start       int            `json:"start_global"`
	End         int            `json:"end_global"`
	Paragraphs  int            `json:"paragraphs"`
	TextBytes   int            `json:"text_bytes"`
	SourceBytes int            `json:"source_bytes"`
	OutputBytes int            `json:"output_bytes"`
	Path        string         `json:"path"`
	Files       map[string]int `json:"file_counts"`
}
type part struct {
	Part        int       `json:"part"`
	Title       string    `json:"title"`
	SourceTitle string    `json:"source_title"`
	Source      string    `json:"source"`
	Chapters    []chapter `json:"chapters"`
}
type sourceMap struct {
	Parts    []part    `json:"parts"`
	Chapters []chapter `json:"chapters"`
	Front    struct {
		Path       string         `json:"path"`
		Paragraphs int            `json:"paragraphs"`
		Files      map[string]int `json:"files"`
	} `json:"front_matter"`
	Inventory struct {
		Spine      int               `json:"spine_files"`
		Paragraphs int               `json:"source_paragraphs"`
		TextBytes  int               `json:"source_text_bytes"`
		Hashes     map[string]string `json:"source_hashes_sha256"`
	} `json:"inventory"`
}
type block struct {
	File, Text, Owner string
	FI, Start, End    int
}
type anchor struct{ TagOffset, AttributeOffset int }
type source struct {
	Href    string
	Raw     []byte
	Blocks  []block
	Anchors map[string]anchor
}
type event struct {
	FI, Offset int
	Owner      string
}

var failures []string

func check(ok bool, msg string) {
	if !ok {
		failures = append(failures, msg)
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func read(path string) []byte { b, e := os.ReadFile(path); must(e); return b }
func digest(b []byte) string  { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

var spaces = regexp.MustCompile(`[\t\r\n ]+`)

// Normalize only layout whitespace. Preserve source Unicode spaces, including
// the leading nonbreaking space in P4-C15-P0046.
func normalize(s string) string { return strings.Trim(spaces.ReplaceAllString(s, " "), " \t\r\n") }
func parseHTML(href string, fi int) source {
	s := source{Href: href, Raw: read("src/" + href), Anchors: map[string]anchor{}}
	check(utf8.Valid(s.Raw), "invalid source UTF-8: "+href)
	d := xml.NewDecoder(bytes.NewReader(s.Raw))
	d.Entity = map[string]string{"nbsp": "\u00a0"}
	insideBody, insideP := false, false
	var current block
	for {
		before := int(d.InputOffset())
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		must(e)
		after := int(d.InputOffset())
		switch v := t.(type) {
		case xml.StartElement:
			if v.Name.Local == "body" {
				insideBody = true
			}
			for _, a := range v.Attr {
				if a.Name.Local == "id" || a.Name.Local == "name" {
					_, dup := s.Anchors[a.Value]
					check(!dup, "duplicate anchor: "+href+"#"+a.Value)
					rawTag := s.Raw[before:after]
					pat := regexp.MustCompile(`\b` + a.Name.Local + `\s*=\s*["']` + regexp.QuoteMeta(a.Value) + `["']`)
					loc := pat.FindIndex(rawTag)
					if loc == nil {
						panic("anchor byte offset unavailable")
					}
					s.Anchors[a.Value] = anchor{before, before + loc[0]}
				}
			}
			if v.Name.Local == "p" {
				check(insideBody && !insideP, "invalid p scope: "+href)
				insideP = true
				current = block{File: href, FI: fi, Start: before}
			}
		case xml.EndElement:
			if v.Name.Local == "p" {
				check(insideP, "unmatched p: "+href)
				current.End = after
				current.Text = normalize(current.Text)
				s.Blocks = append(s.Blocks, current)
				insideP = false
			}
			if v.Name.Local == "body" {
				insideBody = false
			}
		case xml.CharData:
			if insideP {
				current.Text += string(v)
			} else if insideBody {
				check(strings.TrimSpace(string(v)) == "", "body prose outside p: "+href+" at "+strconv.Itoa(before))
			}
		}
	}
	return s
}

func main() {
	var opf struct {
		Items []struct {
			ID   string `xml:"id,attr"`
			Href string `xml:"href,attr"`
			Type string `xml:"media-type,attr"`
		} `xml:"manifest>item"`
		Refs []struct {
			ID string `xml:"idref,attr"`
		} `xml:"spine>itemref"`
	}
	must(xml.Unmarshal(read("src/content.opf"), &opf))
	manifest := map[string]string{}
	htmlManifest := map[string]bool{}
	for _, v := range opf.Items {
		_, dup := manifest[v.ID]
		check(!dup, "duplicate manifest id")
		manifest[v.ID] = v.Href
		if v.Type == "application/xhtml+xml" {
			htmlManifest[v.Href] = true
		}
	}
	var sources []source
	fi := map[string]int{}
	htmlSpine := map[string]bool{}
	var all []block
	for i, r := range opf.Refs {
		h, ok := manifest[r.ID]
		check(ok, "spine id missing from manifest")
		check(!htmlSpine[h], "duplicate spine href")
		htmlSpine[h] = true
		fi[h] = i
		s := parseHTML(h, i)
		sources = append(sources, s)
		all = append(all, s.Blocks...)
	}
	physical := map[string]bool{}
	must(filepath.WalkDir("src", func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && (strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".xhtml")) {
			h, e := filepath.Rel("src", path)
			if e != nil {
				return e
			}
			physical[h] = true
		}
		return nil
	}))
	check(reflect.DeepEqual(physical, htmlManifest), "physical HTML set differs from OPF manifest")
	check(reflect.DeepEqual(physical, htmlSpine), "physical HTML set differs from OPF spine")
	var ncx struct {
		Parts []nav `xml:"navMap>navPoint"`
	}
	must(xml.Unmarshal(read("src/toc.ncx"), &ncx))
	locate := func(src string) (int, string, string, int, int) {
		h, a, _ := strings.Cut(src, "#")
		i, ok := fi[h]
		if !ok {
			panic("NCX path absent from spine: " + h)
		}
		off, attr := 0, 0
		if a != "" {
			v, ok := sources[i].Anchors[a]
			if !ok {
				panic("NCX anchor absent: " + src)
			}
			off, attr = v.TagOffset, v.AttributeOffset
		}
		return i, h, a, off, attr
	}
	var events []event
	var expected []chapter
	var counts []int
	for pi, p := range ncx.Parts {
		check(len(p.Children) > 0, "unexpected leaf NCX root")
		i, _, _, off, _ := locate(p.Content.Src)
		events = append(events, event{i, off, "FRONT"})
		counts = append(counts, len(p.Children))
		for ci, c := range p.Children {
			check(len(c.Children) == 0, "unexpected NCX depth")
			check(c.Label == strconv.Itoa(ci+1), "NCX chapter label is not sequential")
			i, h, a, off, attr := locate(c.Content.Src)
			id := fmt.Sprintf("P%d-C%02d", pi+1, ci+1)
			events = append(events, event{i, off, id})
			expected = append(expected, chapter{ID: id, Part: pi + 1, Chapter: ci + 1, Label: c.Label, Source: "src/" + c.Content.Src, Href: h, Anchor: a, FI: i, Offset: attr, Start: -1, Files: map[string]int{}, Path: "tmp/chapter-text/" + id + ".txt"})
		}
	}
	check(reflect.DeepEqual(counts, []int{10, 9, 13, 15, 11, 15}), "NCX part counts differ")
	check(len(expected) == 73 && len(sources) == 73 && len(all) == 6109, "source inventory totals differ")
	check(len(sources[0].Blocks) == 0 && sources[0].Href == "titlepage.xhtml", "cover has unexpected body text")
	check(sort.SliceIsSorted(events, func(i, j int) bool {
		return events[i].FI < events[j].FI || (events[i].FI == events[j].FI && events[i].Offset < events[j].Offset)
	}), "NCX order differs from spine/anchor order")
	byID := map[string]*chapter{}
	for i := range expected {
		byID[expected[i].ID] = &expected[i]
	}
	rows := map[string][]block{}
	state := "FRONT"
	eventIndex := 0
	textBytes, empty, nbsp := 0, 0, 0
	for i := range all {
		p := &all[i]
		for eventIndex < len(events) && (events[eventIndex].FI < p.FI || (events[eventIndex].FI == p.FI && events[eventIndex].Offset <= p.Start)) {
			state = events[eventIndex].Owner
			eventIndex++
		}
		p.Owner = state
		rows[state] = append(rows[state], *p)
		textBytes += len(p.Text)
		nbsp += strings.Count(p.Text, "\u00a0")
		if p.Text == "" {
			empty++
		}
		if c := byID[state]; c != nil {
			if c.Start < 0 {
				c.Start = i
			}
			c.Paragraphs++
			c.TextBytes += len(p.Text)
			c.SourceBytes += p.End - p.Start
			c.Files[p.File]++
		}
	}
	var m sourceMap
	must(json.Unmarshal(read("docs/knowledge/source-map.json"), &m))
	check(len(m.Chapters) == len(expected), "map chapter count differs")
	check(len(m.Parts) == len(ncx.Parts), "map part count differs")
	check(m.Inventory.Spine == len(sources) && m.Inventory.Paragraphs == len(all) && m.Inventory.TextBytes == textBytes, "map inventory counts/bytes differ")
	check(len(m.Inventory.Hashes) == len(sources), "source hash set size differs")
	for _, s := range sources {
		check(m.Inventory.Hashes["src/"+s.Href] == digest(s.Raw), "source hash differs: "+s.Href)
	}
	for i := range expected {
		c := &expected[i]
		c.End = len(all)
		if i+1 < len(expected) {
			c.End = expected[i+1].Start
		}
		var buf strings.Builder
		fmt.Fprintf(&buf, "# %s | part %d chapter %d\n# source: %s\n", c.ID, c.Part, c.Chapter, c.Source)
		for j, p := range rows[c.ID] {
			a := ""
			if c.Anchor != "" {
				a = "#" + c.Anchor
			}
			fmt.Fprintf(&buf, "%s-P%04d\t%s%s\t%s\n", c.ID, j+1, p.File, a, p.Text)
		}
		c.OutputBytes = buf.Len()
		actual := read(c.Path)
		check(utf8.Valid(actual), "invalid cache UTF-8: "+c.ID)
		check(bytes.Equal(actual, []byte(buf.String())), "cache byte/content/order differs: "+c.ID)
		if !bytes.Equal(actual, []byte(buf.String())) {
			wantLines := strings.Split(buf.String(), "\n")
			gotLines := strings.Split(string(actual), "\n")
			for n, w := range wantLines {
				if n >= len(gotLines) {
					break
				}
				g := gotLines[n]
				if g != w {
					k := 0
					for k < len(w) && k < len(g) && w[k] == g[k] {
						k++
					}
					a := k - 24
					if a < 0 {
						a = 0
					}
					b := k + 40
					if b > len(w) {
						b = len(w)
					}
					z := k + 40
					if z > len(g) {
						z = len(g)
					}
					fmt.Fprintf(os.Stderr, "%s line %d byte %d: expected %q, actual %q\n", c.ID, n+1, k, w[a:b], g[a:z])
				}
			}
		}
		check(len(rows[c.ID]) > 0 && rows[c.ID][0].Text == c.Label, "chapter starts with wrong numbered heading: "+c.ID)
		if i < len(m.Chapters) {
			check(reflect.DeepEqual(*c, m.Chapters[i]), "map chapter metadata differs: "+c.ID)
		}
	}
	english := []string{"Part One — Drive to Power", "Part Two — Sacred Terror", "Part Three — Turning Point", "Part Four — Offensive", "Part Five — The Devil of a God Not Ours", "Part Six — Cleansing"}
	for i, p := range m.Parts {
		if i >= len(ncx.Parts) {
			break
		}
		check(p.Part == i+1, "part number differs")
		check(p.Source == "src/"+ncx.Parts[i].Content.Src, "part source differs")
		check(p.Title == english[i] && p.SourceTitle == ncx.Parts[i].Label, "part title encoding or source title differs: "+strconv.Itoa(i+1))
		var children []chapter
		for _, c := range expected {
			if c.Part == i+1 {
				children = append(children, c)
			}
		}
		check(reflect.DeepEqual(p.Chapters, children), "part chapter copies differ: "+strconv.Itoa(i+1))
	}
	frontBytes := 0
	frontFiles := map[string]int{}
	var front strings.Builder
	front.WriteString("# Front matter and non-chapter source paragraphs\n")
	for i, p := range rows["FRONT"] {
		fmt.Fprintf(&front, "FRONT-P%04d\t%s\t%s\n", i+1, p.File, p.Text)
		frontBytes += len(p.Text)
		frontFiles[p.File]++
	}
	check(len(rows["FRONT"]) == 14 && frontBytes == 326, "front counts/prose-byte total differs")
	check(m.Front.Path == "tmp/chapter-text/front-matter.txt" && m.Front.Paragraphs == len(rows["FRONT"]) && reflect.DeepEqual(m.Front.Files, frontFiles), "front metadata differs")
	check(bytes.Equal(read("tmp/chapter-text/front-matter.txt"), []byte(front.String())), "front cache bytes/content/order differ")
	wantCache := map[string]bool{"front-matter.txt": true}
	for _, c := range expected {
		wantCache[c.ID+".txt"] = true
	}
	gotCache := map[string]bool{}
	paths, e := filepath.Glob("tmp/chapter-text/*.txt")
	must(e)
	for _, p := range paths {
		gotCache[filepath.Base(p)] = true
	}
	check(reflect.DeepEqual(wantCache, gotCache), "cache filename set differs")
	var cov strings.Builder
	cov.WriteString("kind\tid\tsource\tparagraphs\ttext_bytes\toutput\n")
	for _, c := range expected {
		fmt.Fprintf(&cov, "chapter\t%s\t%s\t%d\t%d\t%s\n", c.ID, c.Source, c.Paragraphs, c.TextBytes, c.Path)
	}
	fmt.Fprintf(&cov, "front\tFRONT\tspine paragraphs outside chapter boundaries\t%d\t%d\ttmp/chapter-text/front-matter.txt\n", len(rows["FRONT"]), frontBytes)
	fmt.Fprintf(&cov, "inventory\tTOTAL\tspine HTML files\t%d\t%d\t%d source paragraphs\n", len(sources), textBytes, len(all))
	cov.WriteString("paragraph\tglobal\tsource\towner\ttext_sha256\ttext_bytes\n")
	for i, p := range all {
		fmt.Fprintf(&cov, "paragraph\t%d\t%s\t%s\t%s\t%d\n", i, p.File, p.Owner, digest([]byte(p.Text)), len(p.Text))
	}
	check(bytes.Equal(read("docs/knowledge/source-coverage.tsv"), []byte(cov.String())), "coverage TSV bytes/owner/hash/order differ")
	md := string(read("docs/knowledge/source-map.md"))
	for _, c := range expected {
		var names []string
		for n := range c.Files {
			names = append(names, n)
		}
		sort.Strings(names)
		for i, n := range names {
			names[i] = "`src/" + n + "`"
		}
		row := fmt.Sprintf("| <a id=\"p%d-c%02d\"></a>%s | %s | %d | [`%s`](../../%s) | %s | %d | %d | %d | [`%s`](../../%s) |", c.Part, c.Chapter, c.ID, english[c.Part-1], c.Chapter, c.Source, c.Source, strings.Join(names, ", "), c.Paragraphs, c.TextBytes, c.SourceBytes, c.Path, c.Path)
		check(strings.Count(md, row) == 1, "Markdown source row differs: "+c.ID)
	}
	summary := map[string]any{"parts": counts, "chapters": len(expected), "spine_files": len(sources), "source_blocks": len(all), "chapter_blocks": len(all) - len(rows["FRONT"]), "front_blocks": len(rows["FRONT"]), "front_prose_utf8_bytes": frontBytes, "source_prose_utf8_bytes": textBytes, "empty_blocks": empty, "preserved_nonbreaking_spaces": nbsp, "opf_sha256": digest(read("src/content.opf")), "ncx_sha256": digest(read("src/toc.ncx")), "failures": failures}
	for _, id := range []string{"P1-C01", "P1-C02", "P3-C04", "P3-C13", "P5-C08", "P5-C11", "P6-C15"} {
		summary[id] = byID[id]
	}
	b, e := json.MarshalIndent(summary, "", "  ")
	must(e)
	fmt.Println(string(b))
	if len(failures) > 0 {
		os.Exit(1)
	}
}
