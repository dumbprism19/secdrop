package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dumbprism19/secdrop/internal/store"
)

type bucketItem struct {
	name, project string
	recent        bool
}

// bucketPicker finds Berglas buckets across the user's projects so nobody has
// to remember or type bucket names.
type bucketPicker struct {
	items          []bucketItem
	cur            int
	scanned, total int
	done           bool
	err            error
	typing         bool
	in             textinput.Model
	cancel         context.CancelFunc
	ch             <-chan store.DiscoverEvent
}

func (b *bucketPicker) stop() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

func (b *bucketPicker) wait() tea.Cmd {
	ch := b.ch
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return discoverMsg{Done: true}
		}
		return discoverMsg(ev)
	}
}

func (b *bucketPicker) handle(ev store.DiscoverEvent) tea.Cmd {
	if b.ch == nil {
		return nil
	}
	if f := ev.Found; f != nil {
		found := false
		for i := range b.items {
			if b.items[i].name == f.Name {
				b.items[i].project, found = f.Project, true
			}
		}
		if !found {
			b.items = append(b.items, bucketItem{name: f.Name, project: f.Project})
		}
	}
	if ev.Total > 0 {
		b.scanned, b.total = ev.Scanned, ev.Total
	}
	if ev.Done {
		b.done, b.err, b.ch = true, ev.Err, nil
		return nil
	}
	return b.wait()
}

func (m *Model) openPicker() tea.Cmd {
	m.bk.stop()
	in := textinput.New()
	in.Prompt = "gs://"
	in.PromptStyle = sMuted
	in.TextStyle = sText
	in.Placeholder = "bucket-name"
	in.PlaceholderStyle = sFaint
	in.Cursor.Style = sAmber
	m.bk = bucketPicker{in: in}
	for _, r := range m.cfg.Recent {
		m.bk.items = append(m.bk.items, bucketItem{name: r, recent: true})
	}
	m.scr = scrBuckets
	ctx, cancel := context.WithCancel(m.ctx)
	m.bk.cancel = cancel
	m.bk.ch = m.st.Discover(ctx, []string{m.ident.Project, m.cfg.Project})
	return m.bk.wait()
}

func (m *Model) bucketKey(k tea.KeyMsg) tea.Cmd {
	b := &m.bk
	if b.typing {
		switch k.String() {
		case "esc":
			b.typing = false
			b.in.Blur()
			return nil
		case "enter":
			name := strings.TrimSpace(b.in.Value())
			name = strings.TrimPrefix(strings.TrimPrefix(name, "berglas://"), "gs://")
			name, _, _ = strings.Cut(name, "/")
			if name == "" {
				return nil
			}
			return m.useBucket(name)
		}
		var cmd tea.Cmd
		b.in, cmd = b.in.Update(k)
		return cmd
	}
	switch k.String() {
	case "q":
		return tea.Quit
	case "esc":
		if m.bucket != "" {
			b.stop()
			m.scr = scrBrowser
		}
	case "up", "k":
		b.cur = max(0, b.cur-1)
	case "down", "j":
		b.cur = min(len(b.items)-1, b.cur+1)
	case "t", "/", "i":
		b.typing = true
		return b.in.Focus()
	case "r":
		return m.openPicker()
	case "enter":
		if b.cur >= 0 && b.cur < len(b.items) {
			return m.useBucket(b.items[b.cur].name)
		}
		b.typing = true
		return b.in.Focus()
	}
	return nil
}

func (m *Model) viewBuckets() string {
	b := &m.bk
	w := min(72, m.w-4)
	var rows []string
	rows = append(rows, sWhiteB.Render("Choose a secrets bucket"), "")
	rows = append(rows, sMuted.Render("secdrop looks for Berglas buckets in every project your account can see."), "")

	status := ""
	switch {
	case b.ch != nil && b.total == 0:
		status = m.spin.View() + sMuted.Render(" Listing your projects…")
	case b.ch != nil:
		status = m.spin.View() + sMuted.Render(fmt.Sprintf(" Searching projects · %d of %d", b.scanned, b.total))
	case b.err != nil && len(b.items) == 0:
		status = sHot.Render("Couldn't search: " + trunc(b.err.Error(), w-18))
	case b.done:
		status = sMuted.Render(fmt.Sprintf("Searched %d projects", b.total))
	}
	rows = append(rows, status, "")

	if len(b.items) == 0 && b.ch == nil {
		rows = append(rows, sMuted.Render("No buckets found. Press t to type one."))
	}
	maxRows := max(3, m.h-20)
	start := max(0, min(b.cur-maxRows/2, len(b.items)-maxRows))
	for i := start; i < len(b.items) && i < start+maxRows; i++ {
		it := b.items[i]
		mark, name := "  ", sText.Render(it.name)
		if i == b.cur && !b.typing {
			mark, name = sAmber.Render("▌ "), sAmberB.Render(it.name)
		}
		meta := it.project
		if it.recent {
			meta = strings.TrimSpace("recent  " + it.project)
		}
		gap := max(2, w-2-lipgloss.Width(it.name)-lipgloss.Width(meta))
		rows = append(rows, mark+name+strings.Repeat(" ", gap)+sFaint.Render(meta))
	}
	rows = append(rows, "")
	if b.typing {
		rows = append(rows, sMuted.Render("Bucket name"), b.in.View(), "")
		rows = append(rows, hints([][2]string{{"enter", "open"}, {"esc", "back"}}))
	} else {
		h := [][2]string{{"↑↓", "move"}, {"enter", "open"}, {"t", "type a name"}, {"r", "search again"}}
		if m.bucket != "" {
			h = append(h, [2]string{"esc", "back"})
		}
		rows = append(rows, hints(append(h, [2]string{"q", "quit"})))
	}
	body := lipgloss.NewStyle().Width(w).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	return m.centered(lipgloss.JoinVertical(lipgloss.Left, renderLogoSmall(), "", "", body))
}
