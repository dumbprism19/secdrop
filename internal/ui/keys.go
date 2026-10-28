package ui

import "runtime"

// superK is how Bubble Tea prints the CSI-u sequence ESC[107;9u (Super+K),
// which `secdrop setup-keys` teaches the terminal to send for ⌘K.
const superK = "?CSI[49 48 55 59 57 117]?"

// paletteKey is the shortcut shown in hints.
var paletteKey = func() string {
	if runtime.GOOS == "darwin" {
		return "⌘K"
	}
	return "ctrl+k"
}()

// Opts exposes options to tests.
func (m *Model) Opts() *Options { return &m.opts }
