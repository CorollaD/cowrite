package server

import (
	"encoding/json"
	"net/http"

	"github.com/corollad/cowrite/internal/render"
)

type renderRequest struct {
	Body    string `json:"body"`
	Profile string `json:"profile"`
	Theme   string `json:"theme"`
}

// handleRender converts markdown to HTML for a target.
//
// Rendering lives on the server so the preview and what gets published come
// from the same pipeline and cannot drift apart.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	var req renderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	profile := render.ProfilePreview
	switch req.Profile {
	case string(render.ProfileWeChat):
		profile = render.ProfileWeChat
	case string(render.ProfileGeneric):
		profile = render.ProfileGeneric
	}

	res, err := s.renderer.Render(req.Body, profile, req.Theme)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "render markdown", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type toMarkdownRequest struct {
	HTML string `json:"html"`
}

// handleToMarkdown converts rich-text edits back to markdown, so the file
// on disk stays markdown whichever mode the author used.
func (s *Server) handleToMarkdown(w http.ResponseWriter, r *http.Request) {
	var req toMarkdownRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	md, err := render.ToMarkdown(req.HTML)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "convert to markdown", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"markdown": md})
}

func (s *Server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, render.Themes())
}
