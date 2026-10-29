package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestUpdateNotices(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := fixture()
	m.w = 160
	m.closePanel()

	// Nothing to say: no line.
	if v := m.View(); strings.Contains(v, "secdrop 0.") {
		t.Fatal("no notice expected")
	}

	// Just updated: a confirmation that goes away once you start typing.
	m.opts.UpdatedFrom, m.opts.Version = "0.3.0", "0.4.0"
	v := m.View()
	dump(t, "update-done", v)
	if !strings.Contains(v, "✓ Updated secdrop 0.3.0 → 0.4.0") {
		t.Fatalf("missing updated line:\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if strings.Contains(m.View(), "Updated secdrop") {
		t.Fatal("the updated line should go once you use the app")
	}

	// Couldn't update itself: say so and give the command.
	m.opts.UpdatedFrom = ""
	m.opts.UpdateAvailable = "v0.5.0"
	v = m.View()
	if !strings.Contains(v, "↑ secdrop 0.5.0 is available") || !strings.Contains(v, "go install github.com/dumbprism19/secdrop/cmd/secdrop@latest") {
		t.Fatalf("missing available notice:\n%s", v)
	}
	m.opts.UpdateErr = errors.New("network unreachable")
	v = m.View()
	dump(t, "update-failed", v)
	if !strings.Contains(v, "couldn't update automatically (network unreachable)") {
		t.Fatalf("missing failure reason:\n%s", v)
	}

	// Only on the home screen, not inside a service.
	m.openService("payments-api", "prod")
	if strings.Contains(m.View(), "is available") {
		t.Fatal("the notice belongs on the home screen only")
	}
}
