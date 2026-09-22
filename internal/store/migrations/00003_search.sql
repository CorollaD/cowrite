-- +goose Up
-- Body text is not kept in posts (files are the source of truth), so the
-- search index holds its own copy, rebuilt whenever a post is indexed.
--
-- This is a plain table searched with LIKE rather than FTS5: FTS5 ships no
-- CJK tokenizer, so unicode61 cannot match Chinese at all and trigram
-- misses the two-character terms that most Chinese queries use. For a
-- single-user workspace a scan is fast enough and, unlike FTS5, correct.
CREATE TABLE post_search (
    post_id TEXT PRIMARY KEY,
    title   TEXT NOT NULL DEFAULT '',
    body    TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE post_search;
