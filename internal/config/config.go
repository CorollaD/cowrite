// Package config loads cowrite's settings.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	// Host defaults to loopback: this server has no authentication, so it
	// must not be reachable from the network unless asked for explicitly.
	Host      string
	Port      int
	Workspace string
	Dev       bool
}

func Default() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("locate home directory: %w", err)
	}
	return Config{
		Host:      "127.0.0.1",
		Port:      8080,
		Workspace: filepath.Join(home, "cowrite"),
	}, nil
}

func (c Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

func (c Config) IndexPath() string {
	return filepath.Join(c.Workspace, ".cowrite", "index.db")
}

// IsExposed reports whether the server is bound beyond loopback, which the
// caller warns about since there is no auth in front of it.
func (c Config) IsExposed() bool {
	return c.Host != "127.0.0.1" && c.Host != "localhost" && c.Host != "::1"
}
