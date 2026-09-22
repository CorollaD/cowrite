package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func setupSearch(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func addSearchable(t *testing.T, db *DB, id, title, body string) {
	t.Helper()
	if err := db.UpsertPost(&Post{ID: id, Path: id + ".md", Title: title}); err != nil {
		t.Fatalf("UpsertPost: %v", err)
	}
	if err := db.IndexForSearch(id, title, body); err != nil {
		t.Fatalf("IndexForSearch: %v", err)
	}
}

func TestSearchFindsChineseText(t *testing.T) {
	db := setupSearch(t)
	addSearchable(t, db, "p1", "渲染管线", "这篇讲 goldmark 渲染和微信排版。")
	addSearchable(t, db, "p2", "语音输入", "口述之后要做清洗。")

	hits, err := db.Search("微信排版", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].PostID != "p1" {
		t.Fatalf("got %+v, want only p1", hits)
	}
	if hits[0].Snippet == "" {
		t.Error("no snippet returned")
	}
}

func TestSearchMatchesTitle(t *testing.T) {
	db := setupSearch(t)
	addSearchable(t, db, "p1", "语音输入", "正文内容与标题无关。")

	hits, err := db.Search("语音", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("title match failed: %+v", hits)
	}
}

func TestSearchReindexReplacesOldText(t *testing.T) {
	db := setupSearch(t)
	addSearchable(t, db, "p1", "标题", "最初的内容")
	addSearchable(t, db, "p1", "标题", "换掉之后的内容")

	if hits, _ := db.Search("最初", 10); len(hits) != 0 {
		t.Errorf("stale text still searchable: %+v", hits)
	}
	if hits, _ := db.Search("换掉之后", 10); len(hits) != 1 {
		t.Error("new text is not searchable")
	}
}

func TestSearchExcludesDeletedPosts(t *testing.T) {
	db := setupSearch(t)
	addSearchable(t, db, "p1", "要删的", "内容在这里")
	if err := db.SoftDeletePost("p1"); err != nil {
		t.Fatalf("SoftDeletePost: %v", err)
	}
	if hits, _ := db.Search("内容", 10); len(hits) != 0 {
		t.Errorf("deleted post still in results: %+v", hits)
	}
}

// Punctuation must not be interpreted as FTS query syntax.
func TestSearchHandlesQuerySyntaxSafely(t *testing.T) {
	db := setupSearch(t)
	addSearchable(t, db, "p1", "标题", "普通内容")

	for _, q := range []string{`"`, `a OR b`, `NEAR(`, `*`, `""`} {
		if _, err := db.Search(q, 10); err != nil {
			t.Errorf("query %q returned an error: %v", q, err)
		}
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	db := setupSearch(t)
	hits, err := db.Search("   ", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Error("blank query should match nothing")
	}
}

func TestSearchSnippetMarksMatch(t *testing.T) {
	db := setupSearch(t)
	addSearchable(t, db, "p1", "标题", "前面一些字 关键词 后面一些字")

	hits, _ := db.Search("关键词", 10)
	if len(hits) != 1 {
		t.Fatalf("no hit")
	}
	if !strings.Contains(hits[0].Snippet, "[") {
		t.Errorf("match is not highlighted: %q", hits[0].Snippet)
	}
}
