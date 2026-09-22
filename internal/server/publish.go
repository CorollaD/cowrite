package server

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/corollad/cowrite/internal/publish"
	"github.com/corollad/cowrite/internal/publish/wechat"
	"github.com/corollad/cowrite/internal/render"
	"github.com/corollad/cowrite/internal/store"
	"github.com/go-chi/chi/v5"
	"golang.org/x/net/html"
)

const (
	settingWeChatAppID  = "wechat.appid"
	settingWeChatAuthor = "wechat.author"
	secretWeChatSecret  = "wechat"
)

type wechatConfig struct {
	AppID     string `json:"appId"`
	Author    string `json:"author"`
	HasSecret bool   `json:"hasSecret"`
}

func (s *Server) wechatSettings() (wechatConfig, error) {
	var c wechatConfig
	var err error
	if c.AppID, err = s.db.GetSetting(settingWeChatAppID); err != nil {
		return c, err
	}
	if c.Author, err = s.db.GetSetting(settingWeChatAuthor); err != nil {
		return c, err
	}
	secret, err := s.secrets.Get(secretWeChatSecret)
	if err != nil {
		return c, err
	}
	c.HasSecret = secret != ""
	return c, nil
}

func (s *Server) handleGetPublishConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.wechatSettings()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read publish config", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"wechat": cfg})
}

type savePublishConfigRequest struct {
	AppID     string `json:"appId"`
	Author    string `json:"author"`
	AppSecret string `json:"appSecret"`
}

func (s *Server) handleSavePublishConfig(w http.ResponseWriter, r *http.Request) {
	var req savePublishConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}
	for k, v := range map[string]string{
		settingWeChatAppID:  req.AppID,
		settingWeChatAuthor: req.Author,
	} {
		if err := s.db.SetSetting(k, v); err != nil {
			s.fail(w, http.StatusInternalServerError, "save publish config", err)
			return
		}
	}
	if req.AppSecret != "" {
		if err := s.secrets.Set(secretWeChatSecret, req.AppSecret); err != nil {
			s.fail(w, http.StatusInternalServerError, "save app secret", err)
			return
		}
	}
	cfg, err := s.wechatSettings()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read publish config", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"wechat": cfg})
}

// wechatPublisher builds a publisher from stored settings.
func (s *Server) wechatPublisher() (*wechat.Publisher, error) {
	cfg, err := s.wechatSettings()
	if err != nil {
		return nil, err
	}
	secret, err := s.secrets.Get(secretWeChatSecret)
	if err != nil {
		return nil, err
	}
	if cfg.AppID == "" || secret == "" {
		return nil, errNotConfigured
	}
	// The token is shared through settings so it survives restarts and is
	// not re-minted per process, which would invalidate the live one.
	client := wechat.New(cfg.AppID, secret, settingTokenStore{s.db})
	// Lets the API be pointed at a local stand-in for end-to-end testing
	// without real credentials; unset in normal use.
	if base := os.Getenv("COWRITE_WECHAT_API"); base != "" {
		client.BaseURL = base
	}
	return wechat.NewPublisher(client, cfg.Author), nil
}

type settingTokenStore struct{ db *store.DB }

func (t settingTokenStore) Get(k string) (string, error) { return t.db.GetSetting(k) }
func (t settingTokenStore) Set(k, v string) error        { return t.db.SetSetting(k, v) }

var errNotConfigured = &configError{"公众号尚未配置，请先填写 AppID 和 AppSecret"}

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

func (s *Server) handleValidateWeChat(w http.ResponseWriter, r *http.Request) {
	p, err := s.wechatPublisher()
	if err != nil {
		s.fail(w, http.StatusBadRequest, err.Error(), err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := p.Validate(ctx); err != nil {
		s.fail(w, http.StatusBadGateway, err.Error(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type publishRequest struct {
	Digest string `json:"digest"`
	Theme  string `json:"theme"`
}

// handlePublishWeChat renders the post for WeChat and submits it as a draft.
func (s *Server) handlePublishWeChat(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	p, err := s.db.GetPost(postID)
	if err != nil {
		s.fail(w, http.StatusNotFound, "post not found", err)
		return
	}

	var req publishRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	pub, err := s.wechatPublisher()
	if err != nil {
		s.fail(w, http.StatusBadRequest, err.Error(), err)
		return
	}

	postDir := filepath.Dir(filepath.Join(s.ws.Root, p.Path))
	entry, err := s.ws.Load(filepath.Join(s.ws.Root, p.Path))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read post file", err)
		return
	}

	rendered, err := s.renderer.Render(entry.Post.Body, render.ProfileWeChat, req.Theme)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "render for wechat", err)
		return
	}

	images := collectLocalImages(rendered.HTML, postDir)
	cover, coverCT := loadCover(entry.Post.Meta.Cover, postDir)

	// Snapshot before publishing so there is a record of exactly what went out.
	if _, err := s.history.Snapshot(postID, entry.Post.Body, store.KindPrePublish); err != nil {
		s.log.Warn("pre-publish snapshot failed", "post", postID, "err", err)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	res, err := pub.Publish(ctx, publish.Article{
		Title:   entry.Post.Meta.Title,
		Digest:  req.Digest,
		HTML:    rendered.HTML,
		Cover:   cover,
		CoverCT: coverCT,
		Images:  images,
	})

	rec := &store.PublishRecord{
		PostID: postID, Platform: "wechat",
		ContentHash: entry.Hash, CreatedAt: time.Now().Unix(),
	}
	if err != nil {
		rec.Status, rec.Error = "failed", err.Error()
		_ = s.db.AddPublishRecord(rec)
		s.fail(w, http.StatusBadGateway, err.Error(), err)
		return
	}
	rec.Status, rec.RemoteID, rec.RemoteURL = res.Status, res.RemoteID, res.RemoteURL
	if err := s.db.AddPublishRecord(rec); err != nil {
		s.log.Warn("record publish failed", "err", err)
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleListPublishRecords(w http.ResponseWriter, r *http.Request) {
	records, err := s.db.ListPublishRecords(chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "list publish records", err)
		return
	}
	if records == nil {
		records = []store.PublishRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}

// collectLocalImages finds <img> tags pointing at files in the post's own
// directory and loads them so they can be re-hosted.
func collectLocalImages(htmlStr, postDir string) []publish.Image {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []publish.Image

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, a := range n.Attr {
				if a.Key != "src" || seen[a.Val] {
					continue
				}
				seen[a.Val] = true
				if data, ct, ok := readLocalImage(a.Val, postDir); ok {
					out = append(out, publish.Image{SrcURL: a.Val, Data: data, CT: ct})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// readLocalImage loads an image referenced relative to the post, refusing
// to escape the post's directory.
func readLocalImage(src, postDir string) ([]byte, string, bool) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") ||
		strings.HasPrefix(src, "data:") {
		return nil, "", false
	}
	clean := filepath.Clean(filepath.Join(postDir, src))
	if !strings.HasPrefix(clean, filepath.Clean(postDir)+string(os.PathSeparator)) {
		return nil, "", false
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, "", false
	}
	ct := mime.TypeByExtension(filepath.Ext(clean))
	if ct == "" {
		ct = "image/jpeg"
	}
	return data, ct, true
}

func loadCover(cover, postDir string) ([]byte, string) {
	if cover == "" {
		return nil, ""
	}
	data, ct, ok := readLocalImage(cover, postDir)
	if !ok {
		return nil, ""
	}
	return data, ct
}
