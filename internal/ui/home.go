package ui

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The home screen is one rounded panel: a search bar you type into
// directly, the services grouped by when they last changed, and a preview
// of the highlighted service.

const (
	groupWeek = iota
	groupMonth
	groupEarlier
)

var groupNames = []string{"Changed this week", "Changed this month", "Earlier"}

// lastUpdate is the most recent change across a service's environments.
func (sv service) lastUpdate() time.Time {
	var t time.Time
	for _, s := range sv.secrets {
		if s.Updated.After(t) {
			t = s.Updated
		}
	}
	return t
}

func groupOf(t time.Time) int {
	switch d := time.Since(t); {
	case d < 7*24*time.Hour:
		return groupWeek
	case d < 30*24*time.Hour:
		return groupMonth
	}
	return groupEarlier
}

// services is the home list in display order: grouped by recency, A–Z
// within each group, narrowed by the environment tab and the search box.
func (m *Model) services() []service {
	words := strings.Fields(strings.ToLower(m.homeIn.Value()))
	var out []service
	for _, sv := range m.allServices() {
		if m.envIdx > 0 && sv.envIndex(m.envs[m.envIdx-1]) < 0 {
			continue
		}
		if matchAll(sv.name, words) {
			out = append(out, sv)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := groupOf(out[i].lastUpdate()), groupOf(out[j].lastUpdate())
		if gi != gj {
			return gi < gj
		}
		return out[i].name < out[j].name
	})
	return out
}

func (m *Model) homeKey(k tea.KeyMsg) tea.Cmd {
	n := len(m.services())
	switch k.String() {
	case "up":
		m.cur = max(0, m.cur-1)
	case "down":
		m.cur = min(n-1, m.cur+1)
	case "pgup":
		m.cur = max(0, m.cur-10)
	case "pgdown":
		m.cur = min(n-1, m.cur+10)
	case "home":
		m.cur = 0
	case "end":
		m.cur = n - 1
	case "tab", "shift+tab":
		if c := len(m.envs) + 1; c > 1 {
			if k.String() == "tab" {
				m.envIdx = (m.envIdx + 1) % c
			} else {
				m.envIdx = (m.envIdx + c - 1) % c
			}
			m.cur, m.off = 0, 0
		}
	case "esc":
		m.homeIn.SetValue("")
		m.cur, m.off = 0, 0
	case "enter", "right":
		if sv := m.selectedService(); sv != nil {
			return m.openService(sv.name, "")
		}
	default:
		// Everything else is typing into the search bar.
		prev := m.homeIn.Value()
		if !m.homeIn.Focused() {
			m.homeIn.Focus()
		}
		var cmd tea.Cmd
		m.homeIn, cmd = m.homeIn.Update(k)
		if m.homeIn.Value() != prev {
			m.cur, m.off = 0, 0
		}
		return cmd
	}
	return nil
}

// viewHome draws the panel, centred, filling the body area.
func (m *Model) viewHome(w, h int) []string {
	pw := min(w, 150)
	ph := h
	inner := pw - 4 // border and one space of padding each side

	var body []string
	switch {
	case m.listing && len(m.secrets) == 0:
		body = []string{"", m.spin.View() + sMuted.Render(" Loading secrets from gs://"+m.bucket+"…")}
	case m.listErr != nil:
		body = []string{"", sHot.Render("Couldn't list gs://" + m.bucket), sText.Render(trunc(m.listErr.Error(), inner)), "",
			sMuted.Render("Use " + paletteKey + " → Switch bucket, Reload or Sign in again.")}
	case len(m.secrets) == 0:
		body = []string{"", sMuted.Render("No Berglas secrets in gs://" + m.bucket + "."), "", sMuted.Render("Use " + paletteKey + " → Switch bucket.")}
	default:
		body = m.homeBody(inner, ph-2)
	}
	for len(body) < ph-2 {
		body = append(body, "")
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cLine).
		Padding(0, 1).Width(pw - 2).Render(strings.Join(body[:ph-2], "\n"))
	lines := strings.Split(box, "\n")
	left := strings.Repeat(" ", (w-pw)/2)
	for i := range lines {
		lines[i] = left + lines[i]
	}
	return lines
}

func (m *Model) homeBody(inner, h int) []string {
	// Search bar with the environment filter on the right, like a filter icon.
	var tabs []string
	all := sTab.Render("All")
	if m.envIdx == 0 {
		all = sTabOn.Render("All")
	}
	tabs = append(tabs, all)
	for i, e := range m.envs {
		if m.envIdx == i+1 {
			tabs = append(tabs, sTabOn.Render(e))
		} else {
			tabs = append(tabs, envStyle(e).Padding(0, 1).Render(e))
		}
	}
	filters := strings.Join(tabs, "")
	m.homeIn.Width = inner - lipgloss.Width(filters) - 6
	out := []string{"", spread(m.homeIn.View(), filters, inner), sLine.Render(strings.Repeat("─", inner))}

	lw := max(30, inner*44/100)
	rw := inner - lw - 3
	rows := h - len(out)
	left := m.homeList(lw, rows)
	right := m.homePreview(rw, rows)
	for i := 0; i < rows; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, fit(l, lw)+" "+sLine.Render("│")+" "+fit(r, rw))
	}
	return out
}

// homeList renders the grouped list. Group headings take rows, so scrolling
// works on display lines, keeping the highlighted service in view.
func (m *Model) homeList(w, h int) []string {
	ss := m.services()
	if len(ss) == 0 {
		msg := "No services in this environment."
		if q := m.homeIn.Value(); q != "" {
			msg = "No service matches “" + q + "”."
		}
		return []string{"", sMuted.Render(" " + msg)}
	}
	m.cur = max(0, min(m.cur, len(ss)-1))

	// Build every display line, remembering which one holds the cursor.
	var lines []string
	curLine, group := 0, -1
	for i, sv := range ss {
		if g := groupOf(sv.lastUpdate()); g != group {
			if group != -1 {
				lines = append(lines, "")
			}
			group = g
			lines = append(lines, " "+sMuted.Render(groupNames[g]))
		}
		if i == m.cur {
			curLine = len(lines)
		}
		lines = append(lines, m.homeRow(sv, i == m.cur, w))
	}

	if curLine < m.off {
		m.off = curLine
	}
	if curLine >= m.off+h {
		m.off = curLine - h + 1
	}
	// Show the group heading when the first service of a group is selected.
	if m.off > 0 && curLine == m.off && strings.TrimSpace(lines[m.off-1]) != "" {
		m.off--
	}
	m.off = max(0, min(m.off, max(0, len(lines)-h)))
	return lines[m.off:min(len(lines), m.off+h)]
}

// homeRow is one service: an icon, its name, and a dot per environment.
// The selected row is a filled pill.
func (m *Model) homeRow(sv service, sel bool, w int) string {
	bg := lipgloss.NewStyle()
	if sel {
		bg = bg.Background(cSelBg)
	}
	var dots []string
	changed := false
	for _, e := range m.envs {
		if sv.envIndex(e) >= 0 {
			dots = append(dots, envStyle(e).Inherit(bg).Render("●"))
		} else {
			dots = append(dots, bg.Render(" "))
		}
	}
	for _, s := range sv.secrets {
		changed = changed || m.changed[s.Object]
	}
	dotStr := strings.Join(dots, bg.Render(" "))
	if len(m.envs) == 0 {
		dotStr = sFaint.Inherit(bg).Render("—")
	}

	mark, icon, name := " ", sFaint.Inherit(bg).Render("▤"), sText.Inherit(bg).Render(sv.name)
	if sel {
		mark, icon, name = sAmber.Render("▌"), sAmber.Inherit(bg).Render("▤"), sAmberB.Inherit(bg).Render(sv.name)
	}
	if changed {
		name += sAmber.Inherit(bg).Render(" •")
	}
	nameW := w - 1 - 4 - lipgloss.Width(dotStr) - 2
	name = trunc(name, nameW)
	gap := max(1, w-1-4-lipgloss.Width(name)-lipgloss.Width(dotStr)-1)
	return mark + bg.Render(" ") + icon + bg.Render("  ") + name + bg.Render(strings.Repeat(" ", gap)) + dotStr + bg.Render(" ")
}

// homePreview shows the highlighted service without decrypting anything:
// a card per environment, pod health, then the name and date at the bottom.
func (m *Model) homePreview(w, h int) []string {
	sv := m.selectedService()
	if sv == nil {
		return nil
	}
	out := []string{""}

	per := min(3, len(sv.secrets))
	cw := (w - (per-1)*2) / max(1, per)
	var row []string
	flush := func() {
		if len(row) > 0 {
			out = append(out, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, row...), "\n")...)
			out = append(out, "")
			row = nil
		}
	}
	for _, s := range sv.secrets {
		state := sFaint.Render("locked")
		switch {
		case m.changed[s.Object]:
			state = sAmber.Render("• changed")
		case m.docs[s.Object] != nil:
			state = sMuted.Render("◦ decrypted")
		}
		border := cLine
		if s.Env == "prod" {
			border = cHot
		}
		card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(cw - 2).
			Render(strings.Join([]string{envStyle(s.Env).Bold(true).Render(orDash(s.Env)), sMuted.Render(ago(s.Updated)), state}, "\n"))
		if len(row) > 0 {
			row = append(row, "  ")
		}
		row = append(row, card)
		if len(row) == per*2-1 {
			flush()
		}
	}
	flush()

	if pods := m.podSection(sv.name, w); len(pods) > 0 {
		out = append(out, pods...)
	}

	// Name and date sit at the bottom, like a file preview's caption.
	last := sv.lastUpdate()
	caption := []string{
		sWhiteB.Render(sv.name),
		sMuted.Render("Last changed " + last.Local().Format("Jan 2 2006, 3:04 PM")),
		sFaint.Render(plural(len(sv.secrets), "environment") + " · enter to open"),
	}
	for len(out) < h-len(caption)-1 {
		out = append(out, "")
	}
	return append(out, caption...)
}

func newSearchInput() textinput.Model {
	in := textinput.New()
	in.Prompt = "⌕  "
	in.PromptStyle = sAmber
	in.TextStyle = sWhiteB
	in.Placeholder = "Search services"
	in.PlaceholderStyle = sFaint
	in.Cursor.Style = sAmber
	in.Focus()
	return in
}
