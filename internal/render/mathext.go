package render

import (
	"bytes"
	"html"
	"regexp"
	"strings"
)

// Math and diagram handling runs as a post-processing pass over the
// rendered HTML rather than as goldmark AST extensions.
//
// A parser extension would be tidier, but $...$ overlaps with ordinary
// prose (prices, variables) in ways that are easy to get wrong, and a pass
// over the output lets a failed render fall back to the original source
// without having replaced it in the tree already.

var (
	// A mermaid code block as goldmark emits it, with chroma's inline
	// styles attached.
	mermaidBlockRe = regexp.MustCompile(
		`(?s)<pre[^>]*>\s*<code[^>]*class="[^"]*language-mermaid[^"]*"[^>]*>(.*?)</code>\s*</pre>`)
	// $$...$$ on its own, and inline $...$ that is not a currency amount.
	displayMathRe = regexp.MustCompile(`(?s)\$\$(.+?)\$\$`)
	inlineMathRe  = regexp.MustCompile(`\$([^$\n]+?)\$`)
)

// applyBlocks replaces math and mermaid sources with rendered SVG.
func (r *Renderer) applyBlocks(htmlStr string) string {
	htmlStr = r.applyMermaid(htmlStr)
	htmlStr = r.applyMath(htmlStr)
	return htmlStr
}

func (r *Renderer) applyMermaid(in string) string {
	return mermaidBlockRe.ReplaceAllStringFunc(in, func(match string) string {
		sub := mermaidBlockRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		src := unescapeCode(sub[1])
		key := cacheKey("mermaid", src)
		if svg, ok := r.cache.get(key); ok {
			return svg
		}
		svg, err := renderMermaid(src)
		if err != nil {
			// Keep the source visible: a diagram that fails to lay out is
			// still content the author wrote.
			return match
		}
		wrapped := `<div class="cw-diagram">` + svg + `</div>`
		r.cache.put(key, wrapped)
		return wrapped
	})
}

func (r *Renderer) applyMath(in string) string {
	// Skip anything inside a code block: $ is ordinary text there.
	parts := splitOutsideCode(in)
	var sb strings.Builder
	for _, p := range parts {
		if p.code {
			sb.WriteString(p.text)
			continue
		}
		t := displayMathRe.ReplaceAllStringFunc(p.text, func(m string) string {
			sub := displayMathRe.FindStringSubmatch(m)
			return r.renderMath(sub[1], true, m)
		})
		t = inlineMathRe.ReplaceAllStringFunc(t, func(m string) string {
			sub := inlineMathRe.FindStringSubmatch(m)
			return r.renderMath(sub[1], false, m)
		})
		sb.WriteString(t)
	}
	return sb.String()
}

func (r *Renderer) renderMath(src string, display bool, original string) string {
	src = strings.TrimSpace(unescapeCode(src))
	if src == "" {
		return original
	}
	kind := "math-inline"
	if display {
		kind = "math-display"
	}
	key := cacheKey(kind, src)
	if svg, ok := r.cache.get(key); ok {
		return svg
	}
	svg, err := r.math.render(src, display)
	if err != nil {
		return original // unrenderable: leave the author's source alone
	}
	class := "cw-math-inline"
	if display {
		class = "cw-math-display"
	}
	wrapped := `<span class="` + class + `">` + svg + `</span>`
	r.cache.put(key, wrapped)
	return wrapped
}

type segment struct {
	text string
	code bool
}

// splitOutsideCode separates code spans and blocks from prose so math
// substitution never touches code.
func splitOutsideCode(in string) []segment {
	var out []segment
	rest := in
	for {
		start := indexAny(rest, "<pre", "<code")
		if start < 0 {
			out = append(out, segment{text: rest})
			return out
		}
		tag := "</pre>"
		if strings.HasPrefix(rest[start:], "<code") {
			tag = "</code>"
		}
		end := strings.Index(rest[start:], tag)
		if end < 0 {
			out = append(out, segment{text: rest})
			return out
		}
		end += start + len(tag)
		out = append(out,
			segment{text: rest[:start]},
			segment{text: rest[start:end], code: true})
		rest = rest[end:]
	}
}

func indexAny(s string, subs ...string) int {
	best := -1
	for _, sub := range subs {
		if i := strings.Index(s, sub); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

// unescapeCode reverses the HTML escaping goldmark applies inside code.
func unescapeCode(s string) string {
	s = html.UnescapeString(s)
	// Strip any tags chroma added inside the block.
	return stripTags(s)
}

func stripTags(s string) string {
	var sb bytes.Buffer
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case depth == 0:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
