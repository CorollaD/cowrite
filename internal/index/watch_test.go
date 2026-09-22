package index

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/corollad/cowrite/internal/store"
	"github.com/corollad/cowrite/internal/workspace"
)

func setupWatcher(t *testing.T) (*Watcher, *workspace.Workspace, chan Change) {
	t.Helper()
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	db, err := store.Open(filepath.Join(dir, ".cowrite", "index.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ix := New(ws, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	changes := make(chan Change, 8)
	w := NewWatcher(ix, ws, func(c Change) { changes <- c })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = w.Run(ctx) }()
	time.Sleep(200 * time.Millisecond) // let the watcher start

	return w, ws, changes
}

func TestWatcherReportsExternalEdit(t *testing.T) {
	_, ws, changes := setupWatcher(t)

	path := filepath.Join(ws.PostsDir(), "external.md")
	if err := os.WriteFile(path, []byte("# From another editor\n\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	select {
	case c := <-changes:
		if c.Kind != "updated" {
			t.Errorf("kind = %q, want updated", c.Kind)
		}
		if c.RelPath == "" {
			t.Error("change carries no path")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("external edit was not reported")
	}
}

// The failure this prevents: the server saves a file, the watcher sees it,
// tells the browser the file changed, the browser reloads and saves again.
func TestWatcherIgnoresOwnWrites(t *testing.T) {
	_, ws, changes := setupWatcher(t)

	post := &workspace.Post{Body: "# Written by the server\n"}
	post.EnsureIDs(time.Now())
	path := ws.NewPostPath(post.Meta.Title, time.Now())
	if _, err := ws.Save(path, post, ""); err != nil {
		t.Fatalf("Save: %v", err)
	}

	select {
	case c := <-changes:
		t.Fatalf("our own write was reported as an external change: %+v", c)
	case <-time.After(1200 * time.Millisecond):
		// nothing reported, which is correct
	}
}

func TestWatcherReportsDeletion(t *testing.T) {
	_, ws, changes := setupWatcher(t)

	path := filepath.Join(ws.PostsDir(), "doomed.md")
	if err := os.WriteFile(path, []byte("# Doomed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	select {
	case <-changes:
	case <-time.After(3 * time.Second):
		t.Fatal("creation was not reported")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	select {
	case c := <-changes:
		if c.Kind != "removed" {
			t.Errorf("kind = %q, want removed", c.Kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deletion was not reported")
	}
}

// Editors write a temp file and rename it, so one save produces several
// events; they must collapse into a single notification.
func TestWatcherDebouncesRapidWrites(t *testing.T) {
	_, ws, changes := setupWatcher(t)
	path := filepath.Join(ws.PostsDir(), "rapid.md")

	for i := range 5 {
		if err := os.WriteFile(path, []byte("# Rapid\n\nedit\n"+string(rune('a'+i))), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
	}

	select {
	case <-changes:
	case <-time.After(3 * time.Second):
		t.Fatal("no change reported")
	}

	// Any further events should be few, not one per write.
	extra := 0
	for {
		select {
		case <-changes:
			extra++
		case <-time.After(800 * time.Millisecond):
			if extra > 1 {
				t.Errorf("debounce failed: %d extra notifications for one burst", extra)
			}
			return
		}
	}
}

func TestWatcherIgnoresNonMarkdown(t *testing.T) {
	_, ws, changes := setupWatcher(t)

	for _, name := range []string{"notes.txt", ".hidden.md", "image.png"} {
		if err := os.WriteFile(filepath.Join(ws.PostsDir(), name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	select {
	case c := <-changes:
		t.Errorf("non-post file reported: %+v", c)
	case <-time.After(1200 * time.Millisecond):
	}
}
