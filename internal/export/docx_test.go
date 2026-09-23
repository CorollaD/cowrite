package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"testing"
)

// open reads a generated document's main part, which also proves the zip
// is well formed: Word rejects the whole file otherwise.
func open(t *testing.T, data []byte) (string, []string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a readable zip: %v", err)
	}
	var names []string
	var doc string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open document.xml: %v", err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		doc = string(b)
	}
	if doc == "" {
		t.Fatal("document.xml missing")
	}
	if err := xml.Unmarshal([]byte(doc), new(struct {
		XMLName xml.Name
	})); err != nil {
		t.Fatalf("document.xml is not well-formed: %v", err)
	}
	return doc, names
}

var textRe = regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)

func plainText(doc string) string {
	var sb strings.Builder
	for _, m := range textRe.FindAllStringSubmatch(doc, -1) {
		sb.WriteString(m[1])
		sb.WriteString(" ")
	}
	return sb.String()
}

func TestDOCXHasRequiredParts(t *testing.T) {
	data, err := DOCX("<h1>标题</h1><p>正文</p>", "标题")
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	_, names := open(t, data)

	// Word refuses to open a package missing any of these.
	for _, want := range []string{
		"[Content_Types].xml", "_rels/.rels",
		"word/document.xml", "word/styles.xml",
	} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing required part %q", want)
		}
	}
}

func TestDOCXPreservesStructure(t *testing.T) {
	html := `<h1>一级</h1><h2>二级</h2>` +
		`<p>这段有<strong>加粗</strong>和<em>斜体</em>以及<code>代码</code>。</p>` +
		`<ul><li>第一项</li><li>第二项</li></ul>` +
		`<blockquote><p>引用</p></blockquote>` +
		`<pre><code>func main() {}</code></pre>`

	data, err := DOCX(html, "t")
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	doc, _ := open(t, data)

	for name, want := range map[string]string{
		"heading 1":  `w:val="Heading1"`,
		"heading 2":  `w:val="Heading2"`,
		"bold":       `<w:b/>`,
		"italic":     `<w:i/>`,
		"monospace":  `Consolas`,
		"quote":      `w:val="Quote"`,
		"code block": `w:val="Code"`,
		"list":       `ListParagraph`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("%s not represented in the document", name)
		}
	}

	text := plainText(doc)
	for _, want := range []string{"一级", "二级", "加粗", "斜体", "第一项", "引用", "func main()"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q was dropped", want)
		}
	}
}

// Angle brackets and ampersands in prose must not corrupt the XML.
func TestDOCXEscapesMarkupCharacters(t *testing.T) {
	data, err := DOCX(`<p>a &lt; b &amp; c &gt; d</p>`, "t")
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	doc, _ := open(t, data)
	if !strings.Contains(doc, "&lt;") || !strings.Contains(doc, "&amp;") {
		t.Errorf("special characters were not escaped: %s", doc)
	}
}

func TestDOCXHandlesEmptyDocument(t *testing.T) {
	data, err := DOCX("", "t")
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	doc, _ := open(t, data)
	// An empty body is invalid; there must be at least one paragraph.
	if !strings.Contains(doc, "<w:p>") {
		t.Error("empty input produced a document with no paragraph")
	}
}

func TestDOCXKeepsTableText(t *testing.T) {
	data, err := DOCX(
		`<table><tr><th>列A</th><th>列B</th></tr><tr><td>1</td><td>2</td></tr></table>`, "t")
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	doc, _ := open(t, data)
	text := plainText(doc)
	for _, want := range []string{"列A", "列B", "1", "2"} {
		if !strings.Contains(text, want) {
			t.Errorf("table cell %q was dropped", want)
		}
	}
}
