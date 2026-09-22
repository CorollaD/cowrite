// Package index reconciles the on-disk workspace with the SQLite index.
//
// Disk always wins: the index is rebuilt from files, never the reverse.
package index

import (
	"fmt"
	"log/slog"

	"github.com/corollad/cowrite/internal/store"
	"github.com/corollad/cowrite/internal/workspace"
)

type Index struct {
	ws  *workspace.Workspace
	db  *store.DB
	log *slog.Logger
}

func New(ws *workspace.Workspace, db *store.DB, log *slog.Logger) *Index {
	return &Index{ws: ws, db: db, log: log}
}

// Sync scans the workspace and brings the index in line with it.
//
// Files whose size and mtime are unchanged are skipped without hashing,
// which keeps startup cheap on a large workspace.
func (ix *Index) Sync() error {
	entries, err := ix.ws.Scan()
	if err != nil {
		return err
	}

	known, err := ix.db.AllPaths()
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[e.RelPath] = true

		if prior, ok := known[e.RelPath]; ok &&
			prior.Size == e.Size &&
			prior.MTime == e.MTime.Unix() &&
			prior.DeletedAt == nil {
			continue // unchanged
		}
		if err := ix.upsert(e); err != nil {
			ix.log.Warn("index entry failed", "path", e.RelPath, "err", err)
		}
	}

	// Anything indexed but no longer on disk is gone.
	for relPath, p := range known {
		if seen[relPath] || p.DeletedAt != nil {
			continue
		}
		if err := ix.db.SoftDeletePost(p.ID); err != nil {
			ix.log.Warn("soft delete failed", "path", relPath, "err", err)
		}
	}
	return nil
}

// IndexFile reindexes a single file, used by the watcher.
func (ix *Index) IndexFile(path string) error {
	e, err := ix.ws.Load(path)
	if err != nil {
		return err
	}
	return ix.upsert(*e)
}

// upsert writes one entry, assigning identity to files that lack front
// matter so hand-dropped markdown becomes a first-class post.
func (ix *Index) upsert(e workspace.Entry) error {
	post := e.Post
	if post.Meta.ID == "" {
		post.EnsureIDs(e.MTime)
		saved, err := ix.ws.Save(e.Path, post, e.Hash)
		if err != nil {
			return fmt.Errorf("stamp identity onto %s: %w", e.RelPath, err)
		}
		e.Hash, e.Size, e.MTime = saved.Hash, saved.Size, saved.MTime
	}

	created := post.Meta.Created
	if created.IsZero() {
		created = e.MTime
	}
	updated := post.Meta.Updated
	if updated.IsZero() {
		updated = e.MTime
	}

	return ix.db.UpsertPost(&store.Post{
		ID:          post.Meta.ID,
		Path:        e.RelPath,
		Title:       post.Meta.Title,
		Slug:        workspace.Slugify(post.Meta.Title),
		Tags:        store.EncodeTags(post.Meta.Tags),
		ContentHash: e.Hash,
		MTime:       e.MTime.Unix(),
		Size:        e.Size,
		WordCount:   workspace.WordCount(post.Body),
		CreatedAt:   created.Unix(),
		UpdatedAt:   updated.Unix(),
	})
}

// Remove marks a path as deleted in the index.
func (ix *Index) Remove(relPath string) error {
	p, err := ix.db.GetPostByPath(relPath)
	if err != nil {
		return err
	}
	return ix.db.SoftDeletePost(p.ID)
}
