// Package render turns markdown into HTML for a given target.
//
// Rendering happens here rather than in the browser so that what the user
// previews is exactly what gets published: one pipeline, one result.
package render

import (
	"bytes"
	"fmt"
	"sync"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"go.abhg.dev/goldmark/frontmatter"
)

// Profile selects what the output is for. The same markdown renders
// differently depending on where it is going.
type Profile string

const (
	// ProfilePreview targets the in-app preview: a stylesheet is kept as a
	// <style> block, which is smaller and faster than inlining.
	ProfilePreview Profile = "preview"
	// ProfileWeChat targets the WeChat editor, which strips <style>, so
	// every rule has to be inlined onto the elements themselves.
	ProfileWeChat Profile = "wechat"
	// ProfileGeneric targets editors that accept ordinary semantic HTML.
	ProfileGeneric Profile = "generic"
)

// Result is rendered output plus what the caller needs to present it.
type Result struct {
	HTML  string `json:"html"`
	CSS   string `json:"css"`
	Theme string `json:"theme"`
}

// Renderer holds the configured goldmark pipelines.
//
// Building a goldmark instance is expensive relative to rendering, so they
// are built once and reused; goldmark is safe for concurrent use.
type Renderer struct {
	once     sync.Once
	markdown goldmark.Markdown
	cache    *svgCache
	math     *mathRenderer
}

func New() *Renderer {
	return &Renderer{cache: newSVGCache(), math: &mathRenderer{}}
}

func (r *Renderer) init() {
	r.once.Do(func() {
		r.markdown = goldmark.New(
			goldmark.WithExtensions(
				extension.GFM,
				extension.Footnote,
				extension.DefinitionList,
				&frontmatter.Extender{},
				highlighting.NewHighlighting(
					highlighting.WithStyle("github"),
					// Emit inline styles rather than CSS classes: WeChat
					// drops <style>, and chroma inlines more accurately
					// than a generic CSS inliner can.
					highlighting.WithFormatOptions(
						chromahtml.WithClasses(false),
					),
				),
			),
			goldmark.WithParserOptions(
				parser.WithAutoHeadingID(),
			),
			goldmark.WithRendererOptions(
				// Raw HTML in a post is the author's own, and the output is
				// sanitized per target before it leaves the app.
				html.WithUnsafe(),
			),
		)
	})
}

// Render converts markdown to HTML for the given profile and theme.
func (r *Renderer) Render(md string, profile Profile, themeID string) (*Result, error) {
	r.init()

	theme, ok := FindTheme(themeID)
	if !ok {
		theme = DefaultTheme()
	}

	var buf bytes.Buffer
	if err := r.markdown.Convert([]byte(md), &buf); err != nil {
		return nil, fmt.Errorf("render markdown: %w", err)
	}
	body := r.applyBlocks(buf.String())

	switch profile {
	case ProfileWeChat:
		out, err := ToWeChat(body, theme)
		if err != nil {
			return nil, err
		}
		return &Result{HTML: out, Theme: theme.ID}, nil

	case ProfileGeneric:
		return &Result{HTML: body, Theme: theme.ID}, nil

	default: // ProfilePreview
		return &Result{
			HTML:  `<div class="cw-root">` + body + `</div>`,
			CSS:   theme.CSS,
			Theme: theme.ID,
		}, nil
	}
}
