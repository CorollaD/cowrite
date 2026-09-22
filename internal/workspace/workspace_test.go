package workspace

import (
	"errors"
	"os"
	"testing"
	"time"
)

func newTestWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return w
}

func TestSaveAndScan(t *testing.T) {
	w := newTestWorkspace(t)
	now := time.Now()

	p := &Post{Body: "# First\n\nhello\n"}
	p.EnsureIDs(now)
	path := w.NewPostPath(p.Meta.Title, now)

	e, err := w.Save(path, p, "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if e.Hash == "" {
		t.Fatal("no hash returned")
	}

	entries, err := w.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Scan found %d entries, want 1", len(entries))
	}
	if entries[0].Post.Meta.ID != p.Meta.ID {
		t.Errorf("scanned ID = %q, want %q", entries[0].Post.Meta.ID, p.Meta.ID)
	}
	if entries[0].Post.Body != p.Body {
		t.Errorf("scanned body = %q, want %q", entries[0].Post.Body, p.Body)
	}
}

// The case that protects against data loss: the file changed on disk while
// the editor was holding an older version.
func TestSaveConflictDetection(t *testing.T) {
	w := newTestWorkspace(t)
	now := time.Now()

	p := &Post{Body: "# Original\n"}
	p.EnsureIDs(now)
	path := w.NewPostPath(p.Meta.Title, now)
	e, err := w.Save(path, p, "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	staleHash := e.Hash

	// Something else (vim, git, another tool) rewrites the file.
	external := &Post{Meta: p.Meta, Body: "# Edited elsewhere\n"}
	if _, err := w.Save(path, external, staleHash); err != nil {
		t.Fatalf("external save: %v", err)
	}

	// The editor now saves against the hash it loaded originally.
	p.Body = "# Edited in browser\n"
	_, err = w.Save(path, p, staleHash)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatal("expected a *ConflictError carrying the disk copy")
	}
	if ce.DiskBody != "# Edited elsewhere\n" {
		t.Errorf("conflict disk body = %q", ce.DiskBody)
	}

	// The external edit must still be intact: a rejected save writes nothing.
	onDisk, _ := os.ReadFile(path)
	parsed, _ := ParsePost(onDisk)
	if parsed.Body != "# Edited elsewhere\n" {
		t.Errorf("rejected save still modified the file: %q", parsed.Body)
	}
}

func TestSaveRefusesToClobberOnCreate(t *testing.T) {
	w := newTestWorkspace(t)
	now := time.Now()
	p := &Post{Body: "# A\n"}
	p.EnsureIDs(now)
	path := w.NewPostPath(p.Meta.Title, now)

	if _, err := w.Save(path, p, ""); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := w.Save(path, p, ""); err == nil {
		t.Fatal("expected create against an existing file to fail")
	}
}

// Our own writes must not look like external edits, or the watcher loops.
func TestSelfWrittenTracking(t *testing.T) {
	w := newTestWorkspace(t)
	now := time.Now()
	p := &Post{Body: "# X\n"}
	p.EnsureIDs(now)
	path := w.NewPostPath(p.Meta.Title, now)

	e, err := w.Save(path, p, "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !w.WasSelfWritten(path, e.Hash) {
		t.Error("save was not recorded as self-written")
	}
	// Consumed: a second look must report false so real edits get through.
	if w.WasSelfWritten(path, e.Hash) {
		t.Error("self-written record was not consumed")
	}
	if w.WasSelfWritten(path, "some-other-hash") {
		t.Error("a different hash must not count as self-written")
	}
}

func TestNewPostPathAvoidsCollisions(t *testing.T) {
	w := newTestWorkspace(t)
	now := time.Now()

	first := w.NewPostPath("Same Title", now)
	p := &Post{Body: "# Same Title\n"}
	p.EnsureIDs(now)
	if _, err := w.Save(first, p, ""); err != nil {
		t.Fatalf("Save: %v", err)
	}

	second := w.NewPostPath("Same Title", now)
	if first == second {
		t.Fatalf("NewPostPath reused an occupied path: %s", first)
	}
}

func TestSlugifyKeepsCJK(t *testing.T) {
	if got := Slugify("你好 世界"); got != "你好-世界" {
		t.Errorf("Slugify CJK = %q, want %q", got, "你好-世界")
	}
	if got := Slugify("Hello, World!"); got != "hello-world" {
		t.Errorf("Slugify = %q, want %q", got, "hello-world")
	}
	if got := Slugify("!!!"); got != "untitled" {
		t.Errorf("Slugify(%q) = %q, want untitled", "!!!", got)
	}
}
