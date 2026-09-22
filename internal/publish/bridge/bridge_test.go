package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func serve(t *testing.T, b *Bridge) string {
	t.Helper()
	srv := httptest.NewServer(b.Handler())
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// dial connects as the extension would.
func dial(t *testing.T, url, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	return websocket.Dial(context.Background(), url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{origin}},
	})
}

func connectAuthed(t *testing.T, b *Bridge, url string) *websocket.Conn {
	t.Helper()
	c, _, err := dial(t, url, "chrome-extension://abcdef")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ctx := context.Background()
	if err := wsjson.Write(ctx, c, envelope{Type: "hello", Token: b.Token()}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var ack envelope
	if err := wsjson.Read(ctx, c, &ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	// Give the server a moment to register the connection.
	for range 50 {
		if b.Connected() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return c
}

// A page on any website must not be able to drive this socket: it would
// expose the whole workspace and the user's logged-in platforms.
func TestRejectsNonExtensionOrigin(t *testing.T) {
	b := New()
	url := serve(t, b)

	for _, origin := range []string{
		"https://evil.example.com",
		"http://localhost:3000",
		"null",
		"",
	} {
		c, _, err := dial(t, url, origin)
		if err == nil {
			c.CloseNow()
			t.Errorf("origin %q was accepted", origin)
		}
	}
}

func TestRejectsBadToken(t *testing.T) {
	b := New()
	url := serve(t, b)

	c, _, err := dial(t, url, "chrome-extension://abcdef")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	ctx := context.Background()
	if err := wsjson.Write(ctx, c, envelope{Type: "hello", Token: "wrong-token"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var ack envelope
	if err := wsjson.Read(ctx, c, &ack); err == nil {
		t.Error("connection survived a bad token")
	}
	if b.Connected() {
		t.Error("bridge treated an unauthenticated peer as connected")
	}
}

func TestPublishRoundTrip(t *testing.T) {
	b := New()
	url := serve(t, b)
	c := connectAuthed(t, b, url)
	defer c.CloseNow()

	// Stand in for the extension: read the job, report success.
	go func() {
		ctx := context.Background()
		var msg envelope
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			return
		}
		var req Request
		_ = json.Unmarshal(msg.Payload, &req)
		res, _ := json.Marshal(Response{ID: req.ID, OK: true, URL: "https://zhihu.com/p/1"})
		_ = wsjson.Write(ctx, c, envelope{Type: "result", Payload: res})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := b.Publish(ctx, Request{ID: "job-1", Platform: "zhihu", Title: "标题"})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !res.OK || res.URL != "https://zhihu.com/p/1" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestPublishReportsExtensionFailure(t *testing.T) {
	b := New()
	url := serve(t, b)
	c := connectAuthed(t, b, url)
	defer c.CloseNow()

	go func() {
		ctx := context.Background()
		var msg envelope
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			return
		}
		var req Request
		_ = json.Unmarshal(msg.Payload, &req)
		res, _ := json.Marshal(Response{ID: req.ID, OK: false, Error: "知乎编辑器结构变了"})
		_ = wsjson.Write(ctx, c, envelope{Type: "result", Payload: res})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := b.Publish(ctx, Request{ID: "job-2", Platform: "zhihu"})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.OK {
		t.Error("a failed injection was reported as success")
	}
	// A selector that stopped matching must say so, not fail silently.
	if !strings.Contains(res.Error, "编辑器") {
		t.Errorf("error detail lost: %q", res.Error)
	}
}

func TestPublishWithoutExtension(t *testing.T) {
	b := New()
	_, err := b.Publish(context.Background(), Request{ID: "x", Platform: "zhihu"})
	if err != ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

// A publish must not hang forever if the extension goes away mid-job.
func TestPublishUnblocksOnDisconnect(t *testing.T) {
	b := New()
	url := serve(t, b)
	c := connectAuthed(t, b, url)

	go func() {
		time.Sleep(150 * time.Millisecond)
		c.CloseNow() // extension disappears without replying
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := b.Publish(ctx, Request{ID: "job-3", Platform: "zhihu"}); err == nil {
		t.Error("expected an error when the extension disconnects")
	}
}

func TestPublishHonoursCancellation(t *testing.T) {
	b := New()
	url := serve(t, b)
	c := connectAuthed(t, b, url)
	defer c.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Nothing replies, so this must end at the deadline, not hang.
	if _, err := b.Publish(ctx, Request{ID: "job-4", Platform: "zhihu"}); err == nil {
		t.Error("expected a timeout")
	}
}

func TestTokenIsRandomPerInstance(t *testing.T) {
	a, b := New().Token(), New().Token()
	if a == b {
		t.Error("pairing tokens must not repeat across instances")
	}
	if len(a) < 32 {
		t.Errorf("pairing token is too short to resist guessing: %d chars", len(a))
	}
}
