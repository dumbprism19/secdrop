// Package config remembers the last bucket so secdrop opens straight to it.
// It never stores secrets or tokens.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Bucket  string   `json:"bucket,omitempty"`
	Project string   `json:"project,omitempty"`
	Recent  []string `json:"recent,omitempty"`
	// Shared lists keys that may hold the same value in every environment
	// (for example SENTRY_DSN), so compare doesn't flag them.
	Shared []string `json:"shared,omitempty"`
	// Workspaces are named sets of services, saved per bucket.
	Workspaces []Workspace `json:"workspaces,omitempty"`
}

// Workspace is a named shortlist of services in one bucket. It stores
// service names only, never secrets.
type Workspace struct {
	Name     string   `json:"name"`
	Bucket   string   `json:"bucket"`
	Services []string `json:"services"`
}

// WorkspacesFor lists the workspaces saved for a bucket, in creation order.
func (c Config) WorkspacesFor(bucket string) []Workspace {
	var out []Workspace
	for _, w := range c.Workspaces {
		if w.Bucket == bucket {
			out = append(out, w)
		}
	}
	return out
}

// SaveWorkspace adds w, or replaces the workspace called old in the same
// bucket (old is empty for a new one).
func (c *Config) SaveWorkspace(old string, w Workspace) {
	for i, x := range c.Workspaces {
		if x.Bucket == w.Bucket && strings.EqualFold(x.Name, old) && old != "" {
			c.Workspaces[i] = w
			return
		}
	}
	c.Workspaces = append(c.Workspaces, w)
}

// DeleteWorkspace removes a workspace by name.
func (c *Config) DeleteWorkspace(bucket, name string) {
	out := c.Workspaces[:0]
	for _, x := range c.Workspaces {
		if !(x.Bucket == bucket && strings.EqualFold(x.Name, name)) {
			out = append(out, x)
		}
	}
	c.Workspaces = out
}

// HasWorkspace reports whether a name is taken in a bucket, ignoring case and
// the workspace being renamed.
func (c Config) HasWorkspace(bucket, name, except string) bool {
	for _, x := range c.Workspaces {
		if x.Bucket == bucket && strings.EqualFold(x.Name, name) && !strings.EqualFold(x.Name, except) {
			return true
		}
	}
	return false
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
