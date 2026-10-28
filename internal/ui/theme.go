package ui

import "github.com/charmbracelet/lipgloss"

// Amber phosphor palette, taken from the Secdrop logo. Lip Gloss downsamples
// these on 256- and 16-color terminals and drops them under NO_COLOR.
var (
	cAmber   = lipgloss.Color("#FBB43A")
	cAmberHi = lipgloss.Color("#FFD98A")
	cAmberLo = lipgloss.Color("#B8801A")
	cWhite   = lipgloss.Color("#F2EFEA")
	cText    = lipgloss.Color("#E6DFD3")
	cMuted   = lipgloss.Color("#8E8473")
	cFaint   = lipgloss.Color("#5C5446")
	cLine    = lipgloss.Color("#3A3328")
	cGlass   = lipgloss.Color("#2E2920") // dimmed background behind overlays
	cSelBg   = lipgloss.Color("#2B2414")
	cPanelBg = lipgloss.Color("#17140F")
	cHot     = lipgloss.Color("#FF5B2E")
	cOK      = lipgloss.Color("#9BC27A")
)

var (
	sAmber  = lipgloss.NewStyle().Foreground(cAmber)
	sAmberB = lipgloss.NewStyle().Foreground(cAmber).Bold(true)
	sHi     = lipgloss.NewStyle().Foreground(cAmberHi)
	sWhiteB = lipgloss.NewStyle().Foreground(cWhite).Bold(true)
	sText   = lipgloss.NewStyle().Foreground(cText)
	sMuted  = lipgloss.NewStyle().Foreground(cMuted)
	sFaint  = lipgloss.NewStyle().Foreground(cFaint)
	sLine   = lipgloss.NewStyle().Foreground(cLine)
	sGlass  = lipgloss.NewStyle().Foreground(cGlass)
	sHot    = lipgloss.NewStyle().Foreground(cHot)
	sOK     = lipgloss.NewStyle().Foreground(cOK)
	sKey    = lipgloss.NewStyle().Foreground(cAmber).Bold(true)
	sTitle  = lipgloss.NewStyle().Foreground(cMuted).Bold(true)
	sTab    = lipgloss.NewStyle().Foreground(cMuted).Padding(0, 1)
	sTabOn  = lipgloss.NewStyle().Foreground(lipgloss.Color("#1A1408")).Background(cAmber).Bold(true).Padding(0, 1)
)

// envStyle colors an environment tag; production always stands out.
func envStyle(env string) lipgloss.Style {
	switch env {
	case "prod":
		return sHot
	case "staging", "nonprod", "preprod", "uat", "canary", "beta":
		return sAmber
	case "":
		return sFaint
	}
	return sMuted
}

func sAmberLo() lipgloss.Style { return lipgloss.NewStyle().Foreground(cAmberLo) }
