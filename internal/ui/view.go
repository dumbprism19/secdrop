package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const minW, minH = 72, 20

func (m *Model) View() string {
	if m.quitting || m.w == 0 {
		return ""
	}
	if m.w < minW || m.h < minH {
		return m.centered(sMuted.Render(fmt.Sprintf("Make the window at least %d×%d", minW, minH)))
	}
	switch m.scr {
	case scrSplash:
		return m.viewSplash()
	case scrAuth:
		return m.viewAuth()
	case scrBuckets:
		return m.viewBuckets()
	}
	base := m.viewBrowser()
	switch {
	case m.pal != nil:
		y := max(1, m.h/7) + int(m.pal.y+0.5)
		return m.composite(base, m.viewPalette(), -1, y, m.pal.glass)
	case m.modal != nil:
		return m.composite(base, m.viewModal(), -1, -1, 1)
	case m.help:
		return m.composite(base, m.viewHelp(), -1, -1, 1)
	}
	return base
}

func (m *Model) centered(s string) string {
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, s)
}

func renderLogoBig(w int) string {
	if w < LogoWidth+4 {
		return miniLogo()
	}
	return renderLogo(-1)
}

func renderLogoSmall() string { return miniLogo() }

// ---- splash and sign-in ----

func (m *Model) viewAuth() string {
	var card []string
	card = append(card, sWhiteB.Render("Sign in to Google Cloud"), "")
	switch {
	case m.auth == authChecking:
		card = append(card, m.spin.View()+sMuted.Render(" Checking your gcloud credentials…"))
	case m.auth == authOK && m.connecting:
		card = append(card, sOK.Render("●")+sText.Render(" Signed in as ")+sWhiteB.Render(orWord(m.ident.Email, "your account")), "",
			m.spin.View()+sMuted.Render(" Connecting to Cloud Storage and KMS…"))
	case m.auth == authOK:
		who := orWord(m.ident.Email, "your account")
		card = append(card, sOK.Render("●")+sText.Render(" Signed in as ")+sWhiteB.Render(who))
		if m.ident.Project != "" {
			card = append(card, sMuted.Render("  project "+m.ident.Project))
		}
		card = append(card, "", hints([][2]string{{"enter", "continue"}, {"s", "use another account"}, {"q", "quit"}}))
	default:
		reason := "You're not signed in."
		if m.authErr != nil {
			reason = m.authErr.Error()
		}
		card = append(card,
			sHot.Render("●")+sText.Render(" "+trunc(capFirst(reason), 64)), "",
			sMuted.Render("secdrop uses your own gcloud credentials and stores no tokens."),
			sMuted.Render("Signing in opens your browser."), "",
			hints([][2]string{{"enter", "sign in with gcloud"}, {"r", "check again"}, {"q", "quit"}}))
	}
	body := lipgloss.NewStyle().Width(min(70, m.w-4)).Render(strings.Join(card, "\n"))
	return m.centered(lipgloss.JoinVertical(lipgloss.Center, renderLogoBig(m.w), "", "", "", body))
}

// ---- overlays ----

var glassSteps = []lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.Color("#8E8473")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#6A6253")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#4A4438")),
	sGlass,
}

// composite frosts the base screen (strips its colors and dims it) and lays
// box on top. x or y of -1 centers the box on that axis.
func (m *Model) composite(base, box string, x, y int, frost float64) string {
	gs := glassSteps[max(0, min(len(glassSteps)-1, int(frost*float64(len(glassSteps)-1)+0.5)))]
	bg := strings.Split(ansi.Strip(base), "\n")
	for len(bg) < m.h {
		bg = append(bg, "")
	}
	fg := strings.Split(box, "\n")
	bw := 0
	for _, l := range fg {
		bw = max(bw, lipgloss.Width(l))
	}
	if x < 0 {
		x = max(0, (m.w-bw)/2)
	}
	if y < 0 {
		y = max(0, (m.h-len(fg))/2)
	}
	out := make([]string, m.h)
	for i := 0; i < m.h; i++ {
		line := fit(bg[i], m.w)
		j := i - y
		if j < 0 || j >= len(fg) {
			out[i] = gs.Render(line)
			continue
		}
		left := ansi.Truncate(line, x, "")
		rest := ansi.TruncateLeft(line, x+bw, "")
		out[i] = gs.Render(left) + fit(fg[j], bw) + gs.Render(rest)
	}
	return strings.Join(out, "\n")
}

// ---- small helpers ----

func hints(pairs [][2]string) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = sKey.Render(p[0]) + " " + sMuted.Render(p[1])
	}
	return strings.Join(parts, "   ")
}

// pad right-pads a plain or styled string to width n.
func pad(s string, n int) string {
	if d := n - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// fit pads or cuts a styled string to exactly n cells.
func fit(s string, n int) string {
	if lipgloss.Width(s) > n {
		return ansi.Truncate(s, n, "")
	}
	return pad(s, n)
}

func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return trunc(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

func oneLine(s string) string {
	return strings.NewReplacer("\r\n", "⏎", "\n", "⏎", "\t", " ").Replace(s)
}

func orWord(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

func capFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
