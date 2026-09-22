package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// eventBus fans server-side events out to connected browsers.
type eventBus struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newEventBus() *eventBus {
	return &eventBus{subs: make(map[chan []byte]struct{})}
}

func (b *eventBus) subscribe() chan []byte {
	ch := make(chan []byte, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *eventBus) unsubscribe(ch chan []byte) {
	b.mu.Lock()
	delete(b.subs, ch)
	close(ch)
	b.mu.Unlock()
}

// publish never blocks: a browser that cannot keep up misses an event
// rather than stalling the watcher that produced it.
func (b *eventBus) publish(kind string, payload any) {
	data, err := json.Marshal(map[string]any{"kind": kind, "data": payload})
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- data:
		default:
		}
	}
}

// Publish sends an event to every connected browser.
func (s *Server) Publish(kind string, payload any) { s.events.publish(kind, payload) }

// handleEvents streams file-change and other notices to the browser.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)
	ch := s.events.subscribe()
	defer s.events.unsubscribe(ch)

	writeSSE(w, rc, "ready", "")

	// A periodic comment keeps proxies and browsers from dropping an idle
	// connection.
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			writeSSE(w, rc, "change", string(data))
		case <-ping.C:
			if _, err := w.Write([]byte(":\n\n")); err != nil {
				return
			}
			_ = rc.Flush()
		}
	}
}
