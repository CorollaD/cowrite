package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Volcengine transcribes through 火山引擎 (ByteDance) speech services.
//
// It is not OpenAI-compatible: authentication uses a semicolon after
// "Bearer", credentials are an appid/token/cluster triple rather than a
// single key, and recognition is a submit-then-poll pair rather than one
// request. That is why it gets its own client instead of a catalog entry.
type Volcengine struct {
	AppID   string
	Token   string
	Cluster string
	HTTP    *http.Client
}

const (
	volcSubmitURL = "https://openspeech.bytedance.com/api/v1/auc/submit"
	volcQueryURL  = "https://openspeech.bytedance.com/api/v1/auc/query"
	// defaultCluster is what the console hands out for standard ASR.
	defaultVolcCluster = "volcengine_input_common"
)

func (v *Volcengine) client() *http.Client {
	if v.HTTP != nil {
		return v.HTTP
	}
	return httpClient
}

func (v *Volcengine) cluster() string {
	if v.Cluster != "" {
		return v.Cluster
	}
	return defaultVolcCluster
}

// Transcribe uploads audio and waits for the result.
func (v *Volcengine) Transcribe(ctx context.Context, audio []byte, format, language string) (string, error) {
	if v.AppID == "" || v.Token == "" {
		return "", fmt.Errorf("火山引擎需要填写 AppID 和 Access Token")
	}
	if len(audio) == 0 {
		return "", fmt.Errorf("没有收到音频")
	}

	id, err := v.submit(ctx, audio, format, language)
	if err != nil {
		return "", err
	}
	return v.poll(ctx, id)
}

func (v *Volcengine) submit(ctx context.Context, audio []byte, format, language string) (string, error) {
	if format == "" {
		format = "wav"
	}
	body := map[string]any{
		"app": map[string]string{
			"appid":   v.AppID,
			"token":   v.Token,
			"cluster": v.cluster(),
		},
		"user":  map[string]string{"uid": "cowrite"},
		"audio": map[string]string{"format": format},
		"request": map[string]any{
			"reqid": uuid.NewString(),
			// Punctuation and digit normalisation make the transcript
			// usable as prose rather than a bare word stream.
			"with_punc":       true,
			"enable_itn":      true,
			"show_utterances": false,
			"data":            base64.StdEncoding.EncodeToString(audio),
		},
	}
	if language != "" {
		body["request"].(map[string]any)["language"] = language
	}

	var out struct {
		Resp struct {
			ID      string `json:"id"`
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"resp"`
	}
	if err := v.call(ctx, volcSubmitURL, body, &out); err != nil {
		return "", err
	}
	// 1000 is success on submit; anything else carries a reason.
	if out.Resp.ID == "" {
		return "", fmt.Errorf("火山引擎提交失败: %s", volcMessage(out.Resp.Code, out.Resp.Message))
	}
	return out.Resp.ID, nil
}

// poll waits for the recognition task to finish.
func (v *Volcengine) poll(ctx context.Context, id string) (string, error) {
	body := map[string]any{
		"appid":   v.AppID,
		"token":   v.Token,
		"cluster": v.cluster(),
		"id":      id,
	}

	// A minute of speech is usually transcribed in a few seconds; the
	// ceiling exists so a stuck task cannot hang the request forever.
	deadline := time.Now().Add(2 * time.Minute)
	delay := 400 * time.Millisecond

	for {
		var out struct {
			Resp struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Text    string `json:"text"`
			} `json:"resp"`
		}
		if err := v.call(ctx, volcQueryURL, body, &out); err != nil {
			return "", err
		}

		switch {
		case out.Resp.Code == 1000: // done
			return strings.TrimSpace(out.Resp.Text), nil
		case out.Resp.Code == 2000 || out.Resp.Code == 2001: // still running
		default:
			return "", fmt.Errorf("火山引擎识别失败: %s",
				volcMessage(out.Resp.Code, out.Resp.Message))
		}

		if time.Now().After(deadline) {
			return "", fmt.Errorf("火山引擎识别超时")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
		// Back off gently so a long clip does not hammer the API.
		if delay < 2*time.Second {
			delay += 200 * time.Millisecond
		}
	}
}

func (v *Volcengine) call(ctx context.Context, url string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// The semicolon is required; a normal "Bearer <token>" is rejected.
	req.Header.Set("Authorization", "Bearer; "+v.Token)

	resp, err := v.client().Do(req)
	if err != nil {
		return fmt.Errorf("请求火山引擎失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("火山引擎返回 %s: %s", resp.Status, snippet(data))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析火山引擎响应失败: %w", err)
	}
	return nil
}

// volcMessage turns the codes people actually hit into something
// actionable, since the raw message is often just "error".
func volcMessage(code int, msg string) string {
	switch code {
	case 1001:
		return fmt.Sprintf("请求参数有误（%d %s）", code, msg)
	case 1002:
		return fmt.Sprintf("鉴权失败：检查 AppID 和 Access Token 是否正确，以及该 App 是否开通了语音识别（%d）", code)
	case 1003:
		return fmt.Sprintf("超出调用配额或并发限制（%d）", code)
	case 1004:
		return fmt.Sprintf("音频格式不支持或音频损坏（%d）", code)
	case 1005:
		return fmt.Sprintf("音频时长超限（%d）", code)
	}
	if msg == "" {
		msg = "未知错误"
	}
	return fmt.Sprintf("%d %s", code, msg)
}

// FormatFromFilename maps a recording's extension to the format name
// Volcengine expects; it does not sniff content types.
func FormatFromFilename(name string) string {
	switch {
	case strings.HasSuffix(name, ".wav"):
		return "wav"
	case strings.HasSuffix(name, ".mp3"):
		return "mp3"
	case strings.HasSuffix(name, ".ogg"), strings.HasSuffix(name, ".opus"):
		return "ogg"
	case strings.HasSuffix(name, ".m4a"), strings.HasSuffix(name, ".mp4"):
		return "m4a"
	default:
		// MediaRecorder produces webm/opus, which Volcengine reads as ogg.
		return "ogg"
	}
}
