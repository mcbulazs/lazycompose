package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// themeColor turns a theme colour like "#e06c75" into one with its own
// fallbacks for terminals with 256 or 16 colours, such as tmux without
// truecolor. lipgloss would convert it itself, but its 256 colour pick is
// sometimes far off: onedark's red came out almost black. key is the colour
// key it is used for, like "red", which decides the 16 colour fallback.
func themeColor(key, hex string) lipgloss.TerminalColor {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return lipgloss.Color(hex)
	}
	ansi, ok := roleANSI[key]
	if !ok {
		ansi = nearest16(r, g, b)
	}
	return lipgloss.CompleteColor{
		TrueColor: fmt.Sprintf("#%02x%02x%02x", r, g, b),
		ANSI256:   strconv.Itoa(nearest256(r, g, b)),
		ANSI:      strconv.Itoa(ansi),
	}
}

// roleANSI are the 16 colour fallbacks of the colour keys with a meaning:
// red stays the terminal's red, whatever shade the theme uses. Text is
// white and dim parts grey, never black, which vanishes on a dark
// background. The other keys (focus, select_bg) go by hue.
var roleANSI = map[string]int{
	"border": 8, "text": 7, "dim": 8, "select_fg": 7,
	"green": 2, "yellow": 3, "red": 1,
}

func parseHex(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v >> 16), int(v >> 8 & 0xff), int(v & 0xff), true
}

// cubeLevels are the channel values of the 6x6x6 colour cube, 16 to 231.
var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// nearest256 is the closest colour of the cube and the grey ramp (16 to
// 255). The first 16 are left out, since terminals change them.
func nearest256(r, g, b int) int {
	best, bestD := 16, math.Inf(1)
	try := func(n, cr, cg, cb int) {
		if d := colorDistance(r, g, b, cr, cg, cb); d < bestD {
			best, bestD = n, d
		}
	}
	for i, cr := range cubeLevels {
		for j, cg := range cubeLevels {
			for k, cb := range cubeLevels {
				try(16+36*i+6*j+k, cr, cg, cb)
			}
		}
	}
	for i := range 24 {
		v := 8 + 10*i
		try(232+i, v, v, v)
	}
	return best
}

// colorDistance is the "redmean" distance, a cheap estimate of how
// different two colours look.
func colorDistance(r1, g1, b1, r2, g2, b2 int) float64 {
	rm := float64(r1+r2) / 2
	dr, dg, db := float64(r1-r2), float64(g1-g2), float64(b1-b2)
	return math.Sqrt((2+rm/256)*dr*dr + 4*dg*dg + (2+(255-rm)/256)*db*db)
}

// nearest16 picks one of the terminal's own colours by hue, so red stays
// red whatever the terminal makes of it. Greys become grey or white, never
// black, which would vanish on a dark background.
func nearest16(r, g, b int) int {
	h, s, l := hsl(r, g, b)
	if s < 0.2 || l < 0.12 || l > 0.95 {
		if l > 0.6 {
			return 7
		}
		return 8
	}
	switch {
	case h < 15 || h >= 345:
		return 1 // red
	case h < 75:
		return 3 // yellow, orange
	case h < 165:
		return 2 // green
	case h < 200:
		return 6 // cyan
	case h < 260:
		return 4 // blue
	default:
		return 5 // magenta
	}
}

// hsl returns hue in degrees, saturation and lightness from 0 to 1.
func hsl(r, g, b int) (h, s, l float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	hi, lo := max(rf, gf, bf), min(rf, gf, bf)
	l = (hi + lo) / 2
	d := hi - lo
	if d == 0 {
		return 0, 0, l
	}
	s = d / (1 - math.Abs(2*l-1))
	switch hi {
	case rf:
		h = math.Mod((gf-bf)/d, 6)
	case gf:
		h = (bf-rf)/d + 2
	default:
		h = (rf-gf)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l
}
