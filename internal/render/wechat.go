package render

import (
	"fmt"
	"strings"

	"github.com/vanng822/go-premailer/premailer"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// wechatCSSAllowlist is the set of CSS properties WeChat's editor keeps.
// Layout properties (position, float, flex, grid) are dropped rather than
// shipped broken, since WeChat handles them inconsistently at best.
var wechatCSSAllowlist = map[string]bool{
	"color": true, "background": true, "background-color": true,
	"font-size": true, "font-weight": true, "font-style": true,
	"font-family": true, "line-height": true, "letter-spacing": true,
	"text-align": true, "text-decoration": true, "text-indent": true,
	"margin": true, "margin-top": true, "margin-right": true,
	"margin-bottom": true, "margin-left": true,
	"padding": true, "padding-top": true, "padding-right": true,
	"padding-bottom": true, "padding-left": true,
	"border": true, "border-top": true, "border-right": true,
	"border-bottom": true, "border-left": true, "border-radius": true,
	"border-collapse": true, "border-color": true, "border-width": true,
	"border-style": true,
	"max-width":    true, "width": true, "height": true,
	"overflow-x": true, "word-break": true, "white-space": true,
	"vertical-align": true, "display": true,
}

// droppedTags are removed entirely, contents and all.
var droppedTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Link: true,
	atom.Meta: true, atom.Iframe: true, atom.Object: true,
}

// ToWeChat produces HTML for the WeChat article editor.
//
// WeChat strips <style> blocks, so the theme is inlined onto each element
// first, and the result is then reduced to the subset of markup and CSS the
// editor actually preserves.
func ToWeChat(body string, theme Theme) (string, error) {
	doc := fmt.Sprintf("<html><head><style>%s</style></head><body><div class=\"cw-root\">%s</div></body></html>",
		theme.CSS, body)

	p, err := premailer.NewPremailerFromString(doc, &premailer.Options{
		RemoveClasses:     false, // handled below, after inlining
		CssToAttributes:   false,
		KeepBangImportant: false,
	})
	if err != nil {
		return "", fmt.Errorf("inline css: %w", err)
	}
	inlined, err := p.Transform()
	if err != nil {
		return "", fmt.Errorf("inline css: %w", err)
	}

	cleaned, err := sanitizeWeChat(inlined)
	if err != nil {
		return "", err
	}
	return cleaned, nil
}

// sanitizeWeChat rewrites the inlined document into WeChat-safe markup.
func sanitizeWeChat(doc string) (string, error) {
	node, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	body := findBody(node)
	if body == nil {
		return "", fmt.Errorf("rendered document has no body")
	}
	cleanNode(body)

	var sb strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&sb, c); err != nil {
			return "", fmt.Errorf("render html: %w", err)
		}
	}
	return sb.String(), nil
}

func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == atom.Body {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findBody(c); found != nil {
			return found
		}
	}
	return nil
}

// cleanNode walks the tree applying the WeChat rules in place.
func cleanNode(n *html.Node) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling // captured now: c may be removed below

		if c.Type == html.CommentNode {
			n.RemoveChild(c)
			continue
		}
		if c.Type != html.ElementNode {
			continue
		}
		if droppedTags[c.DataAtom] {
			n.RemoveChild(c)
			continue
		}

		// WeChat's editor mangles <div>; <section> is what every WeChat
		// typesetting tool emits instead.
		if c.DataAtom == atom.Div {
			c.Data = "section"
			c.DataAtom = 0
		}

		c.Attr = filterAttrs(c.Attr)
		cleanNode(c)
	}
}

func filterAttrs(attrs []html.Attribute) []html.Attribute {
	out := attrs[:0]
	for _, a := range attrs {
		key := strings.ToLower(a.Key)
		switch {
		case key == "class" || key == "id":
			// Dead weight once styles are inlined, and WeChat may filter on them.
			continue
		case strings.HasPrefix(key, "on"):
			continue // event handlers
		case key == "style":
			if v := filterStyle(a.Val); v != "" {
				out = append(out, html.Attribute{Key: "style", Val: v})
			}
			continue
		case key == "href" || key == "src":
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a.Val)), "javascript:") {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// filterStyle keeps only the declarations WeChat honors.
func filterStyle(style string) string {
	var kept []string
	for _, decl := range strings.Split(style, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		name, value, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if !wechatCSSAllowlist[name] {
			continue
		}
		kept = append(kept, name+": "+strings.TrimSpace(value))
	}
	return strings.Join(kept, "; ")
}
