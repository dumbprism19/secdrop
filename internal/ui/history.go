package ui

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dumbprism19/secdrop/internal/dotenv"
	"github.com/dumbprism19/secdrop/internal/secret"
	"github.com/dumbprism19/secdrop/internal/store"
)

// historyView is one timeline per environment: every stored version (who
// saved it, which keys changed) and every access from Cloud Audit Logs.
// Values are compared by fingerprint; plaintext is never drawn here.
type historyView struct {
	sec      store.Secret
	info     store.BucketInfo
	versions []store.Version
	vErr     error
	vBusy    bool
	events   []store.Event
	aErr     error
	aBusy    bool
	vals     map[int64]secret.Value // decrypted versions, by generation
	loading  map[int64]bool
	detail   int64 // generation whose changes are shown
	restore  int64 // generation to offer for restore once it is decrypted
	cur, off int
}

type histRow struct {
	t   time.Time
	ver *store.Version
	num int    // 1 = oldest version
	who string // who saved the version, from the audit log
	ev  *store.Event
}

type (
	historyMsg struct {
		obj      string
		info     store.BucketInfo
		versions []store.Version
		err      error
	}
	auditMsg struct {
		obj    string
		events []store.Event
		err    error
	}
	versionMsg struct {
		obj string
		gen int64
		val secret.Value
		err error
	}
)

func (m *Model) openHistory() tea.Cmd {
	o := m.shownDoc()
	if o == nil {
		return nil
	}
	sec := o.sec
	m.hist = &historyView{sec: sec, vBusy: true, aBusy: true, vals: map[int64]secret.Value{}, loading: map[int64]bool{}}
	// The current version is already decrypted.
	m.hist.vals[sec.Generation] = o.raw
	return tea.Batch(
		func() tea.Msg {
			bi, _ := m.st.Bucket(m.ctx, sec.Bucket)
			vs, err := m.st.History(m.ctx, sec)
			return historyMsg{sec.Object, bi, vs, err}
		},
		func() tea.Msg {
			evs, err := m.st.Audit(m.ctx, sec)
			return auditMsg{sec.Object, evs, err}
		},
	)
}

func (m *Model) handleHistory(msg tea.Msg) tea.Cmd {
	h := m.hist
	switch msg := msg.(type) {
	case historyMsg:
		if h == nil || msg.obj != h.sec.Object {
			return nil
		}
		h.vBusy, h.versions, h.vErr, h.info = false, msg.versions, msg.err, msg.info
	case auditMsg:
		if h == nil || msg.obj != h.sec.Object {
			return nil
		}
		h.aBusy, h.events, h.aErr = false, msg.events, msg.err
	case versionMsg:
		if h == nil || msg.obj != h.sec.Object {
			return nil
		}
		delete(h.loading, msg.gen)
		if msg.err != nil {
			return m.failure(msg.err)
		}
		h.vals[msg.gen] = msg.val
		if h.restore == msg.gen {
			h.restore = 0
			return m.offerRestore(msg.gen)
		}
	}
	return nil
}

func (m *Model) loadVersion(gen int64) tea.Cmd {
	h := m.hist
	if _, ok := h.vals[gen]; ok || h.loading[gen] || gen == 0 {
		return nil
	}
	h.loading[gen] = true
	sec := h.sec
	return func() tea.Msg {
		v, err := m.st.ReadVersion(m.ctx, sec, gen)
		return versionMsg{sec.Object, gen, v, err}
	}
}

// rows merges versions and audit events into one timeline, newest first.
// A write event within a few minutes of a version names who saved it.
func (h *historyView) rows() []histRow {
	used := make([]bool, len(h.events))
	var out []histRow
	for i := range h.versions {
		v := &h.versions[i]
		r := histRow{t: v.Updated, ver: v, num: len(h.versions) - i}
		for j, e := range h.events {
			if !used[j] && e.Kind == "write" && !e.Denied && absDur(e.Time.Sub(v.Updated)) < 3*time.Minute {
				r.who, used[j] = e.Who, true
				break
			}
		}
		out = append(out, r)
	}
	for j := range h.events {
		if !used[j] {
			out = append(out, histRow{t: h.events[j].Time, ev: &h.events[j]})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].t.After(out[b].t) })
	return out
}

// previous returns the generation saved just before gen, or 0.
func (h *historyView) previous(gen int64) int64 {
	for i, v := range h.versions {
		if v.Generation == gen && i+1 < len(h.versions) {
			return h.versions[i+1].Generation
		}
	}
	return 0
}

func (m *Model) historyKey(key string) tea.Cmd {
	h := m.hist
	rows := h.rows()
	switch key {
	case "esc", "left", "h":
		m.hist = nil
	case "up", "k":
		h.cur = max(0, h.cur-1)
	case "down", "j":
		h.cur = min(len(rows)-1, h.cur+1)
	case "pgup":
		h.cur = max(0, h.cur-10)
	case "pgdown":
		h.cur = min(len(rows)-1, h.cur+10)
	case "enter":
		if h.cur < len(rows) && rows[h.cur].ver != nil {
			gen := rows[h.cur].ver.Generation
			h.detail = gen
			return tea.Batch(m.loadVersion(gen), m.loadVersion(h.previous(gen)))
		}
	case "r":
		if h.cur >= len(rows) || rows[h.cur].ver == nil {
			return nil
		}
		v := rows[h.cur].ver
		if v.Live {
			return m.flash("That's already the current version", false)
		}
		if _, ok := h.vals[v.Generation]; ok {
			return m.offerRestore(v.Generation)
		}
		h.restore = v.Generation
		return m.loadVersion(v.Generation)
	}
	return nil
}

func (m *Model) offerRestore(gen int64) tea.Cmd {
	h, o := m.hist, m.shownDoc()
	if h == nil || o == nil {
		return nil
	}
	num, when := 0, time.Time{}
	for _, r := range h.rows() {
		if r.ver != nil && r.ver.Generation == gen {
			num, when = r.num, r.t
		}
	}
	val := h.vals[gen]
	m.modal = &modal{kind: mConfirm, o: o, oneStep: true,
		data:   append([]byte(nil), val.Bytes()...),
		change: fmt.Sprintf("Restores version %d, saved %s, as a new version", num, ago(when)),
		note:   fmt.Sprintf("Restored version %d of %s", num, label(o.sec))}
	return nil
}

// changes lists what differs between two decrypted versions, by key name.
func changes(from, to secret.Value) (added, removed, changed []string) {
	a, b := dotenv.Parse(from.Bytes()), dotenv.Parse(to.Bytes())
	if !a.IsEnv() || !b.IsEnv() {
		if !from.Equal(to) {
			changed = []string{"value"}
		}
		return
	}
	old := map[string]secret.Value{}
	for _, e := range a.Entries() {
		old[e.Key] = e.Value
	}
	for _, e := range b.Entries() {
		v, ok := old[e.Key]
		switch {
		case !ok:
			added = append(added, e.Key)
		case !v.Equal(e.Value):
			changed = append(changed, e.Key)
		}
		delete(old, e.Key)
	}
	for k := range old {
		removed = append(removed, k)
	}
	sort.Strings(removed)
	return
}

func (m *Model) viewHistory(w, h int) []string {
	hv := m.hist
	out := []string{sWhiteB.Render("History") + sMuted.Render("  ·  "+label(hv.sec)), ""}

	if hv.vBusy && hv.aBusy {
		return append(out, m.spin.View()+sMuted.Render(" Reading versions and audit logs…"))
	}
	if hv.vErr != nil {
		out = append(out, sHot.Render(trunc("Couldn't list versions: "+hv.vErr.Error(), w)))
	} else if !hv.info.Versioning && hv.info.ProjectNumber != 0 {
		out = append(out, sMuted.Render(trunc("Object Versioning is off for gs://"+hv.sec.Bucket+", so older versions aren't kept.", w)))
	}
	switch {
	case hv.aBusy:
		out = append(out, m.spin.View()+sFaint.Render(" reading audit logs…"))
	case errors.Is(hv.aErr, store.ErrAuditDenied):
		out = append(out, sMuted.Render(trunc("Access records hidden: "+hv.aErr.Error()+".", w)))
	case hv.aErr != nil:
		out = append(out, sMuted.Render(trunc("Access records unavailable: "+hv.aErr.Error(), w)))
	case len(hv.events) == 0:
		out = append(out, sFaint.Render(trunc("No access records in the last 30 days. Cloud Storage Data Access audit logs may be off for this project.", w)))
	}
	out = append(out, "")

	rows := hv.rows()
	detail := m.historyDetail(w)
	n := h - len(out) - len(detail)
	if hv.cur < hv.off {
		hv.off = hv.cur
	}
	if hv.cur >= hv.off+n {
		hv.off = hv.cur - n + 1
	}
	hv.off = max(0, min(hv.off, max(0, len(rows)-n)))
	for i := hv.off; i < len(rows) && i < hv.off+n; i++ {
		r := rows[i]
		mark := "  "
		if i == hv.cur {
			mark = sAmber.Render("▌ ")
		}
		when := sMuted.Render(pad(ago(r.t), 10))
		var line string
		switch {
		case r.ver != nil:
			name := sText.Render(fmt.Sprintf("version %d", r.num))
			if i == hv.cur {
				name = sAmberB.Render(fmt.Sprintf("version %d", r.num))
			}
			tag := ""
			if r.ver.Live {
				tag = "  " + sOK.Render("current")
			}
			who := ""
			if r.who != "" {
				who = sMuted.Render("  saved by " + r.who)
			}
			line = sAmber.Render("● ") + name + tag + who
		case r.ev.Kind == "read" && !r.ev.Denied:
			line = sFaint.Render("◦ decrypted by " + r.ev.Who)
		case r.ev.Denied:
			line = sHot.Render("× " + r.ev.Kind + " denied for " + r.ev.Who)
		case r.ev.Kind == "delete":
			line = sHot.Render("− deleted by " + r.ev.Who)
		default:
			line = sMuted.Render("● saved by " + r.ev.Who)
		}
		out = append(out, mark+when+trunc(line, w-12))
	}
	if len(rows) == 0 && !hv.vBusy {
		out = append(out, sMuted.Render("Nothing recorded yet."))
	}
	for len(out) < h-len(detail) {
		out = append(out, "")
	}
	return append(out, detail...)
}

// historyDetail shows the changes made by the version chosen with enter.
func (m *Model) historyDetail(w int) []string {
	hv := m.hist
	if hv.detail == 0 {
		return nil
	}
	num, live := 0, false
	for _, r := range hv.rows() {
		if r.ver != nil && r.ver.Generation == hv.detail {
			num, live = r.num, r.ver.Live
		}
	}
	head := sLine.Render(strings.Repeat("─", w))
	title := sWhiteB.Render(fmt.Sprintf("Version %d", num))
	prev := hv.previous(hv.detail)
	cur, ok1 := hv.vals[hv.detail]
	before, ok2 := hv.vals[prev]
	if !ok1 || (prev != 0 && !ok2) {
		return []string{head, title, m.spin.View() + sMuted.Render(" Decrypting to compare…")}
	}
	if prev == 0 {
		n := len(dotenv.Parse(cur.Bytes()).Entries())
		lines := []string{head, title + sMuted.Render("  ·  first version · "+plural(n, "key"))}
		if !live {
			lines = append(lines, hints([][2]string{{"r", "restore this version"}}))
		}
		return lines
	}
	added, removed, changed := changes(before, cur)
	var parts []string
	list := func(sym string, keys []string, st func(...string) string) {
		for _, k := range keys {
			parts = append(parts, st(sym+" "+k))
		}
	}
	list("+", added, sOK.Render)
	list("−", removed, sHot.Render)
	list("~", changed, sAmber.Render)
	what := "changes from the version before"
	if len(parts) == 0 {
		parts = []string{sMuted.Render("no key changed")}
	}
	lines := []string{head, title + sMuted.Render("  ·  "+what)}
	line := ""
	for _, p := range parts {
		if line != "" && lipgloss.Width(line)+lipgloss.Width(p)+3 > w {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "   "
		}
		line += p
	}
	if line != "" && len(lines) < 6 {
		lines = append(lines, line)
	}
	if !live {
		lines = append(lines, hints([][2]string{{"r", "restore this version"}}))
	}
	return lines
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
