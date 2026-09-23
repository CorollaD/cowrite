package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The active log must roll over instead of growing without bound.
func TestRotatesWhenLarge(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	line := strings.Repeat("x", 4096) + "\n"
	for range (maxSize / len(line)) + 8 {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	entries, _ := os.ReadDir(dir)
	var rotated int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "cowrite-") {
			rotated++
		}
	}
	if rotated == 0 {
		t.Error("log never rotated; it would grow without bound")
	}
	if _, err := os.Stat(filepath.Join(dir, "cowrite.log")); err != nil {
		t.Errorf("active log missing after rotation: %v", err)
	}
}

// Retention must bound how many rotated files survive.
func TestPruneKeepsBoundedHistory(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	for i := range keepFiles + 6 {
		name := filepath.Join(dir, "cowrite-2026010"+string(rune('0'+i%10))+"-000000.log")
		if err := os.WriteFile(name, []byte("old"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	w.Prune()

	entries, _ := os.ReadDir(dir)
	var rotated int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "cowrite-") {
			rotated++
		}
	}
	if rotated > keepFiles {
		t.Errorf("kept %d rotated logs, limit is %d", rotated, keepFiles)
	}
}

// Logging must never take the app down with it.
func TestWriteAfterCloseIsHarmless(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Close()
	if _, err := w.Write([]byte("after close\n")); err != nil {
		t.Errorf("write after close should be a no-op, got %v", err)
	}
}
