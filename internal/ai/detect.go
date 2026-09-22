package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Detected is a provider found to be usable right now.
type Detected struct {
	Provider Provider `json:"provider"`
	Models   []string `json:"models"`
}

// DetectLocal probes the no-credential providers and reports which are
// actually running, so a fresh install can pick a working default without
// asking the user for an API key.
//
// Probes run concurrently and are individually short: this is called on
// startup and must not delay it noticeably.
func DetectLocal(ctx context.Context) []Detected {
	candidates := make([]Provider, 0, len(Catalog))
	for _, p := range Catalog {
		if p.Auth == AuthNone {
			candidates = append(candidates, p)
		}
	}

	results := make([]Detected, len(candidates))
	done := make(chan struct{})
	for i, p := range candidates {
		go func() {
			defer func() { done <- struct{}{} }()
			models, err := listModels(ctx, p.BaseURL, "", 1500*time.Millisecond)
			if err != nil || len(models) == 0 {
				return
			}
			results[i] = Detected{Provider: p, Models: models}
		}()
	}
	for range candidates {
		<-done
	}

	var found []Detected
	for _, r := range results {
		if r.Provider.ID != "" {
			found = append(found, r)
		}
	}
	return found
}

// listModels calls the OpenAI-compatible /models endpoint.
func listModels(ctx context.Context, baseURL, apiKey string, timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %s", resp.Status)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		out = append(out, m.ID)
	}
	return out, nil
}

// ListModels reports the models a configured provider offers.
func ListModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	return listModels(ctx, baseURL, apiKey, 10*time.Second)
}
