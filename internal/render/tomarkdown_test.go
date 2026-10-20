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

// Colour and size have no markdown syntax, so they must survive as inline
// HTML or formatting applied in rich text vanishes on save.
func TestToMarkdownKeepsColourAndSize(t *testing.T) {
	got, err := ToMarkdown(`<p>普通<span style="color: rgb(208, 49, 45);">红色</span>` +
		`<span style="font-size: 20px; background-color: #fff3a3">大号高亮</span></p>`)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	for _, want := range []string{
		`<span style="color: rgb(208, 49, 45)">红色</span>`,
		`font-size: 20px`, `background-color: #fff3a3`, "大号高亮",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
}

// Text pasted from a web page arrives wrapped in spans full of layout CSS.
// Only styles the publish pipeline honours may reach the file.
func TestToMarkdownDropsJunkStyles(t *testing.T) {
	got, err := ToMarkdown(`<p><span style="position: absolute; line-height: 1.4; margin: 0">纯文字</span></p>`)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if strings.Contains(got, "<span") {
		t.Errorf("a span with nothing worth keeping should unwrap:\n%s", got)
	}
	if !strings.Contains(got, "纯文字") {
		t.Errorf("text was dropped:\n%s", got)
	}
}

func TestToMarkdownKeepsFormattingInsideColour(t *testing.T) {
	got, err := ToMarkdown(`<p><span style="color: red"><strong>重点</strong></span></p>`)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(got, "**重点**") || !strings.Contains(got, `color: red`) {
		t.Errorf("bold inside colour should stay markdown bold:\n%s", got)
	}
}

func TestToMarkdownKeepsUnderlineStrikeAndTables(t *testing.T) {
	got, err := ToMarkdown(`<p><u>下划线</u> 和 <del>删除线</del></p>` +
		`<table><thead><tr><th>列A</th><th>列B</th></tr></thead>` +
		`<tbody><tr><td>1</td><td>2</td></tr></tbody></table>`)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	for _, want := range []string{"<u>下划线</u>", "~~删除线~~", "| 列A", "| 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
}

// A style value that could close the attribute must not be written into
// the markdown, where it would become live HTML.
func TestFilterStyleRejectsAttributeBreakout(t *testing.T) {
	got := FilterStyle(`color: red"><script>x</script>; font-size: 18px`)
	if strings.ContainsAny(got, `"<>`) {
		t.Errorf("unsafe value kept: %q", got)
	}
	if got != "font-size: 18px" {
		t.Errorf("FilterStyle = %q, want only the safe declaration", got)
	}
}

// Chrome writes font-family with double-quoted names; rejecting the quotes
// silently dropped every font chosen in the rich-text toolbar.
func TestToMarkdownKeepsQuotedFontFamily(t *testing.T) {
	got, err := ToMarkdown(`<p><span style="font-family: &quot;Kaiti SC&quot;, KaiTi, serif;">楷体</span></p>`)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	want := `<span style="font-family: 'Kaiti SC', KaiTi, serif">楷体</span>`
	if !strings.Contains(got, want) {
		t.Errorf("font-family lost:\n%s\nwant %s", got, want)
	}
}

func TestToMarkdownMapsLegacyFontTag(t *testing.T) {
	got, err := ToMarkdown(`<p><font color="#1565c0">蓝字</font></p>`)
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	if !strings.Contains(got, `<span style="color: #1565c0">蓝字</span>`) {
		t.Errorf("legacy <font> not mapped:\n%s", got)
	}
}

// Formatting kept in the file is only useful if it reaches the reader, so
// a coloured, resized span must come through the WeChat pipeline intact.
func TestStyledSpanSurvivesWeChatExport(t *testing.T) {
	r := New()
	md := `正文<span style="color: #d0312d; font-size: 20px">强调</span><u>下划线</u>`
	res, err := r.Render(md, ProfileWeChat, "default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"color: #d0312d", "font-size: 20px", "强调", "<u>"} {
		if !strings.Contains(res.HTML, want) {
			t.Errorf("WeChat output lost %q:\n%s", want, res.HTML)
		}
	}
}
