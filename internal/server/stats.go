package server

import (
	"encoding/json"
	"net/http"

	"github.com/corollad/cowrite/internal/workspace"
)

type statsRequest struct {
	Body string `json:"body"`
}

// handleStats measures the text currently in the editor.
//
// Counting runs here rather than in the browser so the editor, the post
// list and anything else all report the same number.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	var req statsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	writeJSON(w, http.StatusOK, workspace.Analyze(req.Body))
}
