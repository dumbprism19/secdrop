package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The Secdrop wordmark as pixel bitmaps: 9-pixel cap height plus a 3-pixel
// descender, drawn two pixels per cell with half blocks.
type glyph struct {
	rows  []string
	color lipgloss.Color
	gap   int // empty columns after the glyph
}

var logoGlyphs = []glyph{
	{color: cAmber, gap: 3, rows: []string{ // the dome lock
		"..#######..",
		".##.....##.",
		"##.......##",
		"##.......##",
		"#####.#####",
		"#####.#####",
		"###########",
		"##.#####.##",
		"##.#####.##",
	}},
	{color: cAmber, gap: 2, rows: []string{ // S
		".#####.",
		"#.....#",
		"#......",
		"#......",
		".#####.",
		"......#",
		"......#",
		"#.....#",
		".#####.",
	}},
	{color: cAmber, gap: 2, rows: []string{"", "", "", ".####.", "#....#", "######", "#.....", "#....#", ".####."}},                               // e
	{color: cAmber, gap: 2, rows: []string{"", "", "", ".####.", "#....#", "#.....", "#.....", "#....#", ".####."}},                               // c
	{color: cWhite, gap: 2, rows: []string{".....#", ".....#", ".....#", ".#####", "#....#", "#....#", "#....#", "#....#", ".#####"}},             // d
	{color: cWhite, gap: 2, rows: []string{"", "", "", "#.###", "##...", "#....", "#....", "#....", "#...."}},                                     // r
	{color: cWhite, gap: 2, rows: []string{"", "", "", ".####.", "#....#", "#....#", "#....#", "#....#", ".####."}},                               // o
	{color: cWhite, gap: 0, rows: []string{"", "", "", "#####.", "#....#", "#....#", "#....#", "#....#", "#####.", "#.....", "#.....", "#....."}}, // p
}

const logoPixelRows = 12

type logoCol struct {
	on    [logoPixelRows]bool
	color lipgloss.Color
}

var logoCols = func() []logoCol {
	var cols []logoCol
	for _, g := range logoGlyphs {
		w := 0
		for _, r := range g.rows {
			w = max(w, len(r))
		}
		for x := 0; x < w; x++ {
			c := logoCol{color: g.color}
			for y, r := range g.rows {
				c.on[y] = x < len(r) && r[x] == '#'
			}
			cols = append(cols, c)
		}
		for i := 0; i < g.gap; i++ {
			cols = append(cols, logoCol{})
		}
	}
	return cols
}()

// LogoWidth is the wordmark's width in cells.
var LogoWidth = len(logoCols)

// renderLogo draws the wordmark. Columns at or past reveal are blank; the
// column just before it is drawn bright, like a scanning beam. reveal < 0
// draws everything.
func renderLogo(reveal int) string {
	if reveal < 0 {
		reveal = len(logoCols) + 1
	}
	var b strings.Builder
	for row := 0; row < logoPixelRows/2; row++ {
		for x, c := range logoCols {
			if x >= reveal {
				b.WriteByte(' ')
				continue
			}
			top, bot := c.on[row*2], c.on[row*2+1]
			ch := " "
			switch {
			case top && bot:
				ch = "█"
			case top:
				ch = "▀"
			case bot:
				ch = "▄"
			}
			st := lipgloss.NewStyle().Foreground(c.color)
			if x == reveal-1 && reveal <= len(logoCols) {
				st = st.Foreground(cAmberHi)
			}
			b.WriteString(st.Render(ch))
		}
		if row < logoPixelRows/2-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// miniLogo is the one-line mark used in the header.
func miniLogo() string {
	return sAmber.Render("▟▀▙") + " " + sAmberB.Render("Sec") + sWhiteB.Render("drop")
}
