// Package server exposes the local HTTP API and the embedded web UI.
package server

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/corollad/cowrite/internal/config"
	"github.com/corollad/cowrite/internal/index"
	"github.com/corollad/cowrite/internal/render"
	"github.com/corollad/cowrite/internal/secret"
	"github.com/corollad/cowrite/internal/store"
	"github.com/corollad/cowrite/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed all:assets
var assetsFS embed.FS

type Server struct {
	cfg config.Config
	ws  *workspace.Workspace
	db  *store.DB
	ix  *index.Index
	log *slog.Logger

	renderer *render.Renderer
	secrets  *secret.Store
	jobs     *jobRegistry
}

func New(cfg config.Config, ws *workspace.Workspace, db *store.DB, ix *index.Index, log *slog.Logger) *Server {
	return &Server{
		cfg: cfg, ws: ws, db: db, ix: ix, log: log,
		renderer: render.New(),
		secrets:  secret.New(filepath.Join(cfg.Workspace, ".cowrite")),
		jobs:     newJobRegistry(),
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/posts", s.handleListPosts)
		r.Post("/posts", s.handleCreatePost)
		r.Get("/posts/{id}", s.handleGetPost)
		r.Put("/posts/{id}", s.handleUpdatePost)
		r.Delete("/posts/{id}", s.handleDeletePost)
		r.Post("/render", s.handleRender)
		r.Route("/ai", func(r chi.Router) {
			r.Get("/config", s.handleGetAIConfig)
			r.Put("/config", s.handleSaveAIConfig)
			r.Get("/detect", s.handleDetectAI)
			r.Get("/models", s.handleListModels)
			r.Post("/run", s.handleAIRun)
			r.Get("/stream/{id}", s.handleAIStream)
			r.Delete("/stream/{id}", s.handleAICancel)
		})
		r.Get("/themes", s.handleListThemes)
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
	})

	r.Handle("/*", s.staticHandler())
	return r
}

// staticHandler serves the UI from disk in dev mode so the frontend can be
// rebuilt without restarting, and from the embedded copy otherwise.
func (s *Server) staticHandler() http.Handler {
	if s.cfg.Dev {
		return http.FileServer(http.Dir("web/dist"))
	}
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		s.log.Error("embedded assets unavailable", "err", err)
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}

func (s *Server) ListenAndServe() error {
	if s.cfg.IsExposed() {
		s.log.Warn("cowrite is bound beyond loopback and has NO authentication; "+
			"anyone who can reach this address can read and publish your posts",
			"addr", s.cfg.Addr())
	}
	srv := &http.Server{
		Addr:              s.cfg.Addr(),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.log.Info("cowrite listening", "url", "http://"+s.cfg.Addr(),
		"workspace", s.cfg.Workspace)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
