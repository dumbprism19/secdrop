package ui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dumbprism19/secdrop/internal/repos"
)

// exportState is the export dialog: local checkouts of the service found
// with git, plus "somewhere else" for any path.
type exportState struct {
	busy   bool
	err    string // why the repo search failed, if it did
	repos  []repos.Repo
	cur    int    // selected destination; len(repos) is "somewhere else"
	warned string // path we already warned is not git-ignored
}

// reposMsg carries the result of a repo search for one secret.
type reposMsg struct {
	obj   string
	repos []repos.Repo
	err   error
}

// openExport opens the export dialog and starts looking for checkouts of
// the service in the background.
func (m *Model) openExport(o *opened) tea.Cmd {
	m.modal = newExportModal(o)
	svc, obj := o.sec.Service, o.sec.Object
	ctx := m.ctx
	find := func() tea.Msg {
		rs, err := repos.Find(ctx, svc, repos.Roots())
		return reposMsg{obj: obj, repos: rs, err: err}
	}
	return tea.Batch(textinput.Blink, find)
}

// exportService opens a service from the home screen and shows the export
// dialog as soon as its environment is decrypted.
func (m *Model) exportService(name string) tea.Cmd {
	cmd := m.openService(name, "")
	if !m.panel {
		return cmd
	}
	if o := m.shownDoc(); o != nil && !o.stale && m.loading != o.sec.Object {
		return tea.Batch(cmd, m.openExport(o))
	}
	m.pendingExport = true
	return cmd
}

func newExportModal(o *opened) *modal {
	in := newInput("path")
	in.SetValue(defaultExportPath(o))
	in.CursorEnd()
	in.Focus()
	return &modal{kind: mExport, o: o, valIn: in, ex: &exportState{busy: true}}
}

func defaultExportPath(o *opened) string {
	name := o.sec.Service
	if o.sec.Env != "" {
		name += "." + o.sec.Env
	}
	return "./" + name + ".env"
}

// handleRepos fills in the destinations once the search finishes, and
// selects the first repo so enter writes straight into it.
func (m *Model) handleRepos(msg reposMsg) {
	md := m.modal
	if md == nil || md.kind != mExport || md.ex == nil || md.o.sec.Object != msg.obj {
		return
	}
	md.ex.busy, md.ex.repos = false, msg.repos
	if msg.err != nil {
		md.ex.err = msg.err.Error()
	}
	if len(msg.repos) > 0 {
		md.selectExport(0)
	} else {
		md.ex.cur = 0
	}
}

// selectExport moves to destination i and puts its file in the path box.
func (md *modal) selectExport(i int) {
	ex := md.ex
	ex.cur = i
	ex.warned = ""
	if i < len(ex.repos) {
		p := repos.Target(ex.repos[i].Path, md.o.sec.Env)
		if p == "" {
			// Both .env and .env.<env> exist; show the name so the error says why.
			p = filepath.Join(ex.repos[i].Path, ".env."+orDash(md.o.sec.Env))
		}
		md.valIn.SetValue(tildify(p))
	} else {
		md.valIn.SetValue(defaultExportPath(md.o))
	}
	md.valIn.CursorEnd()
}

func (m *Model) exportKey(k tea.KeyMsg) tea.Cmd {
	md := m.modal
	ex := md.ex
	switch key := k.String(); key {
	case "esc":
		m.modal = nil
		return nil
	case "up", "shift+tab":
		if ex.cur > 0 {
			md.selectExport(ex.cur - 1)
		}
		md.err = ""
		return nil
	case "down", "tab":
		if ex.cur < len(ex.repos) {
			md.selectExport(ex.cur + 1)
		}
		md.err = ""
		return nil
	case "enter":
		path := expandHome(strings.TrimSpace(md.valIn.Value()))
		if path == "" {
			return nil
		}
		ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
		inRepo, ignored := repos.Tracked(ctx, path)
		cancel()
		if inRepo && !ignored && ex.warned != path {
			ex.warned = path
			md.err = filepath.Base(path) + " isn't git-ignored here, so it could be committed. Press enter again to write it anyway."
			return nil
		}
		n, err := exportFile(path, md.o)
		if err != nil {
			md.err = err.Error()
			return nil
		}
		m.modal = nil
		note := "only you can read it · don't commit it"
		switch {
		case inRepo && ignored:
			note = "only you can read it · git-ignored"
		case inRepo:
			note = "only you can read it · NOT git-ignored, add it to .gitignore"
		}
		return m.flash(fmt.Sprintf("Wrote %s to %s (%s)", plural(n, "key"), tildify(path), note), inRepo && !ignored)
	}
	// Typing edits the path; it no longer matches a picked repo exactly,
	// but the selection stays so ↑/↓ still works from there.
	md.err, ex.warned = "", ""
	var cmd tea.Cmd
	md.valIn, cmd = md.valIn.Update(k)
	return cmd
}

func (m *Model) viewExport(w int, where string) []string {
	md := m.modal
	ex := md.ex
	body := []string{where, "", sMuted.Render("Destination")}
	row := func(sel bool, title, detail string) string {
		mark, st := "  ", sText
		if sel {
			mark, st = sAmber.Render("› "), sHi
		}
		line := mark + st.Render(title)
		if detail != "" {
			line += sFaint.Render("  " + detail)
		}
		return trunc(line, w-4)
	}
	switch {
	case ex.busy:
		body = append(body, "  "+m.spin.View()+sFaint.Render(" Looking for "+md.o.sec.Service+" repos with git…"))
	case ex.err != "":
		body = append(body, "  "+sFaint.Render("Couldn't look for repos: "+ex.err))
	case len(ex.repos) == 0:
		body = append(body, "  "+sFaint.Render("No local git repo named "+md.o.sec.Service+" found"))
	}
	for i, r := range ex.repos {
		detail := "matched by " + r.Why
		if r.Remote != "" {
			detail = remoteShort(r.Remote)
		}
		body = append(body, row(i == ex.cur, tildify(r.Path), detail))
	}
	body = append(body, row(ex.cur == len(ex.repos), "Somewhere else", "type any path"))

	body = append(body, "", sMuted.Render("File"), md.valIn.View(), sLine.Render(strings.Repeat("─", w-4)))
	if md.err != "" {
		body = append(body, sHot.Render(md.err))
	}
	body = append(body, "", sMuted.Render("Written with owner-only permissions. Existing files are never overwritten."),
		"", hints([][2]string{{"↑↓", "destination"}, {"enter", "write"}, {"esc", "cancel"}}))
	return body
}

// remoteShort turns git@github.com:org/repo.git into github.com/org/repo.
func remoteShort(u string) string {
	u = strings.TrimSuffix(u, ".git")
	for _, p := range []string{"https://", "http://", "ssh://", "git@"} {
		u = strings.TrimPrefix(u, p)
	}
	return strings.Replace(u, ":", "/", 1)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func tildify(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// exportFile writes the decrypted env exactly as stored, readable only by
// the user, and never replaces an existing file.
func exportFile(path string, o *opened) (int, error) {
	path = expandHome(path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return 0, errors.New(tildify(path) + " already exists. Choose another name.")
	}
	if err != nil {
		return 0, err
	}
	if _, err := f.Write(o.raw.Bytes()); err != nil {
		f.Close()
		return 0, err
	}
	return len(o.entries(false)), f.Close()
}
