package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/layout"
)

// TestDescriptionAndSidebarTileTheBox pins the pane geometry against the
// regression the gutter is easy to get wrong in: the pane block is rendered into
// one rect and shifted by the gutter, so a rect that is one gutter too narrow
// leaves the sidebar two cells short of the box and clips it, which a screenshot
// review of the left edge would not catch.
func TestDescriptionAndSidebarTileTheBox(t *testing.T) {
	srv := prServer(t)

	for _, w := range []int{120, 100, 80, 60} {
		t.Run(fmt.Sprintf("%d columns", w), func(t *testing.T) {
			m := pressAndDrive(t, prsModel(t, srv, w, 40), "enter", "1")
			out := m.View().Content
			desc, side := detailColumns(layout.ContentWidth(w))
			want := layout.Gutter + desc

			rows := 0
			for i, line := range strings.Split(out, "\n") {
				plain := ansi.Strip(line)
				// A pane row is a row whose only mark is the column rule.
				if !strings.Contains(plain, "│") || strings.Trim(plain, " │") != "" {
					continue
				}
				rows++
				if at := strings.Index(plain, "│"); at != want {
					t.Errorf("row %d: the column rule is at cell %d, want %d (description %d, sidebar %d)",
						i+1, at, want, desc, side)
				}
				if got, want := layout.Width(plain), w; got != want {
					t.Errorf("row %d is %d cells wide, want %d", i+1, got, want)
				}
			}
			if rows == 0 {
				t.Fatal("no pane row found in the details screen")
			}
		})
	}
}

// TestEveryScreenKeepsTheGutter walks the screens a session reaches at three
// sizes and checks the frame's geometry convention: every row is exactly the
// terminal's width, the right gutter is clear, and the left gutter holds nothing
// but a list's selection marker.
//
// This is the test the spacing bug needed. The description and the sidebar used
// to start in column zero, so the body ran into both edges of the terminal while
// the bars above and below it were inset; the rule is one box for the whole
// screen, and a screen that breaks it fails here rather than in a screenshot.
func TestEveryScreenKeepsTheGutter(t *testing.T) {
	srv := prServer(t)

	screens := []struct {
		name string
		keys []string
	}{
		{"pull requests", nil},
		{"workflow runs", []string{"tab"}},
		{"issues", []string{"tab", "tab"}},
		{"help", []string{"?"}},
		{"context switcher", []string{"o"}},
		{"pull request details", []string{"enter", "1"}},
		{"pull request commits", []string{"enter", "2"}},
		{"pull request files", []string{"enter", "3"}},
		{"pull request checks", []string{"enter", "4"}},
		{"pull request comments", []string{"enter", "5"}},
		{"commit details", []string{"enter", "2", "enter"}},
		{"jobs from a check", []string{"enter", "tab", "enter"}},
		{"logs", []string{"enter", "tab", "enter", "j", "enter"}},
		{"issue details", []string{"tab", "tab", "enter"}},
		{"issue conversation", []string{"tab", "tab", "enter", "c"}},
	}
	sizes := []struct{ w, h int }{{120, 40}, {80, 24}, {60, 16}}
	blank := strings.Repeat(" ", layout.Gutter)

	for _, size := range sizes {
		for _, screen := range screens {
			t.Run(fmt.Sprintf("%s at %dx%d", screen.name, size.w, size.h), func(t *testing.T) {
				m := pressAndDrive(t, prsModel(t, srv, size.w, size.h), screen.keys...)
				out := m.View().Content
				assertGeometry(t, screen.name, out, size.w, size.h)

				for i, line := range strings.Split(out, "\n") {
					plain := ansi.Strip(line)
					// Widths are cells, and a box-drawing rule is three bytes a
					// rune, so the rows are cut by width rather than indexed.
					tail := strings.TrimPrefix(plain, ansi.Truncate(plain, size.w-layout.Gutter, ""))
					if tail != blank {
						t.Errorf("row %d writes into the right gutter: %q", i+1, tail)
					}
					if head := ansi.Truncate(plain, layout.Gutter, ""); head != blank && head != selectMarker {
						t.Errorf("row %d writes into the left gutter: %q", i+1, head)
					}
				}
			})
		}
	}
}
