package render

import (
	"fmt"
	"strings"
	"sync"

	"github.com/JohannesKaufmann/dom"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
)

// keptStyles are the inline styles that survive the trip back to markdown.
//
// Markdown has no syntax for colour or type size, so text formatted in the
// rich-text editor keeps them as an inline <span style>. Only properties
// the publishing pipeline also honours are kept: anything else would be
// stripped on the way to WeChat anyway, and text pasted from a web page
// arrives wrapped in spans carrying dozens of declarations that would
// otherwise litter the file.
var keptStyles = map[string]bool{
	"color":            true,
	"background-color": true,
	"font-size":        true,
	"font-family":      true,
}

var (
	convOnce sync.Once
	conv     *converter.Converter
)

func markdownConverter() *converter.Converter {
	convOnce.Do(func() {
		conv = converter.NewConverter(converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			// Without these, tables and strikethrough written in rich
			// text were flattened to plain text on save.
			strikethrough.NewStrikethroughPlugin(),
			table.NewTablePlugin(),
		))
		conv.Register.RendererFor("span", converter.TagTypeInline, renderStyledSpan, converter.PriorityEarly)
		conv.Register.RendererFor("font", converter.TagTypeInline, renderFontTag, converter.PriorityEarly)
		conv.Register.RendererFor("u", converter.TagTypeInline, renderUnderline, converter.PriorityEarly)
	})
	return conv
}

// ToMarkdown converts edited rich text back into markdown source.
//
// The file on disk stays markdown regardless of which mode was used to
// write it, so switching modes never changes what is stored.
func ToMarkdown(htmlStr string) (string, error) {
	md, err := markdownConverter().ConvertString(htmlStr)
	if err != nil {
		return "", fmt.Errorf("convert html to markdown: %w", err)
	}
	// contenteditable leaves trailing blank lines as people edit.
	return strings.TrimRight(md, " \n\t") + "\n", nil
}

// FilterStyle keeps only the declarations in keptStyles, normalised.
func FilterStyle(style string) string {
	var kept []string
	for _, decl := range strings.Split(style, ";") {
		name, value, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		// Browsers quote font names with double quotes, which would end the
		// style attribute this is written into; single quotes mean the same.
		value = strings.ReplaceAll(strings.TrimSpace(value), `"`, "'")
		// An angle bracket has no business in any kept property and would
		// break out of the attribute.
		if !keptStyles[name] || value == "" || strings.ContainsAny(value, "<>") {
			continue
		}
		kept = append(kept, name+": "+value)
	}
	return strings.Join(kept, "; ")
}

func renderStyledSpan(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	style := FilterStyle(dom.GetAttributeOr(n, "style", ""))
	if style == "" {
		// Nothing worth keeping: fall through and write the text alone.
		return converter.RenderTryNext
	}
	return wrapInline(ctx, w, n, `<span style="`+style+`">`, "</span>")
}

// renderFontTag maps the legacy <font> that some browsers emit for colour
// and face onto the same span form.
func renderFontTag(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	var decls []string
	if c := dom.GetAttributeOr(n, "color", ""); c != "" {
		decls = append(decls, "color: "+c)
	}
	if f := dom.GetAttributeOr(n, "face", ""); f != "" {
		decls = append(decls, "font-family: "+f)
	}
	style := FilterStyle(strings.Join(decls, "; "))
	if style == "" {
		return converter.RenderTryNext
	}
	return wrapInline(ctx, w, n, `<span style="`+style+`">`, "</span>")
}

// Markdown has no underline, so it is kept as HTML.
func renderUnderline(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	return wrapInline(ctx, w, n, "<u>", "</u>")
}

func wrapInline(ctx converter.Context, w converter.Writer, n *html.Node, open, closeTag string) converter.RenderStatus {
	w.WriteString(open)
	// Children are still converted, so bold inside a coloured span stays
	// markdown bold rather than becoming more HTML.
	ctx.RenderChildNodes(ctx, w, n)
	w.WriteString(closeTag)
	return converter.RenderSuccess
}
