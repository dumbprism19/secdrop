// Package update finds out whether a newer secdrop release exists and
// installs it with `go install`. Releases come from Go's module mirror;
// retracted versions (such as a tag pushed on the wrong commit) are skipped.
package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

const (
	Module  = "github.com/dumbprism19/secdrop"
	Command = Module + "/cmd/secdrop"
)

var (
	// ProxyURL is Go's public module mirror; tests point it elsewhere.
	ProxyURL = "https://proxy.golang.org"
	// CacheTTL is how long a check is reused, so launching stays instant.
	CacheTTL = time.Hour
)

// IsRelease reports whether v (with or without a leading "v") is a plain
// release such as 0.3.0. Development builds and pseudo-versions are not, so
// they never update themselves.
func IsRelease(v string) bool {
	v = "v" + strings.TrimPrefix(v, "v")
	return semver.IsValid(v) && semver.Prerelease(v) == "" && semver.Build(v) == ""
}

// Newer reports whether release latest is newer than current.
func Newer(current, latest string) bool {
	return IsRelease(current) && IsRelease(latest) &&
		semver.Compare("v"+strings.TrimPrefix(latest, "v"), "v"+strings.TrimPrefix(current, "v")) > 0
}

// Latest asks the mirror for the newest release that the newest go.mod
// doesn't retract.
func Latest(ctx context.Context) (string, error) {
	body, err := get(ctx, ProxyURL+"/"+Module+"/@v/list")
	if err != nil {
		return "", err
	}
	var versions []string
	for _, v := range strings.Fields(string(body)) {
		if IsRelease(v) {
			versions = append(versions, v)
		}
	}
	if len(versions) == 0 {
		return "", errors.New("no releases published yet")
	}
	semver.Sort(versions)
	newest := versions[len(versions)-1]

	// Retractions live in the newest version's go.mod.
	var retracted []modfile.VersionInterval
	if mod, err := get(ctx, ProxyURL+"/"+Module+"/@v/"+newest+".mod"); err == nil {
		if f, err := modfile.ParseLax("go.mod", mod, nil); err == nil {
			for _, r := range f.Retract {
				retracted = append(retracted, r.VersionInterval)
			}
		}
	}
	for i := len(versions) - 1; i >= 0; i-- {
		v, skip := versions[i], false
		for _, r := range retracted {
			if semver.Compare(v, r.Low) >= 0 && semver.Compare(v, r.High) <= 0 {
				skip = true
				break
			}
		}
		if !skip {
			return v, nil
		}
	}
	return "", errors.New("every release is retracted")
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// cache remembers the last answer so most launches skip the network.
type cache struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

// Check returns the newest release, using a cached answer when it's recent.
// dir is where the cache file lives (secdrop's settings folder).
func Check(ctx context.Context, dir string) (string, error) {
	path := filepath.Join(dir, "update.json")
	var c cache
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &c) == nil &&
		time.Since(c.Checked) < CacheTTL && c.Latest != "" {
		return c.Latest, nil
	}
	latest, err := Latest(ctx)
	if err != nil {
		return "", err
	}
	if b, err := json.Marshal(cache{Checked: time.Now(), Latest: latest}); err == nil {
		_ = os.MkdirAll(dir, 0o700)
		_ = os.WriteFile(path, b, 0o600)
	}
	return latest, nil
}

// SelfInstallable reports whether `go install` would replace the running
// program: Go must be available, and this binary must be the one in Go's
// install folder. Copies built elsewhere (a checkout's bin/, Homebrew, a
// download) are left alone.
func SelfInstallable() (string, bool) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", false
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	out, err := exec.Command(goBin, "env", "GOBIN", "GOPATH").Output()
	if err != nil {
		return "", false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	dir := strings.TrimSpace(lines[0])
	if dir == "" && len(lines) > 1 {
		gopath := strings.Split(strings.TrimSpace(lines[1]), string(os.PathListSeparator))[0]
		dir = filepath.Join(gopath, "bin")
	}
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	return exe, filepath.Dir(exe) == dir
}

// Install runs `go install` for one version. On failure the error carries
// the last line Go printed.
func Install(ctx context.Context, version string) error {
	cmd := exec.CommandContext(ctx, "go", "install", Command+"@"+version)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}
