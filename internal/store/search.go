package store

import (
	"fmt"
	"strings"
)

type SearchHit struct {
	PostID  string `db:"post_id" json:"postId"`
	Title   string `db:"title"   json:"title"`
	Snippet string `json:"snippet"`
}

// IndexForSearch replaces a post's searchable text.
func (db *DB) IndexForSearch(postID, title, body string) error {
	_, err := db.Exec(
		`INSERT INTO post_search (post_id, title, body) VALUES (?, ?, ?)
		 ON CONFLICT(post_id) DO UPDATE SET title = excluded.title, body = excluded.body`,
		postID, title, body)
	if err != nil {
		return fmt.Errorf("index for search: %w", err)
	}
	return nil
}

func (db *DB) RemoveFromSearch(postID string) error {
	if _, err := db.Exec(`DELETE FROM post_search WHERE post_id = ?`, postID); err != nil {
		return fmt.Errorf("remove from search: %w", err)
	}
	return nil
}

type searchRow struct {
	PostID string `db:"post_id"`
	Title  string `db:"title"`
	Body   string `db:"body"`
}

// Search returns posts whose title or body contain the query.
func (db *DB) Search(query string, limit int) ([]SearchHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}

	// escape wildcards so a literal % or _ is not read as a pattern
	pattern := "%" + escapeLike(q) + "%"

	var rows []searchRow
	err := db.Select(&rows, `
		SELECT s.post_id, p.title AS title, s.body AS body
		FROM post_search s
		JOIN posts p ON p.id = s.post_id AND p.deleted_at IS NULL
		WHERE s.title LIKE ? ESCAPE '\' OR s.body LIKE ? ESCAPE '\'
		ORDER BY p.updated_at DESC
		LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	hits := make([]SearchHit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, SearchHit{
			PostID:  r.PostID,
			Title:   r.Title,
			Snippet: excerpt(r.Body, q),
		})
	}
	return hits, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// excerpt returns the text around the first match, marking it so the
// reader can see why a post matched.
func excerpt(body, query string) string {
	const window = 30

	idx := strings.Index(strings.ToLower(body), strings.ToLower(query))
	if idx < 0 {
		return trimRunes(body, 2*window)
	}

	r := []rune(body)
	// Convert the byte offset to a rune offset so CJK is not cut mid-character.
	start := len([]rune(body[:idx]))
	qLen := len([]rune(query))

	from := max(0, start-window)
	to := min(len(r), start+qLen+window)

	var sb strings.Builder
	if from > 0 {
		sb.WriteString("…")
	}
	sb.WriteString(string(r[from:start]))
	sb.WriteString("[")
	sb.WriteString(string(r[start : start+qLen]))
	sb.WriteString("]")
	sb.WriteString(string(r[start+qLen : to]))
	if to < len(r) {
		sb.WriteString("…")
	}
	return sb.String()
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
