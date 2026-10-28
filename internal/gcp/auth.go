// Package gcp checks the user's Google Cloud sign-in. secdrop only ever uses
// the user's own Application Default Credentials; it stores no tokens.
package gcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
)

var (
	ErrNotSignedIn = errors.New("not signed in")
	ErrExpired     = errors.New("sign-in expired")
)

type Identity struct {
	Email   string
	Project string
	Service bool // true for a service account key
}

// Check confirms that Application Default Credentials exist and can mint a token.
func Check(ctx context.Context) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return Identity{}, ErrNotSignedIn
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %s", ErrExpired, shortErr(err))
	}

	id := Identity{Project: ActiveProject(creds.ProjectID)}
	var meta struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
	}
	if json.Unmarshal(creds.JSON, &meta) == nil && meta.Type == "service_account" {
		id.Email, id.Service = meta.ClientEmail, true
		return id, nil
	}
	id.Email = tokenEmail(ctx, tok.AccessToken)
	return id, nil
}

// tokenEmail asks Google who the token belongs to. The token is sent in the
// POST body, never in a URL.
func tokenEmail(ctx context.Context, token string) string {
	form := url.Values{"access_token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/tokeninfo", strings.NewReader(form.Encode()))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var info struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&info)
	return info.Email
}

func shortErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "invalid_grant"), strings.Contains(s, "reauth"), strings.Contains(s, "invalid_rapt"):
		return "Google asked you to sign in again"
	case strings.Contains(s, "no such host"), strings.Contains(s, "dial tcp"):
		return "can't reach Google, check your network"
	}
	if len(s) > 90 {
		s = s[:90] + "…"
	}
	return s
}

// ActiveProject finds the project the user is working in, in the same order
// gcloud uses: environment, credentials, then the active gcloud configuration.
func ActiveProject(fromCreds string) string {
	for _, k := range []string{"SECDROP_PROJECT", "CLOUDSDK_CORE_PROJECT", "GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	if p := gcloudProject(); p != "" {
		return p
	}
	return fromCreds
}

// gcloudProject reads the active gcloud configuration file directly, which is
// much faster than starting the gcloud CLI.
func gcloudProject() string {
	dir := os.Getenv("CLOUDSDK_CONFIG")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config", "gcloud")
	}
	active := "default"
	if b, err := os.ReadFile(filepath.Join(dir, "active_config")); err == nil && strings.TrimSpace(string(b)) != "" {
		active = strings.TrimSpace(string(b))
	}
	f, err := os.Open(filepath.Join(dir, "configurations", "config_"+active))
	if err != nil {
		return ""
	}
	defer f.Close()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			section = strings.Trim(l, "[]")
			continue
		}
		if k, v, ok := strings.Cut(l, "="); ok && section == "core" && strings.TrimSpace(k) == "project" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// LoginCmd signs the user in with gcloud and refreshes Application Default
// Credentials in one step. It opens the browser.
func LoginCmd() (*exec.Cmd, error) {
	bin, err := exec.LookPath("gcloud")
	if err != nil {
		return nil, errors.New("gcloud is not installed; see cloud.google.com/sdk/docs/install")
	}
	return exec.Command(bin, "auth", "login", "--update-adc"), nil
}
