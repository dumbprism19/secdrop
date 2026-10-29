package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dumbprism19/secdrop/internal/config"
	"github.com/dumbprism19/secdrop/internal/dotenv"
	"github.com/dumbprism19/secdrop/internal/kube"
	"github.com/dumbprism19/secdrop/internal/secret"
	"github.com/dumbprism19/secdrop/internal/store"
)

// TestScreenshots renders the README screenshots from made-up data. It only
// runs when SECDROP_SHOTS names an output folder:
//
//	SECDROP_SHOTS=/tmp/shots go test ./internal/ui -run Screenshots
//	python3 scripts/ansi2svg.py /tmp/shots assets/screens
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("SECDROP_SHOTS")
	if dir == "" {
		t.Skip("set SECDROP_SHOTS to render screenshots")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	m := New(context.Background(), Options{NoMotion: true, Version: "0.4.0"})
	m.w, m.h = 118, 32
	m.scr, m.auth = scrBrowser, authOK
	m.ident.Email = "you@example.com"
	m.bucket = "acme-secrets"
	m.podsAt = time.Now()

	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour
	add := func(svc string, updated time.Duration, envs ...string) {
		for _, e := range envs {
			obj := svc + "-" + e + "-env"
			m.secrets = append(m.secrets, store.Secret{Bucket: m.bucket, Object: obj, Service: svc, Env: e, Updated: ago(updated)})
		}
	}
	add("api-gateway", 2*day, "dev", "staging", "prod")
	add("auth-service", 5*day, "dev", "staging", "prod")
	add("billing-api", 20*day, "dev", "prod")
	add("checkout-web", 3*day, "dev", "staging", "prod")
	add("notifications", 40*day, "dev", "prod")
	add("orders-api", 12*day, "dev", "staging", "prod")
	add("payments-api", 6*time.Hour, "dev", "staging", "prod")
	add("search-indexer", 90*day, "dev")
	add("user-profile", 25*day, "dev", "staging", "prod")
	m.rebuildEnvs()

	open := func(obj, body string) {
		d := dotenv.Parse([]byte(body))
		for _, s := range m.secrets {
			if s.Object == obj {
				m.docs[obj] = &opened{sec: s, raw: secret.FromString(body), doc: d, env: d.IsEnv()}
			}
		}
	}
	open("payments-api-prod-env", "APP_ENV=production\nDATABASE_URL=postgres://payments@db.internal:5432/payments\n"+
		"REDIS_URL=redis://cache.internal:6379/0\nLOG_LEVEL=info\nRATE_LIMIT_PER_MIN=600\nWEBHOOK_SIGNING_KEY=example-only\n"+
		"SENTRY_DSN=https://example@sentry.example.com/12\nFEATURE_NEW_CHECKOUT=on\nQUEUE_NAME=payments-events\n")
	open("payments-api-dev-env", "APP_ENV=development\nDATABASE_URL=postgres://payments@localhost:5432/payments\n"+
		"REDIS_URL=redis://localhost:6379/0\nLOG_LEVEL=debug\nRATE_LIMIT_PER_MIN=600\nWEBHOOK_SIGNING_KEY=example-only\n"+
		"SENTRY_DSN=https://example@sentry.example.com/12\nFEATURE_NEW_CHECKOUT=on\nFEATURE_REFUNDS_V2=on\n")
	m.pods = kube.Snapshot{Context: "gke_acme_prod", Pods: []kube.Pod{
		{Namespace: "payments", Name: "payments-api-6d9f-x2kq", Workload: "payments-api", Phase: "Running", Ready: 1, Total: 1, Started: ago(3 * time.Hour)},
		{Namespace: "payments", Name: "payments-api-6d9f-lm3a", Workload: "payments-api", Phase: "Running", Ready: 1, Total: 1, Started: ago(3 * time.Hour)},
		{Namespace: "payments", Name: "payments-api-6d9f-p8rd", Workload: "payments-api", Phase: "Running", Ready: 1, Total: 1, Started: ago(3 * time.Hour)},
		{Namespace: "search", Name: "search-indexer-0", Workload: "search-indexer", Phase: "Running", Reason: "CrashLoopBackOff", Ready: 0, Total: 1, Restarts: 6},
	}}
	m.cfg.SaveWorkspace("", config.Workspace{Name: "Payments team", Bucket: m.bucket, Services: []string{"billing-api", "checkout-web", "payments-api"}})
	m.cfg.SaveWorkspace("", config.Workspace{Name: "Platform", Bucket: m.bucket, Services: []string{"api-gateway", "auth-service"}})

	write := func(name, v string) {
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	m.selectService("payments-api")
	write("home", m.View())

	m.openService("payments-api", "prod")
	m.kcur = 6
	m.revealed["REDIS_URL"], m.revealed["LOG_LEVEL"] = 1, 1
	write("service", m.View())

	m.revealed = map[string]int{}
	m.startCompare()
	write("compare", m.View())
	m.cmp = nil

	m.closePanel()
	m.openPalette("")
	m.pal.y, m.pal.glass, m.pal.card, m.pal.wsCur = 0, 1, wsCard, 0
	write("palette", m.View())
}
