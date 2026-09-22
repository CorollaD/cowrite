// Package voice turns dictation into usable prose.
//
// The value here is not the transcription itself but the cleanup pass that
// follows it: raw speech is full of fillers, restarts and mid-sentence
// corrections that nobody wants in a draft.
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Transcriber calls an OpenAI-compatible /audio/transcriptions endpoint.
//
// The same endpoint shape is served by OpenAI, Groq, SiliconFlow and a
// local whisper.cpp server, so switching providers is a base URL change.
type Transcriber struct {
	BaseURL string
	APIKey  string
	Model   string
}

// httpClient bounds the wait for response headers but not the whole
// exchange: uploading audio and waiting for a transcript legitimately takes
// a while, and a total timeout would cut off long dictation.
var httpClient = &http.Client{
	Transport: &http.Transport{
		ResponseHeaderTimeout: 120 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	},
}

// Transcribe sends audio and returns the raw transcript.
//
// vocabulary is a comma-separated list of names and terms passed as a
// prompt hint, which is what makes a personal dictionary improve accuracy
// rather than just fixing things up afterwards.
func (t *Transcriber) Transcribe(ctx context.Context, audio []byte, filename, language, vocabulary string) (string, error) {
	if t.BaseURL == "" {
		return "", fmt.Errorf("语音服务尚未配置")
	}
	if len(audio) == 0 {
		return "", fmt.Errorf("没有收到音频")
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return "", fmt.Errorf("attach audio: %w", err)
	}

	model := t.Model
	if model == "" {
		model = "whisper-1"
	}
	fields := map[string]string{"model": model}
	if language != "" {
		fields["language"] = language
	}
	if vocabulary != "" {
		fields["prompt"] = vocabulary
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return "", fmt.Errorf("build request: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	url := strings.TrimSuffix(t.BaseURL, "/") + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if t.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.APIKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("转写请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("转写服务返回 %s: %s", resp.Status, snippet(data))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("parse transcript: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
