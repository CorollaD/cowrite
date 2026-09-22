package voice

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranscribeSendsAudioAndFields(t *testing.T) {
	var gotModel, gotLanguage, gotPrompt, gotAuth string
	var gotAudio []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		gotModel = r.FormValue("model")
		gotLanguage = r.FormValue("language")
		gotPrompt = r.FormValue("prompt")
		gotAuth = r.Header.Get("Authorization")
		if f, _, err := r.FormFile("file"); err == nil {
			gotAudio, _ = io.ReadAll(f)
			f.Close()
		}
		fmt.Fprint(w, `{"text":"  转写结果  "}`)
	}))
	defer srv.Close()

	tr := &Transcriber{BaseURL: srv.URL, APIKey: "k1", Model: "whisper-large-v3"}
	got, err := tr.Transcribe(context.Background(),
		[]byte("fake-audio-bytes"), "clip.webm", "zh", "cowrite,goldmark")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if got != "转写结果" {
		t.Errorf("text = %q, want trimmed %q", got, "转写结果")
	}
	if string(gotAudio) != "fake-audio-bytes" {
		t.Errorf("audio not forwarded: %q", gotAudio)
	}
	if gotModel != "whisper-large-v3" {
		t.Errorf("model = %q", gotModel)
	}
	if gotLanguage != "zh" {
		t.Errorf("language = %q", gotLanguage)
	}
	// The vocabulary must reach the transcriber, not only the cleanup pass:
	// that is what actually improves recognition of proper nouns.
	if gotPrompt != "cowrite,goldmark" {
		t.Errorf("vocabulary not sent as prompt: %q", gotPrompt)
	}
	if gotAuth != "Bearer k1" {
		t.Errorf("auth header = %q", gotAuth)
	}
}

func TestTranscribeDefaultsModel(t *testing.T) {
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		gotModel = r.FormValue("model")
		fmt.Fprint(w, `{"text":"x"}`)
	}))
	defer srv.Close()

	tr := &Transcriber{BaseURL: srv.URL}
	if _, err := tr.Transcribe(context.Background(), []byte("a"), "a.webm", "", ""); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotModel == "" {
		t.Error("no model sent; the request would be rejected upstream")
	}
}

func TestTranscribeReportsUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"bad key"}}`)
	}))
	defer srv.Close()

	tr := &Transcriber{BaseURL: srv.URL, Model: "m"}
	_, err := tr.Transcribe(context.Background(), []byte("a"), "a.webm", "", "")
	if err == nil {
		t.Fatal("expected an error on 401")
	}
	// The upstream message must survive so the user can act on it.
	if !strings.Contains(err.Error(), "bad key") {
		t.Errorf("upstream detail lost: %v", err)
	}
}

func TestTranscribeValidates(t *testing.T) {
	if _, err := (&Transcriber{}).Transcribe(context.Background(), []byte("a"), "a", "", ""); err == nil {
		t.Error("expected an error when no base URL is configured")
	}
	if _, err := (&Transcriber{BaseURL: "http://x"}).Transcribe(context.Background(), nil, "a", "", ""); err == nil {
		t.Error("expected an error on empty audio")
	}
}

func TestTranscribeCancellation(t *testing.T) {
	// The handler waits on its own signal rather than the request context,
	// so closing the server never has to wait on a cancelled client.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	tr := &Transcriber{BaseURL: srv.URL, Model: "m"}
	if _, err := tr.Transcribe(ctx, []byte("audio"), "a.webm", "", ""); err == nil {
		t.Error("expected cancellation to surface as an error")
	}
}

func TestBuildCleanupPromptSubstitutes(t *testing.T) {
	got := BuildCleanupPrompt("嗯那个我们周二不对周三开会", "")
	if !strings.Contains(got, "嗯那个我们周二不对周三开会") {
		t.Error("transcript not substituted")
	}
	if strings.Contains(got, "{{.Text}}") {
		t.Error("placeholder left in prompt")
	}
}

// The prompt has to forbid rewriting explicitly, or the model improves the
// prose and quietly changes what the person actually said.
func TestCleanupPromptForbidsRewriting(t *testing.T) {
	got := BuildCleanupPrompt("测试", "")
	for _, must := range []string{"不要改写", "不要增加", "自我更正"} {
		if !strings.Contains(got, must) {
			t.Errorf("cleanup prompt is missing the %q rule", must)
		}
	}
}

func TestBuildCleanupPromptAppendsVocabulary(t *testing.T) {
	with := BuildCleanupPrompt("测试", "cowrite, goldmark")
	if !strings.Contains(with, "cowrite, goldmark") {
		t.Error("vocabulary not appended")
	}
	without := BuildCleanupPrompt("测试", "   ")
	if strings.Contains(without, "专有名词") {
		t.Error("blank vocabulary should add nothing")
	}
}
