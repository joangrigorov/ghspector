package layout

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestLayoutTilesExact asserts the frame regions tile the screen exactly: the
// three bands are contiguous, span the full width, and sum to the height. A
// single off-by-one here is what produced the previous TUI's per-view clipping.
func TestLayoutTilesExact(t *testing.T) {
	for h := 1; h <= 60; h++ {
		for w := 1; w <= 200; w += 7 {
			f := Layout(w, h)

			if f.Header.H+f.Body.H+f.Footer.H != h {
				t.Fatalf("size %dx%d: band heights %d+%d+%d != %d",
					w, h, f.Header.H, f.Body.H, f.Footer.H, h)
			}
			for name, r := range map[string]Rect{"header": f.Header, "body": f.Body, "footer": f.Footer} {
				// A collapsed band has zero height but keeps the full width.
				if r.W != w {
					t.Fatalf("size %dx%d: %s width %d != %d", w, h, name, r.W, w)
				}
				if r.X != 0 {
					t.Fatalf("size %dx%d: %s x %d != 0", w, h, name, r.X)
				}
				if r.H < 0 || r.W < 0 {
					t.Fatalf("size %dx%d: %s has negative extent %+v", w, h, name, r)
				}
			}
			if f.Header.Y != 0 {
				t.Fatalf("size %dx%d: header y %d != 0", w, h, f.Header.Y)
			}
			if f.Body.Y != f.Header.H {
				t.Fatalf("size %dx%d: body y %d != header height %d", w, h, f.Body.Y, f.Header.H)
			}
			if f.Footer.Y != f.Body.Y+f.Body.H {
				t.Fatalf("size %dx%d: footer y %d != header+body %d", w, h, f.Footer.Y, f.Body.Y+f.Body.H)
			}
		}
	}
}

// TestLayoutCollapsesBarsWhenShort asserts that a screen too short for both bars
// yields a full-height body rather than negative band heights.
func TestLayoutCollapsesBarsWhenShort(t *testing.T) {
	for h := 1; h < HeaderHeight+FooterHeight; h++ {
		f := Layout(80, h)
		if f.Body.H != h {
			t.Fatalf("height %d: body %d != %d", h, f.Body.H, h)
		}
		if f.Header.H != 0 || f.Footer.H != 0 {
			t.Fatalf("height %d: bars not collapsed: %+v", h, f)
		}
	}
}

func TestSplitsTileTheRect(t *testing.T) {
	r := Rect{X: 3, Y: 4, W: 40, H: 20}

	left, right := r.SplitCols(15)
	if left.W+right.W != r.W || left.W != 15 {
		t.Fatalf("SplitCols widths: %d + %d != %d", left.W, right.W, r.W)
	}
	if right.X != left.X+left.W {
		t.Fatalf("SplitCols positions not contiguous: %+v %+v", left, right)
	}

	top, bottom := r.SplitRows(8)
	if top.H+bottom.H != r.H || top.H != 8 {
		t.Fatalf("SplitRows heights: %d + %d != %d", top.H, bottom.H, r.H)
	}
	if bottom.Y != top.Y+top.H {
		t.Fatalf("SplitRows positions not contiguous: %+v %+v", top, bottom)
	}

	// Over-sized splits clamp instead of producing negative extents.
	if l, rr := r.SplitCols(999); l.W != r.W || rr.W != 0 {
		t.Fatalf("SplitCols clamp: %+v %+v", l, rr)
	}
	if tp, bt := r.SplitRows(999); tp.H != r.H || bt.H != 0 {
		t.Fatalf("SplitRows clamp: %+v %+v", tp, bt)
	}
}

// TestSplitRowsFairTilesTheRectAndCapsTheTopBand checks the split for a block
// whose height is not known in advance: it still tiles, and the block can only
// take a third of the rect.
func TestSplitRowsFairTilesTheRectAndCapsTheTopBand(t *testing.T) {
	for height := 1; height <= 60; height++ {
		r := Rect{W: 40, H: height}
		for _, want := range []int{0, 1, 3, height / 2, height, height * 4} {
			top, bottom := r.SplitRowsFair(want)
			if top.H+bottom.H != r.H || bottom.Y != top.Y+top.H {
				t.Fatalf("height %d want %d: %+v does not tile %+v", height, want, top, bottom)
			}
			if limit := max(height/3, 1); top.H > limit {
				t.Fatalf("height %d want %d: the top band took %d rows, cap is %d", height, want, top.H, limit)
			}
			if top.H != min(want, max(height/3, 1)) {
				t.Fatalf("height %d want %d: top band %d rows", height, want, top.H)
			}
		}
	}
}

// TestFitAlwaysProducesExactGeometry is the resize invariant in miniature:
// whatever the content, Fit returns exactly w cells per line and h lines, with
// no line wider than w. Content includes wide runes, ANSI styling, tabs and
// control characters, which is where the previous rune-slicing compositor
// drifted.
func TestFitAlwaysProducesExactGeometry(t *testing.T) {
	inputs := []string{
		"",
		"short",
		"a line exactly 80 cells wide here................................................................",
		"emoji 🚀 and 🎉🎉 and ✨ mixed in a long line that will need truncating somewhere",
		"日本語のテキストは全角なので幅の計算を間違えやすい",
		"\x1b[1;31mstyled red text\x1b[0m followed by plain text that runs quite long indeed",
		"tab\tseparated\tvalues\there",
		"carriage\rreturn",
		strings.Repeat("wide 🇯🇵 flags ", 30),
		strings.Repeat("line\n", 100),
		"zero\u200bwidth\u200bjoiners",
	}

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 400; i++ {
		in := inputs[rng.Intn(len(inputs))]
		w := rng.Intn(120)
		h := rng.Intn(20)

		got := Fit(in, w, h)

		if w == 0 || h == 0 {
			if got != "" {
				t.Fatalf("Fit(%q, %d, %d) = %q, want empty", in, w, h, got)
			}
			continue
		}

		lines := strings.Split(got, "\n")
		if len(lines) != h {
			t.Fatalf("Fit(%q, %d, %d): %d lines, want %d", in, w, h, len(lines), h)
		}
		for n, line := range lines {
			if width := ansi.StringWidth(line); width != w {
				t.Fatalf("Fit(%q, %d, %d): line %d width %d, want %d (%q)",
					in, w, h, n, width, w, line)
			}
		}
	}
}

func TestFitClipsAndPads(t *testing.T) {
	if got, want := Fit("one\ntwo\nthree", 5, 2), "one  \ntwo  "; got != want {
		t.Fatalf("Fit clip/pad = %q, want %q", got, want)
	}
	if got, want := Fit("ab", 4, 3), "ab  \n    \n    "; got != want {
		t.Fatalf("Fit pad rows = %q, want %q", got, want)
	}
	if got, want := Fit("abcdef", 3, 1), "abc"; got != want {
		t.Fatalf("Fit truncate = %q, want %q", got, want)
	}
	if got := Fit("anything", 10, 0); got != "" {
		t.Fatalf("Fit with zero height = %q, want empty", got)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly", 7, "exactly"},
		{"truncate me", 8, "truncat…"},
		{"truncate me", 1, "t"},
		{"anything", 0, ""},
		{"anything", -3, ""},
		// A wide rune straddling the cut cannot be split, so the result lands
		// one cell short of the budget rather than overflowing it.
		{"日本語テキスト", 4, "日…"},
	}
	for _, c := range cases {
		if got := Truncate(c.in, c.w); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

// TestBoxWidthsLeaveRoomForTheGutter pins the two widths the frame is built
// from: the box a route renders into, and the text area inside it.
func TestBoxWidthsLeaveRoomForTheGutter(t *testing.T) {
	for _, tc := range []struct {
		width int
		box   int
		text  int
	}{
		{140, 138, 136},
		{120, 118, 116},
		{80, 78, 76},
		{50, 48, 46},
		// Below two gutters there is nothing to divide, so both report the
		// smallest region a caller can be handed.
		{4, 2, 1},
		{1, 1, 1},
	} {
		if got := BoxWidth(tc.width); got != tc.box {
			t.Errorf("BoxWidth(%d) = %d, want %d", tc.width, got, tc.box)
		}
		if got := ContentWidth(tc.width); got != tc.text {
			t.Errorf("ContentWidth(%d) = %d, want %d", tc.width, got, tc.text)
		}
	}

	// The two are the same number of cells apart everywhere a box exists, which
	// is what lets a route and the chrome agree on one right edge.
	for w := 2*Gutter + 1; w <= 400; w++ {
		if got, want := BoxWidth(w)-ContentWidth(w), Gutter; got != want {
			t.Fatalf("width %d: box and text are %d cells apart, want %d", w, got, want)
		}
	}
}

func TestTooSmall(t *testing.T) {
	if TooSmall(MinWidth, MinHeight) {
		t.Error("exact minimum reported as too small")
	}
	if !TooSmall(MinWidth-1, MinHeight) || !TooSmall(MinWidth, MinHeight-1) {
		t.Error("below minimum not reported as too small")
	}
}

func TestWidthIgnoresANSI(t *testing.T) {
	plain := "hello"
	styled := "\x1b[1;31mhello\x1b[0m"
	if Width(styled) != Width(plain) {
		t.Fatalf("Width(styled) = %d, Width(plain) = %d", Width(styled), Width(plain))
	}
}

func BenchmarkFit(b *testing.B) {
	line := strings.Repeat("some content here ", 10)
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, "%s\n", line)
	}
	in := sb.String()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Fit(in, 120, 40)
	}
}

func TestWindowKeepsTheCursorVisible(t *testing.T) {
	cases := []struct {
		name               string
		length, cursor     int
		visible            int
		wantStart, wantEnd int
	}{
		{"a list that fits is shown whole", 3, 0, 5, 0, 3},
		{"an empty list has no window", 0, 0, 5, 0, 0},
		{"the cursor at the start anchors the window", 10, 0, 4, 0, 4},
		{"the cursor in the middle is centred", 10, 5, 4, 3, 7},
		{"the cursor at the end pins the window to the bottom", 10, 9, 4, 6, 10},
		{"a cursor past the end is clamped", 10, 99, 4, 6, 10},
		{"a negative cursor is clamped", 10, -3, 4, 0, 4},
		{"a zero viewport still shows the cursor's row", 10, 5, 0, 5, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := Window(tc.length, tc.cursor, tc.visible)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Errorf("Window(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.length, tc.cursor, tc.visible, start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestPadRightPadsWithoutTruncating(t *testing.T) {
	cases := []struct {
		name string
		in   string
		w    int
		want string
	}{
		{"shorter is padded", "ab", 4, "ab  "},
		{"exact is unchanged", "ab", 2, "ab"},
		{"longer is left alone", "abcd", 2, "abcd"},
		{"empty is all padding", "", 3, "   "},
		{"a zero width is unchanged", "ab", 0, "ab"},
		// Colour is not width: a styled cell is padded by its visible length.
		{"escape sequences do not count", "\x1b[31m■\x1b[0m", 3, "\x1b[31m■\x1b[0m  "},
		{"a wide rune counts as two cells", "日", 4, "日  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PadRight(tc.in, tc.w); got != tc.want {
				t.Errorf("PadRight(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
			}
		})
	}
}

func TestFitColumns(t *testing.T) {
	// A table shaped like the runs list: a fixed status cell, a flexible name,
	// and two columns that may be dropped.
	table := func() []Column {
		return []Column{
			{Width: 3, Min: 3},
			{Width: 16, Min: 10},
			{Width: 22, Min: 14, Flex: true},
			{Width: 14},
			{Width: 16},
			{Width: 11, Min: 9},
		}
	}
	widths := func(cols []Column) []int {
		out := make([]int, len(cols))
		for i, c := range cols {
			out[i] = c.Width
		}
		return out
	}
	total := func(cols []Column) int {
		n := 2
		first := true
		for _, c := range cols {
			if c.Width <= 0 {
				continue
			}
			if !first {
				n++
			}
			n += c.Width
			first = false
		}
		return n
	}

	t.Run("spare width goes to the flexible column", func(t *testing.T) {
		got := widths(FitColumns(table(), 120, 2, 1))
		want := []int{3, 16, 53, 14, 16, 11}
		if !slices.Equal(got, want) {
			t.Fatalf("widths = %v, want %v", got, want)
		}
	})

	t.Run("a narrow row drops the optional columns", func(t *testing.T) {
		got := widths(FitColumns(table(), 60, 2, 1))
		if got[4] != 0 || got[3] != 0 {
			t.Fatalf("widths = %v, want the actor and event columns dropped", got)
		}
		if got[5] == 0 {
			t.Fatalf("widths = %v, want the duration column kept", got)
		}
	})

	t.Run("every result fits unless nothing can shrink", func(t *testing.T) {
		for _, w := range []int{200, 120, 100, 80, 70, 60, 50, 40, 30, 20, 12, 1} {
			cols := FitColumns(table(), w, 2, 1)
			if got := total(cols); got > w && got > total(FitColumns(table(), 1, 2, 1)) {
				t.Errorf("width %d: columns take %d cells", w, got)
			}
		}
	})

	t.Run("a dropped column is never narrower than its minimum", func(t *testing.T) {
		for _, w := range []int{200, 90, 60, 40, 20} {
			for i, c := range FitColumns(table(), w, 2, 1) {
				if c.Width > 0 && c.Width < c.Min {
					t.Errorf("width %d: column %d is %d, below its minimum %d", w, i, c.Width, c.Min)
				}
			}
		}
	})

	t.Run("the input is not modified", func(t *testing.T) {
		cols := table()
		_ = FitColumns(cols, 40, 2, 1)
		if cols[2].Width != 22 {
			t.Fatalf("input column changed to %d", cols[2].Width)
		}
	})

	t.Run("a minimum above the preferred width wins", func(t *testing.T) {
		got := FitColumns([]Column{{Width: 4, Min: 9}}, 40, 0, 1)
		if got[0].Width != 9 {
			t.Fatalf("width = %d, want 9", got[0].Width)
		}
	})
}
