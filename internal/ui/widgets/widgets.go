// Package widgets renders the pieces of chrome that draw from shared data
// rather than from a route's state.
//
// The footer and the help screen both read the keymap table. That is the whole
// point: the previous version wrote the footer at sixteen call sites and the
// help text by hand, and the three drifted until the Jobs view advertised keys
// it did not handle.
package widgets

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
)

// Style holds the colours a widget needs. Nil means no colour.
type Style struct {
	Key  color.Color
	Desc color.Color
	// Title colours help section headings.
	Title color.Color
	// Muted colours secondary text such as the separator.
	Muted color.Color
}

// separator sits between footer groups.
const separator = "  "

// Footer renders a single line of "key:description" hints, truncated to width.
//
// Bindings are rendered in the order given; the caller decides the merge order
// so the pinned global keys stay put while scope keys vary.
func Footer(st Style, bindings []keymap.Binding, width int) string {
	if width <= 0 {
		return ""
	}
	keyStyle := styleFor(st.Key, true)
	descStyle := styleFor(st.Desc, false)

	var b strings.Builder
	written := 0
	for _, binding := range bindings {
		if binding.Help == "" {
			continue
		}
		keys := keymap.KeysFor(binding)
		if keys == "" {
			continue
		}
		chunk := keyStyle.Render(keys) + descStyle.Render(":"+binding.Help)
		chunkWidth := lipgloss.Width(chunk)
		if written > 0 {
			if written+len(separator)+chunkWidth > width {
				break
			}
			b.WriteString(separator)
			written += len(separator)
		}
		if chunkWidth > width {
			// A single long hint is trimmed rather than dropped, so the user
			// still sees the key.
			b.WriteString(layout.Truncate(chunk, width))
			return b.String()
		}
		b.WriteString(chunk)
		written += chunkWidth
	}
	return b.String()
}

// HelpLines renders the full keymap as width-bounded lines, one section per
// scope, ready to be scrolled.
func HelpLines(st Style, width int) []string {
	if width < 1 {
		width = 1
	}
	titleStyle := styleFor(st.Title, true)
	keyStyle := styleFor(st.Key, true)
	descStyle := styleFor(st.Desc, false)

	var lines []string
	for i, scope := range keymap.Scopes() {
		documented := keymap.Documented(scope)
		if len(documented) == 0 {
			continue
		}
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, titleStyle.Render(keymap.Title(scope)))

		keyWidth := 0
		for _, b := range documented {
			if w := lipgloss.Width(keymap.KeysFor(b)); w > keyWidth {
				keyWidth = w
			}
		}
		for _, b := range documented {
			keys := keymap.KeysFor(b)
			pad := strings.Repeat(" ", keyWidth-lipgloss.Width(keys))
			lines = append(lines, "  "+keyStyle.Render(keys)+pad+"  "+descStyle.Render(b.Help))
		}
	}
	for i, line := range lines {
		lines[i] = layout.Fit(line, width, 1)
	}
	return lines
}

// Dots is the animated progress suffix used by the splash and any loading
// message. It takes a tick counter so callers stay free of timers.
func Dots(tick int) string {
	return strings.Repeat(".", (tick%4)+1)
}

// styleFor mirrors ui.style so widgets stay independent of the theme type.
func styleFor(c color.Color, bold bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c != nil {
		s = s.Foreground(c)
	}
	if bold {
		s = s.Bold(true)
	}
	return s
}
