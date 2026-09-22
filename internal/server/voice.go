package server

import (
	"encoding/json"
	"net/http"

	"github.com/corollad/cowrite/internal/ai"
	"github.com/corollad/cowrite/internal/voice"
)

const (
	settingVoiceProvider = "voice.provider"
	settingVoiceBaseURL  = "voice.base_url"
	settingVoiceModel    = "voice.model"
	settingVoiceLanguage = "voice.language"
	settingVocabulary    = "voice.vocabulary"
)

// maxAudioBytes caps an upload. A few minutes of opus is well under this;
// the limit exists so a stray request cannot exhaust memory.
const maxAudioBytes = 25 << 20

type voiceConfig struct {
	ProviderID string `json:"providerId"`
	BaseURL    string `json:"baseURL"`
	Model      string `json:"model"`
	Language   string `json:"language"`
	Vocabulary string `json:"vocabulary"`
	HasKey     bool   `json:"hasKey"`
}

func (s *Server) voiceSettings() (voiceConfig, error) {
	var c voiceConfig
	for _, f := range []struct {
		key string
		dst *string
	}{
		{settingVoiceProvider, &c.ProviderID},
		{settingVoiceBaseURL, &c.BaseURL},
		{settingVoiceModel, &c.Model},
		{settingVoiceLanguage, &c.Language},
		{settingVocabulary, &c.Vocabulary},
	} {
		v, err := s.db.GetSetting(f.key)
		if err != nil {
			return c, err
		}
		*f.dst = v
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

func (s *Server) handleGetVoiceConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.voiceSettings()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read voice config", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config":    cfg,
		"providers": ai.Catalog,
	})
}

type saveVoiceConfigRequest struct {
	ProviderID string `json:"providerId"`
	BaseURL    string `json:"baseURL"`
	Model      string `json:"model"`
	Language   string `json:"language"`
	Vocabulary string `json:"vocabulary"`
	APIKey     string `json:"apiKey"`
}

func (s *Server) handleSaveVoiceConfig(w http.ResponseWriter, r *http.Request) {
	var req saveVoiceConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	baseURL := req.BaseURL
	if p, ok := ai.FindProvider(req.ProviderID); ok && baseURL == "" {
		baseURL = p.BaseURL
	}
	for k, v := range map[string]string{
		settingVoiceProvider: req.ProviderID,
		settingVoiceBaseURL:  baseURL,
		settingVoiceModel:    req.Model,
		settingVoiceLanguage: req.Language,
		settingVocabulary:    req.Vocabulary,
	} {
		if err := s.db.SetSetting(k, v); err != nil {
			s.fail(w, http.StatusInternalServerError, "save voice config", err)
			return
		}
	}
	if req.APIKey != "" && req.ProviderID != "" {
		if err := s.secrets.Set(req.ProviderID, req.APIKey); err != nil {
			s.fail(w, http.StatusInternalServerError, "save api key", err)
			return
		}
	}

	cfg, err := s.voiceSettings()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read voice config", err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

type transcribeResponse struct {
	Raw   string `json:"raw"`
	JobID string `json:"jobId,omitempty"`
}

// handleTranscribe accepts recorded audio, transcribes it, and prepares the
// cleanup pass as a streaming job.
//
// The raw transcript comes back immediately so the user sees something at
// once and always has the unedited version to fall back to.
func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.voiceSettings()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read voice config", err)
		return
	}
	if cfg.BaseURL == "" {
		s.fail(w, http.StatusBadRequest, "语音尚未配置，请先在设置里选择转写服务", nil)
		return
	}

	if err := r.ParseMultipartForm(maxAudioBytes); err != nil {
		s.fail(w, http.StatusBadRequest, "读取音频失败", err)
		return
	}
	file, header, err := r.FormFile("audio")
	if err != nil {
		s.fail(w, http.StatusBadRequest, "缺少音频", err)
		return
	}
	defer file.Close()

	audio := make([]byte, 0, header.Size)
	buf := make([]byte, 32<<10)
	for {
		n, err := file.Read(buf)
		audio = append(audio, buf[:n]...)
		if err != nil || len(audio) > maxAudioBytes {
			break
		}
	}

	key, err := s.secrets.Get(cfg.ProviderID)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read api key", err)
		return
	}

	tr := &voice.Transcriber{BaseURL: cfg.BaseURL, APIKey: key, Model: cfg.Model}
	raw, err := tr.Transcribe(r.Context(), audio, header.Filename, cfg.Language, cfg.Vocabulary)
	if err != nil {
		s.fail(w, http.StatusBadGateway, err.Error(), err)
		return
	}
	if raw == "" {
		writeJSON(w, http.StatusOK, transcribeResponse{Raw: ""})
		return
	}

	// Cleanup runs through the chat model, which may be a different
	// provider than transcription; if none is set the raw text still stands
	// on its own.
	aiCfg, err := s.currentConfig()
	if err != nil || aiCfg.BaseURL == "" || aiCfg.Model == "" {
		writeJSON(w, http.StatusOK, transcribeResponse{Raw: raw})
		return
	}
	aiKey, err := s.secrets.Get(aiCfg.ProviderID)
	if err != nil {
		writeJSON(w, http.StatusOK, transcribeResponse{Raw: raw})
		return
	}

	jobID := s.newJobID()
	s.jobs.add(jobID, &aiJob{
		client: ai.NewClient(aiCfg.BaseURL, aiKey),
		req: ai.Request{
			System:      voice.CleanupSystem,
			Prompt:      voice.BuildCleanupPrompt(raw, cfg.Vocabulary),
			Model:       aiCfg.Model,
			Temperature: 0.1, // cleanup must be boring and repeatable
		},
	})
	writeJSON(w, http.StatusOK, transcribeResponse{Raw: raw, JobID: jobID})
}
