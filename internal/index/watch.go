package index

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/corollad/cowrite/internal/workspace"
	"github.com/fsnotify/fsnotify"
)

// debounce collapses the burst of events a single save produces: editors
// write a temp file and rename it, so one logical change arrives as
// several events.
const debounce = 300 * time.Millisecond

// Change is a file event worth telling the browser about.
type Change struct {
	PostID  string `json:"postId"`
	RelPath string `json:"path"`
	Kind    string `json:"kind"` // "updated" | "removed"
}

// Watcher reindexes files as they change on disk and reports the changes
// that were not made by this process.
type Watcher struct {
	ix       *Index
	ws       *workspace.Workspace
	onChange func(Change)

	mu      sync.Mutex
	pending map[string]*time.Timer
}

func NewWatcher(ix *Index, ws *workspace.Workspace, onChange func(Change)) *Watcher {
	return &Watcher{ix: ix, ws: ws, onChange: onChange, pending: map[string]*time.Timer{}}
}

// Run watches the posts tree until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	// fsnotify does not recurse, so every existing directory is added and
	// new ones are added as they appear.
	if err := addTree(fsw, w.ws.PostsDir()); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			w.handle(fsw, ev)
		case _, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			// A watch error on one path should not stop watching the rest.
		}
	}
}

func addTree(fsw *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = fsw.Add(path)
		}
		return nil
	})
}

func (w *Watcher) handle(fsw *fsnotify.Watcher, ev fsnotify.Event) {
	if ev.Op&fsnotify.Chmod != 0 {
		return
	}
	// Directories created later must be watched too.
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			_ = fsw.Add(ev.Name)
			return
		}
	}
	base := filepath.Base(ev.Name)
	if !strings.HasSuffix(base, ".md") || strings.HasPrefix(base, ".") {
		return
	}

	w.mu.Lock()
	if t, ok := w.pending[ev.Name]; ok {
		t.Stop()
	}
	w.pending[ev.Name] = time.AfterFunc(debounce, func() {
		w.mu.Lock()
		delete(w.pending, ev.Name)
		w.mu.Unlock()
		w.settle(ev.Name)
	})
	w.mu.Unlock()
}

// settle reindexes a path once its events have stopped arriving.
func (w *Watcher) settle(path string) {
	rel, err := filepath.Rel(w.ws.Root, path)
	if err != nil {
		rel = path
	}

	entry, err := w.ws.Load(path)
	if err != nil {
		// Gone: drop it from the index and say so.
		if p, err := w.ix.DB().GetPostByPath(rel); err == nil {
			_ = w.ix.DB().SoftDeletePost(p.ID)
			w.emit(Change{PostID: p.ID, RelPath: rel, Kind: "removed"})
		}
		return
	}

	// Our own writes must not be reported back to the browser that made
	// them, or every save bounces back as an external change.
	if w.ws.WasSelfWritten(path, entry.Hash) {
		return
	}

	if err := w.ix.IndexFile(path); err != nil {
		return
	}
	id := entry.Post.Meta.ID
	if id == "" {
		if p, err := w.ix.DB().GetPostByPath(rel); err == nil {
			id = p.ID
		}
	}
	w.emit(Change{PostID: id, RelPath: rel, Kind: "updated"})
}

func (w *Watcher) emit(c Change) {
	if w.onChange != nil {
		w.onChange(c)
	}
}
