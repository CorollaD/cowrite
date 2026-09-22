-- +goose Up
CREATE TABLE publish_records (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    post_id       TEXT NOT NULL,
    platform      TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    remote_id     TEXT NOT NULL DEFAULT '',
    remote_url    TEXT NOT NULL DEFAULT '',
    content_hash  TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_publish_post ON publish_records(post_id, created_at DESC);

-- +goose Down
DROP TABLE publish_records;
