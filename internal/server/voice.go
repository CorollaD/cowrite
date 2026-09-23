package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/corollad/cowrite/internal/ai"
	"github.com/corollad/cowrite/internal/voice"
)

const (
	settingVoiceProvider = "voice.provider"
	settingVoiceBaseURL  = "voice.base_url"
	settingVoiceModel    = "voice.model"
	settingVoiceLanguage = "voice.language"
	settingVocabulary    = "voice.vocabulary"
	// Volcengine authenticates with an appid/token/cluster triple rather
	// than a single key, so it needs its own settings.
	settingVolcAppID   = "voice.volc_appid"
	settingVolcCluster = "voice.volc_cluster"
	secretVolcToken    = "volcengine"
)

// providerVolcengine is handled by a dedicated client: its ASR API is not
// OpenAI-compatible.
const providerVolcengine = "volcengine"

// maxAudioBytes caps an upload. A few minutes of opus is well under this;
// the limit exists so a stray request cannot exhaust memory.
const maxAudioBytes = 25 << 20

type voiceConfig struct {
	ProviderID  string `json:"providerId"`
	BaseURL     string `json:"baseURL"`
	Model       string `json:"model"`
	Language    string `json:"language"`
	Vocabulary  string `json:"vocabulary"`
	HasKey      bool   `json:"hasKey"`
	VolcAppID   string `json:"volcAppId"`
	VolcCluster string `json:"volcCluster"`
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
		{settingVolcAppID, &c.VolcAppID},
		{settingVolcCluster, &c.VolcCluster},
	} {
		v, err := s.db.GetSetting(f.key)
		if err != nil {
			return c, err
		}
		*f.dst = v
	}
	if c.ProviderID != "" {
		secretKey := c.ProviderID
		if c.ProviderID == providerVolcengine {
			secretKey = secretVolcToken
		}
		key, err := s.secrets.Get(secretKey)
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
	ProviderID  string `json:"providerId"`
	BaseURL     string `json:"baseURL"`
	Model       string `json:"model"`
	Language    string `json:"language"`
	Vocabulary  string `json:"vocabulary"`
	APIKey      string `json:"apiKey"`
	VolcAppID   string `json:"volcAppId"`
	VolcCluster string `json:"volcCluster"`
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
		settingVolcAppID:     req.VolcAppID,
		settingVolcCluster:   req.VolcCluster,
	} {
		if err := s.db.SetSetting(k, v); err != nil {
			s.fail(w, http.StatusInternalServerError, "save voice config", err)
			return
		}
	}
	if req.APIKey != "" && req.ProviderID != "" {
		secretKey := req.ProviderID
		if req.ProviderID == providerVolcengine {
			secretKey = secretVolcToken
		}
		if err := s.secrets.Set(secretKey, req.APIKey); err != nil {
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
	// Instruction is set when the transcript reads as a spoken edit
	// command rather than dictated prose.
	Instruction bool `json:"instruction,omitempty"`
}

// handleTranscribe accepts recorded audio, transcribes it, and prepares the
// cleanup pass as a streaming job.
//
// The raw transcript comes back immediately so the user sees something at
// once and always has the unedited version to fall back to.
// handlePartial transcribes one chunk of a recording that is still in
// progress, so text appears while the person is still talking instead of
// only after they stop.
//
// It deliberately skips the cleanup pass: partials are provisional and get
// replaced, and running a model over each chunk would cost more than it
// saves. Cleanup happens once, over the whole transcript, at the end.
func (s *Server) handlePartial(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.voiceSettings()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read voice config", err)
		return
	}
	if cfg.ProviderID == "" || (cfg.ProviderID != providerVolcengine && cfg.BaseURL == "") {
		s.fail(w, http.StatusBadRequest, "语音尚未配置", nil)
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

	audio, err := io.ReadAll(io.LimitReader(file, maxAudioBytes))
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "读取音频失败", err)
		return
	}

	secretKey := cfg.ProviderID
	if cfg.ProviderID == providerVolcengine {
		secretKey = secretVolcToken
	}
	key, err := s.secrets.Get(secretKey)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read api key", err)
		return
	}

	var raw string
	if cfg.ProviderID == providerVolcengine {
		vc := &voice.Volcengine{AppID: cfg.VolcAppID, Token: key, Cluster: cfg.VolcCluster}
		raw, err = vc.Transcribe(r.Context(), audio,
			voice.FormatFromFilename(header.Filename), cfg.Language)
	} else {
		tr := &voice.Transcriber{BaseURL: cfg.BaseURL, APIKey: key, Model: cfg.Model}
		raw, err = tr.Transcribe(r.Context(), audio, header.Filename, cfg.Language, cfg.Vocabulary)
	}
	if err != nil {
		s.fail(w, http.StatusBadGateway, err.Error(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": raw})
}

func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.voiceSettings()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read voice config", err)
		return
	}
	if cfg.ProviderID == "" || (cfg.ProviderID != providerVolcengine && cfg.BaseURL == "") {
		s.fail(w, http.StatusBadRequest, "语音尚未配置，请先在设置里选择转写服务", nil)
		return
	}

	// When text is selected the recording is treated as an instruction
	// about it, so "make this shorter" edits rather than being inserted.
	selection := r.FormValue("selection")

	if err := r.ParseMultipartForm(maxAudioBytes); err != nil {
		s.fail(w, http.StatusBadRequest, "读取音频失败", err)
		return
	}

	// Chunked recordings arrive already transcribed; re-sending the audio
	// would repeat work the partials have done.
	if pre := strings.TrimSpace(r.FormValue("transcript")); pre != "" {
		s.finishTranscript(w, r, cfg, pre, selection)
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

	secretKey := cfg.ProviderID
	if cfg.ProviderID == providerVolcengine {
		secretKey = secretVolcToken
	}
	key, err := s.secrets.Get(secretKey)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "read api key", err)
		return
	}

	var raw string
	if cfg.ProviderID == providerVolcengine {
		vc := &voice.Volcengine{
			AppID:   cfg.VolcAppID,
			Token:   key,
			Cluster: cfg.VolcCluster,
		}
		raw, err = vc.Transcribe(r.Context(), audio,
			voice.FormatFromFilename(header.Filename), cfg.Language)
	} else {
		tr := &voice.Transcriber{BaseURL: cfg.BaseURL, APIKey: key, Model: cfg.Model}
		raw, err = tr.Transcribe(r.Context(), audio, header.Filename, cfg.Language, cfg.Vocabulary)
	}
	if err != nil {
		s.fail(w, http.StatusBadGateway, err.Error(), err)
		return
	}
	s.finishTranscript(w, r, cfg, raw, selection)
}

// finishTranscript prepares the cleanup pass over a finished transcript,
// whether it arrived as one recording or as streamed chunks.
func (s *Server) finishTranscript(w http.ResponseWriter, r *http.Request,
	cfg voiceConfig, raw, selection string) {

	if raw == "" {
		writeJSON(w, http.StatusOK, transcribeResponse{Raw: ""})
		return
	}
	instruction := selection != "" && voice.LooksLikeInstruction(raw)

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

	req := ai.Request{
		System:      voice.CleanupSystem,
		Prompt:      voice.BuildCleanupPrompt(raw, cfg.Vocabulary),
		Model:       aiCfg.Model,
		Temperature: 0.1, // cleanup must be boring and repeatable
		// Dictation is only useful if it keeps up with speaking.
		NoThinking: true,
	}
	if instruction {
		req.System = voice.CommandSystem
		req.Prompt = voice.BuildCommandPrompt(raw, selection)
		req.Temperature = 0.3
		req.NoThinking = true
	}

	jobID := s.newJobID()
	s.jobs.add(jobID, &aiJob{
		client: ai.NewClient(aiCfg.BaseURL, aiKey),
		req:    req,
	})
	writeJSON(w, http.StatusOK, transcribeResponse{
		Raw: raw, JobID: jobID, Instruction: instruction,
	})
}
