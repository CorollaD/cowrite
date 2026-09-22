// Package bridge connects the local server to the companion browser
// extension, which publishes to platforms that have no write API.
//
// This is the largest attack surface cowrite has: any page in the browser
// can attempt to open a WebSocket to localhost. A connection is therefore
// only accepted from the extension's own origin and only with a pairing
// token the user copied from this app, or a malicious page could read the
// whole workspace and post as the user on every logged-in platform.
package bridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Request is a publish job sent to the extension.
type Request struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Title    string `json:"title"`
	HTML     string `json:"html"`
	Markdown string `json:"markdown"`
	Digest   string `json:"digest"`
}

// Response is what the extension reports back.
type Response struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	URL   string `json:"url,omitempty"`
	// LoggedIn reports the platform's login state for a status check.
	LoggedIn *bool `json:"loggedIn,omitempty"`
}

// envelope is the wire format in both directions.
type envelope struct {
	Type     string          `json:"type"` // "hello" | "publish" | "result" | "status"
	Token    string          `json:"token,omitempty"`
	Platform string          `json:"platform,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// Bridge holds the single extension connection and routes jobs to it.
type Bridge struct {
	token string

	mu        sync.Mutex
	conn      *websocket.Conn
	connected bool
	pending   map[string]chan Response
}

func New() *Bridge {
	return &Bridge{token: newToken(), pending: make(map[string]chan Response)}
}

func newToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// A token that is not random is worse than refusing to run.
		panic("bridge: cannot generate pairing token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Token is the pairing secret the user copies into the extension.
func (b *Bridge) Token() string { return b.token }

func (b *Bridge) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

// Handler accepts the extension's WebSocket connection.
func (b *Bridge) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only the extension may connect. Without this, any website the
		// user visits could drive this socket.
		if !allowedOrigin(r.Header.Get("Origin")) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Origin is checked above; this disables the library's own
			// host-based check, which does not understand extension origins.
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer c.CloseNow()

		ctx := r.Context()

		// The first frame must authenticate, within a short window.
		handshakeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		var hello envelope
		if err := wsjson.Read(handshakeCtx, c, &hello); err != nil {
			return
		}
		if hello.Type != "hello" ||
			subtle.ConstantTimeCompare([]byte(hello.Token), []byte(b.token)) != 1 {
			_ = c.Close(websocket.StatusPolicyViolation, "bad token")
			return
		}
		_ = wsjson.Write(ctx, c, envelope{Type: "hello"})

		b.attach(c)
		defer b.detach()

		for {
			var msg envelope
			if err := wsjson.Read(ctx, c, &msg); err != nil {
				return
			}
			if msg.Type != "result" {
				continue
			}
			var res Response
			if err := json.Unmarshal(msg.Payload, &res); err != nil {
				continue
			}
			b.deliver(res)
		}
	}
}

// allowedOrigin accepts only Chrome extension origins.
func allowedOrigin(origin string) bool {
	return strings.HasPrefix(origin, "chrome-extension://") ||
		strings.HasPrefix(origin, "moz-extension://")
}

func (b *Bridge) attach(c *websocket.Conn) {
	b.mu.Lock()
	b.conn, b.connected = c, true
	b.mu.Unlock()
}

func (b *Bridge) detach() {
	b.mu.Lock()
	b.conn, b.connected = nil, false
	// Unblock anything waiting on a reply from a connection that is gone.
	for id, ch := range b.pending {
		close(ch)
		delete(b.pending, id)
	}
	b.mu.Unlock()
}

func (b *Bridge) deliver(res Response) {
	b.mu.Lock()
	ch, ok := b.pending[res.ID]
	if ok {
		delete(b.pending, res.ID)
	}
	b.mu.Unlock()
	if ok {
		ch <- res
		close(ch)
	}
}

// ErrNotConnected is returned when the extension is not paired.
var ErrNotConnected = fmt.Errorf("浏览器扩展未连接")

// Publish sends a job to the extension and waits for its result.
func (b *Bridge) Publish(ctx context.Context, req Request) (*Response, error) {
	b.mu.Lock()
	conn, connected := b.conn, b.connected
	if !connected {
		b.mu.Unlock()
		return nil, ErrNotConnected
	}
	ch := make(chan Response, 1)
	b.pending[req.ID] = ch
	b.mu.Unlock()

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := wsjson.Write(ctx, conn, envelope{
		Type: "publish", Platform: req.Platform, Payload: payload,
	}); err != nil {
		b.mu.Lock()
		delete(b.pending, req.ID)
		b.mu.Unlock()
		return nil, fmt.Errorf("发送到扩展失败: %w", err)
	}

	select {
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, req.ID)
		b.mu.Unlock()
		return nil, ctx.Err()
	case res, ok := <-ch:
		if !ok {
			return nil, ErrNotConnected
		}
		return &res, nil
	}
}
