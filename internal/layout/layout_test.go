package layout

import (
	"fmt"
	"math/rand"
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
