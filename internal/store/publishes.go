package store

import "fmt"

type PublishRecord struct {
	ID          int64  `db:"id"           json:"id"`
	PostID      string `db:"post_id"      json:"postId"`
	Platform    string `db:"platform"     json:"platform"`
	Status      string `db:"status"       json:"status"`
	RemoteID    string `db:"remote_id"    json:"remoteId"`
	RemoteURL   string `db:"remote_url"   json:"remoteUrl"`
	ContentHash string `db:"content_hash" json:"contentHash"`
	Error       string `db:"error"        json:"error"`
	CreatedAt   int64  `db:"created_at"   json:"createdAt"`
}

func (db *DB) AddPublishRecord(r *PublishRecord) error {
	res, err := db.NamedExec(
		`INSERT INTO publish_records
		 (post_id, platform, status, remote_id, remote_url, content_hash, error, created_at)
		 VALUES (:post_id, :platform, :status, :remote_id, :remote_url, :content_hash, :error, :created_at)`,
		r)
	if err != nil {
		return fmt.Errorf("add publish record: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) ListPublishRecords(postID string) ([]PublishRecord, error) {
	var out []PublishRecord
	err := db.Select(&out,
		`SELECT * FROM publish_records WHERE post_id = ? ORDER BY created_at DESC`, postID)
	if err != nil {
		return nil, fmt.Errorf("list publish records: %w", err)
	}
	return out, nil
}
