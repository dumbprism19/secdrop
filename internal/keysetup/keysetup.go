// Package keysetup teaches the user's terminal to forward ⌘K to secdrop.
//
// macOS terminals handle ⌘K themselves (usually "clear"), so apps never see
// it. We ask the terminal to send ESC[107;9u, the CSI-u code for Super+K,
// which secdrop treats like ctrl+k.
package keysetup

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const sequence = `\u001b[107;9u`

// The binding only applies while a full-screen app (the alternate buffer) is
// active, so ⌘K still clears the terminal at a normal shell prompt.
const binding = `{
    "key": "cmd+k",
    "command": "workbench.action.terminal.sendSequence",
    "args": { "text": "` + sequence + `" },
    "when": "terminalFocus && terminalAltBufferActive"
  }`

var editors = []string{"Cursor", "Code", "Code - Insiders", "VSCodium", "Windsurf"}

// Run installs the binding for every VS Code-family editor it finds and
// prints instructions for other terminals.
func Run(w io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	found := false
	for _, app := range editors {
		dir := filepath.Join(home, "Library", "Application Support", app, "User")
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		found = true
		msg, err := install(filepath.Join(dir, "keybindings.json"))
		if err != nil {
			fmt.Fprintf(w, "✗ %s: %v\n", app, err)
			continue
		}
		fmt.Fprintf(w, "✓ %s: %s\n", app, msg)
	}
	if !found {
		fmt.Fprintln(w, "No VS Code-family editor found.")
	}
	fmt.Fprint(w, `
Other terminals: map ⌘K to send the escape sequence ESC [107;9u
  Ghostty   keybind = cmd+k=csi:107;9u
  kitty     map cmd+k send_text all \x1b[107;9u
  WezTerm   { key = "k", mods = "CMD", action = wezterm.action.SendString "\x1b[107;9u" }
  iTerm2    Settings → Keys → Key Bindings → + → ⌘K → Send Escape Sequence → [107;9u
These apply everywhere in that terminal, so ⌘K stops clearing the screen there.
ctrl+k always works without any setup.
`)
	return nil
}

// install adds the binding to a keybindings.json, which may contain comments,
// so it edits the text instead of re-encoding JSON. The original is backed up.
func install(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		b, err = []byte("[]\n"), nil
	}
	if err != nil {
		return "", err
	}
	if bytes.Contains(b, []byte("107;9u")) {
		return "already set up", nil
	}
	s := string(b)
	end := strings.LastIndex(s, "]")
	if end < 0 {
		return "", fmt.Errorf("%s isn't a JSON array; add this entry by hand:\n  %s", path, binding)
	}
	head := strings.TrimRight(s[:end], " \t\r\n")
	sep := ","
	if strings.HasSuffix(head, "[") || strings.HasSuffix(head, ",") {
		sep = ""
	}
	out := head + sep + "\n  " + binding + "\n" + s[end:]
	if len(b) > 0 {
		if err := os.WriteFile(path+".secdrop-backup", b, 0o600); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return "", err
	}
	return "⌘K now opens secdrop's search (backup: keybindings.json.secdrop-backup)", nil
}
