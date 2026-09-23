package server

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/corollad/cowrite/internal/export"
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
	case "docx":
		res, err := s.renderer.Render(entry.Post.Body, render.ProfileGeneric, theme)
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "render post", err)
			return
		}
		data, err := export.DOCX(res.HTML, p.Title)
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "build docx", err)
			return
		}
		setDownloadHeaders(w, name+".docx",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		_, _ = w.Write(data)

	case "pdf":
		// Rendered as a print-ready page rather than a PDF built in Go:
		// every Go PDF library needs an embedded CJK font, and the system
		// ones are .ttc collections that are not redistributable. The
		// browser already has the fonts and a print engine.
		res, err := s.renderer.Render(entry.Post.Body, render.ProfilePreview, theme)
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "render post", err)
			return
		}
		doc := fmt.Sprintf(printableHTML, htmlEscape(p.Title), res.CSS, res.HTML)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(doc))

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

// printableHTML opens the print dialog once the page has rendered, so
// "export PDF" is one click even though the browser does the work.
const printableHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>%s</title>
<style>
@page { margin: 18mm 16mm; }
body { max-width: 760px; margin: 0 auto; padding: 0 16px; }
@media print { body { max-width: none; padding: 0; } .hint { display: none; } }
.hint {
  position: fixed; top: 0; left: 0; right: 0; padding: 9px 14px;
  background: #1f2937; color: #fff; font: 13px/1.5 -apple-system, "PingFang SC", sans-serif;
  text-align: center;
}
.hint + * { margin-top: 44px; }
%s
</style>
</head>
<body>
<div class="hint">在打印对话框里选择「存储为 PDF」即可导出。未自动弹出时按 ⌘P / Ctrl+P。</div>
%s
<script>
  addEventListener('load', () => setTimeout(() => window.print(), 400));
</script>
</body>
</html>
`

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
