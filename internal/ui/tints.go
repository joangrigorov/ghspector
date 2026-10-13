package ui

import (
	"hash/fnv"
	"image/color"
	"math"
	"strings"
)

// The conversation paints a wash behind each contribution, one shade per
// commenter, so a long thread separates into blocks without every block gaining
// a border.
//
// A wash is the palette's own accent mixed into the palette's own background.
// That is what keeps the shades one family: every wash takes its hue from the
// accent and its lightness from the background, so a dark terminal gets blues
// just above black and a light one blues just under white, and neither ends up
// with a colour the rest of the palette does not use. The shades differ only in
// how much accent they carry, which is small enough that the text on top keeps
// its contrast: the body colour clears 9:1 on every shade of either palette and
// the muted colour, which draws the timestamps, stays between 4:1 and 7:1.
const (
	// tintCount is how many shades the ramp holds. A commenter maps to one of
	// them, so two people share a shade once the thread is longer than this.
	tintCount = 6
	// tintDarkFloor and tintLightFloor are the share of accent in the first
	// shade: a dark palette starts just above its background, a light one just
	// below its, and each further shade carries its palette's step more.
	tintDarkFloor  = 0.04
	tintLightFloor = 0.03
	// tintDarkStep and tintLightStep are how much accent each further shade
	// carries. The darker ramp spends more of it because a little light on black
	// goes less far than the same amount of shadow on white.
	tintDarkStep  = 0.04
	tintLightStep = 0.024
)

// authorTints builds the washes for a palette: the accent mixed into the
// background, in even steps.
func authorTints(dark bool, background, accent color.Color) []color.Color {
	floor, step := tintDarkFloor, tintDarkStep
	if !dark {
		floor, step = tintLightFloor, tintLightStep
	}
	out := make([]color.Color, tintCount)
	for i := range out {
		out[i] = mix(background, accent, floor+step*float64(i))
	}
	return out
}

// mix blends amount of accent into base, channel by channel. Zero returns the
// base and one the accent.
func mix(base, accent color.Color, amount float64) color.Color {
	br, bg, bb := channels8(base)
	ar, ag, ab := channels8(accent)
	blend := func(b, a uint8) uint8 {
		return uint8(math.Round(float64(b) + amount*(float64(a)-float64(b))))
	}
	return color.RGBA{R: blend(br, ar), G: blend(bg, ag), B: blend(bb, ab), A: 0xff}
}

// channels8 is a colour's three channels, each 0-255.
func channels8(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

// authorTint is the wash behind a contribution by login. The shade follows from
// the login itself rather than from the order the authors appear in, so a person
// keeps their shade across screens, refreshes and conversations. A palette with
// no washes — the plain theme the goldens render — paints nothing.
func authorTint(theme Theme, login string) color.Color {
	if len(theme.AuthorTints) == 0 {
		return nil
	}
	return theme.AuthorTints[int(loginHash(login)%uint64(len(theme.AuthorTints)))]
}

// loginHash is a stable hash of a login. It is FNV-1a and not the map hash,
// because the shade a person gets has to survive a restart.
func loginHash(login string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(login))))
	return h.Sum64()
}
