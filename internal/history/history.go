// Package history keeps point-in-time copies of posts.
//
// Snapshots are whole compressed copies rather than a delta chain: an
// article is a few kilobytes, zstd takes it to about a third of that, and a
// full copy can always be restored on its own even if neighbours are gone.
package history

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/corollad/cowrite/internal/store"
	"github.com/corollad/cowrite/internal/workspace"
	"github.com/klauspost/compress/zstd"
)

// Thresholds for taking an automatic snapshot. Autosave fires every few
// seconds, so snapshotting on every save would bury real checkpoints; a
// snapshot is worth taking once enough time or enough text has changed.
const (
	minInterval    = 5 * time.Minute
	minWordsChange = 60
)

type History struct {
	dir string
	db  *store.DB
}

func New(metaDir string, db *store.DB) *History {
	return &History{dir: filepath.Join(metaDir, "versions"), db: db}
}

// Snapshot stores a copy of body for post id.
//
// For automatic snapshots it first checks whether one is warranted; manual
// and pre-publish snapshots are always taken.
func (h *History) Snapshot(postID, body, kind string) (*store.Version, error) {
	if postID == "" {
		return nil, fmt.Errorf("no post id")
	}
	hash := workspace.HashContent([]byte(body))
	words := workspace.WordCount(body)

	last, _ := h.db.LatestVersion(postID)
	if last != nil && last.ContentHash == hash {
		return last, nil // nothing changed
	}
	if kind == store.KindAuto && !h.worthTaking(last, words) {
		return nil, nil
	}

	now := time.Now()
	rel := filepath.Join(postID, fmt.Sprintf("%d.md.zst", now.UnixNano()))
	full := filepath.Join(h.dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, fmt.Errorf("create version dir: %w", err)
	}

	blob, err := compress([]byte(body))
	if err != nil {
		return nil, err
	}
	if err := workspace.WriteAtomic(full, blob, 0o644); err != nil {
		return nil, fmt.Errorf("write snapshot: %w", err)
	}

	v := &store.Version{
		PostID: postID, BlobPath: rel, ContentHash: hash,
		Kind: kind, WordCount: words, CreatedAt: now.Unix(),
	}
	if err := h.db.AddVersion(v); err != nil {
		return nil, err
	}
	return v, nil
}

func (h *History) worthTaking(last *store.Version, words int) bool {
	if last == nil {
		return true
	}
	if time.Since(time.Unix(last.CreatedAt, 0)) >= minInterval {
		return true
	}
	delta := words - last.WordCount
	if delta < 0 {
		delta = -delta
	}
	return delta >= minWordsChange
}

// Restore returns the body stored in a version.
func (h *History) Restore(v *store.Version) (string, error) {
	blob, err := os.ReadFile(filepath.Join(h.dir, v.BlobPath))
	if err != nil {
		return "", fmt.Errorf("read snapshot: %w", err)
	}
	body, err := decompress(blob)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// Prune drops automatic snapshots that fall outside the retention policy,
// removing both the row and the blob it points at.
func (h *History) Prune() (int, error) {
	stale, err := h.db.PrunableVersions(time.Now())
	if err != nil {
		return 0, err
	}
	for _, v := range stale {
		_ = os.Remove(filepath.Join(h.dir, v.BlobPath))
		if err := h.db.DeleteVersion(v.ID); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}

func compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, fmt.Errorf("compress: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	return buf.Bytes(), nil
}

func decompress(data []byte) ([]byte, error) {
	r, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	return out, nil
}
