package ui

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"
)

// The startup sequence, drawn on a pixel canvas two pixels per cell:
//
//  1. Rectangles flicker in at random spots across the screen.
//  2. The lock icon, cut into rectangles that start scattered, springs
//     together at the right end of the logo.
//  3. The finished icon slides left to its place, and "Secdrop" appears in
//     its wake.
//  4. The tagline types underneath, then sign-in starts.
//
// Any key skips it; with reduced motion the finished logo shows at once.

const (
	decoyCount     = 18
	decoyWindow    = 800 * time.Millisecond // decoys appear within this
	decoyLife      = 320 * time.Millisecond
	tileWindow     = 450 * time.Millisecond // icon rectangles appear within this
	tileFill       = 140 * time.Millisecond // a rectangle sits still before it flies
	tileFly        = 700 * time.Millisecond
	wipeStart      = 1350 * time.Millisecond // icon has landed; it now slides home
	tagStart       = 2050 * time.Millisecond
	tagCharGap     = 24 * time.Millisecond
	splashHold     = 700 * time.Millisecond
	tagline        = "Berglas secrets, without the typing"
	tileW, tileH   = 2, 2 // icon rectangle size in pixels
	splashNoMotion = 700 * time.Millisecond
)

var splashTotal = tagStart + time.Duration(len(tagline))*tagCharGap + splashHold

// springCurve is a Harmonica spring sampled once at 60 fps, used as the
// easing curve for every flying rectangle: fast, a small overshoot, settle.
var springCurve = func() []float64 {
	s := harmonica.NewSpring(harmonica.FPS(60), 7.5, 0.55)
	pos, vel := 0.0, 0.0
	out := make([]float64, 0, 90)
	for i := 0; i < 90; i++ {
		pos, vel = s.Update(pos, vel, 1)
		out = append(out, pos)
	}
	return out
}()

// slideCurve drives the icon's slide home: firmer damping, so it glides
// in with only a slight overshoot.
var slideCurve = sampleSpring(6.0, 0.82)

func sampleSpring(freq, damping float64) []float64 {
	s := harmonica.NewSpring(harmonica.FPS(60), freq, damping)
	pos, vel := 0.0, 0.0
	out := make([]float64, 0, 90)
	for i := 0; i < 90; i++ {
		pos, vel = s.Update(pos, vel, 1)
		out = append(out, pos)
	}
	return out
}

func springAt(d time.Duration) float64 { return curveAt(springCurve, d) }

func curveAt(curve []float64, d time.Duration) float64 {
	i := int(d.Seconds() * 60)
	if i < 0 {
		return 0
	}
	if i >= len(curve) {
		return 1
	}
	return curve[i]
}

// glyphSpans gives each logo glyph's first column and width.
var glyphSpans = func() [][2]int {
	var out [][2]int
	x := 0
	for _, g := range logoGlyphs {
		w := 0
		for _, r := range g.rows {
			w = max(w, len(r))
		}
		out = append(out, [2]int{x, w})
		x += w + g.gap
	}
	return out
}()

type canvas struct {
	w, h int // h in pixels (two per text row)
	px   []lipgloss.Color
}

func newCanvas(cols, rows int) *canvas {
	return &canvas{w: cols, h: rows * 2, px: make([]lipgloss.Color, cols*rows*2)}
}

func (c *canvas) set(x, y int, col lipgloss.Color) {
	if x >= 0 && y >= 0 && x < c.w && y < c.h {
		c.px[y*c.w+x] = col
	}
}

func (c *canvas) rect(x, y, w, h int, col lipgloss.Color) {
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			c.set(x+i, y+j, col)
		}
	}
}

func (c *canvas) lines() []string {
	out := make([]string, c.h/2)
	for r := range out {
		var b strings.Builder
		for x := 0; x < c.w; x++ {
			top, bot := c.px[2*r*c.w+x], c.px[(2*r+1)*c.w+x]
			switch {
			case top == "" && bot == "":
				b.WriteByte(' ')
			case top == bot:
				b.WriteString(lipgloss.NewStyle().Foreground(top).Render("█"))
			case bot == "":
				b.WriteString(lipgloss.NewStyle().Foreground(top).Render("▀"))
			case top == "":
				b.WriteString(lipgloss.NewStyle().Foreground(bot).Render("▄"))
			default:
				b.WriteString(lipgloss.NewStyle().Foreground(top).Background(bot).Render("▀"))
			}
		}
		out[r] = b.String()
	}
	return out
}

// logoPixel reports whether glyph g has a lit pixel at (x, y) within it.
func logoPixel(g, x, y int) bool {
	rows := logoGlyphs[g].rows
	return y < len(rows) && x < len(rows[y]) && rows[y][x] == '#'
}

func (m *Model) splashElapsed() time.Duration {
	if m.opts.NoMotion {
		return splashTotal
	}
	return time.Since(m.splashT0)
}

func (m *Model) splashDone() bool {
	if m.opts.NoMotion {
		return time.Since(m.splashT0) >= splashNoMotion
	}
	return time.Since(m.splashT0) >= splashTotal
}

func (m *Model) viewSplash() string {
	el := m.splashElapsed()
	if m.w < LogoWidth+4 || m.h < 18 {
		n := min(len(tagline), max(0, int((el-tagStart)/tagCharGap)))
		return m.centered(lipgloss.JoinVertical(lipgloss.Center, miniLogo(), "", sMuted.Render(tagline[:n]+" ")))
	}

	c := newCanvas(m.w, m.h)
	ox := (m.w - LogoWidth) / 2
	oy := (m.h-6)/2*2 - 4 // logo top, in pixels, a little above center

	// 1. Decoy rectangles that fill in and vanish at random spots.
	for i := 0; i < decoyCount; i++ {
		at := time.Duration(noise("decoy", i, 0) * float64(decoyWindow))
		age := el - at
		if age < 0 || age > decoyLife {
			continue
		}
		w := 2 + int(noise("decoy", i, 1)*6)
		h := 2 + int(noise("decoy", i, 2)*4)
		x := int(noise("decoy", i, 3) * float64(m.w-w))
		y := int(noise("decoy", i, 4) * float64(c.h-h))
		col := cLine
		switch {
		case age < decoyLife/3:
			col = cAmberLo // filling
		case age < decoyLife*2/3:
			col = cFaint
		}
		// Fill row by row, so each one visibly fills rather than pops in.
		rows := min(h, 1+int(float64(h)*float64(age)/float64(decoyLife/3)))
		c.rect(x, y, w, rows, col)
	}

	// 2. The icon, assembled from rectangles that fly in from random spots.
	//    It is built at the right end of the logo, then slides left to its
	//    place, uncovering "Secdrop" behind it (step 3).
	icon := glyphSpans[0]
	travel := LogoWidth - icon[1] // from the right end to its home column
	slide := 0.0                  // 0 = at the right end, 1 = home
	if el >= wipeStart {
		slide = curveAt(slideCurve, el-wipeStart)
	}
	shift := int(math.Round((1 - slide) * float64(travel)))
	tile := 0
	for ty := 0; ty < logoPixelRows; ty += tileH {
		for tx := 0; tx < icon[1]; tx += tileW {
			lit := false
			for j := 0; j < tileH; j++ {
				for i := 0; i < tileW; i++ {
					lit = lit || logoPixel(0, tx+i, ty+j)
				}
			}
			if !lit {
				continue
			}
			tile++
			at := time.Duration(noise("tile", tile, 0) * float64(tileWindow))
			age := el - at
			if age < 0 {
				continue
			}
			sx := int(noise("tile", tile, 1) * float64(m.w-tileW))
			sy := int(noise("tile", tile, 2) * float64(c.h-tileH))
			p := springAt(age - tileFill)
			x := sx + int(math.Round(float64(ox+icon[0]+shift+tx-sx)*p))
			y := sy + int(math.Round(float64(oy+ty-sy)*p))
			col := cAmber
			switch {
			case age < tileFill:
				col = cAmberLo
			case p < 0.98:
				col = cAmberHi
			}
			for j := 0; j < tileH; j++ {
				for i := 0; i < tileW; i++ {
					if logoPixel(0, tx+i, ty+j) {
						c.set(x+i, y+j, col)
					} else if age < tileFill {
						c.set(x+i, y+j, cAmberLo) // a full rectangle until it moves
					}
				}
			}
		}
	}

	// 3. The word appears in the icon's wake: every wordmark column to the
	//    right of the icon's trailing edge is revealed. Reveal uses the
	//    clamped slide so a spring overshoot never hides letters again.
	edge := ox + icon[0] + icon[1] + logoGlyphs[0].gap + int(math.Round((1-min(1, slide))*float64(travel)))
	for g := 1; g < len(logoGlyphs); g++ {
		sp := glyphSpans[g]
		for y := 0; y < logoPixelRows; y++ {
			for x := 0; x < sp[1]; x++ {
				cx := ox + sp[0] + x
				if cx < edge || !logoPixel(g, x, y) {
					continue
				}
				col := logoGlyphs[g].color
				if cx < edge+3 && slide < 1 {
					col = cAmberHi // freshly uncovered pixels glow for a moment
				}
				c.set(cx, oy+y, col)
			}
		}
	}
	tagN := 0
	if el >= tagStart {
		tagN = min(len(tagline), int((el-tagStart)/tagCharGap)+1)
	}

	// 4. The tagline types under the logo with a text cursor.
	lines := c.lines()
	if row := (oy+logoPixelRows)/2 + 2; row < len(lines) {
		text := sMuted.Render(tagline[:tagN])
		if tagN > 0 && tagN < len(tagline) {
			text += sAmber.Render("▌")
		}
		lines[row] = lipgloss.PlaceHorizontal(m.w, lipgloss.Center, text+strings.Repeat(" ", len(tagline)-tagN))
	}
	return strings.Join(lines, "\n")
}
