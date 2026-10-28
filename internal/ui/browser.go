package ui

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dumbprism19/secdrop/internal/dotenv"
	"github.com/dumbprism19/secdrop/internal/secret"
	"github.com/dumbprism19/secdrop/internal/store"
)

// The browser has two levels: a list of services, and a panel for one
// service that shows its keys, one environment at a time.

type service struct {
	name    string
	secrets []store.Secret // ordered dev → prod
}

func (sv service) envIndex(env string) int {
	for i, s := range sv.secrets {
		if s.Env == env {
			return i
		}
	}
	return -1
}

// allServices groups every secret by service.
func (m *Model) allServices() []service {
	var out []service
	idx := map[string]int{}
	for _, s := range m.secrets {
		i, ok := idx[s.Service]
		if !ok {
			i = len(out)
			idx[s.Service] = i
			out = append(out, service{name: s.Service})
		}
		out[i].secrets = append(out[i].secrets, s)
	}
	return out
}

// panelEntries is the panel's key list after the "/" filter.
func (m *Model) panelEntries() []dotenv.Entry {
	o := m.shownDoc()
	if o == nil {
		return nil
	}
	words := strings.Fields(strings.ToLower(m.keyIn.Value()))
	if len(words) == 0 {
		return o.entries(m.sortAZ)
	}
	var out []dotenv.Entry
	for _, e := range o.entries(m.sortAZ) {
		if matchAll(e.Key, words) {
			out = append(out, e)
		}
	}
	return out
}

func matchAll(s string, words []string) bool {
	s = strings.ToLower(s)
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

func newFilterInput(placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt = "/ "
	in.PromptStyle = sAmber
	in.TextStyle = sWhiteB
	in.Placeholder = placeholder
	in.PlaceholderStyle = sFaint
	in.Cursor.Style = sAmber
	return in
}

// filterKey handles typing in a "/" filter. Arrows still move the list so
// you can pick a result without leaving the box.
func (m *Model) filterKey(k tea.KeyMsg) tea.Cmd {
	in := &m.homeIn
	if m.typing == 2 {
		in = &m.keyIn
	}
	switch k.String() {
	case "esc":
		in.SetValue("")
		in.Blur()
		m.typing = 0
		m.cur, m.off, m.kcur, m.koff = 0, 0, 0, 0
		return nil
	case "up", "down", "pgup", "pgdown":
		if m.typing == 1 {
			return m.homeKey(k)
		}
		return m.panelKey(k.String())
	case "enter":
		in.Blur()
		wasHome := m.typing == 1
		m.typing = 0
		if strings.TrimSpace(in.Value()) == "" {
			in.SetValue("")
		}
		if wasHome {
			if sv := m.selectedService(); sv != nil {
				return m.openService(sv.name, "")
			}
		}
		return nil
	}
	prev := in.Value()
	var cmd tea.Cmd
	*in, cmd = in.Update(k)
	if in.Value() != prev {
		m.cur, m.off, m.kcur, m.koff = 0, 0, 0, 0
	}
	return cmd
}

func (m *Model) startFilter() tea.Cmd {
	if m.panel {
		if m.shownDoc() == nil || m.cmp != nil {
			return nil
		}
		m.typing = 2
		return m.keyIn.Focus()
	}
	m.typing = 1
	return m.homeIn.Focus()
}

func (m *Model) selectedService() *service {
	ss := m.services()
	if m.cur < 0 || m.cur >= len(ss) {
		return nil
	}
	return &ss[m.cur]
}

func (m *Model) selectService(name string) bool {
	for i, sv := range m.services() {
		if sv.name == name {
			m.cur = i
			return true
		}
	}
	return false
}

func (m *Model) clampCursor() { m.cur = max(0, min(m.cur, len(m.services())-1)) }

func (m *Model) panelService() *service {
	for _, sv := range m.allServices() {
		if sv.name == m.svc {
			return &sv
		}
	}
	return nil
}

func (m *Model) panelSecret() *store.Secret {
	sv := m.panelService()
	if sv == nil || m.penv < 0 || m.penv >= len(sv.secrets) {
		return nil
	}
	return &sv.secrets[m.penv]
}

// openService opens the panel. env picks the environment; empty means the
// one selected by the tab bar, or the first.
func (m *Model) openService(name, env string) tea.Cmd {
	m.svc, m.panel, m.cmp = name, true, nil
	sv := m.panelService()
	if sv == nil {
		m.panel = false
		return nil
	}
	if env == "" && m.envIdx > 0 {
		env = m.envs[m.envIdx-1]
	}
	m.penv = max(0, sv.envIndex(env))
	return m.showEnv()
}

// showEnv shows the panel's current environment, decrypting it if needed.
func (m *Model) showEnv() tea.Cmd {
	s := m.panelSecret()
	if s == nil {
		return nil
	}
	m.show(s.Object)
	if o := m.docs[s.Object]; o != nil && !o.stale {
		m.applyPendingKey()
		return nil
	}
	return m.open(*s, true)
}

func (m *Model) closePanel() {
	m.panel, m.cmp, m.pendingKey, m.hist = false, nil, "", nil
	m.keyIn.SetValue("")
	m.keyIn.Blur()
	if m.typing == 2 {
		m.typing = 0
	}
	m.revealed, m.fx = map[string]int{}, map[string]revealFx{}
	m.selectService(m.svc)
}

// show puts a decrypted secret in the panel and hides any revealed values.
func (m *Model) show(obj string) {
	if m.shown != obj {
		m.revealed, m.fx = map[string]int{}, map[string]revealFx{}
		m.kcur, m.koff = 0, 0
		m.cmp, m.hist = nil, nil
	}
	m.shown = obj
}

func (m *Model) shownDoc() *opened { return m.docs[m.shown] }

func (m *Model) currentEntry() (dotenv.Entry, bool) {
	o := m.shownDoc()
	if o == nil {
		return dotenv.Entry{}, false
	}
	es := m.panelEntries()
	if m.kcur < 0 || m.kcur >= len(es) {
		return dotenv.Entry{}, false
	}
	return es[m.kcur], true
}

func (m *Model) applyPendingKey() {
	if m.pendingKey == "" || m.shownDoc() == nil {
		return
	}
	m.keyIn.SetValue("")
	for i, e := range m.panelEntries() {
		if e.Key == m.pendingKey {
			m.kcur = i
		}
	}
	m.pendingKey = ""
}

// ---- data events ----

func (m *Model) useBucket(b string) tea.Cmd {
	m.bk.stop()
	m.scr = scrBrowser
	if b != m.bucket {
		m.bucket, m.secrets, m.envs, m.envIdx = b, nil, nil, 0
		m.docs, m.openErr, m.changed = map[string]*opened{}, map[string]error{}, map[string]bool{}
		m.shown, m.cur, m.off, m.kcur, m.koff, m.cmp, m.panel = "", 0, 0, 0, 0, nil, false
		m.revealed, m.fx = map[string]int{}, map[string]revealFx{}
	}
	return tea.Batch(m.list(b, false), poll(), m.loadPods())
}

func (m *Model) handleList(msg listMsg) tea.Cmd {
	if msg.bucket != m.bucket {
		return nil
	}
	if !msg.poll {
		m.listing = false
	}
	if msg.err != nil {
		if !msg.poll {
			m.listErr = msg.err
		}
		return nil
	}
	if !msg.poll {
		m.cfg.UseBucket(m.bucket)
		_ = m.cfg.Save()
		m.secrets = msg.secrets
		m.rebuildEnvs()
		m.clampCursor()
		return nil
	}

	// A poll: work out what changed since the last look.
	old := map[string]int64{}
	for _, s := range m.secrets {
		old[s.Object] = s.Generation
	}
	var names []string
	for _, s := range msg.secrets {
		if g, ok := old[s.Object]; ok && g == s.Generation {
			continue
		}
		m.changed[s.Object] = true
		if o := m.docs[s.Object]; o != nil && o.sec.Generation != s.Generation {
			o.stale = true
		}
		names = append(names, label(s))
	}
	var sel string
	if sv := m.selectedService(); sv != nil {
		sel = sv.name
	}
	m.secrets = msg.secrets
	m.rebuildEnvs()
	m.selectService(sel)
	m.clampCursor()
	switch len(names) {
	case 0:
		return nil
	case 1:
		return m.flash(names[0]+" changed in the bucket", false)
	}
	return m.flash(fmt.Sprintf("%d secrets changed in the bucket", len(names)), false)
}

func (m *Model) handleOpen(msg openMsg) tea.Cmd {
	if msg.show && m.loading == msg.sec.Object {
		m.loading = ""
	}
	if msg.err != nil {
		m.openErr[msg.sec.Object] = msg.err
		return m.failure(msg.err)
	}
	doc := dotenv.Parse(msg.val.Bytes())
	if prev := m.docs[msg.sec.Object]; prev != nil {
		prev.raw.Wipe()
	}
	m.docs[msg.sec.Object] = &opened{sec: msg.sec, raw: msg.val, doc: doc, env: doc.IsEnv()}
	delete(m.changed, msg.sec.Object)
	delete(m.openErr, msg.sec.Object)
	m.updateSecret(msg.sec)
	if msg.show && msg.sec.Object == m.shown {
		m.applyPendingKey()
	}
	return nil
}

func (m *Model) handleSave(msg saveMsg) tea.Cmd {
	m.modal = nil
	if msg.err != nil {
		if errors.Is(msg.err, store.ErrConflict) {
			return tea.Batch(m.failure(msg.err), m.open(msg.sec, true))
		}
		return m.failure(msg.err)
	}
	doc := dotenv.Parse(msg.data)
	if o := m.docs[msg.sec.Object]; o != nil {
		o.raw.Wipe()
	}
	m.docs[msg.sec.Object] = &opened{sec: msg.sec, raw: secret.New(msg.data), doc: doc, env: doc.IsEnv()}
	for i := range msg.data {
		msg.data[i] = 0
	}
	m.updateSecret(msg.sec)
	m.revealed, m.fx = map[string]int{}, map[string]revealFx{}
	m.hist = nil
	return m.flash(msg.note, false)
}

func (m *Model) updateSecret(s store.Secret) {
	for i := range m.secrets {
		if m.secrets[i].Object == s.Object {
			m.secrets[i] = s
		}
	}
}

func (m *Model) rebuildEnvs() {
	cur := ""
	if m.envIdx > 0 && m.envIdx <= len(m.envs) {
		cur = m.envs[m.envIdx-1]
	}
	seen := map[string]bool{}
	m.envs = nil
	for _, s := range m.secrets {
		if s.Env != "" && !seen[s.Env] {
			seen[s.Env] = true
			m.envs = append(m.envs, s.Env)
		}
	}
	sort.Slice(m.envs, func(i, j int) bool { return store.EnvOrder[m.envs[i]] < store.EnvOrder[m.envs[j]] })
	m.envIdx = 0
	for i, e := range m.envs {
		if e == cur {
			m.envIdx = i + 1
		}
	}
}

// ---- keys ----

func (m *Model) browserKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if key == "ctrl+k" || key == "ctrl+p" {
		m.typing = 0
		return m.openPalette("")
	}
	if m.typing != 0 {
		return m.filterKey(k)
	}
	if !m.panel {
		return m.homeKey(k)
	}
	switch key {
	case "/":
		return m.startFilter()
	case "q":
		m.quitting = true
		return tea.Quit
	case "?":
		m.help = true
		return nil
	case "b":
		return m.openPicker()
	case "L":
		m.scr = scrAuth
		return m.login()
	case "R":
		cmds := []tea.Cmd{m.list(m.bucket, false)}
		if s := m.panelSecret(); m.panel && s != nil {
			cmds = append(cmds, m.open(*s, true))
		}
		return tea.Batch(cmds...)
	}
	if m.hist != nil {
		return m.historyKey(key)
	}
	if m.cmp != nil {
		return m.compareKey(k)
	}
	return m.panelKey(key)
}

func (m *Model) panelKey(key string) tea.Cmd {
	sv := m.panelService()
	switch key {
	case "esc", "left", "backspace":
		if key == "esc" && m.keyIn.Value() != "" {
			m.keyIn.SetValue("")
			m.kcur, m.koff = 0, 0
			return nil
		}
		m.closePanel()
		return nil
	case "tab", "shift+tab", "]", "[":
		if sv == nil || len(sv.secrets) < 2 {
			return nil
		}
		if key == "tab" || key == "]" {
			m.penv = (m.penv + 1) % len(sv.secrets)
		} else {
			m.penv = (m.penv + len(sv.secrets) - 1) % len(sv.secrets)
		}
		return m.showEnv()
	}

	o := m.shownDoc()
	if o == nil {
		if key == "enter" {
			return m.showEnv()
		}
		return nil
	}
	es := m.panelEntries()
	e, ok := m.currentEntry()
	switch key {
	case "up", "k":
		m.kcur = max(0, m.kcur-1)
	case "down", "j":
		m.kcur = min(len(es)-1, m.kcur+1)
	case "pgup":
		m.kcur = max(0, m.kcur-10)
	case "pgdown":
		m.kcur = min(len(es)-1, m.kcur+10)
	case "home", "g":
		m.kcur = 0
	case "end", "G":
		m.kcur = len(es) - 1
	case "enter":
		if !ok {
			return nil
		}
		if _, on := m.revealed[e.Key]; on {
			return m.hide(e.Key)
		}
		return m.reveal([]string{e.Key})
	case "v", "V", "A":
		// Reveal or hide every key of this environment at once (all of
		// them, not just the ones a "/" filter is showing).
		es := o.entries(m.sortAZ)
		if len(m.revealed) > 0 {
			var cmds []tea.Cmd
			for k := range m.revealed {
				cmds = append(cmds, m.hide(k))
			}
			return tea.Batch(cmds...)
		}
		keys := make([]string, 0, len(es))
		for _, x := range es {
			keys = append(keys, x.Key)
		}
		return m.reveal(keys)
	case "c", "C":
		all := len(o.entries(false))
		return m.copy(o.raw.Reveal(), fmt.Sprintf("Copied the whole %s env (%d keys)", label(o.sec), all))
	case "y":
		if ok {
			return m.copy(e.Value.Reveal(), "Copied "+e.Key)
		}
	case "e":
		if ok {
			m.modal = newEditModal(o, e)
			return textinput.Blink
		}
	case "a", "n":
		if o.env {
			m.modal = newAddModal(o)
			return textinput.Blink
		}
	case "x", "delete":
		if ok && o.env {
			m.modal = newDeleteModal(o, e.Key)
		}
	case "d":
		return m.startCompare()
	case "h":
		return m.openHistory()
	}
	return nil
}

// ---- views ----

func (m *Model) viewBrowser() string {
	w, h := m.w, m.h
	right := sMuted.Render("gs://"+m.bucket) + sFaint.Render("  ·  ") + sMuted.Render(orWord(m.ident.Email, "signed in"))
	lines := []string{" " + spread(miniLogo(), trunc(right, w-20), w-2), ""}

	var body []string
	bh := h - 4 // header, blank, blank, footer
	if m.panel {
		body = m.viewPanel(w-2, bh)
	} else {
		body = m.viewHome(w-2, bh)
	}
	for i := 0; i < bh; i++ {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		lines = append(lines, " "+fit(l, w-2))
	}
	lines = append(lines, "", " "+m.footer(w-2))
	return strings.Join(lines, "\n")
}

func (m *Model) viewPanel(w, h int) []string {
	sv := m.panelService()
	s := m.panelSecret()
	if sv == nil || s == nil {
		return nil
	}
	o := m.docs[s.Object]

	back := sAmber.Render("← ") + sWhiteB.Render(sv.name)
	out := []string{spread(back, sKey.Render("esc")+sMuted.Render(" back"), w), ""}

	var tabs []string
	for i, x := range sv.secrets {
		label := orDash(x.Env)
		if i == m.penv {
			st := sTabOn
			if x.Env == "prod" {
				st = st.Background(cHot)
			}
			tabs = append(tabs, st.Render(label))
		} else {
			tabs = append(tabs, envStyle(x.Env).Padding(0, 1).Render(label))
		}
	}
	out = append(out, strings.Join(tabs, " "), "")

	meta := "updated " + ago(s.Updated)
	if o != nil {
		if o.env {
			meta += " · " + plural(len(o.entries(false)), "key")
		} else {
			meta += " · single value"
		}
	}
	meta += " · " + s.Ref()
	if o != nil && o.stale {
		out = append(out, sHot.Render(trunc("Changed in the bucket since you opened it. Press R to reload.", w)))
	} else {
		out = append(out, sFaint.Render(trunc(meta, w)))
	}
	if pods := m.podSection(sv.name, w); len(pods) > 0 {
		out = append(out, "")
		out = append(out, pods...)
	}
	out = append(out, "")

	switch {
	case m.loading == s.Object:
		return append(out, m.spin.View()+sMuted.Render(" Decrypting with Cloud KMS…"))
	case o == nil && m.openErr[s.Object] != nil:
		return append(out, sHot.Render("Couldn't decrypt this secret."), sText.Render(trunc(m.openErr[s.Object].Error(), w)), "",
			sMuted.Render("You need roles/cloudkms.cryptoKeyDecrypter on the Berglas key and read access to the object."),
			"", hints([][2]string{{"enter", "try again"}, {"esc", "back"}}))
	case o == nil:
		return append(out, hints([][2]string{{"enter", "decrypt"}}))
	}

	// Action bar: the things people do with a whole env.
	revealLabel := "reveal all"
	if len(m.revealed) > 0 {
		revealLabel = "hide all"
	}
	actions := []string{action("v", revealLabel), action("c", "copy env"), action("d", "compare"), action("h", "history")}
	out = append(out, strings.Join(actions, "  "), "")

	if m.hist != nil {
		return append(out, m.viewHistory(w, h-len(out))...)
	}
	if m.cmp != nil {
		return append(out, m.viewCompare(w, h-len(out))...)
	}

	es := m.panelEntries()
	if !o.env {
		out = append(out, sFaint.Render("Not a KEY=value file, so it's shown as one value."), "")
	}
	if m.typing == 2 || m.keyIn.Value() != "" {
		m.keyIn.Width = w - 20
		count := sMuted.Render(fmt.Sprintf("%d of %d keys", len(es), len(o.entries(false))))
		out = append(out, spread(m.keyIn.View(), count, w), "")
		if len(es) == 0 {
			out = append(out, sMuted.Render("  No key matches “"+m.keyIn.Value()+"”. Press esc to clear."))
		}
	}
	kw := 8
	for _, e := range es {
		kw = max(kw, lipgloss.Width(e.Key)+3)
	}
	kw = min(kw, w*40/100)
	out = append(out, sFaint.Render("  "+pad("KEY", kw)+"VALUE"), sLine.Render(strings.Repeat("─", w)))

	n := h - len(out)
	more := len(es) > n
	if more {
		n-- // keep a row for the "x–y of z" line instead of covering a key
	}
	if m.kcur < m.koff {
		m.koff = m.kcur
	}
	if m.kcur >= m.koff+n {
		m.koff = m.kcur - n + 1
	}
	m.koff = max(0, min(m.koff, max(0, len(es)-n)))
	vw := w - kw - 2
	for i := m.koff; i < len(es) && i < m.koff+n; i++ {
		e := es[i]
		mark, key := "  ", highlight(trunc(e.Key, kw-2), m.keyIn.Value(), sText)
		if i == m.kcur {
			mark, key = sAmber.Render("▌ "), highlight(trunc(e.Key, kw-2), m.keyIn.Value(), sAmberB)
		}
		out = append(out, mark+pad(key, kw)+m.renderValue(e, vw))
	}
	if more {
		out = append(out, sFaint.Render(fmt.Sprintf("  %d–%d of %d", m.koff+1, min(len(es), m.koff+n), len(es))))
	}
	return out
}

func (m *Model) footer(w int) string {
	if m.toast != "" {
		dot := sAmber
		if m.toastHot {
			dot = sHot
		}
		return dot.Render("● ") + sText.Render(trunc(m.toast, w-2))
	}
	var h [][2]string
	switch {
	case m.typing != 0:
		h = [][2]string{{"type", "to filter"}, {"↑↓", "move"}, {"enter", "done"}, {"esc", "clear"}}
	case m.panel && m.hist != nil:
		h = [][2]string{{"↑↓", "move"}, {"enter", "what changed"}, {"r", "restore"}, {"esc", "back"}}
	case m.panel && m.cmp != nil:
		enter := "go to key"
		if c := m.copyHint(); c != "" {
			enter = c
		}
		h = [][2]string{{"↑↓", "move"}, {"tab", "other env"}, {"enter", enter}, {"esc", "close"}}
	case m.panel && m.shownDoc() != nil:
		h = [][2]string{{"enter", "reveal"}, {"y", "copy"}, {"e", "edit"}, {"/", "search"}, {"tab", "environment"}, {"esc", "back"}, {paletteKey, "more"}}
	case m.panel:
		h = [][2]string{{"enter", "decrypt"}, {"tab", "environment"}, {"esc", "back"}}
	default:
		h = [][2]string{{"type", "to search"}, {"↑↓", "move"}, {"enter", "open"}, {"tab", "environment"}, {paletteKey, "everything"}, {"ctrl+c", "quit"}}
	}
	return trunc(hints(h), w)
}

var sKeycap = lipgloss.NewStyle().Foreground(cAmber).Background(cSelBg).Bold(true).Padding(0, 1)

func action(key, label string) string { return sKeycap.Render(key) + " " + sText.Render(label) }

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// ---- reveal animation ----

// revealFx animates one value: the mask shatters into fragments and the
// value assembles out of them, or the reverse when hiding.
type revealFx struct {
	start time.Time
	show  bool
}

const (
	revealDur = 560 * time.Millisecond
	hideDur   = 340 * time.Millisecond
	stagger   = 22 * time.Millisecond
	maskWidth = 12
)

func (m *Model) reveal(keys []string) tea.Cmd {
	m.revealSeq++
	id, obj, now := m.revealSeq, m.shown, time.Now()
	var cmds []tea.Cmd
	for i, k := range keys {
		k := k
		m.revealed[k] = id
		if !m.opts.NoMotion {
			m.fx[k] = revealFx{start: now.Add(time.Duration(min(i, 30)) * stagger), show: true}
		}
		cmds = append(cmds, tea.Tick(revealFor, func(time.Time) tea.Msg { return hideMsg{obj, k, id} }))
	}
	return tea.Batch(append(cmds, m.tick())...)
}

func (m *Model) hide(key string) tea.Cmd {
	delete(m.revealed, key)
	if m.opts.NoMotion {
		delete(m.fx, key)
		return nil
	}
	m.fx[key] = revealFx{start: time.Now(), show: false}
	return m.tick()
}

// stepFx drops finished animations and reports whether any are running.
func (m *Model) stepFx() bool {
	busy := false
	now := time.Now()
	for k, f := range m.fx {
		d := hideDur
		if f.show {
			d = revealDur
		}
		if now.Sub(f.start) >= d {
			delete(m.fx, k)
		} else {
			busy = true
		}
	}
	return busy
}

var fragments = []rune("░▒▓▖▗▘▝▚▞·:'`,.")

func (m *Model) renderValue(e dotenv.Entry, w int) string {
	_, shown := m.revealed[e.Key]
	f, animating := m.fx[e.Key]
	if !animating {
		switch {
		case !shown:
			return sFaint.Render(strings.Repeat("•", maskWidth))
		case e.Value.IsZero():
			return sFaint.Render("(empty)")
		}
		return sHi.Render(trunc(oneLine(e.Value.Reveal()), w))
	}

	el := time.Since(f.start)
	if el < 0 {
		el = 0
	}
	p := float64(el) / float64(revealDur)
	if !f.show {
		p = 1 - float64(el)/float64(hideDur)
	}
	p = max(0, min(1, p))
	return shatter([]rune(oneLine(e.Value.Reveal())), w, p, int(el/(40*time.Millisecond)), e.Key)
}

// shatter draws one frame between the mask (p=0) and the value (p=1). For
// the first third the dots break into fragments and fall away; after that
// each character settles in, roughly left to right.
func shatter(val []rune, w int, p float64, frame int, seed string) string {
	if len(val) > w {
		val = val[:w]
	}
	n := max(len(val), maskWidth)
	n = min(n, w)
	var b strings.Builder
	for i := 0; i < n; i++ {
		r := noise(seed, i, 0)
		var ch rune
		var st lipgloss.Style
		switch {
		case p < 0.33:
			q := p / 0.33
			switch {
			case i >= maskWidth:
				ch, st = ' ', sFaint
			case r < q*0.65:
				ch, st = fragments[int(noise(seed, i, frame)*float64(len(fragments)))], sAmberLo()
			case r < q:
				ch, st = ' ', sFaint
			default:
				ch, st = '•', sFaint
			}
		default:
			q := (p - 0.33) / 0.67
			settle := 0.55*float64(i)/float64(n) + 0.45*r
			switch {
			case q >= settle && i < len(val):
				ch, st = val[i], sHi
			case q >= settle:
				ch, st = ' ', sFaint
			case noise(seed, i, frame+7) < 0.55:
				ch = fragments[int(noise(seed, i, frame+3)*float64(len(fragments)))]
				st = sAmber
				if noise(seed, i, frame+11) < 0.5 {
					st = sAmberLo()
				}
			default:
				ch, st = ' ', sFaint
			}
		}
		b.WriteString(st.Render(string(ch)))
	}
	return b.String()
}

// noise is a cheap deterministic hash in [0, 1).
func noise(seed string, i, j int) float64 {
	h := uint32(2166136261)
	for _, c := range seed {
		h = (h ^ uint32(c)) * 16777619
	}
	h = (h ^ uint32(i*7919)) * 16777619
	h = (h ^ uint32(j*104729)) * 16777619
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15
	return float64(h%10000) / 10000
}

// highlight draws s in st, underlining the first match of query.
func highlight(s, query string, st lipgloss.Style) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return st.Render(s)
	}
	if f := strings.Fields(q); len(f) > 0 {
		q = f[0]
	}
	i := strings.Index(strings.ToLower(s), q)
	if i < 0 {
		return st.Render(s)
	}
	return st.Render(s[:i]) + st.Underline(true).Foreground(cAmberHi).Render(s[i:i+len(q)]) + st.Render(s[i+len(q):])
}
