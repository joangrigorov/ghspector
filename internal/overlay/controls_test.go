package overlay

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
)

// press builds a key press for a key string, failing the test if the key has no
// representation.
func press(t *testing.T, key string) tea.KeyPressMsg {
	t.Helper()
	msg, ok := pressFor(key)
	if !ok {
		t.Fatalf("no key press representation for %q", key)
	}
	return msg
}

// chosen records what a control reported, so a test can assert on the callback
// without a Cmd round trip.
type chosen struct {
	action keymap.Action
	index  int
	value  string
}

func TestMenuRendersEachChoiceWithItsKey(t *testing.T) {
	m := Menu{
		Scope:   keymap.FilterType,
		Heading: "Filter",
		Intro:   []string{"Narrow the list."},
		Choices: []Choice{
			{Action: keymap.ActionFilterUser, Label: "Filter by user"},
			{Action: keymap.ActionFilterRepo, Label: "Filter by repository"},
		},
	}
	view := m.View(40)

	for _, want := range []string{"Narrow the list.", "u", "Filter by user", "r", "Filter by repository"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu view is missing %q:\n%s", want, view)
		}
	}
	if got := m.Title(); got != "Filter" {
		t.Errorf("Title() = %q, want Filter", got)
	}
	if got := len(m.Bindings()); got != len(keymap.For(keymap.FilterType)) {
		t.Errorf("Bindings() returned %d entries, want the scope's %d", got, len(keymap.For(keymap.FilterType)))
	}
}

func TestMenuPickRunsTheCallbackAndKeepsTheMenuOpen(t *testing.T) {
	got := chosen{}
	m := Menu{
		Scope:   keymap.FilterType,
		Choices: []Choice{{Action: keymap.ActionFilterRepo, Label: "Repository"}},
		OnPick: func(a keymap.Action) tea.Cmd {
			got.action = a
			return nil
		},
	}

	next, _ := m.Update(press(t, "r"))

	if got.action != keymap.ActionFilterRepo {
		t.Errorf("picked action = %q, want the repository filter", got.action)
	}
	// A choice may open another overlay above this one, so the menu stays.
	if next == nil {
		t.Error("the menu closed on a choice; it must stay open for a nested overlay")
	}
}

func TestMenuCancelCloses(t *testing.T) {
	cancelled := false
	m := Menu{
		Scope:    keymap.FilterType,
		OnCancel: func() tea.Cmd { cancelled = true; return nil }(),
	}
	next, _ := m.Update(press(t, "esc"))
	if next != nil {
		t.Error("esc did not close the menu")
	}
	_ = cancelled
}

func TestMenuIgnoresUnboundKeys(t *testing.T) {
	m := Menu{Scope: keymap.FilterType}
	for _, key := range []string{"j", "k", "enter"} {
		if next, _ := m.Update(press(t, key)); next == nil {
			t.Errorf("%q closed the menu; only esc should", key)
		}
	}
}

func TestListMovesTheCursorAndClamps(t *testing.T) {
	l := List{Scope: keymap.RepoSelect, Rows: []string{"a", "b"}}

	next, _ := l.Update(press(t, "j"))
	l = next.(List)
	if l.Cursor != 1 {
		t.Errorf("cursor = %d after one move down, want 1", l.Cursor)
	}
	next, _ = l.Update(press(t, "j"))
	l = next.(List)
	if l.Cursor != 1 {
		t.Errorf("cursor = %d after moving past the end, want it clamped at 1", l.Cursor)
	}
	next, _ = l.Update(press(t, "k"))
	l = next.(List)
	if l.Cursor != 0 {
		t.Errorf("cursor = %d after one move up, want 0", l.Cursor)
	}
	if next, _ = l.Update(press(t, "k")); next.(List).Cursor != 0 {
		t.Error("cursor moved above the first row")
	}
}

func TestListSelectReportsTheRowAndCloses(t *testing.T) {
	got := chosen{index: -1}
	l := List{
		Scope: keymap.RepoSelect,
		Rows:  []string{"api", "web"},
		OnSelect: func(i int) tea.Cmd {
			got.index = i
			return nil
		},
	}
	next, _ := l.Update(press(t, "j"))
	l = next.(List)
	next, _ = l.Update(press(t, "enter"))

	if got.index != 1 {
		t.Errorf("selected index = %d, want the highlighted row", got.index)
	}
	if next != nil {
		t.Error("the list stayed open after a selection")
	}
}

func TestListSelectOnAnEmptyListDoesNothing(t *testing.T) {
	l := List{Scope: keymap.RepoSelect, OnSelect: func(int) tea.Cmd { return nil }}
	if next, _ := l.Update(press(t, "enter")); next == nil {
		t.Error("enter closed the list although there was nothing to select")
	}
}

func TestListCancelCloses(t *testing.T) {
	l := List{Scope: keymap.RepoSelect, Rows: []string{"api"}}
	if next, _ := l.Update(press(t, "esc")); next != nil {
		t.Error("esc did not close the list")
	}
}

func TestListShowsTheWindowAroundTheCursor(t *testing.T) {
	rows := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10"}
	l := List{Scope: keymap.RepoSelect, Rows: rows, MaxRows: 3, Cursor: 9}
	view := l.View(30)

	if !strings.Contains(view, "> r10") {
		t.Errorf("the selected row is not marked:\n%s", view)
	}
	if strings.Contains(view, "r1\n") {
		t.Errorf("a row outside the window was rendered:\n%s", view)
	}
	if got := len(strings.Split(view, "\n")); got != 3 {
		t.Errorf("rendered %d rows, want the 3 the budget allows:\n%s", got, view)
	}
}

func TestListEmptyState(t *testing.T) {
	l := List{Scope: keymap.RepoSelect, Empty: "No repositories found."}
	view := l.View(40)
	if !strings.Contains(view, "No repositories found.") {
		t.Errorf("view = %q, want the empty state", view)
	}
}

func TestListTruncatesLongRows(t *testing.T) {
	l := List{Scope: keymap.RepoSelect, Rows: []string{strings.Repeat("x", 60)}}
	for _, line := range strings.Split(l.View(20), "\n") {
		if got := layout.Width(line); got > 20 {
			t.Errorf("line of %d columns exceeds the width: %q", got, line)
		}
	}
}

// stubInput is a minimal TextInput that records what it was asked to render.
type stubInput struct {
	value string
	seen  []tea.Msg
}

func (s *stubInput) Update(msg tea.Msg) (TextInput, tea.Cmd) {
	s.seen = append(s.seen, msg)
	return s, nil
}

func (s *stubInput) View() string  { return "[" + s.value + "]" }
func (s *stubInput) Value() string { return s.value }

func TestPromptSubmitsTheEnteredValue(t *testing.T) {
	got := chosen{}
	input := &stubInput{value: "octocat"}
	p := Prompt{
		Scope: keymap.UserInput,
		Input: input,
		OnSubmit: func(v string) tea.Cmd {
			got.value = v
			return nil
		},
	}

	next, _ := p.Update(press(t, "enter"))

	if got.value != "octocat" {
		t.Errorf("submitted %q, want the input's value", got.value)
	}
	if next != nil {
		t.Error("the prompt stayed open after submitting")
	}
}

func TestPromptCancelCloses(t *testing.T) {
	p := Prompt{Scope: keymap.UserInput, Input: &stubInput{}}
	if next, _ := p.Update(press(t, "esc")); next != nil {
		t.Error("esc did not close the prompt")
	}
}

func TestPromptForwardsUnboundKeysToTheInput(t *testing.T) {
	input := &stubInput{}
	p := Prompt{Scope: keymap.UserInput, Input: input}

	for _, key := range []string{"a", "backspace"} {
		msg := press(t, key)
		if next, _ := p.Update(msg); next == nil {
			t.Fatalf("%q closed the prompt", key)
		}
	}
	if len(input.seen) != 2 {
		t.Errorf("the input saw %d messages, want both unbound keys", len(input.seen))
	}
}

func TestPromptRendersTheInputAndAHint(t *testing.T) {
	p := Prompt{
		Scope: keymap.UserInput,
		Intro: []string{"Filter by a GitHub login."},
		Input: &stubInput{value: "octocat"},
	}
	view := p.View(40)

	if !strings.Contains(view, "Filter by a GitHub login.") {
		t.Errorf("view is missing the intro:\n%s", view)
	}
	if !strings.Contains(view, "[octocat]") {
		t.Errorf("view is missing the input:\n%s", view)
	}
	if !strings.Contains(strings.ToLower(view), "empty") {
		t.Errorf("view does not explain how to clear the filter:\n%s", view)
	}
}

func TestCloseAllPopsEveryOverlay(t *testing.T) {
	st := NewStack(plainChrome()).
		Push(&stubOverlay{title: "one"}).
		Push(&stubOverlay{title: "two"})

	if st.Len() != 2 {
		t.Fatalf("stack has %d overlays, want 2", st.Len())
	}
	got, _ := st.Update(CloseAllMsg{})
	if !got.Empty() {
		t.Errorf("stack still holds %d overlays after CloseAllMsg", got.Len())
	}
}

// TestControlsKeepTheFrameGeometry covers every overlay control, not just the
// confirm: a modal body of any shape must composite without changing the
// frame's dimensions.
func TestControlsKeepTheFrameGeometry(t *testing.T) {
	const w, h = 60, 20
	background := strings.Repeat(strings.Repeat(".", w)+"\n", h-1) + strings.Repeat(".", w)

	controls := []Overlay{
		Menu{
			Scope:   keymap.FilterType,
			Heading: "Filter",
			Intro:   []string{"Narrow the list to one user or one repository."},
			Choices: []Choice{
				{Action: keymap.ActionFilterUser, Label: "Filter by user"},
				{Action: keymap.ActionFilterRepo, Label: "Filter by repository"},
			},
		},
		List{Scope: keymap.RepoSelect, Heading: "Repository", Rows: []string{"api", "web"}, MaxRows: 4},
		Prompt{Scope: keymap.UserInput, Heading: "Filter by user", Input: &stubInput{value: "octocat"}},
		Confirm{Scope: keymap.ApproveRun, Heading: "Approve workflow run", Body: []string{"Approve run 1?"}},
	}

	for i, control := range controls {
		view := NewStack(plainChrome()).Push(control).View(background, w, h)
		lines := strings.Split(view, "\n")
		if len(lines) != h {
			t.Errorf("control %d: composited %d lines, want %d", i, len(lines), h)
		}
		for n, line := range lines {
			if got := layout.Width(line); got != w {
				t.Errorf("control %d: line %d is %d columns, want %d", i, n, got, w)
			}
		}
	}
}
