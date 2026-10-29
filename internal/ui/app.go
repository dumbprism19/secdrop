// Package ui is the secdrop terminal interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dumbprism19/secdrop/internal/clip"
	"github.com/dumbprism19/secdrop/internal/config"
	"github.com/dumbprism19/secdrop/internal/dotenv"
	"github.com/dumbprism19/secdrop/internal/gcp"
	"github.com/dumbprism19/secdrop/internal/kube"
	"github.com/dumbprism19/secdrop/internal/secret"
	"github.com/dumbprism19/secdrop/internal/store"
)

type Options struct {
	Bucket   string // overrides the remembered bucket
	Pick     bool   // always show the bucket picker
	NoMotion bool
	Version  string

	// Set by the launcher's update check.
	UpdatedFrom     string // this run follows an automatic update from that version
	UpdateAvailable string // a newer release (e.g. v0.4.0) wasn't installed
	UpdateErr       error  // why installing it failed, if it was tried
}

type screen int

const (
	scrSplash screen = iota
	scrAuth
	scrBuckets
	scrBrowser
)

type authState int

const (
	authChecking authState = iota
	authOK
	authFailed
)

const (
	revealFor    = 20 * time.Second
	clipboardFor = 30 * time.Second
	pollEvery    = 60 * time.Second
	podsMaxAge   = 30 * time.Second
)

// opened is a decrypted secret held in memory for this session only.
type opened struct {
	sec   store.Secret
	raw   secret.Value
	doc   *dotenv.Doc
	env   bool // parsed as KEY=value lines
	stale bool // changed in the bucket since we decrypted it
}

func (o *opened) entries(sortAZ bool) []dotenv.Entry {
	if !o.env {
		return []dotenv.Entry{{Key: "value", Value: o.raw}}
	}
	es := o.doc.Entries()
	if sortAZ {
		sort.SliceStable(es, func(i, j int) bool { return strings.ToLower(es[i].Key) < strings.ToLower(es[j].Key) })
	}
	return es
}

type Model struct {
	ctx  context.Context
	opts Options
	cfg  config.Config
	w, h int
	scr  screen
	spin spinner.Model

	// animation
	ticking  bool
	splashT0 time.Time

	// sign-in
	auth       authState
	connecting bool
	ident      gcp.Identity
	authErr    error

	st *store.Store

	// bucket picker
	bk bucketPicker

	// browser
	bucket      string
	secrets     []store.Secret
	listing     bool
	listErr     error
	envs        []string
	envIdx      int    // 0 = all
	panel       bool   // the service panel is open
	svc         string // service shown in the panel
	penv        int    // environment index within that service
	pendingKey  string // key to highlight once a decrypt finishes
	fx          map[string]revealFx
	hist        *historyView
	wsReturn    string          // workspace to return to when leaving the open service
	updatedSeen bool            // the "Updated to …" notice has been seen
	homeIn      textinput.Model // "/" filter for services
	keyIn       textinput.Model // "/" filter for keys in the panel
	typing      int             // 0 none, 1 home filter, 2 key filter
	cur, off    int
	kcur        int
	koff        int
	sortAZ      bool
	docs        map[string]*opened // by object name
	shown       string             // object shown in the keys pane
	loading     string             // object being decrypted
	openErr     map[string]error
	revealed    map[string]int // key → reveal id, for the shown object
	revealSeq   int
	changed     map[string]bool
	cmp         *compareView

	toast    string
	toastHot bool
	toastSeq int
	help     bool
	modal    *modal
	pal      *palette
	pods     kube.Snapshot
	podsErr  error
	podsAt   time.Time
	podsBusy bool
	quitting bool
}

func New(ctx context.Context, opts Options) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Spinner{Frames: []string{"◜", "◠", "◝", "◞", "◡", "◟"}, FPS: time.Second / 10}
	sp.Style = sAmber
	m := &Model{
		ctx: ctx, opts: opts, cfg: config.Load(), spin: sp,
		splashT0: time.Now(),
		docs:     map[string]*opened{},
		openErr:  map[string]error{},
		revealed: map[string]int{},
		changed:  map[string]bool{},
		fx:       map[string]revealFx{},
		homeIn:   newSearchInput(),
		keyIn:    newFilterInput("filter keys"),
		sortAZ:   true,
	}
	return m
}

// ---- messages ----

type (
	frameMsg time.Time
	authMsg  struct {
		id  gcp.Identity
		err error
	}
	loginDoneMsg struct{ err error }
	storeMsg     struct {
		st  *store.Store
		err error
	}
	discoverMsg store.DiscoverEvent
	listMsg     struct {
		bucket  string
		secrets []store.Secret
		err     error
		poll    bool
	}
	openMsg struct {
		sec  store.Secret
		val  secret.Value
		err  error
		show bool
	}
	saveMsg struct {
		sec  store.Secret
		data []byte
		note string
		err  error
	}
	podsMsg struct {
		snap kube.Snapshot
		err  error
	}
	toastDoneMsg int
	hideMsg      struct {
		obj, key string
		id       int
	}
	pollMsg struct{}
)

// ---- commands ----

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.checkAuth(), m.tick(), m.spin.Tick)
}

func (m *Model) tick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tea.Tick(time.Second/60, func(t time.Time) tea.Msg { return frameMsg(t) })
}

func (m *Model) checkAuth() tea.Cmd {
	m.auth, m.authErr = authChecking, nil
	return func() tea.Msg {
		id, err := gcp.Check(m.ctx)
		return authMsg{id, err}
	}
}

func (m *Model) login() tea.Cmd {
	cmd, err := gcp.LoginCmd()
	if err != nil {
		m.auth, m.authErr = authFailed, err
		return nil
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return loginDoneMsg{err} })
}

func (m *Model) connect() tea.Cmd {
	return func() tea.Msg {
		st, err := store.New(m.ctx)
		return storeMsg{st, err}
	}
}

func (m *Model) list(bucket string, poll bool) tea.Cmd {
	if !poll {
		m.listing, m.listErr = true, nil
	}
	return func() tea.Msg {
		ss, err := m.st.List(m.ctx, bucket)
		return listMsg{bucket, ss, err, poll}
	}
}

func (m *Model) open(sec store.Secret, show bool) tea.Cmd {
	if show {
		m.loading = sec.Object
		delete(m.openErr, sec.Object)
	}
	return func() tea.Msg {
		s, v, err := m.st.Read(m.ctx, sec)
		return openMsg{s, v, err, show}
	}
}

func (m *Model) save(sec store.Secret, data []byte, note string) tea.Cmd {
	return func() tea.Msg {
		s, err := m.st.Write(m.ctx, sec, data)
		return saveMsg{s, data, note, err}
	}
}

func (m *Model) loadPods() tea.Cmd {
	if m.podsBusy || time.Since(m.podsAt) < podsMaxAge {
		return nil
	}
	m.podsBusy = true
	return func() tea.Msg {
		s, err := kube.Load(m.ctx)
		return podsMsg{s, err}
	}
}

func poll() tea.Cmd { return tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{} }) }

func (m *Model) flash(msg string, hot bool) tea.Cmd {
	m.toastSeq++
	m.toast, m.toastHot = msg, hot
	id := m.toastSeq
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return toastDoneMsg(id) })
}

func (m *Model) failure(err error) tea.Cmd {
	msg := err.Error()
	switch {
	case errors.Is(err, store.ErrConflict):
		msg = "Someone changed this secret while you were editing. Reloaded it, try again."
	case errors.Is(err, store.ErrDenied):
		msg = "Permission denied. " + strings.TrimPrefix(strings.TrimPrefix(msg, store.ErrDenied.Error()), ": ")
	case strings.Contains(msg, "oauth2"), strings.Contains(msg, "invalid_grant"), strings.Contains(msg, "reauth"):
		msg = "Your Google sign-in expired. Press L to sign in again."
	}
	return m.flash(msg, true)
}

// ---- update ----

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case frameMsg:
		m.ticking = false
		return m, m.animate()

	case authMsg:
		m.ident, m.authErr = msg.id, msg.err
		if msg.err != nil {
			m.auth = authFailed
		} else {
			m.auth = authOK
		}
		return m, nil

	case loginDoneMsg:
		if msg.err != nil {
			m.auth, m.authErr = authFailed, fmt.Errorf("gcloud sign-in did not finish: %w", msg.err)
			return m, nil
		}
		m.scr = scrAuth
		m.podsAt, m.podsErr = time.Time{}, nil // kubectl uses the same gcloud login
		return m, m.checkAuth()

	case storeMsg:
		m.connecting = false
		if msg.err != nil {
			m.scr, m.auth, m.authErr = scrAuth, authFailed, msg.err
			return m, nil
		}
		m.st = msg.st
		bucket := m.opts.Bucket
		if bucket == "" && !m.opts.Pick {
			bucket = m.cfg.Bucket
		}
		if bucket != "" {
			return m, m.useBucket(bucket)
		}
		return m, m.openPicker()

	case discoverMsg:
		return m, m.bk.handle(store.DiscoverEvent(msg))

	case listMsg:
		return m, m.handleList(msg)

	case openMsg:
		return m, m.handleOpen(msg)

	case saveMsg:
		return m, m.handleSave(msg)

	case historyMsg, auditMsg, versionMsg:
		return m, m.handleHistory(msg)

	case podsMsg:
		m.podsBusy, m.podsAt = false, time.Now()
		m.pods, m.podsErr = msg.snap, msg.err
		if m.pal != nil {
			m.pal.refresh(m)
		}
		return m, nil

	case toastDoneMsg:
		if int(msg) == m.toastSeq {
			m.toast = ""
		}
		return m, nil

	case hideMsg:
		if msg.obj == m.shown && m.revealed[msg.key] == msg.id {
			return m, m.hide(msg.key)
		}
		return m, nil

	case pollMsg:
		if m.scr == scrBrowser && m.st != nil && m.bucket != "" {
			return m, tea.Batch(m.list(m.bucket, true), poll(), m.loadPods())
		}
		return m, poll()

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, m.key(msg)
	}

	// ⌘K reaches us as the CSI-u sequence ESC[107;9u, which Bubble Tea
	// doesn't name; treat it exactly like ctrl+k. See `secdrop setup-keys`.
	if st, ok := msg.(fmt.Stringer); ok && st.String() == superK {
		return m, m.key(tea.KeyMsg{Type: tea.KeyCtrlK})
	}

	// Let a focused text input see everything else (cursor blink etc.).
	if m.pal != nil {
		var cmd tea.Cmd
		m.pal.in, cmd = m.pal.in.Update(msg)
		return m, cmd
	}
	if m.modal != nil {
		return m, m.modal.updateInputs(msg)
	}
	if m.typing != 0 {
		var c1, c2 tea.Cmd
		m.homeIn, c1 = m.homeIn.Update(msg)
		m.keyIn, c2 = m.keyIn.Update(msg)
		return m, tea.Batch(c1, c2)
	}
	if m.scr == scrBuckets && m.bk.typing {
		var cmd tea.Cmd
		m.bk.in, cmd = m.bk.in.Update(msg)
		return m, cmd
	}
	return m, nil
}

// animate advances the splash reveal and the palette spring by one frame.
func (m *Model) animate() tea.Cmd {
	busy := false
	if m.scr == scrSplash {
		if m.splashDone() {
			m.scr = scrAuth
		} else {
			busy = true
		}
	}
	if m.pal != nil && m.pal.step() {
		busy = true
	}
	if m.stepFx() {
		busy = true
	}
	if busy {
		return m.tick()
	}
	return nil
}

func (m *Model) key(k tea.KeyMsg) tea.Cmd {
	if m.pal != nil {
		return m.paletteKey(k)
	}
	if m.modal != nil {
		return m.modalKey(k)
	}
	if m.help {
		m.help = false
		return nil
	}
	switch m.scr {
	case scrSplash:
		m.scr = scrAuth
		return nil
	case scrAuth:
		return m.authKey(k)
	case scrBuckets:
		return m.bucketKey(k)
	case scrBrowser:
		return m.browserKey(k)
	}
	return nil
}

func (m *Model) authKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "q", "esc":
		return tea.Quit
	case "enter":
		switch m.auth {
		case authOK:
			if m.st != nil {
				m.scr = scrBrowser
				return nil
			}
			if m.connecting {
				return nil
			}
			m.connecting = true
			return m.connect()
		case authFailed:
			return m.login()
		}
	case "s", "l", "L":
		if m.auth != authChecking {
			return m.login()
		}
	case "r":
		if m.auth == authFailed {
			return m.checkAuth()
		}
	}
	return nil
}

func (m *Model) copy(text, note string) tea.Cmd {
	if err := clip.Copy(text); err != nil {
		return m.flash("Couldn't reach the clipboard: "+err.Error(), true)
	}
	clip.ClearAfter(clipboardFor, text)
	return m.flash(note+" · clipboard clears in 30s", false)
}

func label(s store.Secret) string {
	if s.Env == "" {
		return s.Service
	}
	return s.Service + " · " + s.Env
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 2*365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
}
