package index

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/corollad/cowrite/internal/store"
	"github.com/corollad/cowrite/internal/workspace"
)

func setup(t *testing.T) (*Index, *workspace.Workspace, *store.DB) {
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
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(ws, db, log), ws, db
}

func writePost(t *testing.T, ws *workspace.Workspace, body string) string {
	t.Helper()
	now := time.Now()
	p := &workspace.Post{Body: body}
	p.EnsureIDs(now)
	path := ws.NewPostPath(p.Meta.Title, now)
	if _, err := ws.Save(path, p, ""); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

func TestSyncIndexesPosts(t *testing.T) {
	ix, ws, db := setup(t)
	writePost(t, ws, "# One\n\n你好世界\n")
	writePost(t, ws, "# Two\n\nhello there\n")

	if err := ix.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	posts, err := db.ListPosts()
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("indexed %d posts, want 2", len(posts))
	}
}

// A file dropped in by hand has no front matter; the index must give it an
// identity rather than skipping it.
func TestSyncStampsIdentityOnBareMarkdown(t *testing.T) {
	ix, ws, db := setup(t)
	path := filepath.Join(ws.PostsDir(), "dropped-in.md")
	if err := os.WriteFile(path, []byte("# Dropped In\n\nbody\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := ix.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	posts, _ := db.ListPosts()
	if len(posts) != 1 {
		t.Fatalf("indexed %d posts, want 1", len(posts))
	}
	if posts[0].ID == "" {
		t.Error("no ID assigned")
	}
	if posts[0].Title != "Dropped In" {
		t.Errorf("title = %q, want %q", posts[0].Title, "Dropped In")
	}

	// The ID must be persisted back to the file, or it changes every scan.
	data, _ := os.ReadFile(path)
	parsed, _ := workspace.ParsePost(data)
	if parsed.Meta.ID != posts[0].ID {
		t.Errorf("ID not written back to file: %q vs %q", parsed.Meta.ID, posts[0].ID)
	}
}

func TestSyncSoftDeletesMissingFiles(t *testing.T) {
	ix, ws, db := setup(t)
	path := writePost(t, ws, "# Gone Soon\n")
	if err := ix.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := ix.Sync(); err != nil {
		t.Fatalf("Sync after delete: %v", err)
	}

	posts, _ := db.ListPosts()
	if len(posts) != 0 {
		t.Errorf("deleted file still listed: %d posts", len(posts))
	}
}

// Reindexing must be idempotent: IDs and rows stay stable across scans.
func TestSyncIsIdempotent(t *testing.T) {
	ix, ws, db := setup(t)
	writePost(t, ws, "# Stable\n\ncontent\n")

	if err := ix.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	first, _ := db.ListPosts()

	for range 3 {
		if err := ix.Sync(); err != nil {
			t.Fatalf("resync: %v", err)
		}
	}
	again, _ := db.ListPosts()

	if len(again) != len(first) {
		t.Fatalf("post count drifted: %d -> %d", len(first), len(again))
	}
	if again[0].ID != first[0].ID {
		t.Errorf("ID changed across syncs: %s -> %s", first[0].ID, again[0].ID)
	}
}
