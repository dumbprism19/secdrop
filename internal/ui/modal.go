package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dumbprism19/secdrop/internal/dotenv"
	"github.com/dumbprism19/secdrop/internal/kube"
	"github.com/dumbprism19/secdrop/internal/secret"
)

type modalKind int

const (
	mEdit modalKind = iota
	mAdd
	mDelete
	mConfirm
	mSaving
	mPod
	mExport
)

// modal is the one dialog that can be open over the browser: edit, add,
// delete, the review step before a write, or pod details.
type modal struct {
	kind   modalKind
	o      *opened
	key    string
	keyIn  textinput.Model
	valIn  textinput.Model
	field  int // add: 0 key, 1 value
	err    string
	hidden bool

	// review step
	data   []byte
	note   string
	change string
	back   modalKind

	pod kube.Pod
	// bulk holds several KEY=value pairs pasted into the add dialog at once.
	bulk    []dotenv.Assignment
	addNote string // what a paste filled in
	// oneStep review dialogs (restore, copy across) close on "n" instead of
	// returning to an earlier step.
	oneStep bool
	ctx     string
}

func newInput(placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.PlaceholderStyle = sFaint
	in.TextStyle = sHi
	in.Cursor.Style = sAmber
	in.CharLimit = 0
	return in
}

func newEditModal(o *opened, e dotenv.Entry) *modal {
	v := newInput("new value")
	v.SetValue(e.Value.Reveal())
	v.CursorEnd()
	v.Focus()
	return &modal{kind: mEdit, o: o, key: e.Key, valIn: v}
}

func newAddModal(o *opened) *modal {
	k := newInput("KEY_NAME")
	k.Focus()
	return &modal{kind: mAdd, o: o, keyIn: k, valIn: newInput("value")}
}

func newDeleteModal(o *opened, key string) *modal {
	return &modal{kind: mDelete, o: o, key: key}
}

func (md *modal) updateInputs(msg tea.Msg) tea.Cmd {
	var c1, c2 tea.Cmd
	md.keyIn, c1 = md.keyIn.Update(msg)
	md.valIn, c2 = md.valIn.Update(msg)
	return tea.Batch(c1, c2)
}

func (m *Model) modalKey(k tea.KeyMsg) tea.Cmd {
	md := m.modal
	key := k.String()
	switch md.kind {
	case mSaving:
		return nil

	case mExport:
		switch key {
		case "esc":
			m.modal = nil
			return nil
		case "enter":
			path := strings.TrimSpace(md.valIn.Value())
			if path == "" {
				return nil
			}
			n, err := exportFile(path, md.o)
			if err != nil {
				md.err = err.Error()
				return nil
			}
			m.modal = nil
			return m.flash(fmt.Sprintf("Wrote %s to %s (only you can read it) · don't commit it", plural(n, "key"), path), false)
		}
		md.err = ""
		var cmd tea.Cmd
		md.valIn, cmd = md.valIn.Update(k)
		return cmd

	case mPod:
		switch key {
		case "c":
			m.modal = nil
			return m.copy(fmt.Sprintf("kubectl describe pod -n %s %s", md.pod.Namespace, md.pod.Name), "Copied the kubectl command")
		case "l":
			m.modal = nil
			return m.copy(fmt.Sprintf("kubectl logs -n %s %s --tail=200", md.pod.Namespace, md.pod.Name), "Copied the kubectl logs command")
		}
		m.modal = nil
		return nil

	case mConfirm:
		switch key {
		case "y", "Y", "enter":
			md.kind = mSaving
			return tea.Batch(m.save(md.o.sec, md.data, md.note), m.spin.Tick)
		case "n", "esc", "backspace":
			md.kind = md.back
			if md.back == mDelete || md.oneStep {
				m.modal = nil
			}
		}
		return nil

	case mDelete:
		switch key {
		case "y", "Y":
			doc := md.o.doc.Clone()
			doc.Delete(md.key)
			m.review(md, doc.Bytes(), "Removes "+md.key, "Deleted "+md.key)
		case "n", "esc", "q":
			m.modal = nil
		}
		return nil

	case mEdit:
		switch key {
		case "esc":
			m.modal = nil
			return nil
		case "ctrl+r":
			md.hidden = !md.hidden
			if md.hidden {
				md.valIn.EchoMode = textinput.EchoPassword
			} else {
				md.valIn.EchoMode = textinput.EchoNormal
			}
			return nil
		case "enter":
			val := md.valIn.Value()
			var data []byte
			if md.o.env {
				doc := md.o.doc.Clone()
				doc.Set(md.key, secret.FromString(val))
				data = doc.Bytes()
			} else {
				data = []byte(val)
			}
			if string(data) == md.o.raw.Reveal() {
				m.modal = nil
				return m.flash("Nothing changed", false)
			}
			m.review(md, data, "Changes the value of "+md.key, "Saved "+md.key)
			return nil
		}
		var cmd tea.Cmd
		md.valIn, cmd = md.valIn.Update(k)
		return cmd

	case mAdd:
		// A paste that looks like KEY=value (or "export KEY = value", or a
		// whole block of them) fills the dialog in instead of landing in a box.
		if k.Paste && md.takePaste(string(k.Runes)) {
			if md.field == 1 {
				md.keyIn.Blur()
				return md.valIn.Focus()
			}
			return nil
		}
		switch key {
		case "esc":
			if md.bulk != nil {
				md.bulk, md.addNote = nil, ""
				return nil
			}
			m.modal = nil
			return nil
		case "tab", "shift+tab", "down", "up":
			md.field = 1 - md.field
			if md.field == 0 {
				md.valIn.Blur()
				return md.keyIn.Focus()
			}
			md.keyIn.Blur()
			return md.valIn.Focus()
		case "enter":
			if md.bulk != nil {
				doc := md.o.doc.Clone()
				var added, replaced []string
				for _, a := range md.bulk {
					if md.o.doc.Has(a.Key) {
						replaced = append(replaced, a.Key)
					} else {
						added = append(added, a.Key)
					}
					doc.Set(a.Key, secret.FromString(a.Value))
				}
				m.review(md, doc.Bytes(), describeBulk(added, replaced), "Saved "+plural(len(md.bulk), "key"))
				return nil
			}
			// "export KEY=value" typed into the Key box works like a paste.
			if strings.Contains(md.keyIn.Value(), "=") && md.takePaste(md.keyIn.Value()) {
				if md.bulk == nil {
					md.keyIn.Blur()
					return md.valIn.Focus()
				}
				return nil
			}
			name := strings.TrimSpace(md.keyIn.Value())
			switch {
			case !dotenv.ValidKey(name):
				md.err = "Use letters, digits and underscores, starting with a letter."
				return nil
			case md.field == 0:
				md.field, md.err = 1, ""
				if md.o.doc.Has(name) {
					md.addNote = name + " already exists. Saving replaces its value."
				}
				md.keyIn.Blur()
				return md.valIn.Focus()
			}
			doc := md.o.doc.Clone()
			doc.Set(name, secret.FromString(md.valIn.Value()))
			md.key = name
			if md.o.doc.Has(name) {
				m.review(md, doc.Bytes(), "Replaces the value of "+name, "Saved "+name)
			} else {
				m.review(md, doc.Bytes(), "Adds "+name, "Added "+name)
			}
			return nil
		}
		md.err = ""
		return md.updateInputs(k)
	}
	return nil
}

// takePaste reads pasted text as KEY=value lines. One line fills the Key and
// Value boxes; several become a list to save together. It reports false when
// the text isn't assignments, so a plain value can still be pasted normally.
func (md *modal) takePaste(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "=") {
		return false
	}
	pairs, err := dotenv.ParseAssignments(text)
	if err != nil {
		if strings.Contains(text, "\n") {
			md.err = "Couldn't read the pasted lines: " + err.Error()
			return true
		}
		return false
	}
	if len(pairs) == 0 {
		return false
	}
	md.err = ""
	if len(pairs) == 1 {
		md.bulk = nil
		md.keyIn.SetValue(pairs[0].Key)
		md.valIn.SetValue(pairs[0].Value)
		md.valIn.CursorEnd()
		md.field = 1
		md.addNote = "Read " + pairs[0].Key + " from the pasted line."
		if md.o.doc.Has(pairs[0].Key) {
			md.addNote = pairs[0].Key + " already exists. Saving replaces its value."
		}
		return true
	}
	// Later lines win if a key repeats, as in a .env file.
	seen := map[string]int{}
	var uniq []dotenv.Assignment
	for _, p := range pairs {
		if i, ok := seen[p.Key]; ok {
			uniq[i] = p
			continue
		}
		seen[p.Key] = len(uniq)
		uniq = append(uniq, p)
	}
	md.bulk = uniq
	return true
}

func describeBulk(added, replaced []string) string {
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "Adds "+joinKeys(added))
	}
	if len(replaced) > 0 {
		parts = append(parts, "replaces "+joinKeys(replaced))
	}
	return strings.Join(parts, "; ")
}

func joinKeys(keys []string) string {
	if len(keys) > 4 {
		return strings.Join(keys[:4], ", ") + fmt.Sprintf(" and %d more", len(keys)-4)
	}
	return strings.Join(keys, ", ")
}

func (m *Model) review(md *modal, data []byte, change, note string) {
	md.back, md.kind = md.kind, mConfirm
	md.data, md.change = data, change
	md.note = note + " in " + label(md.o.sec)
}

func (m *Model) viewModal() string {
	md := m.modal
	w := min(70, m.w-6)
	o := md.o
	where := ""
	if o != nil {
		where = sMuted.Render(o.sec.Service+" · ") + envStyle(o.sec.Env).Render(orDash(o.sec.Env))
	}
	var title string
	var body []string
	switch md.kind {
	case mEdit:
		title = "Edit " + md.key
		body = []string{where, "", md.valIn.View(), sLine.Render(strings.Repeat("─", w-4)), "",
			hints([][2]string{{"enter", "review"}, {"ctrl+r", map[bool]string{true: "show", false: "hide"}[md.hidden]}, {"esc", "cancel"}})}
	case mAdd:
		title = "Add a key"
		k, v := sMuted, sMuted
		if md.field == 0 {
			k = sAmber
		} else {
			v = sAmber
		}
		if md.bulk != nil {
			title = fmt.Sprintf("Add %s", plural(len(md.bulk), "key"))
			body = []string{where, "", sMuted.Render("From the pasted lines:")}
			for i, a := range md.bulk {
				if i == 10 {
					body = append(body, sFaint.Render(fmt.Sprintf("  and %d more", len(md.bulk)-10)))
					break
				}
				mark, note := sOK.Render("+ "), ""
				if md.o.doc.Has(a.Key) {
					mark, note = sAmber.Render("~ "), sAmber.Render("  replaces current")
				}
				body = append(body, "  "+mark+sText.Render(pad(trunc(a.Key, 26), 28))+sFaint.Render("••••••••")+note)
			}
			body = append(body, "", hints([][2]string{{"enter", "review"}, {"esc", "back"}}))
			break
		}
		body = []string{where, "", k.Render("Key"), md.keyIn.View(), sLine.Render(strings.Repeat("─", w-4)),
			v.Render("Value"), md.valIn.View(), sLine.Render(strings.Repeat("─", w-4))}
		if md.err != "" {
			body = append(body, sHot.Render(md.err))
		} else if md.addNote != "" {
			body = append(body, sAmber.Render(md.addNote))
		} else {
			body = append(body, sFaint.Render("Tip: paste a line like  export KEY=value  or a whole block."))
		}
		body = append(body, "", hints([][2]string{{"tab", "next field"}, {"enter", "continue"}, {"esc", "cancel"}}))
	case mDelete:
		title = "Delete " + md.key + "?"
		body = []string{where, "", sText.Render("The key is removed from the file. Earlier versions stay in"),
			sText.Render("the bucket's object history if versioning is on."), "", hints([][2]string{{"y", "continue"}, {"n", "cancel"}})}
	case mConfirm, mSaving:
		title = "Save to " + label(o.sec) + "?"
		body = []string{sText.Render(md.change + "."),
			sMuted.Render("Writes a new version of " + trunc(o.sec.Ref(), w-30) + ","),
			sMuted.Render("only if nobody changed it since you opened it."), ""}
		if o.sec.Env == "prod" {
			body = append(body, sHot.Render("▲ Production. Services pick this up on their next restart."), "")
		}
		if md.kind == mSaving {
			body = append(body, m.spin.View()+sMuted.Render(" Encrypting and saving…"))
		} else {
			body = append(body, hints([][2]string{{"y", "save"}, {"n", "go back"}}))
		}
	case mExport:
		title = "Export to a file"
		body = []string{where, "", sMuted.Render("File"), md.valIn.View(), sLine.Render(strings.Repeat("─", w-4))}
		if md.err != "" {
			body = append(body, sHot.Render(md.err))
		}
		body = append(body, "", sMuted.Render("Written with owner-only permissions. Existing files are never overwritten."),
			"", hints([][2]string{{"enter", "write"}, {"esc", "cancel"}}))
	case mPod:
		p := md.pod
		glyph, st := sOK.Render("●"), sOK
		if !p.Healthy() {
			glyph, st = sHot.Render("▲"), sHot
		}
		title = p.Name
		row := func(k, v string) string { return sMuted.Render(pad(k, 12)) + v }
		body = []string{
			row("status", glyph+" "+st.Render(p.Status())),
			row("ready", sText.Render(fmt.Sprintf("%d of %d containers", p.Ready, p.Total))),
			row("restarts", sText.Render(fmt.Sprint(p.Restarts))),
			row("started", sText.Render(agoOr(p.Started))),
			row("namespace", sText.Render(p.Namespace)),
			row("context", sMuted.Render(trunc(md.ctx, w-18))),
			"", hints([][2]string{{"c", "copy describe"}, {"l", "copy logs"}, {"esc", "close"}}),
		}
	}
	return panel(title, w, body)
}

func (m *Model) viewHelp() string {
	w := min(64, m.w-6)
	row := func(k, v string) string { return sKey.Render(pad(k, 14)) + sText.Render(v) }
	body := []string{
		sTitle.Render("EVERYWHERE"),
		row(paletteKey, "search and every other action"),
		row("/", "filter the list you're looking at"),
		row("tab", "switch environment"),
		row("enter  esc", "open and go back"),
		"", sTitle.Render("IN A SERVICE"),
		row("enter", "reveal a value (hides after 20s)"),
		row("v", "reveal every value"),
		row("y", "copy as export KEY=value"),
		row("c", "copy the whole env file"),
		row("e", "edit a value"),
		row("d", "compare; enter copies a missing key across"),
		row("h", "history: versions, who changed it, restore"),
		"", sMuted.Render("Add, delete, export to a file, switch bucket,"),
		sMuted.Render("reload and sign in again are in " + paletteKey + "."),
	}
	return panel("Keys", w, body)
}

// panel draws a titled, rounded box used by every overlay.
func panel(title string, w int, body []string) string {
	head := sWhiteB.Render(trunc(title, w-6))
	content := lipgloss.JoinVertical(lipgloss.Left, append([]string{head, ""}, body...)...)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cAmberLo).
		Padding(1, 2).
		Width(w).
		Render(content)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func agoOr(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return ago(t)
}
