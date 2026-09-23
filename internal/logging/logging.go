// Package logging writes the server's log to a file as well as the
// terminal, and keeps that file from growing without bound.
//
// A local app is often started by double-clicking, with no terminal to
// read, so "what went wrong an hour ago" has to be recoverable from disk.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// maxSize is when the active file is rotated. Logs here are one line
	// per request at most, so a few megabytes covers weeks of writing.
	maxSize = 4 << 20
	// keepFiles bounds the rotated history.
	keepFiles = 5
	// keepFor discards rotated files older than this regardless of count,
	// so an idle install does not keep last year's logs forever.
	keepFor = 14 * 24 * time.Hour
)

// Writer is an io.Writer that rotates when the file gets large.
type Writer struct {
	dir  string
	name string

	mu   sync.Mutex
	file *os.File
	size int64
}

// New opens the log file under dir and returns a writer for it.
func New(dir string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	w := &Writer{dir: dir, name: "cowrite.log"}
	if err := w.reopen(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) path() string { return filepath.Join(w.dir, w.name) }

func (w *Writer) reopen() error {
	f, err := os.OpenFile(w.path(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log file: %w", err)
	}
	w.file, w.size = f, info.Size()
	return nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return len(p), nil // never fail a write just because logging is broken
	}
	if w.size+int64(len(p)) > maxSize {
		if err := w.rotate(); err != nil {
			// Keep logging to the existing file rather than losing output.
			_, _ = fmt.Fprintf(os.Stderr, "cowrite: log rotation failed: %v\n", err)
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate renames the active file out of the way and starts a new one.
func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	rotated := filepath.Join(w.dir, fmt.Sprintf("cowrite-%s.log", stamp))
	if err := os.Rename(w.path(), rotated); err != nil {
		// Reopen regardless, or logging stops entirely.
		_ = w.reopen()
		return err
	}
	if err := w.reopen(); err != nil {
		return err
	}
	w.prune()
	return nil
}

// prune drops rotated files beyond the retention limits. The active file
// is never a candidate.
func (w *Writer) prune() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}

	type rotated struct {
		path string
		mod  time.Time
	}
	var old []rotated
	cutoff := time.Now().Add(-keepFor)

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == w.name ||
			!strings.HasPrefix(name, "cowrite-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(w.dir, name)
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(full)
			continue
		}
		old = append(old, rotated{full, info.ModTime()})
	}

	// Newest first, then drop anything past the count limit.
	sort.Slice(old, func(i, j int) bool { return old[i].mod.After(old[j].mod) })
	for i := keepFiles; i < len(old); i++ {
		_ = os.Remove(old[i].path)
	}
}

// Prune runs the retention sweep on demand, so startup clears anything
// left by an earlier run that exited before rotating.
func (w *Writer) Prune() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prune()
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// Setup builds a logger that writes to both the terminal and the log file.
//
// If the file cannot be opened the terminal logger is returned anyway:
// losing logs is better than refusing to start.
func Setup(dir string, level slog.Level) (*slog.Logger, *Writer) {
	opts := &slog.HandlerOptions{Level: level}

	w, err := New(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cowrite: file logging unavailable: %v\n", err)
		return slog.New(slog.NewTextHandler(os.Stderr, opts)), nil
	}
	w.Prune()

	out := io.MultiWriter(os.Stderr, w)
	return slog.New(slog.NewTextHandler(out, opts)), w
}
