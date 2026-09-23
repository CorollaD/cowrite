package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/corollad/cowrite/internal/workspace"
	"github.com/go-chi/chi/v5"
)

// maxSoundBytes bounds an upload. Ambient loops are short; anything much
// larger is a whole album and belongs somewhere else.
const maxSoundBytes = 40 << 20

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

// handleUploadSound stores an audio file the user picked in the browser,
// so adding a loop does not mean finding the workspace directory by hand.
func (s *Server) handleUploadSound(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxSoundBytes); err != nil {
		s.fail(w, http.StatusBadRequest, "读取上传内容失败", err)
		return
	}
	file, header, err := r.FormFile("audio")
	if err != nil {
		s.fail(w, http.StatusBadRequest, "缺少音频文件", err)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if _, ok := soundExts[ext]; !ok {
		s.fail(w, http.StatusBadRequest,
			"只支持 mp3 / ogg / wav / m4a / flac / opus", nil)
		return
	}

	name := safeSoundName(header.Filename)
	if name == "" {
		s.fail(w, http.StatusBadRequest, "文件名无效", nil)
		return
	}

	dir := s.soundsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.fail(w, http.StatusInternalServerError, "创建音频目录失败", err)
		return
	}

	// Never silently replace an existing loop.
	dest := filepath.Join(dir, name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		dest = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
	}

	data, err := io.ReadAll(io.LimitReader(file, maxSoundBytes+1))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "读取音频失败", err)
		return
	}
	if len(data) > maxSoundBytes {
		s.fail(w, http.StatusRequestEntityTooLarge, "文件太大（上限 40MB）", nil)
		return
	}
	if err := workspace.WriteAtomic(dest, data, 0o644); err != nil {
		s.fail(w, http.StatusInternalServerError, "保存音频失败", err)
		return
	}

	final := filepath.Base(dest)
	writeJSON(w, http.StatusCreated, map[string]string{
		"file": final,
		"name": strings.TrimSuffix(final, filepath.Ext(final)),
	})
}

// handleDeleteSound removes an uploaded loop.
func (s *Server) handleDeleteSound(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name != filepath.Base(name) || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	if _, ok := soundExts[strings.ToLower(filepath.Ext(name))]; !ok {
		http.NotFound(w, r)
		return
	}
	if err := os.Remove(filepath.Join(s.soundsDir(), name)); err != nil && !os.IsNotExist(err) {
		s.fail(w, http.StatusInternalServerError, "删除失败", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// safeSoundName reduces an uploaded filename to something that cannot
// escape the sounds directory or confuse the filesystem, while keeping
// CJK so a Chinese filename survives.
func safeSoundName(orig string) string {
	base := filepath.Base(orig)
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	var sb strings.Builder
	for _, r := range stem {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == 0:
			sb.WriteRune('-')
		case unicode.IsControl(r):
			// drop
		default:
			sb.WriteRune(r)
		}
	}
	clean := strings.TrimSpace(sb.String())
	clean = strings.Trim(clean, ".")
	if clean == "" {
		clean = "sound"
	}
	if r := []rune(clean); len(r) > 60 {
		clean = string(r[:60])
	}
	return clean + ext
}
