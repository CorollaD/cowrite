// Package wechat talks to the WeChat Official Account API.
//
// Neither silenceper/wechat nor PowerWeChat implements the draft box, so
// the handful of endpoints cowrite needs are written here directly.
package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"time"
)

const apiBase = "https://api.weixin.qq.com"

// TokenStore persists the access token between restarts.
//
// The token is global per AppID and refreshing it invalidates any other
// copy, so it must be shared rather than fetched per process.
type TokenStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

type Client struct {
	AppID     string
	AppSecret string
	Store     TokenStore
	HTTP      *http.Client
	BaseURL   string // overridden in tests

	// mu makes token refresh single-flight: concurrent refreshes would
	// each mint a token and invalidate the others.
	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(appID, appSecret string, store TokenStore) *Client {
	return &Client{
		AppID: appID, AppSecret: appSecret, Store: store,
		HTTP: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return apiBase
}

// apiError is WeChat's error envelope, returned on both success and
// failure responses.
type apiError struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func (e apiError) err() error {
	if e.ErrCode == 0 {
		return nil
	}
	return &Error{Code: e.ErrCode, Msg: e.ErrMsg}
}

// Error is a WeChat API error, with the codes worth explaining translated
// into something a user can act on.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string {
	if hint := hintFor(e.Code); hint != "" {
		return fmt.Sprintf("%s（errcode %d: %s）", hint, e.Code, e.Msg)
	}
	return fmt.Sprintf("微信接口错误 errcode %d: %s", e.Code, e.Msg)
}

// hintFor turns the codes people actually hit into instructions.
func hintFor(code int) string {
	switch code {
	case 40164:
		return "你的 IP 不在公众号后台的白名单里。家用宽带 IP 会变，需要到「设置与开发 → 基本配置 → IP 白名单」更新"
	case 40001, 40014, 42001:
		return "access_token 无效或已过期，请重试；若持续失败请确认 AppSecret 是否正确"
	case 40013:
		return "AppID 不正确"
	case 41001:
		return "缺少 access_token"
	case 45009:
		return "接口调用超过每日限额"
	case 48001:
		return "这个接口未授权：草稿箱需要已认证的订阅号或服务号"
	case 40007, 40008:
		return "素材 media_id 无效，封面图可能上传失败"
	}
	return ""
}

// token returns a valid access token, refreshing it if needed.
func (c *Client) token_(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Refresh five minutes early so a token does not expire mid-request.
	if c.token != "" && time.Now().Before(c.expires.Add(-5*time.Minute)) {
		return c.token, nil
	}
	if c.Store != nil && c.token == "" {
		if cached, err := c.Store.Get(tokenKey(c.AppID)); err == nil && cached != "" {
			var saved struct {
				Token   string    `json:"token"`
				Expires time.Time `json:"expires"`
			}
			if json.Unmarshal([]byte(cached), &saved) == nil &&
				time.Now().Before(saved.Expires.Add(-5*time.Minute)) {
				c.token, c.expires = saved.Token, saved.Expires
				return c.token, nil
			}
		}
	}

	url := fmt.Sprintf("%s/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s",
		c.base(), c.AppID, c.AppSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取 access_token 失败: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		apiError
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("解析 access_token 响应失败: %w", err)
	}
	if err := out.err(); err != nil {
		return "", err
	}

	c.token = out.AccessToken
	c.expires = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	if c.Store != nil {
		if blob, err := json.Marshal(map[string]any{
			"token": c.token, "expires": c.expires,
		}); err == nil {
			_ = c.Store.Set(tokenKey(c.AppID), string(blob))
		}
	}
	return c.token, nil
}

func tokenKey(appID string) string { return "wechat.token." + appID }

// postJSON calls an API endpoint with a JSON body.
func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	token, err := c.token_(ctx)
	if err != nil {
		return err
	}

	// WeChat rejects escaped non-ASCII in some fields, so HTML escaping is
	// disabled rather than using json.Marshal directly.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	url := fmt.Sprintf("%s%s?access_token=%s", c.base(), path, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("请求 %s 失败: %w", path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var e apiError
	if json.Unmarshal(data, &e) == nil {
		if err := e.err(); err != nil {
			return err
		}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("解析 %s 响应失败: %w", path, err)
		}
	}
	return nil
}

// uploadFile posts a multipart file to a media endpoint.
func (c *Client) uploadFile(ctx context.Context, path, field, filename, contentType string, data []byte, extra map[string]string, out any) error {
	token, err := c.token_(ctx)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	for k, v := range extra {
		if err := mw.WriteField(k, v); err != nil {
			return err
		}
	}
	if err := mw.Close(); err != nil {
		return err
	}

	url := fmt.Sprintf("%s%s?access_token=%s", c.base(), path, token)
	if t, ok := extra["type"]; ok {
		url += "&type=" + t
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("上传失败: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var e apiError
	if json.Unmarshal(respData, &e) == nil {
		if err := e.err(); err != nil {
			return err
		}
	}
	if out != nil {
		if err := json.Unmarshal(respData, out); err != nil {
			return fmt.Errorf("解析上传响应失败: %w", err)
		}
	}
	return nil
}

// UploadImage uploads an inline image and returns its mmbiz URL.
//
// Body images must be re-hosted: WeChat strips <img> pointing anywhere
// else. This endpoint does not consume the permanent material quota.
func (c *Client) UploadImage(ctx context.Context, data []byte, filename, contentType string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	err := c.uploadFile(ctx, "/cgi-bin/media/uploadimg", "media",
		filename, contentType, data, nil, &out)
	if err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("图片上传未返回 URL")
	}
	return out.URL, nil
}

// UploadThumb uploads a permanent image and returns its media_id, which a
// draft requires for its cover.
func (c *Client) UploadThumb(ctx context.Context, data []byte, filename, contentType string) (string, error) {
	var out struct {
		MediaID string `json:"media_id"`
	}
	err := c.uploadFile(ctx, "/cgi-bin/material/add_material", "media",
		filename, contentType, data, map[string]string{"type": "image"}, &out)
	if err != nil {
		return "", err
	}
	if out.MediaID == "" {
		return "", fmt.Errorf("封面上传未返回 media_id")
	}
	return out.MediaID, nil
}

// DraftArticle is one article in a draft.
type DraftArticle struct {
	ArticleType      string `json:"article_type,omitempty"`
	Title            string `json:"title"`
	Author           string `json:"author,omitempty"`
	Digest           string `json:"digest,omitempty"`
	Content          string `json:"content"`
	ContentSourceURL string `json:"content_source_url,omitempty"`
	ThumbMediaID     string `json:"thumb_media_id,omitempty"`
	NeedOpenComment  int    `json:"need_open_comment,omitempty"`
}

// AddDraft creates a draft and returns its media_id.
func (c *Client) AddDraft(ctx context.Context, articles []DraftArticle) (string, error) {
	var out struct {
		MediaID string `json:"media_id"`
	}
	body := map[string]any{"articles": articles}
	if err := c.postJSON(ctx, "/cgi-bin/draft/add", body, &out); err != nil {
		return "", err
	}
	return out.MediaID, nil
}

// Validate checks that the credentials work, without publishing anything.
func (c *Client) Validate(ctx context.Context) error {
	_, err := c.token_(ctx)
	return err
}

// TitleLimit and DigestLimit are WeChat's documented field limits.
const (
	TitleLimit  = 64
	DigestLimit = 120
)

// TrimTitle and TrimDigest cut oversized fields rather than letting the
// API reject the whole submission.
func TrimTitle(s string) string  { return trimRunes(strings.TrimSpace(s), TitleLimit) }
func TrimDigest(s string) string { return trimRunes(strings.TrimSpace(s), DigestLimit) }

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
