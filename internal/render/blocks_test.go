package render

import (
	"strings"
	"testing"
)

func TestDisplayMathRendersToSVG(t *testing.T) {
	r := New()
	res, err := r.Render("公式如下：\n\n$$E = mc^2$$\n", ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(res.HTML, "<svg") {
		t.Errorf("display math was not rendered: %s", res.HTML)
	}
	if strings.Contains(res.HTML, "$$") {
		t.Errorf("math delimiters left in output: %s", res.HTML)
	}
}

func TestInlineMathRendersToSVG(t *testing.T) {
	r := New()
	res, err := r.Render("变量 $x^2$ 在这里。\n", ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(res.HTML, "cw-math-inline") {
		t.Errorf("inline math was not rendered: %s", res.HTML)
	}
}

func TestMermaidRendersToSVG(t *testing.T) {
	r := New()
	md := "```mermaid\ngraph TD;\n A[开始] --> B[结束];\n```\n"
	res, err := r.Render(md, ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(res.HTML, "<svg") {
		t.Errorf("mermaid was not rendered: %s", res.HTML)
	}
	if !strings.Contains(res.HTML, "cw-diagram") {
		t.Error("diagram wrapper missing")
	}
}

// A diagram that will not lay out is still the author's content and must
// not disappear.
func TestUnrenderableMermaidKeepsSource(t *testing.T) {
	r := New()
	md := "```mermaid\n!!! this is not a valid diagram !!!\n```\n"
	res, err := r.Render(md, ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(res.HTML, "not a valid diagram") {
		t.Errorf("source was dropped on failure: %s", res.HTML)
	}
}

func TestUnrenderableMathKeepsSource(t *testing.T) {
	r := New()
	res, err := r.Render(`$\thisCommandDoesNotExist{`+"\n", ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(res.HTML, "thisCommandDoesNotExist") {
		t.Errorf("source was dropped on failure: %s", res.HTML)
	}
}

// A dollar sign in prose or code is not math.
func TestDollarsOutsideMathAreLeftAlone(t *testing.T) {
	r := New()
	res, err := r.Render("价格是 $100 到 $200 之间。\n\n```sh\necho $HOME\n```\n",
		ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(res.HTML, "$HOME") {
		t.Errorf("shell variable was mangled: %s", res.HTML)
	}
}

func TestBlocksAreCached(t *testing.T) {
	r := New()
	md := "```mermaid\ngraph TD;\n A-->B;\n```\n"

	first, err := r.Render(md, ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	second, err := r.Render(md, ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if first.HTML != second.HTML {
		t.Error("cached render differs from the first")
	}
	if len(r.cache.m) == 0 {
		t.Error("nothing was cached; preview would re-render on every keystroke")
	}
}

// WeChat's editor drops inline <svg>, so formulas must travel as images
// or the article publishes with them missing.
func TestWeChatConvertsSVGToImage(t *testing.T) {
	r := New()
	res, err := r.Render("$$E = mc^2$$\n", ProfileWeChat, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(res.HTML, "<svg") {
		t.Error("inline SVG survived into WeChat output")
	}
	if !strings.Contains(res.HTML, "data:image/svg+xml;base64,") {
		t.Errorf("formula did not become an image: %s", res.HTML)
	}
}

// Preview keeps inline SVG: it is smaller and sharper than a data URI.
func TestPreviewKeepsInlineSVG(t *testing.T) {
	r := New()
	res, err := r.Render("$$x^2$$\n", ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(res.HTML, "<svg") {
		t.Error("preview should keep inline SVG")
	}
}
