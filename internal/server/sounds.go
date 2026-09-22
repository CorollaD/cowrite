package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// soundExts are the formats browsers decode reliably.
var soundExts = map[string]string{
	".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".oga": "audio/ogg",
	".wav": "audio/wav", ".m4a": "audio/mp4", ".flac": "audio/flac",
	".opus": "audio/opus",
}

func (s *Server) soundsDir() string {
	return filepath.Join(s.cfg.Workspace, ".cowrite", "sounds")
}

// handleListSounds reports audio files the user dropped into the workspace,
// so their own recordings sit alongside the synthesised ones.
func (s *Server) handleListSounds(w http.ResponseWriter, r *http.Request) {
	dir := s.soundsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Missing directory just means no custom sounds yet.
		writeJSON(w, http.StatusOK, []map[string]string{})
		return
	}

	out := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if _, ok := soundExts[ext]; !ok {
			continue
		}
		out = append(out, map[string]string{
			"name": strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			"file": e.Name(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetSound serves one audio file from the sounds directory.
func (s *Server) handleGetSound(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	// Only a bare filename: no traversal out of the sounds directory.
	if name != filepath.Base(name) || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	ct, ok := soundExts[ext]
	if !ok {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(s.soundsDir(), name)
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	// ServeContent handles range requests, which audio seeking needs.
	http.ServeContent(w, r, name, info.ModTime(), f)
}
