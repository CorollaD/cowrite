package workspace

import (
	"strings"
	"testing"
)

func TestAnalyzeMixedScript(t *testing.T) {
	// 4 Han characters + 2 Latin words.
	s := Analyze("你好世界 hello world")
	if s.Words != 6 {
		t.Errorf("Words = %d, want 6", s.Words)
	}
	if s.CJK != 4 {
		t.Errorf("CJK = %d, want 4", s.CJK)
	}
	if s.Latin != 2 {
		t.Errorf("Latin = %d, want 2", s.Latin)
	}
}

func TestAnalyzeCharacterCounts(t *testing.T) {
	s := Analyze("你好 world")
	if s.Characters != 8 {
		t.Errorf("Characters = %d, want 8", s.Characters)
	}
	// The single space is the only whitespace.
	if s.NoSpaces != 7 {
		t.Errorf("NoSpaces = %d, want 7", s.NoSpaces)
	}
}

func TestAnalyzeCountsParagraphs(t *testing.T) {
	s := Analyze("第一段。\n\n第二段。\n\n\n\n第三段。")
	if s.Paragraphs != 3 {
		t.Errorf("Paragraphs = %d, want 3", s.Paragraphs)
	}
}

// Chinese prose ends sentences with full-width punctuation, which an
// ASCII-only check would miss entirely.
func TestAnalyzeCountsFullWidthSentences(t *testing.T) {
	if got := Analyze("第一句。第二句！第三句？").Sentences; got != 3 {
		t.Errorf("Sentences = %d, want 3", got)
	}
	if got := Analyze("One. 两句。Three!").Sentences; got != 3 {
		t.Errorf("mixed Sentences = %d, want 3", got)
	}
}

// A short draft should read as one minute, not zero.
func TestAnalyzeReadingTimeRoundsUp(t *testing.T) {
	if got := Analyze("短文").ReadMinutes; got != 1 {
		t.Errorf("short text ReadMinutes = %d, want 1", got)
	}
	if got := Analyze("").ReadMinutes; got != 0 {
		t.Errorf("empty text ReadMinutes = %d, want 0", got)
	}
	long := strings.Repeat("字", readingSpeed+1)
	if got := Analyze(long).ReadMinutes; got != 2 {
		t.Errorf("just over one minute of text = %d, want 2", got)
	}
}

func TestAnalyzeEmpty(t *testing.T) {
	s := Analyze("")
	if s.Words != 0 || s.Characters != 0 || s.Paragraphs != 0 {
		t.Errorf("empty input produced %+v", s)
	}
}

// WordCount is what the index stores; it must agree with Analyze.
func TestWordCountMatchesAnalyze(t *testing.T) {
	for _, body := range []string{
		"你好世界 hello world",
		"# 标题\n\n正文内容。",
		"",
		"Numbers 123 and 中文456混排",
	} {
		if got, want := WordCount(body), Analyze(body).Words; got != want {
			t.Errorf("WordCount(%q) = %d, Analyze = %d", body, got, want)
		}
	}
}

func TestAnalyzeCountsHangulAndKana(t *testing.T) {
	// Each CJK-family character counts individually.
	if got := Analyze("안녕").CJK; got != 2 {
		t.Errorf("Hangul CJK = %d, want 2", got)
	}
	if got := Analyze("ひらがな").CJK; got != 4 {
		t.Errorf("Hiragana CJK = %d, want 4", got)
	}
}

// A pasted image is thousands of base64 characters; counting it as text
// turned a short note into a five-figure word count.
func TestAnalyzeIgnoresEmbeddedImages(t *testing.T) {
	img := "![](data:image/png;base64," + strings.Repeat("iVBORw0KGgoAAAANSUhEUg", 200) + ")"
	if got := Analyze("你好世界\n\n" + img).Words; got != 4 {
		t.Errorf("Words = %d, want 4: the image must not count", got)
	}
}

func TestAnalyzeIgnoresLinkTargets(t *testing.T) {
	// 看 + 这篇文章 (4) + 吧 = 6; the URL contributes nothing.
	if got := Analyze("看[这篇文章](https://example.com/a/very/long/path?x=1)吧").Words; got != 6 {
		t.Errorf("Words = %d, want 6", got)
	}
}
