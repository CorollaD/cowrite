package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update regenerates the golden files: go test ./internal/render -update
var update = os.Getenv("UPDATE_GOLDEN") != ""

func goldenPath(name string) string {
	return filepath.Join("testdata", name)
}

// checkGolden compares output against a recorded file so that rendering
// changes have to be reviewed deliberately rather than slipping through.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(name)
	if update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with UPDATE_GOLDEN=1 to create): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s",
			path, got, want)
	}
}

func loadInput(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("read input: %v", err)
	}
	return string(data)
}

func TestRenderPreviewGolden(t *testing.T) {
	r := New()
	res, err := r.Render(loadInput(t, "kitchen-sink.md"), ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	checkGolden(t, "kitchen-sink.preview.html", res.HTML)

	if res.CSS == "" {
		t.Error("preview profile must carry the theme stylesheet")
	}
}

func TestRenderWeChatGolden(t *testing.T) {
	r := New()
	res, err := r.Render(loadInput(t, "kitchen-sink.md"), ProfileWeChat, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	checkGolden(t, "kitchen-sink.wechat.html", res.HTML)
}

// The rules that make output survive WeChat's editor.
func TestWeChatSanitizerRules(t *testing.T) {
	r := New()
	res, err := r.Render(loadInput(t, "kitchen-sink.md"), ProfileWeChat, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := res.HTML

	if strings.Contains(out, "<div") {
		t.Error("<div> must be rewritten to <section>")
	}
	if !strings.Contains(out, "<section") {
		t.Error("expected a <section> wrapper")
	}
	if strings.Contains(out, "class=") || strings.Contains(out, " id=") {
		t.Error("class/id attributes must be stripped after inlining")
	}
	if strings.Contains(out, "<style") {
		t.Error("<style> blocks must be removed; WeChat drops them")
	}
	if !strings.Contains(out, "style=") {
		t.Error("theme CSS must be inlined onto elements")
	}
}

func TestWeChatDropsDisallowedCSS(t *testing.T) {
	// position/float are unreliable in WeChat and must not be emitted.
	got := filterStyle("color: red; position: absolute; float: left; font-size: 16px")
	if strings.Contains(got, "position") || strings.Contains(got, "float") {
		t.Errorf("disallowed properties survived: %q", got)
	}
	if !strings.Contains(got, "color") || !strings.Contains(got, "font-size") {
		t.Errorf("allowed properties were dropped: %q", got)
	}
}

func TestWeChatStripsScriptsAndHandlers(t *testing.T) {
	out, err := sanitizeWeChat(
		`<html><body><div onclick="steal()">hi</div>` +
			`<script>bad()</script><a href="javascript:x()">link</a></body></html>`)
	if err != nil {
		t.Fatalf("sanitizeWeChat: %v", err)
	}
	for _, bad := range []string{"<script", "onclick", "javascript:"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q survived sanitizing: %s", bad, out)
		}
	}
}

func TestCodeHighlightingIsInline(t *testing.T) {
	r := New()
	res, err := r.Render("```go\nfunc main() {}\n```\n", ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Chroma must emit inline styles, or highlighting is lost once the
	// stylesheet is stripped by a target like WeChat.
	if !strings.Contains(res.HTML, "style=") {
		t.Errorf("expected inline highlight styles, got: %s", res.HTML)
	}
	if strings.Contains(res.HTML, `class="chroma`) {
		t.Error("chroma emitted classes; it must be configured with WithClasses(false)")
	}
}

func TestFrontMatterIsNotRendered(t *testing.T) {
	r := New()
	res, err := r.Render("---\ntitle: Hidden\n---\n\n# Visible\n", ProfilePreview, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(res.HTML, "Hidden") {
		t.Errorf("front matter leaked into output: %s", res.HTML)
	}
	if !strings.Contains(res.HTML, "Visible") {
		t.Errorf("body missing: %s", res.HTML)
	}
}

func TestThemesAvailable(t *testing.T) {
	list := Themes()
	if len(list) < 2 {
		t.Fatalf("expected at least 2 themes, got %d", len(list))
	}
	if list[0].ID != "default" {
		t.Errorf("default theme should sort first, got %q", list[0].ID)
	}
	for _, th := range list {
		if th.CSS == "" {
			t.Errorf("theme %q has no CSS", th.ID)
		}
	}
}

func TestUnknownThemeFallsBack(t *testing.T) {
	r := New()
	res, err := r.Render("# x\n", ProfilePreview, "no-such-theme")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if res.Theme != "default" {
		t.Errorf("expected fallback to default, got %q", res.Theme)
	}
}
