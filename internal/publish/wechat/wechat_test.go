package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/corollad/cowrite/internal/publish"
)

// memStore is a TokenStore backed by a map.
type memStore struct {
	mu sync.Mutex
	m  map[string]string
}

func newMemStore() *memStore { return &memStore{m: map[string]string{}} }

func (s *memStore) Get(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[k], nil
}

func (s *memStore) Set(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
	return nil
}

type mockWeChat struct {
	*httptest.Server
	tokenCalls atomic.Int32
	drafts     []map[string]any
	mu         sync.Mutex
}

func newMockWeChat(t *testing.T) *mockWeChat {
	t.Helper()
	m := &mockWeChat{}
	mux := http.NewServeMux()

	mux.HandleFunc("/cgi-bin/token", func(w http.ResponseWriter, r *http.Request) {
		m.tokenCalls.Add(1)
		fmt.Fprint(w, `{"access_token":"tok-123","expires_in":7200}`)
	})
	mux.HandleFunc("/cgi-bin/media/uploadimg", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"url":"https://mmbiz.qpic.cn/uploaded.png"}`)
	})
	mux.HandleFunc("/cgi-bin/material/add_material", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"media_id":"thumb-media-1","url":"https://mmbiz.qpic.cn/thumb.png"}`)
	})
	mux.HandleFunc("/cgi-bin/draft/add", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Articles []map[string]any `json:"articles"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.drafts = append(m.drafts, body.Articles...)
		m.mu.Unlock()
		fmt.Fprint(w, `{"media_id":"draft-media-1"}`)
	})

	m.Server = httptest.NewServer(mux)
	t.Cleanup(m.Close)
	return m
}

func newTestClient(m *mockWeChat) *Client {
	c := New("appid", "secret", newMemStore())
	c.BaseURL = m.URL
	return c
}

func TestPublishCreatesDraft(t *testing.T) {
	m := newMockWeChat(t)
	p := NewPublisher(newTestClient(m), "默认作者")

	res, err := p.Publish(context.Background(), publish.Article{
		Title:   "测试标题",
		Digest:  "摘要内容",
		HTML:    `<section><p>正文</p><img src="local://a.png"></section>`,
		Cover:   []byte("fake-png"),
		CoverCT: "image/png",
		Images: []publish.Image{
			{SrcURL: "local://a.png", Data: []byte("img"), CT: "image/png"},
		},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.RemoteID != "draft-media-1" {
		t.Errorf("RemoteID = %q", res.RemoteID)
	}
	if res.Status != "draft" {
		t.Errorf("Status = %q, want draft", res.Status)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.drafts) != 1 {
		t.Fatalf("got %d drafts, want 1", len(m.drafts))
	}
	d := m.drafts[0]

	// Body images must be rewritten to the uploaded mmbiz URL, or the
	// article renders with broken images.
	content, _ := d["content"].(string)
	if strings.Contains(content, "local://a.png") {
		t.Error("local image URL was not rewritten")
	}
	if !strings.Contains(content, "mmbiz.qpic.cn/uploaded.png") {
		t.Errorf("uploaded URL missing from content: %s", content)
	}
	if d["thumb_media_id"] != "thumb-media-1" {
		t.Errorf("cover media_id = %v", d["thumb_media_id"])
	}
	if d["author"] != "默认作者" {
		t.Errorf("author = %v, want the configured default", d["author"])
	}
}

// The token is global per AppID and refreshing invalidates other copies,
// so repeated calls must reuse the cached one.
func TestTokenIsCachedAcrossCalls(t *testing.T) {
	m := newMockWeChat(t)
	p := NewPublisher(newTestClient(m), "")

	for range 3 {
		if _, err := p.Publish(context.Background(), publish.Article{
			Title: "标题", HTML: "<p>x</p>",
			Cover: []byte("c"), CoverCT: "image/png",
		}); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	if got := m.tokenCalls.Load(); got != 1 {
		t.Errorf("token fetched %d times, want 1", got)
	}
}

// Concurrent refreshes would each mint a token and invalidate the others.
func TestTokenRefreshIsSingleFlight(t *testing.T) {
	m := newMockWeChat(t)
	c := newTestClient(m)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.token_(context.Background()); err != nil {
				t.Errorf("token: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := m.tokenCalls.Load(); got != 1 {
		t.Errorf("token fetched %d times concurrently, want 1", got)
	}
}

func TestIPWhitelistErrorIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"errcode":40164,"errmsg":"invalid ip not in whitelist"}`)
	}))
	defer srv.Close()

	c := New("a", "b", newMemStore())
	c.BaseURL = srv.URL
	err := c.Validate(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	// A raw errcode is useless to the person who has to fix it.
	if !strings.Contains(err.Error(), "白名单") {
		t.Errorf("error not translated for the user: %v", err)
	}
	if !strings.Contains(err.Error(), "40164") {
		t.Errorf("original code should still be present: %v", err)
	}
}

func TestUnverifiedAccountErrorIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "token") {
			fmt.Fprint(w, `{"access_token":"t","expires_in":7200}`)
			return
		}
		fmt.Fprint(w, `{"errcode":48001,"errmsg":"api unauthorized"}`)
	}))
	defer srv.Close()

	c := New("a", "b", newMemStore())
	c.BaseURL = srv.URL
	_, err := c.AddDraft(context.Background(), []DraftArticle{{Title: "t", Content: "c"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "认证") {
		t.Errorf("error not translated: %v", err)
	}
}

func TestPublishRequiresTitle(t *testing.T) {
	m := newMockWeChat(t)
	p := NewPublisher(newTestClient(m), "")
	_, err := p.Publish(context.Background(), publish.Article{
		HTML: "<p>x</p>", Cover: []byte("c"), CoverCT: "image/png",
	})
	if err == nil {
		t.Error("expected an error with no title")
	}
}

// WeChat rejects a coverless draft only after images are uploaded, so the
// check has to happen before any of that work.
func TestPublishRequiresCover(t *testing.T) {
	m := newMockWeChat(t)
	p := NewPublisher(newTestClient(m), "")
	_, err := p.Publish(context.Background(), publish.Article{
		Title: "标题", HTML: "<p>x</p>",
	})
	if err == nil {
		t.Fatal("expected an error with no cover")
	}
	if !strings.Contains(err.Error(), "cover") {
		t.Errorf("error should say how to set a cover: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.drafts) != 0 {
		t.Error("no draft should have been submitted")
	}
}

// Oversized fields should be trimmed rather than rejected wholesale.
func TestFieldsAreTrimmedToLimits(t *testing.T) {
	long := strings.Repeat("标", 200)
	if n := len([]rune(TrimTitle(long))); n != TitleLimit {
		t.Errorf("title trimmed to %d runes, want %d", n, TitleLimit)
	}
	if n := len([]rune(TrimDigest(long))); n != DigestLimit {
		t.Errorf("digest trimmed to %d runes, want %d", n, DigestLimit)
	}
	if got := TrimTitle("短标题"); got != "短标题" {
		t.Errorf("short title altered: %q", got)
	}
}

func TestCapsReflectWeChatConstraints(t *testing.T) {
	caps := NewPublisher(newTestClient(newMockWeChat(t)), "").Caps()
	if !caps.DraftOnly {
		t.Error("WeChat publishing must stop at the draft box")
	}
	if !caps.RequiresCover {
		t.Error("a draft requires a cover image")
	}
	if caps.LinksAllowed {
		t.Error("external links are stripped for unverified accounts")
	}
}
