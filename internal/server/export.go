package server

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/corollad/cowrite/internal/render"
	"github.com/go-chi/chi/v5"
)

// handleExport downloads a post as markdown or standalone HTML.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetPost(chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, http.StatusNotFound, "post not found", err)
		return
	}
	entry, err := s.ws.Load(filepath.Join(s.ws.Root, p.Path))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read post file", err)
		return
	}

	format := r.URL.Query().Get("format")
	theme := r.URL.Query().Get("theme")
	name := sanitizeFilename(p.Title)

	switch format {
	case "html":
		res, err := s.renderer.Render(entry.Post.Body, render.ProfilePreview, theme)
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "render post", err)
			return
		}
		// Self-contained: styles inline in a <style> block so the file
		// opens correctly anywhere, with no sidecar assets.
		doc := fmt.Sprintf(
			"<!doctype html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"utf-8\">\n"+
				"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n"+
				"<title>%s</title>\n<style>\nbody{max-width:740px;margin:40px auto;padding:0 20px;}\n%s\n</style>\n"+
				"</head>\n<body>\n%s\n</body>\n</html>\n",
			htmlEscape(p.Title), res.CSS, res.HTML)

		setDownloadHeaders(w, name+".html", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(doc))

	default: // markdown, including the front matter so a round trip is lossless
		data, err := entry.Post.Bytes()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "encode post", err)
			return
		}
		setDownloadHeaders(w, name+".md", "text/markdown; charset=utf-8")
		_, _ = w.Write(data)
	}
}

func setDownloadHeaders(w http.ResponseWriter, filename, contentType string) {
	w.Header().Set("Content-Type", contentType)
	// filename* carries the UTF-8 name; filename is the ASCII fallback for
	// clients that do not understand RFC 5987.
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`,
		asciiFallback(filename), url.PathEscape(filename)))
}

// sanitizeFilename strips characters that are awkward in a filename,
// keeping CJK since a Chinese title would otherwise vanish entirely.
func sanitizeFilename(title string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-",
		"?", "", `"`, "", "<", "", ">", "", "|", "-", "\n", " ")
	name := strings.TrimSpace(replacer.Replace(title))
	if name == "" {
		return "untitled"
	}
	r := []rune(name)
	if len(r) > 80 {
		name = string(r[:80])
	}
	return name
}

func asciiFallback(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if r < 128 {
			sb.WriteRune(r)
		}
	}
	out := strings.TrimSpace(sb.String())
	if out == "" || out == ".md" || out == ".html" {
		return "post" + filepath.Ext(name)
	}
	return out
}

func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
