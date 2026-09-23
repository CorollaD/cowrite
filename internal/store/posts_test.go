package store

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// A file deleted and then recreated arrives with a new id at a path the
// old row still holds. Both columns are unique, so without releasing that
// path the new file can never be indexed.
func TestUpsertReclaimsPathFromDeletedPost(t *testing.T) {
	db := openTestDB(t)
	const path = "posts/2026/09/note.md"

	if err := db.UpsertPost(&Post{ID: "old-id", Path: path, Title: "旧"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.SoftDeletePost("old-id"); err != nil {
		t.Fatalf("SoftDeletePost: %v", err)
	}

	if err := db.UpsertPost(&Post{ID: "new-id", Path: path, Title: "新"}); err != nil {
		t.Fatalf("recreating a post at the same path must succeed: %v", err)
	}

	got, err := db.GetPostByPath(path)
	if err != nil {
		t.Fatalf("GetPostByPath: %v", err)
	}
	if got.ID != "new-id" {
		t.Errorf("path resolves to %q, want the new post", got.ID)
	}

	// The displaced row stays addressable so publish history and version
	// snapshots keep resolving.
	if _, err := db.GetVersion(0); err == nil {
		_ = err // no versions here; the point is the row below
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM posts WHERE id = ?`, "old-id"); err != nil {
		t.Fatalf("count old row: %v", err)
	}
	if count != 1 {
		t.Error("the displaced row was dropped; its history would be orphaned")
	}
}

// Live posts must never collide either: two different files cannot claim
// one path, and re-indexing the same file must stay idempotent.
func TestUpsertSamePostIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	const path = "posts/2026/09/same.md"

	for i := range 3 {
		if err := db.UpsertPost(&Post{
			ID: "stable", Path: path, Title: "标题", WordCount: i,
		}); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}

	posts, err := db.ListPosts()
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d rows, want 1", len(posts))
	}
	if posts[0].WordCount != 2 {
		t.Errorf("WordCount = %d, want the latest value", posts[0].WordCount)
	}
}

// Re-indexing a soft-deleted file brings it back rather than staying hidden.
func TestUpsertRevivesSoftDeletedPost(t *testing.T) {
	db := openTestDB(t)
	p := &Post{ID: "revive", Path: "posts/a.md", Title: "回来"}

	if err := db.UpsertPost(p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.SoftDeletePost("revive"); err != nil {
		t.Fatalf("SoftDeletePost: %v", err)
	}
	if err := db.UpsertPost(p); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	if _, err := db.GetPost("revive"); err != nil {
		t.Errorf("post stayed deleted after being re-indexed: %v", err)
	}
}
