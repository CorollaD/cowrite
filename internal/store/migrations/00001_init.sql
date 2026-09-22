-- +goose Up
CREATE TABLE posts (
    id            TEXT PRIMARY KEY,
    path          TEXT NOT NULL UNIQUE,
    title         TEXT NOT NULL DEFAULT '',
    slug          TEXT NOT NULL DEFAULT '',
    tags          TEXT NOT NULL DEFAULT '[]',
    content_hash  TEXT NOT NULL DEFAULT '',
    mtime         INTEGER NOT NULL DEFAULT 0,
    size          INTEGER NOT NULL DEFAULT 0,
    word_count    INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL DEFAULT 0,
    deleted_at    INTEGER
);

CREATE INDEX idx_posts_updated ON posts(updated_at DESC);
CREATE INDEX idx_posts_deleted ON posts(deleted_at);

CREATE TABLE versions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    post_id       TEXT NOT NULL,
    blob_path     TEXT NOT NULL,
    content_hash  TEXT NOT NULL,
    kind          TEXT NOT NULL DEFAULT 'auto',
    word_count    INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_versions_post ON versions(post_id, created_at DESC);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE settings;
DROP TABLE versions;
DROP TABLE posts;
