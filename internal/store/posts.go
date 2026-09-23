package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("post not found")

// Post is an index row. The body is never stored here: the file on disk is
// the source of truth, and this table only carries what listing and search
// need without opening every file.
type Post struct {
	ID          string `db:"id"`
	Path        string `db:"path"`
	Title       string `db:"title"`
	Slug        string `db:"slug"`
	Tags        string `db:"tags"`
	ContentHash string `db:"content_hash"`
	MTime       int64  `db:"mtime"`
	Size        int64  `db:"size"`
	WordCount   int    `db:"word_count"`
	CreatedAt   int64  `db:"created_at"`
	UpdatedAt   int64  `db:"updated_at"`
	DeletedAt   *int64 `db:"deleted_at"`
}

func (p *Post) TagList() []string {
	var tags []string
	if p.Tags == "" {
		return nil
	}
	_ = json.Unmarshal([]byte(p.Tags), &tags)
	return tags
}

func EncodeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(b)
}

const upsertPostFields = `
    path = excluded.path, title = excluded.title, slug = excluded.slug,
    tags = excluded.tags, content_hash = excluded.content_hash,
    mtime = excluded.mtime, size = excluded.size,
    word_count = excluded.word_count, updated_at = excluded.updated_at,
    deleted_at = NULL`

const upsertPost = `
INSERT INTO posts (id, path, title, slug, tags, content_hash, mtime, size,
                   word_count, created_at, updated_at, deleted_at)
VALUES (:id, :path, :title, :slug, :tags, :content_hash, :mtime, :size,
        :word_count, :created_at, :updated_at, NULL)
ON CONFLICT(id) DO UPDATE SET` + upsertPostFields

// UpsertPost writes an index row, clearing any soft-delete mark since the
// post evidently exists again.
//
// Both id and path are unique. A post can arrive with a new id at a path
// some older row still occupies, usually because a file was deleted and
// another created in its place, so that row is released first rather than
// letting the write fail and leaving the file permanently unindexed.
func (db *DB) UpsertPost(p *Post) error {
	if _, err := db.NamedExec(upsertPost, p); err == nil {
		return nil
	}

	var holder string
	err := db.Get(&holder, `SELECT id FROM posts WHERE path = ? AND id != ?`,
		p.Path, p.ID)
	if err != nil {
		// The conflict was not on path after all.
		if _, err := db.NamedExec(upsertPost, p); err != nil {
			return fmt.Errorf("upsert post %s: %w", p.ID, err)
		}
		return nil
	}

	// Free the path, keeping the old row addressable by id so publish
	// history and version snapshots still resolve.
	if _, err := db.Exec(
		`UPDATE posts SET path = ?, deleted_at = COALESCE(deleted_at, ?) WHERE id = ?`,
		"（已移除）"+holder, time.Now().Unix(), holder); err != nil {
		return fmt.Errorf("release path %s: %w", p.Path, err)
	}
	if _, err := db.NamedExec(upsertPost, p); err != nil {
		return fmt.Errorf("upsert post %s: %w", p.ID, err)
	}
	return nil
}

func (db *DB) GetPost(id string) (*Post, error) {
	var p Post
	err := db.Get(&p, `SELECT * FROM posts WHERE id = ? AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get post %s: %w", id, err)
	}
	return &p, nil
}

func (db *DB) GetPostByPath(path string) (*Post, error) {
	var p Post
	err := db.Get(&p, `SELECT * FROM posts WHERE path = ?`, path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get post at %s: %w", path, err)
	}
	return &p, nil
}

func (db *DB) ListPosts() ([]Post, error) {
	var posts []Post
	err := db.Select(&posts,
		`SELECT * FROM posts WHERE deleted_at IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	return posts, nil
}

// AllPaths returns every indexed path, including soft-deleted ones, so a
// scan can tell which rows it did not see on disk.
func (db *DB) AllPaths() (map[string]Post, error) {
	var posts []Post
	if err := db.Select(&posts, `SELECT * FROM posts`); err != nil {
		return nil, fmt.Errorf("list all paths: %w", err)
	}
	byPath := make(map[string]Post, len(posts))
	for _, p := range posts {
		byPath[p.Path] = p
	}
	return byPath, nil
}

// SoftDeletePost marks a post gone without dropping the row, so publish
// history and version snapshots keep resolving.
func (db *DB) SoftDeletePost(id string) error {
	_, err := db.Exec(`UPDATE posts SET deleted_at = ? WHERE id = ?`,
		time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("soft delete post %s: %w", id, err)
	}
	return nil
}
