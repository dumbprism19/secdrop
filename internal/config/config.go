// Package config remembers the last bucket so secdrop opens straight to it.
// It never stores secrets or tokens.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type Config struct {
	Bucket  string   `json:"bucket,omitempty"`
	Project string   `json:"project,omitempty"`
	Recent  []string `json:"recent,omitempty"`
	// Shared lists keys that may hold the same value in every environment
	// (for example SENTRY_DSN), so compare doesn't flag them.
	Shared []string `json:"shared,omitempty"`
}

func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "secdrop")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "secdrop")
}

func path() string { return filepath.Join(Dir(), "config.json") }

func Load() Config {
	var c Config
	b, err := os.ReadFile(path())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

func (c Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path())
}

// UseBucket makes b the current bucket and moves it to the front of Recent.
func (c *Config) UseBucket(b string) {
	c.Bucket = b
	out := []string{b}
	for _, r := range c.Recent {
		if r != b && len(out) < 8 {
			out = append(out, r)
		}
	}
	c.Recent = out
}

func Exists() bool {
	_, err := os.Stat(path())
	return !errors.Is(err, fs.ErrNotExist)
}
