package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dumbprism19/secdrop/internal/secret"
	"github.com/dumbprism19/secdrop/internal/store"
)

func TestChangesBetweenVersions(t *testing.T) {
	a := secret.FromString("A=1\nB=2\nC=3\n")
	b := secret.FromString("A=1\nB=changed\nD=4\n")
	add, rem, chg := changes(a, b)
	if strings.Join(add, ",") != "D" || strings.Join(rem, ",") != "C" || strings.Join(chg, ",") != "B" {
		t.Fatalf("added %v removed %v changed %v", add, rem, chg)
	}
}

func historyFixture(t *testing.T) *Model {
	m := fixture()
	o := m.shownDoc()
	now := time.Now()
	o.sec.Generation = 300
	m.hist = &historyView{sec: o.sec, vals: map[int64]secret.Value{300: o.raw}, loading: map[int64]bool{},
		info: store.BucketInfo{ProjectNumber: 1, Versioning: true},
		versions: []store.Version{
			{Generation: 300, Updated: now.Add(-2 * time.Hour), Live: true},
			{Generation: 200, Updated: now.Add(-48 * time.Hour)},
			{Generation: 100, Updated: now.Add(-30 * 24 * time.Hour)},
		},
		events: []store.Event{
			{Time: now.Add(-2*time.Hour + time.Minute), Who: "ci@example.iam", Kind: "write"},
			{Time: now.Add(-time.Hour), Who: "dev@example.com", Kind: "read"},
			{Time: now.Add(-3 * time.Hour), Who: "intruder@example.com", Kind: "read", Denied: true},
		}}
	return m
}

func TestHistoryTimeline(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := historyFixture(t)
	rows := m.hist.rows()
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 3 versions + 2 accesses", len(rows))
	}
	if rows[0].ev == nil || rows[0].ev.Who != "dev@example.com" {
		t.Fatal("newest row should be the read")
	}
	if rows[1].ver == nil || rows[1].who != "ci@example.iam" || rows[1].num != 3 {
		t.Fatalf("version 3 should be saved by ci: %+v", rows[1])
	}
	m.hist.cur = 3 // version 2
	m.hist.vals[200] = secret.FromString("DATABASE_URL=old\nREDIS_URL=redis://10.0.0.3:6379\n")
	m.hist.vals[100] = secret.FromString("DATABASE_URL=old\n")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	v := m.View()
	dump(t, "history", v)
	for _, want := range []string{"History", "version 3", "current", "saved by ci@example.iam", "decrypted by dev@example.com",
		"read denied for intruder@example.com", "Version 2", "+ REDIS_URL", "r restore this version"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if m.modal == nil || m.modal.kind != mConfirm || string(m.modal.data) != "DATABASE_URL=old\nREDIS_URL=redis://10.0.0.3:6379\n" {
		t.Fatalf("restore should review version 2's contents: %+v", m.modal)
	}
	dump(t, "restore", m.View())
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.modal != nil {
		t.Fatal("n should close a one-step review")
	}
}

func TestCopyKeyAcross(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.startCompare() // prod ⇄ dev; first row is REDIS_URL, missing in dev
	rows := m.compareRows()
	if rows[0].key != "REDIS_URL" || m.copyHint() != "copy to dev" {
		t.Fatalf("first row %s, hint %q", rows[0].key, m.copyHint())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	md := m.modal
	if md == nil || md.o.sec.Env != "dev" || !strings.Contains(string(md.data), "REDIS_URL=redis://10.0.0.3:6379") {
		t.Fatalf("should review writing REDIS_URL into dev: %+v", md)
	}
	if !strings.Contains(string(md.data), "STRIPE_KEY=sk_test_SAMPLE") {
		t.Fatal("other dev keys must be kept")
	}
	dump(t, "copy-across", m.View())
}

func TestExportFile(t *testing.T) {
	m := fixture()
	p := filepath.Join(t.TempDir(), "out.env")
	n, err := exportFile(p, m.shownDoc())
	if err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	if _, err := exportFile(p, m.shownDoc()); err == nil {
		t.Fatal("must not overwrite")
	}
}

func TestPaletteCards(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.podsAt = time.Now()
	key := func(k tea.KeyMsg) { m.Update(k) }
	runes := func(s string) { key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }

	m.Update(fakeCSI(superK))
	v := m.View()
	dump(t, "palette-cards", v)
	for _, want := range []string{"Services", "Pod health", "Search", "3 services", "▲ 1 unhealthy", "Start typing to search"} {
		if !strings.Contains(v, want) {
			t.Fatalf("card screen missing %q:\n%s", want, v)
		}
	}

	// Services card: one row per service, not per environment.
	key(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pal.mode != pmServices || len(m.pal.matches) != 3 {
		t.Fatalf("services mode with %d rows", len(m.pal.matches))
	}
	dump(t, "palette-services", m.View())
	key(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pal == nil || m.pal.mode != pmCards {
		t.Fatal("esc should go back to the cards")
	}

	// Pod health card.
	key(tea.KeyMsg{Type: tea.KeyRight})
	key(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pal.mode != pmPods || len(m.pal.matches) != 2 {
		t.Fatalf("pods mode with %d rows", len(m.pal.matches))
	}
	dump(t, "palette-pods", m.View())
	key(tea.KeyMsg{Type: tea.KeyEnter}) // the unhealthy pod opens its service
	if m.pal != nil || !m.panel || m.svc != "payments-api" {
		t.Fatal("a pod should open its service")
	}

	// Typing on the cards goes straight to Search.
	m.Update(fakeCSI(superK))
	runes("redis")
	if m.pal.mode != pmSearch || len(m.pal.matches) == 0 || m.pal.items[m.pal.matches[0]].title != "REDIS_URL" {
		t.Fatal("typing should search keys")
	}
	dump(t, "palette-search", m.View())
}

func TestExportLine(t *testing.T) {
	cases := map[string]string{
		"8080":                        "export K=8080",
		"postgres://u:p@10.0.0.4/app": "export K=postgres://u:p@10.0.0.4/app",
		"two words":                   "export K='two words'",
		"it's":                        `export K='it'\''s'`,
		"$HOME and `cmd`":             "export K='$HOME and `cmd`'",
		"":                            "export K=''",
		"line1\nline2":                "export K='line1\nline2'",
	}
	for in, want := range cases {
		if got := exportLine("K", in); got != want {
			t.Errorf("%q → %s, want %s", in, got, want)
		}
	}
	// Each line must give the original value back when run by a real shell.
	for in, line := range cases {
		out, err := exec.Command("sh", "-c", line+`; printf %s "$K"`).Output()
		if err != nil || string(out) != in {
			t.Errorf("shell read %q back as %q (%v)", in, out, err)
		}
	}
}

// Paste tests use the in-memory sample service only and stop at the review
// screen; nothing is ever saved (the fixture has no cloud store at all).
func pasteMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

func openAdd(t *testing.T) *Model {
	m := fixture()
	m.modal = newAddModal(m.shownDoc())
	if m.st != nil {
		t.Fatal("tests must not have a cloud store")
	}
	return m
}

func TestPasteOneExportLine(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := openAdd(t)
	m.Update(pasteMsg("export NEW_KEY = 'hello world'"))
	md := m.modal
	if md.keyIn.Value() != "NEW_KEY" || md.valIn.Value() != "hello world" || md.field != 1 {
		t.Fatalf("key %q value %q field %d", md.keyIn.Value(), md.valIn.Value(), md.field)
	}
	dump(t, "add-pasted", m.View())
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if md.kind != mConfirm || !strings.Contains(md.change, "Adds NEW_KEY") {
		t.Fatalf("should be reviewing: kind %v change %q", md.kind, md.change)
	}
	data := string(md.data)
	if !strings.Contains(data, `NEW_KEY="hello world"`) || !strings.HasPrefix(data, "DATABASE_URL=postgres://u:p@10.0.0.4/pay\n") {
		t.Fatalf("file would be:\n%s", data)
	}
}

func TestTypedExportLineInKeyBox(t *testing.T) {
	m := openAdd(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("export PORT=8080")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if md := m.modal; md.keyIn.Value() != "PORT" || md.valIn.Value() != "8080" || md.field != 1 {
		t.Fatalf("key %q value %q", md.keyIn.Value(), md.valIn.Value())
	}
}

func TestPasteBlock(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := openAdd(t)
	m.Update(pasteMsg("# from my shell\nexport A=1\nexport B = 'two words'\nexport REDIS_URL=redis://new:6379\n"))
	md := m.modal
	if len(md.bulk) != 3 {
		t.Fatalf("bulk %v", md.bulk)
	}
	v := m.View()
	dump(t, "add-block", v)
	if !strings.Contains(v, "Add 3 keys") || !strings.Contains(v, "replaces current") || strings.Contains(v, "two words") {
		t.Fatalf("block view wrong (values must stay hidden):\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if md.kind != mConfirm || md.change != "Adds A, B; replaces REDIS_URL" {
		t.Fatalf("change %q", md.change)
	}
	if strings.Count(string(md.data), "REDIS_URL=") != 1 {
		t.Fatal("REDIS_URL must be replaced in place, not duplicated")
	}
}

func TestPastePlainValueStillWorks(t *testing.T) {
	m := openAdd(t)
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // to the Value box
	m.Update(pasteMsg("just-a-token-value"))
	if md := m.modal; md.valIn.Value() != "just-a-token-value" || md.keyIn.Value() != "" {
		t.Fatalf("plain paste went wrong: key %q value %q", md.keyIn.Value(), md.valIn.Value())
	}
}
