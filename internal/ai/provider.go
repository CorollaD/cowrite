package ai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Delta is one piece of a streamed response.
type Delta struct {
	Text string
	Done bool
	Err  error
}

// Request is a single-turn completion.
type Request struct {
	System      string
	Prompt      string
	Model       string
	Temperature float64
	// NoThinking asks a reasoning model to answer directly. Rewriting
	// tasks need no deliberation, and on Doubao Seed the thinking pass
	// costs 15-25 seconds for a sentence that takes two to produce.
	NoThinking bool
}

// Client talks to one OpenAI-compatible endpoint.
//
// Every provider cowrite supports speaks this protocol, so a provider is a
// base URL and a key rather than a separate implementation.
type Client struct {
	api openai.Client
}

// httpClient is shared by all providers.
//
// It deliberately sets no overall timeout: http.Client.Timeout covers the
// whole exchange including reading the body, which would cut off a long
// generation mid-stream. Only the wait for response headers is bounded.
var httpClient = &http.Client{
	Transport: &http.Transport{
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	},
}

func NewClient(baseURL, apiKey string) *Client {
	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(httpClient),
	}
	// Local servers accept any key, but the SDK requires one to be set.
	if apiKey == "" {
		apiKey = "not-needed"
	}
	opts = append(opts, option.WithAPIKey(apiKey))
	return &Client{api: openai.NewClient(opts...)}
}

// Stream runs a completion, emitting deltas as they arrive.
//
// The returned channel is closed when the stream ends. Cancelling ctx stops
// the upstream request, so a user who closes the page or hits stop does not
// keep paying for tokens.
func (c *Client) Stream(ctx context.Context, req Request) (<-chan Delta, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("no model selected")
	}

	messages := []openai.ChatCompletionMessageParamUnion{}
	if req.System != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}
	messages = append(messages, openai.UserMessage(req.Prompt))

	params := openai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: messages,
	}
	if req.Temperature > 0 {
		params.Temperature = openai.Float(req.Temperature)
	}

	// Not part of the OpenAI schema, so it goes in as a raw field.
	// Providers that do not understand it ignore it.
	var opts []option.RequestOption
	if req.NoThinking {
		opts = append(opts,
			option.WithJSONSet("thinking", map[string]string{"type": "disabled"}))
	}

	out := make(chan Delta)
	go func() {
		defer close(out)

		stream := c.api.Chat.Completions.NewStreaming(ctx, params, opts...)
		defer stream.Close()

		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) == 0 {
				continue
			}
			text := chunk.Choices[0].Delta.Content
			if text == "" {
				continue
			}
			select {
			case out <- Delta{Text: text}:
			case <-ctx.Done():
				return
			}
		}

		if err := stream.Err(); err != nil {
			// A cancelled request is the user's doing, not a failure.
			if ctx.Err() != nil {
				return
			}
			select {
			case out <- Delta{Err: err}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case out <- Delta{Done: true}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}
