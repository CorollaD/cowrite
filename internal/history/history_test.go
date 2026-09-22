package history

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corollad/cowrite/internal/store"
)

func setup(t *testing.T) (*History, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "index.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(dir, db), db
}

func TestSnapshotRoundTrip(t *testing.T) {
	h, _ := setup(t)
	body := "# 标题\n\n正文内容，包含中文和 English。\n"

	v, err := h.Snapshot("post-1", body, store.KindManual)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if v == nil {
		t.Fatal("manual snapshot was skipped")
	}

	got, err := h.Restore(v)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got != body {
		t.Errorf("restored %q, want %q", got, body)
	}
}

func TestSnapshotSkipsUnchangedContent(t *testing.T) {
	h, db := setup(t)
	body := "# same\n"

	if _, err := h.Snapshot("p", body, store.KindManual); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := h.Snapshot("p", body, store.KindManual); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	list, _ := db.ListVersions("p")
	if len(list) != 1 {
		t.Errorf("identical content produced %d snapshots, want 1", len(list))
	}
}

// Autosave fires constantly; snapshotting every save would bury the
// checkpoints a user actually wants.
func TestAutoSnapshotIsThrottled(t *testing.T) {
	h, db := setup(t)

	if _, err := h.Snapshot("p", "start", store.KindAuto); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// A small edit right after the first snapshot is not worth keeping.
	if v, err := h.Snapshot("p", "start plus a tiny edit", store.KindAuto); err != nil {
		t.Fatalf("Snapshot: %v", err)
	} else if v != nil {
		t.Error("a trivial change should not create a snapshot")
	}

	// A large change is.
	big := strings.Repeat("新增内容", 40)
	if v, err := h.Snapshot("p", big, store.KindAuto); err != nil {
		t.Fatalf("Snapshot: %v", err)
	} else if v == nil {
		t.Error("a substantial change should create a snapshot")
	}

	list, _ := db.ListVersions("p")
	if len(list) != 2 {
		t.Errorf("got %d snapshots, want 2", len(list))
	}
}

// Manual snapshots bypass the throttle: the user asked for them.
func TestManualSnapshotAlwaysTaken(t *testing.T) {
	h, db := setup(t)
	for i, body := range []string{"a", "ab", "abc"} {
		if v, err := h.Snapshot("p", body, store.KindManual); err != nil {
			t.Fatalf("Snapshot %d: %v", i, err)
		} else if v == nil {
			t.Fatalf("manual snapshot %d was skipped", i)
		}
	}
	list, _ := db.ListVersions("p")
	if len(list) != 3 {
		t.Errorf("got %d manual snapshots, want 3", len(list))
	}
}

func TestPruneKeepsRecentAndManual(t *testing.T) {
	h, db := setup(t)
	now := time.Now()

	add := func(kind string, age time.Duration, body string) {
		v := &store.Version{
			PostID: "p", BlobPath: "p/x.md.zst", ContentHash: body,
			Kind: kind, CreatedAt: now.Add(-age).Unix(),
		}
		if err := db.AddVersion(v); err != nil {
			t.Fatalf("AddVersion: %v", err)
		}
	}

	add(store.KindAuto, 1*time.Hour, "recent")             // within 24h: kept
	add(store.KindManual, 40*24*time.Hour, "old-manual")   // manual: kept forever
	// Three automatic snapshots in the same old hour: only one survives.
	add(store.KindAuto, 48*time.Hour, "old-1")
	add(store.KindAuto, 48*time.Hour+time.Minute, "old-2")
	add(store.KindAuto, 48*time.Hour+2*time.Minute, "old-3")

	if _, err := h.Prune(); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	left, _ := db.ListVersions("p")
	var manual, auto int
	for _, v := range left {
		if v.Kind == store.KindManual {
			manual++
		} else {
			auto++
		}
	}
	if manual != 1 {
		t.Errorf("manual snapshots = %d, want 1 (they never expire)", manual)
	}
	// recent + one representative of the old hour
	if auto != 2 {
		t.Errorf("auto snapshots = %d, want 2", auto)
	}
}

func TestSnapshotRequiresPostID(t *testing.T) {
	h, _ := setup(t)
	if _, err := h.Snapshot("", "body", store.KindManual); err == nil {
		t.Error("expected an error with no post id")
	}
}
