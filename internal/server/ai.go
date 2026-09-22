package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/corollad/cowrite/internal/ai"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	settingProviderID = "ai.provider"
	settingBaseURL    = "ai.base_url"
	settingModel      = "ai.model"
)

// aiJob is a prepared request waiting for its EventSource to connect.
//
// The work is split in two because EventSource cannot send a request body,
// and a whole document does not belong in a query string.
type aiJob struct {
	req    ai.Request
	client *ai.Client
	cancel context.CancelFunc
}

type jobRegistry struct {
	mu   sync.Mutex
	jobs map[string]*aiJob
}

func newJobRegistry() *jobRegistry {
	return &jobRegistry{jobs: make(map[string]*aiJob)}
}

func (r *jobRegistry) add(id string, j *aiJob) {
	r.mu.Lock()
	r.jobs[id] = j
	r.mu.Unlock()
}

func (r *jobRegistry) take(id string) (*aiJob, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	delete(r.jobs, id)
	return j, ok
}

func (r *jobRegistry) cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if ok {
		delete(r.jobs, id)
		if j.cancel != nil {
			j.cancel()
		}
	}
	return ok
}

type aiConfig struct {
	ProviderID string `json:"providerId"`
	BaseURL    string `json:"baseURL"`
	Model      string `json:"model"`
	HasKey     bool   `json:"hasKey"`
}

func (s *Server) currentConfig() (aiConfig, error) {
	var c aiConfig
	var err error
	if c.ProviderID, err = s.db.GetSetting(settingProviderID); err != nil {
		return c, err
	}
	if c.BaseURL, err = s.db.GetSetting(settingBaseURL); err != nil {
		return c, err
	}
	if c.Model, err = s.db.GetSetting(settingModel); err != nil {
		return c, err
	}
	if c.ProviderID != "" {
		key, err := s.secrets.Get(c.ProviderID)
		if err != nil {
			return c, err
		}
		c.HasKey = key != ""
	}
	return c, nil
}

func (s *Server) handleGetAIConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.currentConfig()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read ai config", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config":    cfg,
		"providers": ai.Catalog,
		"commands":  ai.Commands,
	})
}

type saveAIConfigRequest struct {
	ProviderID string `json:"providerId"`
	BaseURL    string `json:"baseURL"`
	Model      string `json:"model"`
	APIKey     string `json:"apiKey"`
}

func (s *Server) handleSaveAIConfig(w http.ResponseWriter, r *http.Request) {
	var req saveAIConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	baseURL := req.BaseURL
	if p, ok := ai.FindProvider(req.ProviderID); ok && baseURL == "" {
		baseURL = p.BaseURL
	}

	for k, v := range map[string]string{
		settingProviderID: req.ProviderID,
		settingBaseURL:    baseURL,
		settingModel:      req.Model,
	} {
		if err := s.db.SetSetting(k, v); err != nil {
			s.fail(w, http.StatusInternalServerError, "save ai config", err)
			return
		}
	}

	// An empty key means "leave what is stored alone", so that saving the
	// model does not wipe a key the browser never sees.
	if req.APIKey != "" && req.ProviderID != "" {
		if err := s.secrets.Set(req.ProviderID, req.APIKey); err != nil {
			s.fail(w, http.StatusInternalServerError, "save api key", err)
			return
		}
	}

	cfg, err := s.currentConfig()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read ai config", err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handleDetectAI reports local providers that are running right now.
func (s *Server) handleDetectAI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ai.DetectLocal(r.Context()))
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.currentConfig()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read ai config", err)
		return
	}
	if cfg.BaseURL == "" {
		s.fail(w, http.StatusBadRequest, "no provider configured", nil)
		return
	}
	key, err := s.secrets.Get(cfg.ProviderID)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read api key", err)
		return
	}
	models, err := ai.ListModels(r.Context(), cfg.BaseURL, key)
	if err != nil {
		s.fail(w, http.StatusBadGateway, "list models: "+err.Error(), err)
		return
	}
	writeJSON(w, http.StatusOK, models)
}

type aiRunRequest struct {
	Command string `json:"command"`
	Text    string `json:"text"`
}

// handleAIRun prepares a job and returns its id for the EventSource to open.
func (s *Server) handleAIRun(w http.ResponseWriter, r *http.Request) {
	var req aiRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	cmd, ok := ai.FindCommand(req.Command)
	if !ok {
		s.fail(w, http.StatusBadRequest, "unknown command", nil)
		return
	}

	cfg, err := s.currentConfig()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read ai config", err)
		return
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		s.fail(w, http.StatusBadRequest, "AI 尚未配置，请先在设置里选择模型", nil)
		return
	}

	prompt, err := cmd.Render(req.Text)
	if err != nil {
		s.fail(w, http.StatusBadRequest, err.Error(), err)
		return
	}

	key, err := s.secrets.Get(cfg.ProviderID)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read api key", err)
		return
	}

	id := uuid.NewString()
	s.jobs.add(id, &aiJob{
		client: ai.NewClient(cfg.BaseURL, key),
		req: ai.Request{
			System:      cmd.System,
			Prompt:      prompt,
			Model:       cfg.Model,
			Temperature: cmd.Temperature,
		},
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": id})
}

// handleAIStream streams a prepared job to the browser over SSE.
func (s *Server) handleAIStream(w http.ResponseWriter, r *http.Request) {
	job, ok := s.jobs.take(chi.URLParam(r, "id"))
	if !ok {
		s.fail(w, http.StatusNotFound, "job not found or already run", nil)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Stop any reverse proxy from buffering the stream into one blob.
	h.Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)

	// Closing the page cancels the request context, which cancels the
	// upstream call: no tokens are spent on output nobody will read.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	deltas, err := job.client.Stream(ctx, job.req)
	if err != nil {
		writeSSE(w, rc, "error", err.Error())
		return
	}

	for d := range deltas {
		switch {
		case d.Err != nil:
			writeSSE(w, rc, "error", d.Err.Error())
			return
		case d.Done:
			writeSSE(w, rc, "done", "")
			return
		default:
			payload, _ := json.Marshal(d.Text)
			writeSSE(w, rc, "delta", string(payload))
		}
	}
}

func (s *Server) handleAICancel(w http.ResponseWriter, r *http.Request) {
	s.jobs.cancel(chi.URLParam(r, "id"))
	w.WriteHeader(http.StatusNoContent)
}

// writeSSE emits one event and flushes it so the browser sees it now
// rather than whenever a buffer happens to fill.
func writeSSE(w http.ResponseWriter, rc *http.ResponseController, event, data string) {
	fmt.Fprintf(w, "event: %s\n", event)
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
	_ = rc.Flush()
}
