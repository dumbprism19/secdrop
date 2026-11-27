// Package repos finds local git checkouts of a service, so an env can be
// exported straight into the repo that runs it. It only reads directory
// names and git metadata; it never changes a repo.
package repos

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Repo is a git checkout that looks like it belongs to a service.
type Repo struct {
	Path   string // absolute path to the working tree
	Remote string // origin URL, if any
	Why    string // "folder name" or "origin remote"
}

// maxDepth is how far below each root to look for checkouts.
const maxDepth = 3

// skip lists folders that never hold a user's own checkouts and are slow to walk.
var skip = map[string]bool{
	"node_modules": true, "vendor": true, "Library": true, "Applications": true,
	"Pictures": true, "Music": true, "Movies": true, "Downloads": true,
	"target": true, "dist": true, "build": true, "venv": true, "__pycache__": true,
}

// Roots returns the folders searched for checkouts: the working directory,
// its parent, and common code folders in the home directory, if they exist.
func Roots() []string {
	var out []string
	if wd, err := os.Getwd(); err == nil {
		out = append(out, wd, filepath.Dir(wd))
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, d := range []string{"code", "Code", "src", "dev", "Dev", "Developer", "projects", "Projects",
			"repos", "Repos", "work", "Work", "workspace", "git", "GitHub", "github", "go/src", "Documents", "Desktop"} {
			out = append(out, filepath.Join(home, d))
		}
	}
	return out
}

// Find looks under roots for git checkouts whose folder name or origin
// remote names the service. Matching ignores case and treats - and _ alike.
func Find(ctx context.Context, service string, roots []string) ([]Repo, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git isn't installed")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	want := norm(service)
	seen := map[string]bool{}
	var candidates []string
	for _, root := range roots {
		walk(ctx, root, 0, func(dir string) {
			if real, err := filepath.EvalSymlinks(dir); err == nil {
				dir = real
			}
			if !seen[dir] {
				seen[dir] = true
				candidates = append(candidates, dir)
			}
		})
	}

	var (
		mu  sync.Mutex
		out []Repo
		wg  sync.WaitGroup
		sem = make(chan struct{}, 8)
	)
	for _, dir := range candidates {
		wg.Add(1)
		go func(dir string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			remote := originURL(ctx, dir)
			r := Repo{Path: dir, Remote: remote}
			switch {
			case norm(filepath.Base(dir)) == want:
				r.Why = "folder name"
			case remote != "" && norm(remoteName(remote)) == want:
				r.Why = "origin remote"
			default:
				return
			}
			mu.Lock()
			out = append(out, r)
			mu.Unlock()
		}(dir)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if len(out) == 0 && ctx.Err() != nil {
		return nil, errors.New("searching for repos took too long")
	}
	return out, nil
}

// walk calls found for every git working tree at or below dir. It does not
// descend into a checkout once found, nor into hidden or skipped folders.
func walk(ctx context.Context, dir string, depth int, found func(string)) {
	if ctx.Err() != nil {
		return
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		found(dir)
		return
	}
	if depth >= maxDepth {
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || skip[name] || e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		walk(ctx, filepath.Join(dir, name), depth+1, found)
	}
}

func originURL(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// remoteName is the repository name in a git URL:
// git@github.com:org/orders-svc.git and https://host/org/orders-svc both give orders-svc.
func remoteName(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		url = url[i+1:]
	}
	return url
}

func norm(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", "-"))
}

// Target picks the file to write in a repo: .env, or .env.<env> when .env
// already exists. It returns "" when both exist, since secdrop never
// overwrites a file.
func Target(repo, env string) string {
	names := []string{".env"}
	if env != "" {
		names = append(names, ".env."+env)
	}
	for _, n := range names {
		p := filepath.Join(repo, n)
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return p
		}
	}
	return ""
}

// Tracked reports how git treats path. inRepo is false outside a git
// working tree; ignored is true when git would leave the file out of commits.
func Tracked(ctx context.Context, path string) (inRepo, ignored bool) {
	dir := filepath.Dir(path)
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, false
		}
		dir = parent
	}
	if err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return false, false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return true, false
	}
	// check-ignore exits 0 when ignored, 1 when not, 128 on error.
	err = exec.CommandContext(ctx, "git", "-C", dir, "check-ignore", "-q", "--no-index", abs).Run()
	return true, err == nil
}
