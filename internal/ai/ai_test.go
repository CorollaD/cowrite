package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeOpenAI serves a canned SSE stream so the client can be tested without
// a network, an API key, or spending tokens.
func fakeOpenAI(t *testing.T, chunks []string, hold chan struct{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		rc := http.NewResponseController(w)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", c)
			_ = rc.Flush()
			if hold != nil {
				select {
				case <-hold:
				case <-r.Context().Done():
					return
				}
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		_ = rc.Flush()
	}))
}

func TestStreamCollectsDeltas(t *testing.T) {
	srv := fakeOpenAI(t, []string{"你好", "，", "世界"}, nil)
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	deltas, err := c.Stream(context.Background(), Request{Model: "test", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var sb strings.Builder
	var sawDone bool
	for d := range deltas {
		if d.Err != nil {
			t.Fatalf("stream error: %v", d.Err)
		}
		if d.Done {
			sawDone = true
			continue
		}
		sb.WriteString(d.Text)
	}
	if got := sb.String(); got != "你好，世界" {
		t.Errorf("text = %q, want %q", got, "你好，世界")
	}
	if !sawDone {
		t.Error("stream never reported done")
	}
}

// Cancelling must stop the stream promptly and close the channel, or the
// user pays for tokens they cancelled and goroutines pile up.
func TestStreamCancellation(t *testing.T) {
	hold := make(chan struct{})
	srv := fakeOpenAI(t, []string{"one", "two", "three", "four"}, hold)
	defer srv.Close()
	defer close(hold)

	ctx, cancel := context.WithCancel(context.Background())
	c := NewClient(srv.URL, "k")
	deltas, err := c.Stream(ctx, Request{Model: "test", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	hold <- struct{}{} // let the first chunk through
	<-deltas           // receive it
	cancel()

	done := make(chan struct{})
	go func() {
		for range deltas { // drain until closed
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("channel not closed within 3s of cancel")
	}
}

func TestStreamRequiresModel(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "")
	if _, err := c.Stream(context.Background(), Request{Prompt: "x"}); err == nil {
		t.Fatal("expected an error when no model is set")
	}
}

func TestCommandRenderSubstitutesText(t *testing.T) {
	cmd, ok := FindCommand("polish")
	if !ok {
		t.Fatal("polish command missing")
	}
	out, err := cmd.Render("这是原文")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "这是原文") {
		t.Errorf("text not substituted: %s", out)
	}
	if strings.Contains(out, "{{.Text}}") {
		t.Errorf("placeholder left in prompt: %s", out)
	}
}

func TestCommandRejectsEmptyText(t *testing.T) {
	cmd, _ := FindCommand("polish")
	if _, err := cmd.Render("   \n  "); err == nil {
		t.Error("expected an error for blank input")
	}
}

func TestTruncateKeepsHeadAndTail(t *testing.T) {
	long := strings.Repeat("头", 100) + strings.Repeat("中", maxContext) + strings.Repeat("尾", 100)
	got := Truncate(long)

	if len([]rune(got)) > maxContext+40 {
		t.Errorf("truncated text still too long: %d runes", len([]rune(got)))
	}
	if !strings.HasPrefix(got, "头") {
		t.Error("beginning of document was lost")
	}
	if !strings.HasSuffix(got, "尾") {
		t.Error("end of document was lost")
	}
	if !strings.Contains(got, "省略") {
		t.Error("truncation should be visible to the model")
	}
}

func TestTruncateLeavesShortTextAlone(t *testing.T) {
	s := "短文本"
	if got := Truncate(s); got != s {
		t.Errorf("Truncate altered short text: %q", got)
	}
}

func TestFreeProvidersExist(t *testing.T) {
	free := FreeProviders()
	if len(free) == 0 {
		t.Fatal("no zero-cost providers listed; a fresh install has nothing to default to")
	}
	for _, p := range free {
		if p.BaseURL == "" {
			t.Errorf("provider %q has no base URL", p.ID)
		}
	}
}

func TestDetectLocalHandlesNothingRunning(t *testing.T) {
	// Must return promptly and empty rather than hanging at startup.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_ = DetectLocal(ctx)
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("DetectLocal took %v; startup must not block", elapsed)
	}
}

// Document-scope commands describe the article rather than replacing it.
// Inserting a list of title candidates over the body would destroy the post,
// so scope is what the UI keys "replace" versus "copy" off.
func TestDocumentScopeCommandsAreNotReplacements(t *testing.T) {
	for _, id := range []string{"title", "summary"} {
		cmd, ok := FindCommand(id)
		if !ok {
			t.Fatalf("command %q missing", id)
		}
		if cmd.Scope != ScopeDocument {
			t.Errorf("%s scope = %q, want document", id, cmd.Scope)
		}
	}
	// Polish and continue do rewrite text in place.
	for _, id := range []string{"polish", "continue"} {
		cmd, _ := FindCommand(id)
		if cmd.Scope == ScopeDocument {
			t.Errorf("%s should act on a selection or the cursor, not the whole document", id)
		}
	}
}
