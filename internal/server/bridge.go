package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/corollad/cowrite/internal/publish/bridge"
	"github.com/corollad/cowrite/internal/render"
	"github.com/corollad/cowrite/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// extensionPlatforms are the targets reached through the companion
// extension, for platforms with no usable write API.
var extensionPlatforms = map[string]string{
	"zhihu":  "知乎",
	"juejin": "掘金",
}

func (s *Server) handleBridgeStatus(w http.ResponseWriter, r *http.Request) {
	platforms := make([]map[string]string, 0, len(extensionPlatforms))
	for id, name := range extensionPlatforms {
		platforms = append(platforms, map[string]string{"id": id, "name": name})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connected": s.bridge.Connected(),
		// The pairing token is shown so the user can copy it into the
		// extension. It never leaves this machine.
		"token":     s.bridge.Token(),
		"platforms": platforms,
	})
}

type extPublishRequest struct {
	Platform string `json:"platform"`
	Theme    string `json:"theme"`
}

// handlePublishExtension renders a post and hands it to the extension,
// which fills the platform's own editor.
func (s *Server) handlePublishExtension(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	p, err := s.db.GetPost(postID)
	if err != nil {
		s.fail(w, http.StatusNotFound, "post not found", err)
		return
	}

	var req extPublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	if _, ok := extensionPlatforms[req.Platform]; !ok {
		s.fail(w, http.StatusBadRequest, "不支持的平台", nil)
		return
	}
	if !s.bridge.Connected() {
		s.fail(w, http.StatusBadRequest,
			"浏览器扩展未连接：请先安装扩展并填入配对码", nil)
		return
	}

	entry, err := s.ws.Load(filepath.Join(s.ws.Root, p.Path))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read post file", err)
		return
	}

	// Generic HTML rather than the WeChat profile: these editors accept
	// ordinary semantic markup and apply their own styling.
	rendered, err := s.renderer.Render(entry.Post.Body, render.ProfileGeneric, req.Theme)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "render post", err)
		return
	}

	if _, err := s.history.Snapshot(postID, entry.Post.Body, store.KindPrePublish); err != nil {
		s.log.Warn("pre-publish snapshot failed", "post", postID, "err", err)
	}

	// Opening a tab and waiting for an editor to mount is slow, so this
	// allows more time than an API call would.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	res, err := s.bridge.Publish(ctx, bridge.Request{
		ID:       uuid.NewString(),
		Platform: req.Platform,
		Title:    entry.Post.Meta.Title,
		HTML:     rendered.HTML,
		Markdown: entry.Post.Body,
	})

	rec := &store.PublishRecord{
		PostID: postID, Platform: req.Platform,
		ContentHash: entry.Hash, CreatedAt: time.Now().Unix(),
	}
	if err != nil {
		rec.Status, rec.Error = "failed", err.Error()
		_ = s.db.AddPublishRecord(rec)
		s.fail(w, http.StatusBadGateway, err.Error(), err)
		return
	}
	if !res.OK {
		rec.Status, rec.Error = "failed", res.Error
		_ = s.db.AddPublishRecord(rec)
		s.fail(w, http.StatusBadGateway, res.Error, nil)
		return
	}

	// The article is filled into the editor, not posted: the user reviews
	// and publishes it themselves on the platform.
	rec.Status, rec.RemoteURL = "filled", res.URL
	if err := s.db.AddPublishRecord(rec); err != nil {
		s.log.Warn("record publish failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "filled", "url": res.URL,
	})
}
