package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dumbprism19/secdrop/internal/dotenv"
	"github.com/dumbprism19/secdrop/internal/kube"
	"github.com/dumbprism19/secdrop/internal/secret"
	"github.com/dumbprism19/secdrop/internal/store"
)

// fixture builds a browser with sample data, no cloud access needed.
func fixture() *Model {
	m := New(context.Background(), Options{NoMotion: true})
	m.w, m.h = 110, 32
	m.scr = scrBrowser
	m.auth = authOK
	m.ident.Email = "dev@example.com"
	m.bucket = "secret-buckets-example"
	now := time.Now()
	for _, n := range []string{"payments-api-dev-env", "payments-api-staging-env", "payments-api-prod-env", "auth-service-dev-env", "auth-service-prod-env", "notifier-prod-env"} {
		svc, env := store.SplitName(n)
		m.secrets = append(m.secrets, store.Secret{Bucket: m.bucket, Object: n, Service: svc, Env: env, Updated: now.Add(-50 * time.Hour)})
	}
	m.rebuildEnvs()
	add := func(obj, body string) {
		d := dotenv.Parse([]byte(body))
		for _, s := range m.secrets {
			if s.Object == obj {
				m.docs[obj] = &opened{sec: s, raw: secret.FromString(body), doc: d, env: d.IsEnv()}
			}
		}
	}
	add("payments-api-prod-env", "DATABASE_URL=postgres://u:p@10.0.0.4/pay\nREDIS_URL=redis://10.0.0.3:6379\nSTRIPE_KEY=sk_live_SAMPLE\nLEDGER_HMAC=same\nSENTRY_DSN=https://x@sentry.io/1\n")
	add("payments-api-dev-env", "DATABASE_URL=postgres://u:p@localhost/pay\nSTRIPE_KEY=sk_test_SAMPLE\nLEDGER_HMAC=same\nFEATURE_FLAGS=on\n")
	m.openService("payments-api", "prod")
	m.kcur = 2
	m.revealed["REDIS_URL"] = 1
	m.pods = kube.Snapshot{Context: "gke_example", Pods: []kube.Pod{{Namespace: "pay", Name: "payments-api-7c9-x2kq", Workload: "payments-api", Phase: "Running", Reason: "CrashLoopBackOff", Ready: 0, Total: 1, Restarts: 7}, {Namespace: "pay", Name: "payments-api-7c9-lm3a", Workload: "payments-api", Phase: "Running", Ready: 1, Total: 1}}}
	return m
}

func dump(t *testing.T, name, s string) {
	dir := os.Getenv("SECDROP_DUMP")
	if dir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, name+".txt"), []byte(s), 0o600)
}

func TestScreensRender(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	check := func(name, v string) {
		t.Helper()
		lines := len(splitLines(v))
		if lines != m.h {
			t.Errorf("%s: %d lines, want %d", name, lines, m.h)
		}
		for i, l := range splitLines(v) {
			if w := lipgloss.Width(l); w > m.w {
				t.Errorf("%s: line %d is %d wide", name, i, w)
			}
		}
		dump(t, name, v)
	}
	check("panel", m.View())
	m.reveal([]string{"DATABASE_URL", "REDIS_URL"})
	m.fx["DATABASE_URL"] = revealFx{start: time.Now().Add(-200 * time.Millisecond), show: true}
	check("panel-revealing", m.View())
	delete(m.fx, "DATABASE_URL")
	m.closePanel()
	check("home", m.View())
	m.openService("payments-api", "prod")

	m.openPalette("")
	m.pal.y, m.pal.glass = 0, 1
	check("palette", m.View())
	m.pal.setMode(m, pmPods)
	m.pal.in.SetValue("crash")
	m.pal.filter()
	check("palette-pods", m.View())
	m.pal = nil

	m.startCompare()
	check("compare", m.View())
	m.cmp = nil

	e, _ := m.currentEntry()
	m.modal = newEditModal(m.shownDoc(), e)
	check("edit", m.View())
	m.review(m.modal, []byte("X=1\n"), "Changes the value of STRIPE_KEY", "Saved")
	check("confirm", m.View())
	m.modal = nil

	m.scr = scrAuth
	check("auth", m.View())
	m.scr = scrSplash
	check("splash", m.View())
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func TestHomeListsEachServiceOnce(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.closePanel()
	n := 0
	for _, l := range m.viewHome(100, 25) {
		if strings.Contains(l, "▤  payments-api") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("payments-api row shown %d times", n)
	}
}

func TestShatterEndsOnValue(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	val := []rune("redis://10.0.0.3:6379")
	if got := shatter(val, 40, 1, 0, "K"); strings.TrimSpace(got) != string(val) {
		t.Fatalf("final frame = %q", got)
	}
	if got := shatter(val, 40, 0, 0, "K"); !strings.HasPrefix(got, strings.Repeat("•", maskWidth)) {
		t.Fatalf("first frame = %q", got)
	}
	for f := 0; f <= 10; f++ {
		if w := lipgloss.Width(shatter(val, 40, float64(f)/10, f, "K")); w != len(val) {
			t.Fatalf("frame %d width %d", f, w)
		}
	}
}

func TestHomeScrollsToCursor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.closePanel()
	m.secrets = nil
	for i := 0; i < 35; i++ {
		n := fmt.Sprintf("svc-%02d-dev-env", i)
		svc, env := store.SplitName(n)
		m.secrets = append(m.secrets, store.Secret{Object: n, Service: svc, Env: env})
	}
	m.rebuildEnvs()
	for _, h := range []int{20, 26, 32} {
		m.h, m.cur, m.off = h, 0, 0
		for c := 0; c < 35; c++ {
			m.cur = c
			v := m.View()
			if !strings.Contains(v, "▌ ▤  "+fmt.Sprintf("svc-%02d", c)) {
				t.Fatalf("h=%d cursor %d not visible:\n%s", h, c, v)
			}
		}
	}
}

type fakeCSI string

func (f fakeCSI) String() string { return string(f) }

func TestKeySearchAndCmdK(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	press := func(keys ...string) {
		for _, k := range keys {
			var msg tea.KeyMsg
			switch k {
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			default:
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
			}
			m.Update(msg)
		}
	}
	// "/" inside the panel filters keys; letters like q go to the box, not quit.
	press("/", "r", "e", "d", "i", "s")
	if es := m.panelEntries(); len(es) != 1 || es[0].Key != "REDIS_URL" {
		t.Fatalf("filter gave %v", es)
	}
	v := m.View()
	dump(t, "panel-search", v)
	if !strings.Contains(v, "1 of 5 keys") {
		t.Fatalf("missing count:\n%s", v)
	}
	press("enter")
	if e, _ := m.currentEntry(); e.Key != "REDIS_URL" {
		t.Fatalf("cursor on %s", e.Key)
	}
	press("esc")
	if len(m.panelEntries()) != 5 || !m.panel {
		t.Fatal("first esc should clear the filter and stay in the panel")
	}
	press("esc")
	if m.panel {
		t.Fatal("second esc should leave the panel")
	}

	// On the home screen you just type to filter; enter opens the match.
	press("n", "o", "t")
	if ss := m.services(); len(ss) != 1 || ss[0].name != "notifier" {
		t.Fatalf("service filter gave %v", ss)
	}
	dump(t, "home-filter", m.View())
	press("enter")
	if !m.panel || m.svc != "notifier" {
		t.Fatal("enter should open the filtered service")
	}

	// ⌘K (ESC[107;9u) opens the palette like ctrl+k.
	m.Update(fakeCSI(superK))
	if m.pal == nil {
		t.Fatal("⌘K did not open the palette")
	}
}

func TestRevealAllShowsEveryValue(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.Opts().NoMotion = false
	m.revealed = map[string]int{}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if len(m.revealed) != 5 {
		t.Fatalf("v revealed %d of 5", len(m.revealed))
	}
	// Run the animation to its end, as the frame ticks would.
	time.Sleep(revealDur + 30*stagger)
	m.stepFx()
	v := m.View()
	for _, want := range []string{"postgres://u:p@10.0.0.4/pay", "redis://10.0.0.3:6379", "sk_live_SAMPLE", "https://x@sentry.io/1"} {
		if !strings.Contains(v, want) {
			t.Fatalf("%s not revealed:\n%s", want, v)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if len(m.revealed) != 0 {
		t.Fatal("second v should hide everything")
	}
}

func TestPanelScrollsToCursor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	var body strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&body, "KEY_%02d=v%d\n", i, i)
	}
	d := dotenv.Parse([]byte(body.String()))
	o := m.docs["payments-api-prod-env"]
	o.doc, o.raw = d, secret.FromString(body.String())
	m.kcur, m.koff = 0, 0
	for _, h := range []int{20, 24, 32} {
		m.h, m.kcur, m.koff = h, 0, 0
		for c := 0; c < 60; c++ {
			v := m.View()
			if !strings.Contains(v, fmt.Sprintf("▌ KEY_%02d", c)) {
				t.Fatalf("h=%d key %d not visible:\n%s", h, c, v)
			}
			m.Update(tea.KeyMsg{Type: tea.KeyDown})
		}
	}
}

func TestSplashFrames(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := New(context.Background(), Options{})
	m.w, m.h = 110, 30
	for _, ms := range []int{1300, 1500, 1650, 1800, 2000, 4000} {
		m.splashT0 = time.Now().Add(-time.Duration(ms) * time.Millisecond)
		v := m.viewSplash()
		if n := len(splitLines(v)); n != m.h {
			t.Fatalf("%dms: %d lines", ms, n)
		}
		for i, l := range splitLines(v) {
			if w := lipgloss.Width(l); w > m.w {
				t.Fatalf("%dms: line %d is %d wide", ms, i, w)
			}
		}
		dump(t, fmt.Sprintf("splash-%04d", ms), v)
	}
	m.splashT0 = time.Now().Add(-splashTotal)
	if !m.splashDone() {
		t.Fatal("splash should finish")
	}
}

// The finished splash must show exactly the same logo as the static render.
func TestSplashEndsOnExactLogo(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := New(context.Background(), Options{})
	m.w, m.h = 110, 30
	m.splashT0 = time.Now().Add(-splashTotal)
	v := m.viewSplash()
	for _, l := range splitLines(renderLogo(-1)) {
		if !strings.Contains(v, strings.TrimRight(l, " ")) {
			t.Fatalf("final frame differs from the logo at %q:\n%s", l, v)
		}
	}
}

func TestPodHealthInPanel(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.podsAt = time.Now() // don't shell out to kubectl in tests
	v := m.View()
	dump(t, "panel-pods", v)
	for _, want := range []string{"Pods  ▲ 1 of 2 unhealthy", "CrashLoopBackOff", "payments-api-7c9-x2kq", "0/1 ready · 7 restarts"} {
		if !strings.Contains(v, want) {
			t.Fatalf("panel missing %q:\n%s", want, v)
		}
	}
	// Healthy services get a single line.
	m.pods.Pods = m.pods.Pods[1:]
	if v := m.View(); !strings.Contains(v, "Pods  ● all 1 healthy") {
		t.Fatalf("healthy summary missing:\n%s", v)
	}
	// p no longer opens anything.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if !m.panel || m.modal != nil {
		t.Fatal("p should do nothing")
	}
}

func TestEnterRevealsOneKey(t *testing.T) {
	m := fixture()
	m.revealed = map[string]int{}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.revealed) != 1 {
		t.Fatalf("enter revealed %d keys", len(m.revealed))
	}
	m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if len(m.revealed) != 1 {
		t.Fatal("space should no longer toggle reveal")
	}
}
