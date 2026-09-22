package server

import (
	"net/http"
	"strconv"

	"github.com/corollad/cowrite/internal/store"
)

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hits, err := s.db.Search(r.URL.Query().Get("q"), limit)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "search", err)
		return
	}
	if hits == nil {
		hits = []store.SearchHit{}
	}
	writeJSON(w, http.StatusOK, hits)
}
