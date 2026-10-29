package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dumbprism19/secdrop/internal/config"
)

// Workspaces are personal shortlists of services, listed beside the cards
// in ⌘K. Opening one lists its services; enter on a service opens its
// secrets. They are stored per bucket in secdrop's local settings.

const wsCard = 3 // the workspace column, after the three cards

func (m *Model) workspaces() []config.Workspace { return m.cfg.WorkspacesFor(m.bucket) }

func (m *Model) findWorkspace(name string) (config.Workspace, bool) {
	for _, w := range m.workspaces() {
		if strings.EqualFold(w.Name, name) {
			return w, true
		}
	}
	return config.Workspace{}, false
}

// workspaceItems lists a workspace's services, then the actions to change it.
func (m *Model) workspaceItems(name string) []pitem {
	ws, ok := m.findWorkspace(name)
	if !ok {
		return nil
	}
	byName := map[string]service{}
	for _, sv := range m.allServices() {
		byName[sv.name] = sv
	}
	nameW := 0
	for _, n := range ws.Services {
		nameW = max(nameW, len(n))
	}
	var items []pitem
	for _, n := range ws.Services {
		sv, ok := byName[n]
		if !ok {
			n := n
			items = append(items, pitem{kind: pkService, title: n, detail: "not in this bucket any more",
				render: func(bool) string { return sFaint.Render(n) },
				run:    func(m *Model) tea.Cmd { return m.flash(n+" isn't in gs://"+m.bucket+" any more", true) }})
			continue
		}
		items = append(items, m.serviceItem(sv, nameW))
	}
	return items
}

// viewWorkspaceColumn is the list beside the cards: each workspace with its
// service count, then "+ New workspace".
func (m *Model) viewWorkspaceColumn(w, h int) []string {
	p := m.pal
	focused := p.card == wsCard
	title := sMuted.Render("WORKSPACES")
	if focused {
		title = sAmberB.Render("WORKSPACES")
	}
	out := []string{title}
	wss := m.workspaces()
	rows := len(wss) + 1
	p.wsCur = max(0, min(p.wsCur, rows-1))
	n := h - 1
	start := max(0, min(p.wsCur-n/2, rows-n))
	for i := start; i < rows && i < start+n; i++ {
		sel := focused && i == p.wsCur
		mark := "  "
		if sel {
			mark = sAmber.Render("▌ ")
		}
		if i == len(wss) {
			label := sMuted.Render("+ New workspace")
			if sel {
				label = sAmberB.Render("+ New workspace")
			}
			out = append(out, mark+label)
			continue
		}
		ws := wss[i]
		name := sText.Render(trunc(ws.Name, w-8))
		if sel {
			name = sAmberB.Render(trunc(ws.Name, w-8))
		}
		count := sFaint.Render(fmt.Sprint(len(ws.Services)))
		out = append(out, spread(mark+name, count, w))
	}
	if len(wss) == 0 && h > 3 {
		out = append(out, "", sFaint.Render("  Save the services you"), sFaint.Render("  use most."))
	}
	return out
}

// ---- create, edit, delete ----

func newWorkspaceModal(m *Model, ws *config.Workspace) *modal {
	name := newInput("e.g. Payments team")
	name.CharLimit = 40
	filter := newInput("type to filter services")
	md := &modal{kind: mWorkspace, keyIn: name, valIn: filter, wsSel: map[string]bool{}}
	if ws != nil {
		md.wsOrig = ws.Name
		name.SetValue(ws.Name)
		for _, s := range ws.Services {
			md.wsSel[s] = true
		}
	}
	md.keyIn = name
	md.keyIn.CursorEnd()
	md.keyIn.Focus()
	return md
}

// wsChoices is the service list in the picker, narrowed by the filter.
func (m *Model) wsChoices(md *modal) []service {
	words := strings.Fields(strings.ToLower(md.valIn.Value()))
	var out []service
	for _, sv := range m.allServices() {
		if matchAll(sv.name, words) {
			out = append(out, sv)
		}
	}
	return out
}

func (m *Model) workspaceKey(k tea.KeyMsg) tea.Cmd {
	md := m.modal
	key := k.String()
	if md.field == 0 { // naming
		switch key {
		case "esc":
			m.modal = nil
			return nil
		case "enter":
			name := strings.TrimSpace(md.keyIn.Value())
			switch {
			case name == "":
				md.err = "Give the workspace a name."
			case m.cfg.HasWorkspace(m.bucket, name, md.wsOrig):
				md.err = "You already have a workspace called “" + name + "”."
			default:
				md.err, md.field = "", 1
				md.keyIn.Blur()
				return md.valIn.Focus()
			}
			return nil
		}
		md.err = ""
		var cmd tea.Cmd
		md.keyIn, cmd = md.keyIn.Update(k)
		return cmd
	}

	choices := m.wsChoices(md)
	switch key {
	case "esc":
		md.field, md.err = 0, ""
		md.valIn.Blur()
		return md.keyIn.Focus()
	case "up":
		md.wsCur = max(0, md.wsCur-1)
		return nil
	case "down":
		md.wsCur = min(len(choices)-1, md.wsCur+1)
		return nil
	case " ":
		if md.wsCur < len(choices) {
			n := choices[md.wsCur].name
			md.wsSel[n] = !md.wsSel[n]
			if !md.wsSel[n] {
				delete(md.wsSel, n)
			}
		}
		return nil
	case "enter":
		if len(md.wsSel) == 0 {
			md.err = "Pick at least one service with space."
			return nil
		}
		return m.saveWorkspace(md)
	}
	prev := md.valIn.Value()
	var cmd tea.Cmd
	md.valIn, cmd = md.valIn.Update(k)
	if md.valIn.Value() != prev {
		md.wsCur, md.err = 0, ""
	}
	return cmd
}

func (m *Model) saveWorkspace(md *modal) tea.Cmd {
	// Keep the services in the bucket's order, A–Z.
	var svcs []string
	for n := range md.wsSel {
		svcs = append(svcs, n)
	}
	sort.Strings(svcs)
	ws := config.Workspace{Name: strings.TrimSpace(md.keyIn.Value()), Bucket: m.bucket, Services: svcs}
	m.cfg.SaveWorkspace(md.wsOrig, ws)
	m.modal = nil
	if err := m.cfg.Save(); err != nil {
		return m.flash("Couldn't save the workspace: "+err.Error(), true)
	}
	verb := "Created"
	if md.wsOrig != "" {
		verb = "Updated"
	}
	return m.flash(fmt.Sprintf("%s workspace %s with %s · find it in %s", verb, ws.Name, plural(len(svcs), "service"), paletteKey), false)
}

func (m *Model) workspaceDeleteKey(key string) tea.Cmd {
	name := m.modal.key
	switch key {
	case "y", "Y":
		m.cfg.DeleteWorkspace(m.bucket, name)
		m.modal = nil
		if err := m.cfg.Save(); err != nil {
			return m.flash("Couldn't delete the workspace: "+err.Error(), true)
		}
		return m.flash("Deleted workspace "+name+". Its services are untouched.", false)
	case "n", "N", "esc":
		m.modal = nil
	}
	return nil
}

func (m *Model) viewWorkspaceModal(w int) (string, []string) {
	md := m.modal
	title := "New workspace"
	if md.wsOrig != "" {
		title = "Edit workspace"
	}
	if md.field == 0 {
		body := []string{sMuted.Render("Name"), md.keyIn.View(), sLine.Render(strings.Repeat("─", w-4))}
		if md.err != "" {
			body = append(body, sHot.Render(md.err))
		}
		return title, append(body, "", sFaint.Render("Next you'll pick its services."), "",
			hints([][2]string{{"enter", "next"}, {"esc", "cancel"}}))
	}

	choices := m.wsChoices(md)
	md.wsCur = max(0, min(md.wsCur, len(choices)-1))
	body := []string{spread(sWhiteB.Render(strings.TrimSpace(md.keyIn.Value())), sMuted.Render(plural(len(md.wsSel), "service")+" picked"), w-4),
		"", md.valIn.View(), sLine.Render(strings.Repeat("─", w-4))}
	n := min(10, max(4, m.h-18))
	start := max(0, min(md.wsCur-n/2, len(choices)-n))
	for i := start; i < len(choices) && i < start+n; i++ {
		sv := choices[i]
		box := sFaint.Render("[ ] ")
		if md.wsSel[sv.name] {
			box = sAmber.Render("[x] ")
		}
		mark, name := "  ", sText.Render(sv.name)
		if i == md.wsCur {
			mark, name = sAmber.Render("▌ "), sAmberB.Render(sv.name)
		}
		var envs []string
		for _, s := range sv.secrets {
			envs = append(envs, envStyle(s.Env).Render(orDash(s.Env)))
		}
		body = append(body, spread(mark+box+name, strings.Join(envs, " "), w-4))
	}
	if len(choices) == 0 {
		body = append(body, sMuted.Render("  No service matches."))
	}
	if md.err != "" {
		body = append(body, "", sHot.Render(md.err))
	}
	return title, append(body, "", hints([][2]string{{"space", "pick"}, {"enter", "save"}, {"esc", "back"}}))
}

// openWorkspaces opens the workspace list on its own: w inside a service,
// ctrl+w on the home screen (where letters go to the search box).
func (m *Model) openWorkspaces() tea.Cmd {
	cmd := m.openPalette("")
	m.pal.setMode(m, pmWorkspaces)
	if len(m.workspaces()) > 0 {
		m.pal.cur = 1 // start on the first workspace; + New is one row up
	}
	return cmd
}

// workspaceListItems is "+ New workspace" followed by every workspace.
// Enter on a workspace shows its services; esc from there comes back here.
func (m *Model) workspaceListItems() []pitem {
	items := []pitem{{kind: pkAction, title: "+ New workspace", detail: "name it, then pick services",
		render: func(sel bool) string {
			if sel {
				return sAmberB.Render("+ New workspace")
			}
			return sAmber.Render("+ New workspace")
		},
		run: func(m *Model) tea.Cmd { m.modal = newWorkspaceModal(m, nil); return textinput.Blink }}}
	for _, ws := range m.workspaces() {
		ws := ws
		items = append(items, pitem{kind: pkWorkspace, title: ws.Name, detail: plural(len(ws.Services), "service"),
			extra: strings.Join(ws.Services, " "),
			run: func(m *Model) tea.Cmd {
				// Reopen on this workspace's services, with esc leading back here.
				cmd := m.openPalette("")
				m.pal.setMode(m, pmWorkspaces)
				m.pal.ws = ws.Name
				m.pal.setMode(m, pmWorkspace)
				return cmd
			}})
	}
	return items
}

// workspaceActionKey handles e (edit) and d (delete) while a workspace's
// services are listed. The list has no filter box, so letters are commands.
func (m *Model) workspaceActionKey(key string) (tea.Cmd, bool) {
	ws, ok := m.findWorkspace(m.pal.ws)
	if !ok {
		return nil, false
	}
	switch key {
	case "e":
		m.pal = nil
		m.modal = newWorkspaceModal(m, &ws)
		return textinput.Blink, true
	case "d":
		m.pal = nil
		m.modal = &modal{kind: mWsDelete, key: ws.Name}
		return nil, true
	}
	return nil, false
}

// returnToWorkspace reopens a workspace after you leave one of its
// services, with the cursor on that service. It appears in place rather
// than dropping in, and esc from it goes to the home screen.
func (m *Model) returnToWorkspace(name, service string) tea.Cmd {
	ws, ok := m.findWorkspace(name)
	if !ok {
		return nil // deleted in the meantime; stay home
	}
	cmd := m.openPalette("")
	p := m.pal
	p.y, p.vy, p.glass = 0, 0, 1
	p.ws = ws.Name
	p.setMode(m, pmWorkspace)
	p.closeOnEsc = true
	for i, idx := range p.matches {
		if p.items[idx].title == service {
			p.cur = i
		}
	}
	return cmd
}
