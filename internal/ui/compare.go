package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dumbprism19/secdrop/internal/secret"
	"github.com/dumbprism19/secdrop/internal/store"
)

// compareView lines up the shown secret against the same service in another
// environment, using fingerprints only. Plaintext is never drawn.
type compareView struct {
	others []store.Secret
	idx    int
	cur    int
}

type cmpRow struct {
	key         string
	left, right string // fingerprints, "" when absent
	status      int
}

const (
	cmpMissingRight = iota
	cmpSameWarn
	cmpMissingLeft
	cmpDiffers
	cmpSameShared
)

func (m *Model) startCompare() tea.Cmd {
	o := m.shownDoc()
	if o == nil {
		return nil
	}
	var others []store.Secret
	for _, s := range m.secrets {
		if s.Service == o.sec.Service && s.Object != o.sec.Object {
			others = append(others, s)
		}
	}
	if len(others) == 0 {
		return m.flash("No other environment of "+o.sec.Service+" to compare with", false)
	}
	// Prefer comparing against prod, or dev when looking at prod.
	idx := 0
	for i, s := range others {
		if (o.sec.Env != "prod" && s.Env == "prod") || (o.sec.Env == "prod" && s.Env == "dev") {
			idx = i
		}
	}
	m.cmp = &compareView{others: others, idx: idx}
	return m.loadCompare()
}

func (m *Model) loadCompare() tea.Cmd {
	other := m.cmp.others[m.cmp.idx]
	if d := m.docs[other.Object]; d != nil && !d.stale {
		return nil
	}
	return m.open(other, false)
}

func (m *Model) compareKey(k tea.KeyMsg) tea.Cmd {
	c := m.cmp
	rows := m.compareRows()
	switch k.String() {
	case "esc", "d", "left":
		m.cmp = nil
	case "tab", "]":
		c.idx, c.cur = (c.idx+1)%len(c.others), 0
		return m.loadCompare()
	case "up", "k":
		c.cur = max(0, c.cur-1)
	case "down", "j":
		c.cur = min(len(rows)-1, c.cur+1)
	case "enter":
		if c.cur < len(rows) {
			if cmd, ok := m.copyAcross(rows[c.cur]); ok {
				return cmd
			}
			key := rows[c.cur].key
			m.cmp = nil
			m.keyIn.SetValue("")
			for i, e := range m.panelEntries() {
				if e.Key == key {
					m.kcur = i
				}
			}
		}
	}
	return nil
}

func (m *Model) compareRows() []cmpRow {
	a := m.shownDoc()
	b := m.docs[m.cmp.others[m.cmp.idx].Object]
	if a == nil || b == nil {
		return nil
	}
	shared := map[string]bool{}
	for _, s := range m.cfg.Shared {
		shared[s] = true
	}
	rows := map[string]*cmpRow{}
	for _, e := range a.entries(false) {
		rows[e.Key] = &cmpRow{key: e.Key, left: e.Value.Fingerprint()}
	}
	for _, e := range b.entries(false) {
		r := rows[e.Key]
		if r == nil {
			r = &cmpRow{key: e.Key}
			rows[e.Key] = r
		}
		r.right = e.Value.Fingerprint()
	}
	out := make([]cmpRow, 0, len(rows))
	for _, r := range rows {
		switch {
		case r.right == "":
			r.status = cmpMissingRight
		case r.left == "":
			r.status = cmpMissingLeft
		case r.left == r.right && shared[r.key]:
			r.status = cmpSameShared
		case r.left == r.right:
			r.status = cmpSameWarn
		default:
			r.status = cmpDiffers
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].status != out[j].status {
			return out[i].status < out[j].status
		}
		return out[i].key < out[j].key
	})
	return out
}

func (m *Model) viewCompare(w, h int) []string {
	a := m.shownDoc()
	other := m.cmp.others[m.cmp.idx]
	lines := []string{
		sWhiteB.Render(a.sec.Service) + sMuted.Render("  ") + envStyle(a.sec.Env).Render(orDash(a.sec.Env)) +
			sMuted.Render("  ⇄  ") + envStyle(other.Env).Render(orDash(other.Env)),
		sFaint.Render("Values are compared by fingerprint. Nothing is shown in plain text."),
		"",
	}
	if m.docs[other.Object] == nil {
		if err := m.openErr[other.Object]; err != nil {
			return append(lines, sHot.Render("Couldn't decrypt "+label(other)+": "+trunc(err.Error(), w-30)))
		}
		return append(lines, m.spin.View()+sMuted.Render(" Decrypting "+label(other)+"…"))
	}
	rows := m.compareRows()
	counts := map[int]int{}
	for _, r := range rows {
		counts[r.status]++
	}
	var sum []string
	add := func(n int, text string, st func(...string) string) {
		if n > 0 {
			sum = append(sum, st(fmt.Sprintf("%d %s", n, text)))
		}
	}
	add(counts[cmpMissingRight], "missing in "+orDash(other.Env), sHot.Render)
	add(counts[cmpMissingLeft], "only in "+orDash(other.Env), sAmber.Render)
	add(counts[cmpSameWarn], "identical", sOK.Render)
	add(counts[cmpDiffers], "differ", sMuted.Render)
	add(counts[cmpSameShared], "shared", sMuted.Render)
	lines = append(lines, strings.Join(sum, sFaint.Render("  ·  ")), "")

	kw := min(32, max(14, w/3))
	fw := 12
	lines = append(lines, sFaint.Render("  "+pad("KEY", kw)+pad(strings.ToUpper(orDash(a.sec.Env)), fw)+pad(strings.ToUpper(orDash(other.Env)), fw)))
	body := h - len(lines) - 1
	c := m.cmp
	start := max(0, min(c.cur-body/2, len(rows)-body))
	for i := start; i < len(rows) && i < start+body; i++ {
		r := rows[i]
		var st string
		switch r.status {
		case cmpMissingRight:
			st = sHot.Render("missing in " + orDash(other.Env))
		case cmpMissingLeft:
			st = sAmber.Render("only in " + orDash(other.Env))
		case cmpSameWarn:
			st = sOK.Render("identical in both")
		case cmpSameShared:
			st = sMuted.Render("identical · shared")
		default:
			st = sFaint.Render("differs")
		}
		fp := func(s string) string {
			if s == "" {
				return sFaint.Render(pad("—", fw))
			}
			return sMuted.Render(pad(s, fw))
		}
		mark, key := "  ", sText.Render(pad(trunc(r.key, kw-1), kw))
		if i == c.cur {
			mark, key = sAmber.Render("▌ "), sAmberB.Render(pad(trunc(r.key, kw-1), kw))
		}
		lines = append(lines, mark+key+fp(r.left)+fp(r.right)+st)
	}
	return lines
}

// copyAcross offers to copy a key into the environment that's missing it,
// through the usual review step. It reports false for rows it doesn't apply to.
func (m *Model) copyAcross(r cmpRow) (tea.Cmd, bool) {
	here := m.shownDoc()
	there := m.docs[m.cmp.others[m.cmp.idx].Object]
	if here == nil || there == nil {
		return nil, false
	}
	from, to := here, there
	switch r.status {
	case cmpMissingRight:
	case cmpMissingLeft:
		from, to = there, here
	default:
		return nil, false
	}
	if !to.env {
		return m.flash(label(to.sec)+" isn't a KEY=value file, so keys can't be added to it", true), true
	}
	var val secret.Value
	for _, e := range from.entries(false) {
		if e.Key == r.key {
			val = e.Value
		}
	}
	doc := to.doc.Clone()
	doc.Set(r.key, val)
	m.modal = &modal{kind: mConfirm, o: to, oneStep: true, key: r.key, data: doc.Bytes(),
		change: "Copies " + r.key + " from " + orDash(from.sec.Env) + " into " + orDash(to.sec.Env),
		note:   "Copied " + r.key + " into " + label(to.sec)}
	return nil, true
}

// copyHint names what enter does on the highlighted compare row.
func (m *Model) copyHint() string {
	rows := m.compareRows()
	if m.cmp == nil || m.cmp.cur >= len(rows) {
		return ""
	}
	other := orDash(m.cmp.others[m.cmp.idx].Env)
	here := ""
	if o := m.shownDoc(); o != nil {
		here = orDash(o.sec.Env)
	}
	switch rows[m.cmp.cur].status {
	case cmpMissingRight:
		return "copy to " + other
	case cmpMissingLeft:
		return "copy to " + here
	}
	return ""
}
