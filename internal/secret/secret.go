// Package secret stores API keys outside the workspace.
//
// Keys never go into SQLite or the markdown tree: the workspace is meant to
// be safe to sync or commit to git, and an index file is meant to be
// disposable. The OS keyring is used where available.
package secret

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

const service = "cowrite"

// Store reads and writes secrets, falling back to a 0600 file when no OS
// keyring is available (headless Linux, CI, some containers).
type Store struct {
	fallbackPath string

	mu       sync.Mutex
	fallback map[string]string
	useFile  bool
}

func New(dir string) *Store {
	return &Store{fallbackPath: filepath.Join(dir, "secrets.json")}
}

// Set stores a secret under key. An empty value deletes it.
func (s *Store) Set(key, value string) error {
	if value == "" {
		return s.Delete(key)
	}
	if !s.fileMode() {
		if err := keyring.Set(service, key, value); err == nil {
			return nil
		}
		// Keyring unavailable: switch to the file for the rest of the run.
		s.setFileMode()
	}
	return s.setFile(key, value)
}

func (s *Store) Get(key string) (string, error) {
	// An environment variable always wins: it is what people reach for
	// first locally, and it keeps keys out of any persistent store.
	if v := os.Getenv(envName(key)); v != "" {
		return v, nil
	}
	if !s.fileMode() {
		v, err := keyring.Get(service, key)
		if err == nil {
			return v, nil
		}
		if err == keyring.ErrNotFound {
			return "", nil
		}
		s.setFileMode()
	}
	return s.getFile(key)
}

func (s *Store) Delete(key string) error {
	if !s.fileMode() {
		if err := keyring.Delete(service, key); err == nil || err == keyring.ErrNotFound {
			return nil
		}
		s.setFileMode()
	}
	return s.setFile(key, "")
}

// envName maps a provider id to the env var people expect, so an existing
// OPENAI_API_KEY or DEEPSEEK_API_KEY is picked up with no configuration.
func envName(key string) string {
	k := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	return k + "_API_KEY"
}

func (s *Store) fileMode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.useFile
}

func (s *Store) setFileMode() {
	s.mu.Lock()
	s.useFile = true
	s.mu.Unlock()
}

func (s *Store) loadFileLocked() error {
	if s.fallback != nil {
		return nil
	}
	s.fallback = make(map[string]string)
	data, err := os.ReadFile(s.fallbackPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read secrets: %w", err)
	}
	if err := json.Unmarshal(data, &s.fallback); err != nil {
		return fmt.Errorf("parse secrets: %w", err)
	}
	return nil
}

func (s *Store) getFile(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadFileLocked(); err != nil {
		return "", err
	}
	return s.fallback[key], nil
}

func (s *Store) setFile(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadFileLocked(); err != nil {
		return err
	}
	if value == "" {
		delete(s.fallback, key)
	} else {
		s.fallback[key] = value
	}
	data, err := json.MarshalIndent(s.fallback, "", "  ")
	if err != nil {
		return fmt.Errorf("encode secrets: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.fallbackPath), 0o700); err != nil {
		return fmt.Errorf("create secrets dir: %w", err)
	}
	// 0600: readable only by the user who owns it.
	if err := os.WriteFile(s.fallbackPath, data, 0o600); err != nil {
		return fmt.Errorf("write secrets: %w", err)
	}
	return nil
}
