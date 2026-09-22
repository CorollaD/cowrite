package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/corollad/cowrite/internal/store"
	"github.com/corollad/cowrite/internal/workspace"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.db.ListVersions(chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "list versions", err)
		return
	}
	if versions == nil {
		versions = []store.Version{}
	}
	writeJSON(w, http.StatusOK, versions)
}

// handleGetVersion returns the body stored in a snapshot so the browser can
// show it before deciding whether to restore.
func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "versionId"), 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "invalid version id", err)
		return
	}
	v, err := s.db.GetVersion(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "version not found", err)
		return
	}
	body, err := s.history.Restore(v)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read snapshot", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": v, "body": body})
}

type restoreRequest struct {
	BaseHash string `json:"baseHash"`
}

// handleRestoreVersion writes a snapshot back over the current file.
//
// The current text is snapshotted first, so restoring is itself undoable.
func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(chi.URLParam(r, "versionId"), 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "invalid version id", err)
		return
	}

	var req restoreRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	p, err := s.db.GetPost(postID)
	if err != nil {
		s.fail(w, http.StatusNotFound, "post not found", err)
		return
	}
	v, err := s.db.GetVersion(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "version not found", err)
		return
	}
	body, err := s.history.Restore(v)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read snapshot", err)
		return
	}

	path := filepath.Join(s.ws.Root, p.Path)
	current, err := s.ws.Load(path)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read post file", err)
		return
	}
	if _, err := s.history.Snapshot(postID, current.Post.Body, store.KindManual); err != nil {
		s.fail(w, http.StatusInternalServerError, "snapshot before restore", err)
		return
	}

	post := current.Post
	post.Body = body
	post.Meta.Title = workspace.DeriveTitle(body)
	post.Meta.Updated = time.Now()

	baseHash := req.BaseHash
	if baseHash == "" {
		baseHash = current.Hash
	}
	entry, err := s.ws.Save(path, post, baseHash)
	var ce *workspace.ConflictError
	if errors.As(err, &ce) {
		writeJSON(w, http.StatusConflict, conflictResponse{
			Error: "file changed on disk", DiskBody: ce.DiskBody, DiskHash: ce.DiskHash,
		})
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "restore version", err)
		return
	}
	if err := s.ix.IndexFile(entry.Path); err != nil {
		s.fail(w, http.StatusInternalServerError, "reindex post", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"body": body, "hash": entry.Hash})
}

// handleSnapshot takes an explicit checkpoint the user asked for.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	p, err := s.db.GetPost(postID)
	if err != nil {
		s.fail(w, http.StatusNotFound, "post not found", err)
		return
	}
	entry, err := s.ws.Load(filepath.Join(s.ws.Root, p.Path))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read post file", err)
		return
	}
	v, err := s.history.Snapshot(postID, entry.Post.Body, store.KindManual)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "snapshot", err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
