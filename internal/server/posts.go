package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"github.com/corollad/cowrite/internal/store"
	"github.com/corollad/cowrite/internal/workspace"
	"github.com/go-chi/chi/v5"
)

type postSummary struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Path      string   `json:"path"`
	Tags      []string `json:"tags"`
	WordCount int      `json:"wordCount"`
	UpdatedAt int64    `json:"updatedAt"`
}

type postDetail struct {
	postSummary
	Body string `json:"body"`
	// Hash is the editor's concurrency token: it must be sent back on save
	// so a write based on stale content can be rejected instead of
	// silently overwriting someone else's edit.
	Hash string `json:"hash"`
}

func (s *Server) handleListPosts(w http.ResponseWriter, r *http.Request) {
	posts, err := s.db.ListPosts()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "list posts", err)
		return
	}
	out := make([]postSummary, 0, len(posts))
	for _, p := range posts {
		out = append(out, summarize(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetPost(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetPost(chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "post not found", err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "get post", err)
		return
	}

	// Read the body from disk, not the index: the file is the source of
	// truth and may have changed since it was indexed.
	e, err := s.ws.Load(filepath.Join(s.ws.Root, p.Path))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read post file", err)
		return
	}
	writeJSON(w, http.StatusOK, postDetail{
		postSummary: summarize(*p),
		Body:        e.Post.Body,
		Hash:        e.Hash,
	})
}

type createPostRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	var req createPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	now := time.Now()
	post := &workspace.Post{
		Meta: workspace.Meta{Title: req.Title},
		Body: req.Body,
	}
	post.EnsureIDs(now)

	path := s.ws.NewPostPath(post.Meta.Title, now)
	e, err := s.ws.Save(path, post, "")
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "create post", err)
		return
	}
	if err := s.ix.IndexFile(e.Path); err != nil {
		s.fail(w, http.StatusInternalServerError, "index new post", err)
		return
	}

	p, err := s.db.GetPost(post.Meta.ID)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "reload new post", err)
		return
	}
	writeJSON(w, http.StatusCreated, postDetail{
		postSummary: summarize(*p), Body: post.Body, Hash: e.Hash,
	})
}

type updatePostRequest struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Tags     []string `json:"tags"`
	BaseHash string   `json:"baseHash"`
}

type conflictResponse struct {
	Error    string `json:"error"`
	DiskBody string `json:"diskBody"`
	DiskHash string `json:"diskHash"`
}

func (s *Server) handleUpdatePost(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetPost(chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "post not found", err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "get post", err)
		return
	}

	var req updatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	path := filepath.Join(s.ws.Root, p.Path)
	current, err := s.ws.Load(path)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read post file", err)
		return
	}

	post := current.Post
	post.Body = req.Body
	if req.Title != "" {
		post.Meta.Title = req.Title
	} else {
		post.Meta.Title = workspace.DeriveTitle(req.Body)
	}
	if req.Tags != nil {
		post.Meta.Tags = req.Tags
	}
	post.Meta.Updated = time.Now()

	e, err := s.ws.Save(path, post, req.BaseHash)
	var ce *workspace.ConflictError
	if errors.As(err, &ce) {
		// 409 rather than an overwrite: the editor shows both versions and
		// lets the user decide.
		writeJSON(w, http.StatusConflict, conflictResponse{
			Error:    "file changed on disk",
			DiskBody: ce.DiskBody,
			DiskHash: ce.DiskHash,
		})
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "save post", err)
		return
	}
	if err := s.ix.IndexFile(e.Path); err != nil {
		s.fail(w, http.StatusInternalServerError, "reindex post", err)
		return
	}

	updated, err := s.db.GetPost(p.ID)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "reload post", err)
		return
	}
	writeJSON(w, http.StatusOK, postDetail{
		postSummary: summarize(*updated), Body: post.Body, Hash: e.Hash,
	})
}

func (s *Server) handleDeletePost(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetPost(chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "post not found", err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "get post", err)
		return
	}
	// The file is left on disk on purpose: deleting is the user's call to
	// make in their own file manager, and this keeps the tool from ever
	// destroying writing.
	if err := s.db.SoftDeletePost(p.ID); err != nil {
		s.fail(w, http.StatusInternalServerError, "delete post", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func summarize(p store.Post) postSummary {
	tags := p.TagList()
	if tags == nil {
		tags = []string{}
	}
	return postSummary{
		ID: p.ID, Title: p.Title, Path: p.Path, Tags: tags,
		WordCount: p.WordCount, UpdatedAt: p.UpdatedAt,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, code int, msg string, err error) {
	if code >= 500 {
		s.log.Error(msg, "err", err)
	}
	writeJSON(w, code, map[string]string{"error": msg})
}
