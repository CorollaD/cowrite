// Package export renders a post into the file formats people send to
// other people: Word documents and PDFs alongside markdown and HTML.
package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// DOCX builds a Word document from rendered HTML.
//
// A .docx is a zip of OOXML parts, so it is written directly rather than
// through a library: the ones available either only find-and-replace in an
// existing template, or are licensed in a way that would relicense this
// project.
func DOCX(htmlStr, title string) ([]byte, error) {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var body strings.Builder
	walkDocx(doc, &body, blockCtx{})
	if strings.TrimSpace(body.String()) == "" {
		body.WriteString(docxPara("", runProps{}))
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := map[string]string{
		"[Content_Types].xml": contentTypes,
		"_rels/.rels":         rootRels,
		"word/styles.xml":     docxStyles,
		"word/document.xml": xmlHeader +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:body>` + body.String() +
			`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
			`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>` +
			`</w:body></w:document>`,
	}
	for name, content := range parts {
		w, err := zw.Create(name)
		if err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finish docx: %w", err)
	}
	return buf.Bytes(), nil
}

type runProps struct {
	bold   bool
	italic bool
	mono   bool
}

type blockCtx struct {
	style string // paragraph style id
	runs  runProps
	list  bool
}

// walkDocx turns the HTML tree into OOXML paragraphs.
func walkDocx(n *html.Node, out *strings.Builder, ctx blockCtx) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			// Text directly inside a block is emitted by its parent.
		case html.ElementNode:
			switch c.Data {
			case "h1", "h2", "h3", "h4", "h5", "h6":
				out.WriteString(docxPara(collectText(c), runProps{}, "Heading"+string(c.Data[1])))
			case "p":
				out.WriteString(docxParaRich(c, ctx))
			case "li":
				inner := blockCtx{runs: ctx.runs, list: true}
				out.WriteString(docxParaRich(c, inner))
			case "blockquote":
				walkDocx(c, out, blockCtx{style: "Quote", runs: ctx.runs})
			case "pre":
				for _, line := range strings.Split(collectText(c), "\n") {
					out.WriteString(docxPara(line, runProps{mono: true}, "Code"))
				}
			case "hr":
				out.WriteString(docxPara("", runProps{}))
			case "table":
				// Tables degrade to one paragraph per row; a real w:tbl is
				// more markup than this is worth for an export.
				walkDocxTable(c, out)
			case "ul", "ol", "div", "section", "article", "main", "body", "html", "head":
				walkDocx(c, out, ctx)
			}
		}
	}
}

func walkDocxTable(n *html.Node, out *strings.Builder) {
	var rows func(*html.Node)
	rows = func(x *html.Node) {
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "tr" {
				var cells []string
				for td := c.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == html.ElementNode && (td.Data == "td" || td.Data == "th") {
						cells = append(cells, collectText(td))
					}
				}
				out.WriteString(docxPara(strings.Join(cells, "  |  "), runProps{}))
				continue
			}
			rows(c)
		}
	}
	rows(n)
}

// docxParaRich keeps inline emphasis inside a paragraph.
func docxParaRich(n *html.Node, ctx blockCtx) string {
	var runs strings.Builder
	var walk func(*html.Node, runProps)
	walk = func(x *html.Node, rp runProps) {
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				if c.Data != "" {
					runs.WriteString(docxRun(c.Data, rp))
				}
			case html.ElementNode:
				next := rp
				switch c.Data {
				case "strong", "b":
					next.bold = true
				case "em", "i":
					next.italic = true
				case "code":
					next.mono = true
				case "br":
					runs.WriteString(`<w:r><w:br/></w:r>`)
					continue
				}
				walk(c, next)
			}
		}
	}
	walk(n, ctx.runs)
	if runs.Len() == 0 {
		return ""
	}

	style := ctx.style
	if ctx.list {
		style = "ListParagraph"
	}
	return `<w:p>` + paraProps(style, ctx.list) + runs.String() + `</w:p>`
}

func docxPara(text string, rp runProps, style ...string) string {
	st := ""
	if len(style) > 0 {
		st = style[0]
	}
	return `<w:p>` + paraProps(st, false) + docxRun(text, rp) + `</w:p>`
}

func paraProps(style string, list bool) string {
	if style == "" && !list {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`<w:pPr>`)
	if style != "" {
		sb.WriteString(`<w:pStyle w:val="` + style + `"/>`)
	}
	if list {
		sb.WriteString(`<w:ind w:left="720"/>`)
	}
	sb.WriteString(`</w:pPr>`)
	return sb.String()
}

func docxRun(text string, rp runProps) string {
	var props strings.Builder
	if rp.bold || rp.italic || rp.mono {
		props.WriteString(`<w:rPr>`)
		if rp.bold {
			props.WriteString(`<w:b/>`)
		}
		if rp.italic {
			props.WriteString(`<w:i/>`)
		}
		if rp.mono {
			props.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/>`)
		}
		props.WriteString(`</w:rPr>`)
	}
	// xml:space preserve keeps the spaces around inline emphasis.
	return `<w:r>` + props.String() +
		`<w:t xml:space="preserve">` + escapeXML(text) + `</w:t></w:r>`
}

func collectText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			sb.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func escapeXML(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const contentTypes = xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>`

const rootRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

const docxStyles = xmlHeader + `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr>
<w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="SimSun"/>
<w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/>
<w:pPr><w:spacing w:before="320" w:after="160"/></w:pPr>
<w:rPr><w:b/><w:sz w:val="40"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/>
<w:pPr><w:spacing w:before="280" w:after="140"/></w:pPr>
<w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/>
<w:pPr><w:spacing w:before="240" w:after="120"/></w:pPr>
<w:rPr><w:b/><w:sz w:val="26"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/>
<w:rPr><w:b/><w:sz w:val="24"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/>
<w:pPr><w:ind w:left="720"/></w:pPr><w:rPr><w:i/><w:color w:val="555555"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="HTML Preformatted"/>
<w:pPr><w:ind w:left="360"/></w:pPr>
<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/><w:sz w:val="19"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/>
<w:pPr><w:ind w:left="720"/></w:pPr></w:style>
</w:styles>`
