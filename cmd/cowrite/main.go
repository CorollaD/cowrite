// Command cowrite runs the local writing server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/corollad/cowrite/internal/ai"
	"github.com/corollad/cowrite/internal/config"
	"github.com/corollad/cowrite/internal/history"
	"github.com/corollad/cowrite/internal/index"
	"github.com/corollad/cowrite/internal/logging"
	"github.com/corollad/cowrite/internal/server"
	"github.com/corollad/cowrite/internal/store"
	"github.com/corollad/cowrite/internal/workspace"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cowrite: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Default()
	if err != nil {
		return err
	}
	flag.StringVar(&cfg.Host, "host", cfg.Host, "address to bind (loopback by default; there is no authentication)")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "port to listen on")
	flag.StringVar(&cfg.Workspace, "workspace", cfg.Workspace, "directory holding your markdown posts")
	flag.BoolVar(&cfg.Dev, "dev", false, "serve the web UI from disk instead of the embedded copy")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("cowrite", version)
		return nil
	}

	// Logs go to the workspace as well as the terminal: a local app is
	// often started by double-clicking, with no terminal left to read.
	log, logWriter := logging.Setup(
		filepath.Join(cfg.Workspace, ".cowrite", "logs"), slog.LevelInfo)
	if logWriter != nil {
		defer logWriter.Close()
	}

	ws, err := workspace.New(cfg.Workspace)
	if err != nil {
		return err
	}
	db, err := store.Open(cfg.IndexPath())
	if err != nil {
		return err
	}
	defer db.Close()

	ix := index.New(ws, db, log)
	start := time.Now()
	if err := ix.Sync(); err != nil {
		return fmt.Errorf("initial index sync: %w", err)
	}
	log.Info("workspace indexed", "took", time.Since(start).Round(time.Millisecond))

	// Report a usable zero-config AI setup if one is already running, so
	// the user is not sent to a settings page they do not need.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if found := ai.DetectLocal(ctx); len(found) > 0 {
			log.Info("local AI provider detected",
				"provider", found[0].Provider.Name,
				"models", len(found[0].Models))
		}
	}()

	srv := server.New(cfg, ws, db, ix, log)

	// Watch the workspace so edits made in another editor show up here,
	// and tell any open browser about them.
	ctx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	watcher := index.NewWatcher(ix, ws, func(c index.Change) {
		log.Info("file changed on disk", "path", c.RelPath, "kind", c.Kind)
		srv.Publish("file", c)
	})
	go func() {
		if err := watcher.Run(ctx); err != nil {
			log.Warn("file watcher stopped", "err", err)
		}
	}()

	// Thin out old automatic snapshots once at startup.
	go func() {
		if n, err := history.New(filepath.Join(cfg.Workspace, ".cowrite"), db).Prune(); err == nil && n > 0 {
			log.Info("pruned old snapshots", "count", n)
		}
	}()

	return srv.ListenAndServe()
}
