package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
	"ghspector/internal/store"
)

// Switcher picks the account or organization whose repositories the lists
// cover. It replaces the previous version's "targets" overlay, which mixed
// accounts, organizations and repositories into one list and returned a
// differently-shaped value depending on which kind was picked.
type Switcher struct {
	entries []store.OwnerRef
	active  store.OwnerRef
	cursor  int
	// visible is recorded during View so scrolling can be clamped exactly.
	visible int
}

// NewSwitcher returns the switcher route, with the cursor on the active entry.
func NewSwitcher(entries []store.OwnerRef, active store.OwnerRef) *Switcher {
	s := &Switcher{entries: entries, active: active}
	for i, e := range entries {
		if sameOwner(e, active) {
			s.cursor = i
			break
		}
	}
	return s
}

// Title is the breadcrumb label.
func (s *Switcher) Title() string { return "Switch context" }

// Scope is the switcher keymap scope.
func (s *Switcher) Scope() keymap.Scope { return keymap.Switcher }

// Update ignores messages.
func (s *Switcher) Update(Deps, tea.Msg) (Route, tea.Cmd) { return s, nil }

// Act moves the cursor, selects an entry, or closes.
func (s *Switcher) Act(_ Deps, action keymap.Action) (Route, tea.Cmd, bool) {
	switch action {
	case keymap.ActionDown:
		if s.cursor < len(s.entries)-1 {
			s.cursor++
		}
		return s, nil, true
	case keymap.ActionUp:
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil, true
	case keymap.ActionSelect:
		if s.cursor < 0 || s.cursor >= len(s.entries) {
			return s, nil, true
		}
		chosen := s.entries[s.cursor]
		return s, func() tea.Msg { return ContextSelectedMsg{Owner: chosen} }, true
	case keymap.ActionBack:
		return s, func() tea.Msg { return PopMsg{} }, true
	}
	return s, nil, false
}

// View renders the entry list.
func (s *Switcher) View(deps Deps, r layout.Rect) string {
	if r.Empty() {
		return ""
	}
	theme := deps.Theme
	lines := []string{
		"  " + style(theme.Muted, normal).Render("Choose the account or organization to browse."),
		"",
	}

	if len(s.entries) == 0 {
		lines = append(lines, "  "+style(theme.Muted, normal).Render("No accounts available."))
		return layout.Fit(strings.Join(lines, "\n"), r.W, r.H)
	}

	s.visible = r.H - len(lines)
	if s.visible < 1 {
		s.visible = 1
	}
	start, end := window(len(s.entries), s.cursor, s.visible)
	for i := start; i < end; i++ {
		lines = append(lines, s.row(deps, i, r.W))
	}
	return layout.Fit(strings.Join(lines, "\n"), r.W, r.H)
}

// row renders one entry, marking the active context and the cursor. The cursor
// row takes the same marker and accent as the highlighted row in every list.
func (s *Switcher) row(deps Deps, i, width int) string {
	theme := deps.Theme
	entry := s.entries[i]

	kind := "account"
	if entry.IsOrg {
		kind = "organization"
	}

	row := entry.Login + "  " + style(theme.Muted, normal).Render("("+kind+")")
	if sameOwner(entry, s.active) {
		row += "  " + style(theme.StatusOK, normal).Render("← active")
	}
	if i == s.cursor {
		return selectedRow(theme, row, width)
	}
	return tableIndent + row
}

// sameOwner compares two owner references.
func sameOwner(a, b store.OwnerRef) bool {
	return a.Login == b.Login && a.IsOrg == b.IsOrg
}

// window returns the visible slice [start,end) of a list with the cursor kept
// inside it.
func window(length, cursor, visible int) (int, int) {
	if length <= visible {
		return 0, length
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > length {
		start = length - visible
	}
	return start, start + visible
}
