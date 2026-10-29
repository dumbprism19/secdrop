package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dumbprism19/secdrop/internal/config"
)

func TestWorkspaces(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // never touch the real settings
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.podsAt = time.Now()
	m.closePanel()
	key := func(k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }
	typ := func(s string) {
		for _, r := range s {
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}

	// ⌘K shows the workspace column beside the cards, empty at first.
	m.Update(fakeCSI(superK))
	v := m.View()
	dump(t, "palette-ws-empty", v)
	if !strings.Contains(v, "WORKSPACES") || !strings.Contains(v, "+ New workspace") {
		t.Fatalf("workspace column missing:\n%s", v)
	}

	// Move into the column and create one.
	key(tea.KeyLeft) // from Services, left wraps to the workspace column
	if m.pal.card != wsCard {
		t.Fatalf("card %d, want the workspace column", m.pal.card)
	}
	key(tea.KeyEnter)
	if m.modal == nil || m.modal.kind != mWorkspace {
		t.Fatal("+ New workspace should open the editor")
	}
	key(tea.KeyEnter)
	if m.modal.err == "" {
		t.Fatal("an empty name must be refused")
	}
	typ("Payments team")
	key(tea.KeyEnter)
	typ("pay") // filter the picker
	m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	key(tea.KeyBackspace)
	key(tea.KeyBackspace)
	key(tea.KeyBackspace)
	typ("noti")
	m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	dump(t, "ws-picker", m.View())
	key(tea.KeyEnter)
	if m.modal != nil {
		t.Fatalf("save should close the editor: %+v", m.modal.err)
	}
	wss := config.Load().WorkspacesFor(m.bucket)
	if len(wss) != 1 || wss[0].Name != "Payments team" || strings.Join(wss[0].Services, ",") != "notifier,payments-api" {
		t.Fatalf("saved %+v", wss)
	}

	// It's listed in ⌘K; opening it lists its services.
	m.Update(fakeCSI(superK))
	key(tea.KeyLeft)
	v = m.View()
	dump(t, "palette-ws", v)
	if !strings.Contains(v, "Payments team") {
		t.Fatalf("workspace not listed:\n%s", v)
	}
	key(tea.KeyEnter)
	if m.pal.mode != pmWorkspace || m.pal.ws != "Payments team" {
		t.Fatal("enter should open the workspace")
	}
	v = m.View()
	dump(t, "palette-ws-open", v)
	for _, want := range []string{"Workspaces › Payments team", "notifier", "payments-api", "e edit workspace", "d delete workspace", "2 results"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "auth-service") {
		t.Fatal("services outside the workspace must not be listed")
	}

	// Enter on a service goes straight to its secrets.
	key(tea.KeyDown) // payments-api is second
	key(tea.KeyEnter)
	if m.pal != nil || !m.panel || m.svc != "payments-api" {
		t.Fatalf("should open payments-api's secrets: panel=%v svc=%q", m.panel, m.svc)
	}

	// Duplicate names are refused; deleting asks first.
	m.modal = newWorkspaceModal(m, nil)
	typ("payments TEAM")
	key(tea.KeyEnter)
	if !strings.Contains(m.modal.err, "already have") {
		t.Fatal("duplicate names must be refused")
	}
	m.modal = &modal{kind: mWsDelete, key: "Payments team"}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if len(config.Load().WorkspacesFor(m.bucket)) != 0 {
		t.Fatal("delete should remove it")
	}
}

func TestWorkspaceShortcuts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.podsAt = time.Now()
	m.closePanel()
	key := func(msg tea.KeyMsg) { m.Update(msg) }

	// On the home screen a plain w is typing, not a shortcut.
	key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if m.pal != nil || m.homeIn.Value() != "w" {
		t.Fatal("w on the home screen should go to the search box")
	}
	key(tea.KeyMsg{Type: tea.KeyEsc})

	// ctrl+w opens the list with + New workspace first.
	key(tea.KeyMsg{Type: tea.KeyCtrlW})
	if m.pal == nil || m.pal.mode != pmWorkspaces {
		t.Fatal("ctrl+w should open the workspace list")
	}
	v := m.View()
	dump(t, "ws-list-empty", v)
	if !strings.Contains(v, "+ New workspace") || !strings.Contains(v, "Workspaces ›") {
		t.Fatalf("list view wrong:\n%s", v)
	}
	key(tea.KeyMsg{Type: tea.KeyEnter})
	if m.modal == nil || m.modal.kind != mWorkspace {
		t.Fatal("enter on + New workspace should open the editor")
	}
	m.modal = nil

	// With a workspace saved, the list starts on it; enter opens it and esc comes back.
	m.cfg.SaveWorkspace("", config.Workspace{Name: "Payments team", Bucket: m.bucket, Services: []string{"payments-api"}})
	key(tea.KeyMsg{Type: tea.KeyCtrlW})
	v = m.View()
	dump(t, "ws-list", v)
	if !strings.Contains(v, "Payments team") {
		t.Fatalf("saved workspace missing:\n%s", v)
	}
	key(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pal == nil || m.pal.mode != pmWorkspace || m.pal.ws != "Payments team" {
		t.Fatal("enter should open the workspace's services")
	}
	key(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pal == nil || m.pal.mode != pmWorkspaces {
		t.Fatal("esc should go back to the workspace list")
	}
	key(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pal != nil {
		t.Fatal("esc on the list should close it")
	}

	// Inside a service, a plain w opens the same list.
	m.openService("payments-api", "prod")
	key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if m.pal == nil || m.pal.mode != pmWorkspaces {
		t.Fatal("w inside a service should open the workspace list")
	}
}

func TestWorkspaceEditDeleteKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.podsAt = time.Now()
	m.closePanel()
	m.cfg.SaveWorkspace("", config.Workspace{Name: "Payments team", Bucket: m.bucket, Services: []string{"notifier", "payments-api"}})
	open := func() {
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if m.pal == nil || m.pal.mode != pmWorkspace {
			t.Fatal("workspace didn't open")
		}
	}
	letter := func(s string) { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }

	open()
	dump(t, "ws-open-actions", m.View())
	letter("x") // other letters do nothing; there's no filter box here
	if m.pal == nil || m.pal.mode != pmWorkspace || len(m.pal.matches) != 2 {
		t.Fatal("stray letters must not filter or close the workspace")
	}

	letter("e")
	if m.pal != nil || m.modal == nil || m.modal.kind != mWorkspace || m.modal.wsOrig != "Payments team" || len(m.modal.wsSel) != 2 {
		t.Fatalf("e should open the editor on this workspace: %+v", m.modal)
	}
	m.modal = nil

	open()
	letter("d")
	if m.modal == nil || m.modal.kind != mWsDelete || m.modal.key != "Payments team" {
		t.Fatal("d should ask to delete this workspace")
	}
	letter("n")
	if len(m.workspaces()) != 1 {
		t.Fatal("n must keep the workspace")
	}
}

// From a workspace into a service: esc goes back to the workspace (on that
// service), and esc again goes to the home screen.
func TestEscFromServiceReturnsToWorkspace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.podsAt = time.Now()
	m.closePanel()
	m.cfg.SaveWorkspace("", config.Workspace{Name: "Payments team", Bucket: m.bucket, Services: []string{"notifier", "payments-api"}})
	key := func(k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }

	key(tea.KeyCtrlW) // workspace list
	key(tea.KeyEnter) // Payments team
	key(tea.KeyDown)  // payments-api
	key(tea.KeyEnter) // its secrets
	if m.pal != nil || !m.panel || m.svc != "payments-api" {
		t.Fatal("should be in payments-api's secrets")
	}

	key(tea.KeyEsc)
	if m.panel || m.pal == nil || m.pal.mode != pmWorkspace || m.pal.ws != "Payments team" {
		t.Fatalf("esc should return to the workspace: panel=%v pal=%v", m.panel, m.pal != nil)
	}
	if it := m.pal.items[m.pal.matches[m.pal.cur]]; it.title != "payments-api" {
		t.Fatalf("cursor should be on the service just left, got %s", it.title)
	}
	dump(t, "ws-returned", m.View())

	key(tea.KeyEsc)
	if m.pal != nil || m.panel {
		t.Fatal("second esc should go to the home screen")
	}

	// Opening a service from home still goes straight home on esc.
	for _, r := range "payments" { // typed, as a user would
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	key(tea.KeyEnter)
	if !m.panel {
		t.Fatal("home enter should open the service")
	}
	key(tea.KeyEsc)
	if m.panel || m.pal != nil {
		t.Fatal("esc after opening from home must go home, not to a workspace")
	}

	// The same return works when the workspace was opened from ⌘K's column.
	m.Update(fakeCSI(superK))
	key(tea.KeyLeft)
	key(tea.KeyEnter) // Payments team
	key(tea.KeyEnter) // notifier
	key(tea.KeyEsc)
	if m.pal == nil || m.pal.mode != pmWorkspace {
		t.Fatal("esc should return to the workspace opened from ⌘K too")
	}
	key(tea.KeyEsc)
	if m.pal != nil {
		t.Fatal("and esc again goes home")
	}
}
