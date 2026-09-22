package render

import (
	"strings"
	"testing"
)

// Round trip: markdown the app renders must convert back to equivalent
// markdown, or switching modes would quietly rewrite the file.
func TestMarkdownRoundTrip(t *testing.T) {
	r := New()
	src := "# 标题\n\n这是**加粗**和*斜体*。\n\n- 第一项\n- 第二项\n\n> 引用\n"

	res, err := r.Render(src, ProfileGeneric, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	back, err := ToMarkdown(res.HTML)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}

	for _, want := range []string{"# 标题", "**加粗**", "第一项", "第二项", "> 引用"} {
		if !strings.Contains(back, want) {
			t.Errorf("round trip lost %q:\n%s", want, back)
		}
	}
}

func TestToMarkdownHandlesEmptyInput(t *testing.T) {
	got, err := ToMarkdown("")
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("empty input produced %q", got)
	}
}
