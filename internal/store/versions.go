package store

import (
	"fmt"
	"time"
)

// Snapshot kinds. Manual and pre-publish snapshots are kept forever;
// automatic ones are thinned out over time.
const (
	KindAuto       = "auto"
	KindManual     = "manual"
	KindPrePublish = "pre-publish"
	KindPreMerge   = "pre-merge"
)

type Version struct {
	ID          int64  `db:"id"           json:"id"`
	PostID      string `db:"post_id"      json:"postId"`
	BlobPath    string `db:"blob_path"    json:"-"`
	ContentHash string `db:"content_hash" json:"hash"`
	Kind        string `db:"kind"         json:"kind"`
	WordCount   int    `db:"word_count"   json:"wordCount"`
	CreatedAt   int64  `db:"created_at"   json:"createdAt"`
}

func (db *DB) AddVersion(v *Version) error {
	res, err := db.NamedExec(
		`INSERT INTO versions (post_id, blob_path, content_hash, kind, word_count, created_at)
		 VALUES (:post_id, :blob_path, :content_hash, :kind, :word_count, :created_at)`, v)
	if err != nil {
		return fmt.Errorf("add version: %w", err)
	}
	v.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) ListVersions(postID string) ([]Version, error) {
	var out []Version
	err := db.Select(&out,
		`SELECT * FROM versions WHERE post_id = ? ORDER BY created_at DESC, id DESC`, postID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	return out, nil
}

func (db *DB) GetVersion(id int64) (*Version, error) {
	var v Version
	if err := db.Get(&v, `SELECT * FROM versions WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("get version %d: %w", id, err)
	}
	return &v, nil
}

// LatestVersion returns the most recent snapshot, used to decide whether
// enough has changed to take another one.
func (db *DB) LatestVersion(postID string) (*Version, error) {
	var v Version
	err := db.Get(&v,
		`SELECT * FROM versions WHERE post_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`,
		postID)
	if err != nil {
		return nil, nil // no snapshots yet
	}
	return &v, nil
}

// PrunableVersions returns automatic snapshots outside the retention
// policy: everything from the last day is kept, then one per hour for a
// week, then one per day. Manual and pre-publish snapshots never expire.
func (db *DB) PrunableVersions(now time.Time) ([]Version, error) {
	var all []Version
	err := db.Select(&all,
		`SELECT * FROM versions WHERE kind = ? ORDER BY created_at DESC`, KindAuto)
	if err != nil {
		return nil, fmt.Errorf("list prunable versions: %w", err)
	}

	dayAgo := now.Add(-24 * time.Hour).Unix()
	weekAgo := now.Add(-7 * 24 * time.Hour).Unix()

	seen := make(map[string]bool)
	var prune []Version
	for _, v := range all {
		if v.CreatedAt >= dayAgo {
			continue // keep everything from the last 24h
		}
		t := time.Unix(v.CreatedAt, 0)
		var bucket string
		if v.CreatedAt >= weekAgo {
			bucket = t.Format("2006-01-02T15") // hourly
		} else {
			bucket = t.Format("2006-01-02") // daily
		}
		if seen[bucket] {
			prune = append(prune, v) // a newer one already represents this bucket
			continue
		}
		seen[bucket] = true
	}
	return prune, nil
}

func (db *DB) DeleteVersion(id int64) error {
	if _, err := db.Exec(`DELETE FROM versions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete version %d: %w", id, err)
	}
	return nil
}
