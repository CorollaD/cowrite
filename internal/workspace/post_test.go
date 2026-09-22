package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParsePostRoundTrip(t *testing.T) {
	src := []byte("---\nid: abc-123\ntitle: Hello\ncreated: 2026-09-22T10:00:00Z\nupdated: 2026-09-22T10:00:00Z\ntags:\n    - go\n---\n\n# Hello\n\nbody text\n")

	p, err := ParsePost(src)
	if err != nil {
		t.Fatalf("ParsePost: %v", err)
	}
	if p.Meta.ID != "abc-123" || p.Meta.Title != "Hello" {
		t.Fatalf("meta not parsed: %+v", p.Meta)
	}
	if !strings.HasPrefix(p.Body, "# Hello") {
		t.Fatalf("body not parsed: %q", p.Body)
	}
	if len(p.Meta.Tags) != 1 || p.Meta.Tags[0] != "go" {
		t.Fatalf("tags not parsed: %v", p.Meta.Tags)
	}

	out, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	again, err := ParsePost(out)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if again.Meta.ID != p.Meta.ID || again.Body != p.Body {
		t.Fatalf("round trip lost data:\n%q\nvs\n%q", again.Body, p.Body)
	}
}

// A file with no front matter must still open, with its content intact.
func TestParsePostNoFrontMatter(t *testing.T) {
	for _, src := range []string{
		"# Just markdown\n\ntext\n",
		"---not actually front matter\n",
		"---\nunterminated: true\n",
	} {
		p, err := ParsePost([]byte(src))
		if err != nil {
			t.Fatalf("ParsePost(%q): %v", src, err)
		}
		if p.Body != src {
			t.Errorf("content altered:\ngot  %q\nwant %q", p.Body, src)
		}
	}
}

func TestDeriveTitle(t *testing.T) {
	cases := map[string]string{
		"# Heading\n\nbody":     "Heading",
		"\n\n## Second level\n": "Second level",
		"plain first line\n":    "plain first line",
		"":                      "Untitled",
		"#\n\nreal title\n":     "real title",
	}
	for body, want := range cases {
		if got := DeriveTitle(body); got != want {
			t.Errorf("DeriveTitle(%q) = %q, want %q", body, got, want)
		}
	}
}

func TestWordCountMixedScript(t *testing.T) {
	// 4 Han characters + 2 Latin words.
	if got := WordCount("你好世界 hello world"); got != 6 {
		t.Errorf("WordCount = %d, want 6", got)
	}
}

func TestEnsureIDsPreservesExisting(t *testing.T) {
	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Post{Meta: Meta{ID: "keep-me", Created: created}, Body: "# T\n"}
	p.EnsureIDs(time.Now())

	if p.Meta.ID != "keep-me" {
		t.Errorf("ID overwritten: %s", p.Meta.ID)
	}
	if !p.Meta.Created.Equal(created) {
		t.Errorf("Created overwritten: %v", p.Meta.Created)
	}
	if p.Meta.Title != "T" {
		t.Errorf("Title not derived: %q", p.Meta.Title)
	}
}

func TestWriteAtomicReplacesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "post.md")

	if err := WriteAtomic(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	if err := WriteAtomic(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteAtomic overwrite: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}

	// No temp files may survive a successful write.
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".cowrite-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}
