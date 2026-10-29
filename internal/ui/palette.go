package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"

	"github.com/dumbprism19/secdrop/internal/kube"
	"github.com/dumbprism19/secdrop/internal/store"
)

// The ⌘K palette opens on three cards — Services, Pod health, Search — and
// each card is its own short list. Typing straight away goes to Search.

type pmode int

const (
	pmCards pmode = iota
	pmServices
	pmPods
	pmSearch
	pmWorkspace  // one workspace's services
	pmWorkspaces // the list of workspaces, opened with w / ctrl+w
)

var cardTitles = []string{"Services", "Pod health", "Search"}

type pkind int

const (
	pkAction pkind = iota
	pkService
	pkKey
	pkEnv
	pkPod
	pkWorkspace
)

var pkindLabel = map[pkind]string{pkAction: "action", pkService: "service", pkKey: "key", pkEnv: "env", pkPod: "pod", pkWorkspace: "workspace"}

type pitem struct {
	kind   pkind
	title  string // plain text, used for matching
	detail string
	extra  string // searchable but not shown
	render func(selected bool) string
	run    func(m *Model) tea.Cmd
}

type palette struct {
	in     textinput.Model
	mode   pmode
	card   int    // highlighted card on the card screen; wsCard is the workspace column
	wsCur  int    // highlighted row in the workspace column
	ws     string // the workspace open in pmWorkspace
	parent pmode  // where esc goes back to from a list
	// closeOnEsc is set when the palette reopened on a workspace because
	// you came back from one of its services: esc then goes home.
	closeOnEsc bool
	items      []pitem
	matches    []int
	cur        int

	spring harmonica.Spring
	y, vy  float64 // offset from the resting row; starts above it
	glass  float64 // 0 → clear, 1 → fully frosted
	gv     float64
}

const paletteDrop = -5

func (m *Model) openPalette(query string) tea.Cmd {
	in := textinput.New()
	in.Prompt = "⌕  "
	in.PromptStyle = sAmber
	in.TextStyle = sWhiteB
	in.PlaceholderStyle = sFaint
	in.Cursor.Style = sAmber
	in.SetValue(query)
	in.CursorEnd()
	p := &palette{in: in, spring: harmonica.NewSpring(harmonica.FPS(60), 9.0, 0.62), y: paletteDrop}
	if m.opts.NoMotion {
		p.y, p.glass = 0, 1
	}
	m.pal = p
	if query != "" {
		p.mode = pmSearch
	}
	p.refresh(m)
	return tea.Batch(p.in.Focus(), m.loadPods(), m.tick())
}

func (p *palette) setMode(m *Model, mode pmode) {
	switch {
	case mode == pmCards:
		p.parent = pmCards
	case mode == pmWorkspace && p.mode == pmWorkspaces:
		p.parent = pmWorkspaces
	case mode != pmWorkspace:
		p.parent = pmCards
	}
	p.mode, p.cur = mode, 0
	p.refresh(m)
}

// step advances the spring one frame and reports whether it is still moving.
func (p *palette) step() bool {
	p.y, p.vy = p.spring.Update(p.y, p.vy, 0)
	p.glass, p.gv = p.spring.Update(p.glass, p.gv, 1)
	if math.Abs(p.y) < 0.05 && math.Abs(p.vy) < 0.05 && math.Abs(1-p.glass) < 0.02 {
		p.y, p.vy, p.glass = 0, 0, 1
		return false
	}
	return true
}

func (p *palette) refresh(m *Model) {
	switch p.mode {
	case pmWorkspaces:
		p.in.Placeholder = "Filter your workspaces"
	case pmWorkspace:
		p.in.Placeholder = "Filter this workspace"
	case pmServices:
		p.in.Placeholder = "Filter services"
	case pmPods:
		p.in.Placeholder = "Filter pods by name, namespace or status"
	default:
		p.in.Placeholder = "Start typing to search"
	}
	p.items = m.paletteItems(p.mode)
	p.filter()
}

// filter keeps items containing every typed word, best matches first: a
// word at the start of the title beats one in the middle, and titles beat
// details.
func (p *palette) filter() {
	words := strings.Fields(strings.ToLower(p.in.Value()))
	type hit struct{ i, score int }
	var hits []hit
	for i, it := range p.items {
		title := strings.ToLower(it.title)
		rest := strings.ToLower(pkindLabel[it.kind] + " " + it.detail + " " + it.extra)
		score, ok := 0, true
		for _, w := range words {
			switch j := strings.Index(title, w); {
			case j == 0:
			case j > 0:
				score += 1 + j
			case strings.Contains(rest, w):
				score += 100
			default:
				ok = false
			}
		}
		if ok {
			hits = append(hits, hit{i, score})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].score < hits[b].score })
	p.matches = p.matches[:0]
	for _, h := range hits {
		p.matches = append(p.matches, h.i)
	}
	p.cur = max(0, min(p.cur, len(p.matches)-1))
}

func (m *Model) paletteKey(k tea.KeyMsg) tea.Cmd {
	p := m.pal
	key := k.String()
	if key == "ctrl+k" {
		m.pal = nil
		return nil
	}

	if p.mode == pmCards {
		switch key {
		case "esc":
			m.pal = nil
		case "left", "shift+tab":
			p.card = (p.card + wsCard) % (wsCard + 1)
		case "right", "tab":
			p.card = (p.card + 1) % (wsCard + 1)
		case "up":
			if p.card == wsCard {
				p.wsCur = max(0, p.wsCur-1)
			}
		case "down":
			if p.card == wsCard {
				p.wsCur = min(len(m.workspaces()), p.wsCur+1)
			} else {
				p.setMode(m, pmode(p.card+1))
			}
		case "enter":
			if p.card != wsCard {
				p.setMode(m, pmode(p.card+1))
				break
			}
			wss := m.workspaces()
			if p.wsCur >= len(wss) {
				m.pal = nil
				m.modal = newWorkspaceModal(m, nil)
				return textinput.Blink
			}
			p.ws = wss[p.wsCur].Name
			p.setMode(m, pmWorkspace)
		default:
			// Typing starts a search right away.
			var cmd tea.Cmd
			p.in, cmd = p.in.Update(k)
			if p.in.Value() != "" {
				p.setMode(m, pmSearch)
			}
			return cmd
		}
		return nil
	}

	switch key {
	case "esc":
		p.in.SetValue("")
		back := p.parent
		if p.mode == pmWorkspaces || p.closeOnEsc {
			m.pal = nil // opened on its own with w; esc closes it
			return nil
		}
		p.setMode(m, back)
		return nil
	case "backspace":
		if p.in.Value() == "" {
			if p.mode == pmWorkspaces {
				return nil
			}
			p.setMode(m, p.parent)
			return nil
		}
	}
	if p.mode == pmWorkspace {
		if cmd, ok := m.workspaceActionKey(key); ok {
			return cmd
		}
		switch key {
		case "up", "down", "ctrl+p", "ctrl+n", "tab", "enter":
		default:
			return nil // no filter box in a workspace; other letters do nothing
		}
	}
	switch key {
	case "up", "ctrl+p":
		p.cur = max(0, p.cur-1)
		return nil
	case "down", "ctrl+n", "tab":
		p.cur = min(len(p.matches)-1, p.cur+1)
		return nil
	case "enter":
		if p.cur < len(p.matches) {
			it := p.items[p.matches[p.cur]]
			// Remember the workspace a service was opened from, so esc in
			// the service comes back to it.
			m.wsReturn = ""
			if p.mode == pmWorkspace && it.kind == pkService {
				m.wsReturn = p.ws
			}
			m.pal = nil
			return it.run(m)
		}
		return nil
	}
	prev := p.in.Value()
	var cmd tea.Cmd
	p.in, cmd = p.in.Update(k)
	if p.in.Value() != prev {
		p.cur = 0
		p.filter()
	}
	return cmd
}

// paletteItems builds the list for one card. Keys only come from secrets
// already decrypted this session; searching never triggers a KMS call.
func (m *Model) paletteItems(mode pmode) []pitem {
	switch mode {
	case pmWorkspace:
		return m.workspaceItems(m.pal.ws)
	case pmWorkspaces:
		return m.workspaceListItems()
	case pmServices:
		return m.serviceItems()
	case pmPods:
		return m.podItems()
	case pmSearch:
		return m.searchItems()
	}
	return nil
}

func (m *Model) serviceItems() []pitem {
	var items []pitem
	nameW := 0
	for _, sv := range m.allServices() {
		nameW = max(nameW, len(sv.name))
	}
	for _, sv := range m.allServices() {
		items = append(items, m.serviceItem(sv, nameW))
	}
	return items
}

// serviceItem is one service row: enter opens that service's secrets.
func (m *Model) serviceItem(sv service, nameW int) pitem {
	var envs []string
	for _, s := range sv.secrets {
		envs = append(envs, s.Env)
	}
	return pitem{kind: pkService, title: sv.name, extra: strings.Join(envs, " "),
		render: func(sel bool) string {
			st := sText
			if sel {
				st = sAmberB
			}
			// One fixed column per environment, as on the home screen.
			var tags []string
			for _, e := range m.envs {
				if sv.envIndex(e) >= 0 {
					tags = append(tags, envStyle(e).Render(e))
				} else {
					tags = append(tags, strings.Repeat(" ", len(e)))
				}
			}
			return st.Render(pad(sv.name, nameW)) + "  " + strings.Join(tags, " ")
		},
		run: func(m *Model) tea.Cmd {
			if !m.selectService(sv.name) {
				m.envIdx = 0
				m.homeIn.SetValue("")
				m.selectService(sv.name)
			}
			return m.openService(sv.name, "")
		}}
}

func (m *Model) podItems() []pitem {
	var items []pitem
	for _, p := range m.pods.Pods {
		p, ctx := p, m.pods.Context
		items = append(items, pitem{kind: pkPod, title: p.Name, detail: p.Namespace,
			extra:  p.Status() + " " + map[bool]string{true: "healthy", false: "unhealthy"}[p.Healthy()],
			render: func(sel bool) string { return podLine(p, sel) },
			run: func(m *Model) tea.Cmd {
				// Open the pod's service if we know it; otherwise show the pod.
				for _, sv := range m.allServices() {
					if p.BelongsTo(sv.name) {
						m.selectService(sv.name)
						return m.openService(sv.name, "")
					}
				}
				m.modal = &modal{kind: mPod, pod: p, ctx: ctx}
				return nil
			}})
	}
	return items
}

// searchItems is everything that isn't a service or a pod: actions, keys
// from decrypted secrets, and environments. Services join in once you type.
func (m *Model) searchItems() []pitem {
	var items []pitem
	act := func(title, detail string, run func(m *Model) tea.Cmd) {
		items = append(items, pitem{kind: pkAction, title: title, detail: detail, run: run})
	}

	if o := m.shownDoc(); o != nil && m.panel {
		act("Export to a .env file", label(o.sec), func(m *Model) tea.Cmd { m.modal = newExportModal(o); return textinput.Blink })
		act("History and access log", label(o.sec), func(m *Model) tea.Cmd { return m.openHistory() })
		if o.env {
			act("Add a key", label(o.sec), func(m *Model) tea.Cmd { m.modal = newAddModal(o); return textinput.Blink })
			if e, ok := m.currentEntry(); ok {
				key := e.Key
				act("Delete "+key, label(o.sec), func(m *Model) tea.Cmd { m.modal = newDeleteModal(o, key); return nil })
			}
		}
	}
	act("Switch bucket", "gs://"+m.bucket, func(m *Model) tea.Cmd { return m.openPicker() })
	act("Reload", "fetch the latest versions", func(m *Model) tea.Cmd { return m.list(m.bucket, false) })
	act("Sign in again", m.ident.Email, func(m *Model) tea.Cmd { m.scr = scrAuth; return m.login() })

	for _, s := range m.secrets {
		o := m.docs[s.Object]
		if o == nil || !o.env {
			continue
		}
		for _, e := range o.entries(true) {
			s, key := s, e.Key
			items = append(items, pitem{kind: pkKey, title: key, detail: label(s),
				run: func(m *Model) tea.Cmd { return m.jumpTo(s, key) }})
		}
	}
	for i, e := range m.envs {
		i, e := i, e
		items = append(items, pitem{kind: pkEnv, title: e, detail: "show only " + e,
			render: func(bool) string { return envStyle(e).Render(e) },
			run:    func(m *Model) tea.Cmd { m.closePanel(); m.envIdx, m.cur, m.off = i+1, 0, 0; return nil }})
	}
	act("Keyboard shortcuts", "?", func(m *Model) tea.Cmd { m.help = true; return nil })
	act("Quit", "q", func(m *Model) tea.Cmd { return tea.Quit })
	if m.pal != nil && m.pal.in.Value() != "" || m.pal == nil {
		items = append(items, m.serviceItems()...)
	}
	return items
}

func podLine(p kube.Pod, sel bool) string {
	glyph := sOK.Render("●")
	status := sMuted.Render(fmt.Sprintf("%d/%d", p.Ready, p.Total))
	if !p.Healthy() {
		glyph, status = sHot.Render("▲"), sHot.Render(p.Status())
	}
	name := sText
	if sel {
		name = sAmberB
	}
	return glyph + " " + name.Render(p.Name) + "  " + status
}

// jumpTo opens a secret's service panel on its environment and optionally
// highlights a key once it is decrypted.
func (m *Model) jumpTo(s store.Secret, key string) tea.Cmd {
	if !m.selectService(s.Service) {
		m.envIdx = 0
		m.selectService(s.Service)
	}
	m.pendingKey = key
	return m.openService(s.Service, s.Env)
}

// ---- views ----

func (m *Model) viewPalette() string {
	p := m.pal
	w := min(112, m.w-8)
	inner := w - 2 // Width includes the padding, not the border

	crumb := ""
	switch p.mode {
	case pmCards:
	case pmWorkspace:
		crumb = sMuted.Render("Workspaces › " + trunc(p.ws, 24))
	case pmWorkspaces:
		crumb = sMuted.Render("Workspaces ›")
	default:
		crumb = sMuted.Render(cardTitles[p.mode-1] + " ›")
	}
	p.in.Width = inner - lipgloss.Width(crumb) - 6
	top := p.in.View()
	if p.mode == pmWorkspace {
		n := 0
		if ws, ok := m.findWorkspace(p.ws); ok {
			n = len(ws.Services)
		}
		top = sAmber.Render("▦  ") + sWhiteB.Render(p.ws) + sMuted.Render("  ·  "+plural(n, "service"))
	}
	rows := []string{spread(top, crumb, inner), sLine.Render(strings.Repeat("─", inner))}

	if p.mode == pmCards {
		rows = append(rows, "", m.viewCardScreen(inner), "",
			hints([][2]string{{"←→", "choose"}, {"↑↓", "workspaces"}, {"enter", "open"}, {"type", "to search"}, {"esc", "close"}}))
	} else {
		rows = append(rows, m.viewPaletteList(inner)...)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAmber).
		Padding(0, 1).Width(w).Render(strings.Join(rows, "\n"))
}

// viewCardScreen puts the workspace column to the right of the cards, or
// under them when the window is narrow.
func (m *Model) viewCardScreen(inner int) string {
	const colW = 30
	if inner >= 90 {
		cards := m.viewCards(inner - colW - 3)
		h := lipgloss.Height(cards)
		col := lipgloss.NewStyle().Width(colW).Render(strings.Join(m.viewWorkspaceColumn(colW, h), "\n"))
		sep := lipgloss.NewStyle().Foreground(cLine).Render(strings.TrimRight(strings.Repeat("│\n", h), "\n"))
		return lipgloss.JoinHorizontal(lipgloss.Top, cards, " ", sep, " ", col)
	}
	return m.viewCards(inner) + "\n\n" + strings.Join(m.viewWorkspaceColumn(inner, 6), "\n")
}

// viewCards draws the three tiles: an icon at the top, the name and a live
// one-line status at the bottom.
func (m *Model) viewCards(inner int) string {
	gap := 2
	cw := (inner - 2*gap) / 3
	extra := inner - 2*gap - 3*cw // the last card takes the remainder
	healthy, bad := 0, 0
	for _, pd := range m.pods.Pods {
		if pd.Healthy() {
			healthy++
		} else {
			bad++
		}
	}
	var podStatus string
	switch {
	case m.podsBusy && len(m.pods.Pods) == 0:
		podStatus = sFaint.Render("checking…")
	case m.podsErr != nil && len(m.pods.Pods) == 0:
		podStatus = sFaint.Render("unavailable")
	case bad > 0:
		podStatus = sHot.Render(fmt.Sprintf("▲ %d unhealthy", bad))
	case healthy > 0:
		podStatus = sOK.Render(fmt.Sprintf("● %d healthy", healthy))
	default:
		podStatus = sFaint.Render("no pods")
	}
	type card struct{ icon, title, sub string }
	cards := []card{
		{sAmber.Render("▤"), "Services", sMuted.Render(plural(len(m.allServices()), "service"))},
		{sAmber.Render("◈"), "Pod health", podStatus},
		{sAmber.Render("⌕"), "Search", sMuted.Render("keys & actions")},
	}
	var tiles []string
	for i, c := range cards {
		sel := i == m.pal.card
		border, title := cLine, sText.Render(c.title)
		if sel {
			border, title = cAmber, sAmberB.Render(c.title)
		}
		width := cw
		if i == len(cards)-1 {
			width += extra
		}
		body := strings.Join([]string{c.icon, "", "", title, trunc(c.sub, width-4)}, "\n")
		tiles = append(tiles, lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
			Padding(0, 1).Width(width-2).Render(body))
	}
	sep := strings.Repeat(" ", gap)
	return lipgloss.JoinHorizontal(lipgloss.Top, tiles[0], sep, tiles[1], sep, tiles[2])
}

func (m *Model) viewPaletteList(inner int) []string {
	p := m.pal
	var rows []string
	if p.mode == pmPods {
		rows = append(rows, m.podsHeader(inner))
	}
	maxRows := min(12, max(4, m.h/2))
	if len(p.matches) == 0 {
		msg := "Nothing matches."
		switch {
		case p.mode == pmPods && m.podsBusy:
			msg = "Reading pods with kubectl…"
		case p.mode == pmPods && m.podsErr != nil:
			msg = "Pod health is unavailable: " + trunc(m.podsErr.Error(), inner-30)
		case p.mode == pmPods:
			msg = "No pods in the current kubectl context."
		}
		rows = append(rows, "", sMuted.Render("  "+msg), "")
	}
	start := max(0, min(p.cur-maxRows/2, len(p.matches)-maxRows))
	for i := start; i < len(p.matches) && i < start+maxRows; i++ {
		it := p.items[p.matches[i]]
		sel := i == p.cur
		mark := "  "
		if sel {
			mark = sAmber.Render("▌ ")
		}
		badge := ""
		if p.mode == pmSearch {
			badge = sFaint.Render(pad(pkindLabel[it.kind], 9))
		}
		title := sText.Render(it.title)
		if it.render != nil {
			title = it.render(sel)
		} else if sel {
			title = sAmberB.Render(it.title)
		}
		left := mark + badge + title
		detail := trunc(it.detail, max(0, inner-lipgloss.Width(left)-3))
		rows = append(rows, spread(left, sMuted.Render(detail), inner))
	}
	for i := len(p.matches) - start; i < maxRows && len(p.matches) > 0; i++ {
		rows = append(rows, "")
	}
	count := sFaint.Render(plural(len(p.matches), "result"))
	foot := hints([][2]string{{"↑↓", "move"}, {"enter", "open"}, {"esc", "back"}})
	rows = append(rows, sLine.Render(strings.Repeat("─", inner)), spread(foot, count, inner))
	if p.mode == pmWorkspace {
		dim := func(k, l string) string { return sFaint.Bold(true).Render(k) + " " + sFaint.Render(l) }
		rows = append(rows, dim("e", "edit workspace")+"   "+dim("d", "delete workspace"))
	}
	return rows
}

func (m *Model) podsHeader(inner int) string {
	if m.pods.Context == "" {
		return ""
	}
	bad := 0
	for _, p := range m.pods.Pods {
		if !p.Healthy() {
			bad++
		}
	}
	state := sOK.Render("all healthy")
	if bad > 0 {
		state = sHot.Render(fmt.Sprintf("%d unhealthy", bad))
	}
	return sFaint.Render(trunc(m.pods.Context, inner/2)) + sFaint.Render("  ·  ") + sMuted.Render(plural(len(m.pods.Pods), "pod")) + sFaint.Render("  ·  ") + state
}
