package keymap

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestNoDuplicateKeysInScope guards the invariant that one physical key does not
// trigger two actions in the same scope. This is the bug class that made the old
// TUI unpredictable: a duplicate silently shadowed whichever binding the switch
// happened to test last.
func TestNoDuplicateKeysInScope(t *testing.T) {
	for scope, bindings := range table {
		seen := make(map[string]Action)
		for _, b := range bindings {
			for _, k := range b.Keys {
				if prev, dup := seen[k]; dup {
					t.Errorf("scope %q: key %q bound to both %q and %q", scope, k, prev, b.Action)
				}
				seen[k] = b.Action
			}
		}
	}
}

// TestEveryBindingIsWellFormed checks the fields the footer and help overlay
// depend on: at least one key, and a declared action.
func TestEveryBindingIsWellFormed(t *testing.T) {
	for scope, bindings := range table {
		for i, b := range bindings {
			if b.Action == "" {
				t.Errorf("scope %q binding %d has no action", scope, i)
			}
			if len(b.Keys) == 0 {
				t.Errorf("scope %q action %q has no keys", scope, b.Action)
			}
			for _, k := range b.Keys {
				if k == "" {
					t.Errorf("scope %q action %q has an empty key", scope, b.Action)
				}
			}
		}
	}
}

// TestHelpOrderCoversEveryScope keeps the help overlay from silently dropping a
// scope when a new one is added.
func TestHelpOrderCoversEveryScope(t *testing.T) {
	inHelp := make(map[Scope]bool, len(helpOrder))
	for _, s := range helpOrder {
		if inHelp[s] {
			t.Errorf("scope %q appears twice in helpOrder", s)
		}
		inHelp[s] = true

		if _, ok := table[s]; !ok {
			t.Errorf("helpOrder lists scope %q which has no bindings", s)
		}
		if _, ok := titles[s]; !ok {
			t.Errorf("scope %q has no title", s)
		}
	}

	for s := range table {
		if !inHelp[s] {
			t.Errorf("scope %q has bindings but is missing from helpOrder", s)
		}
	}
}

// TestEveryBindingIsReachableByLookup asserts Lookup resolves each binding's own
// keys, which is what route dispatch relies on.
func TestEveryBindingIsReachableByLookup(t *testing.T) {
	for scope, bindings := range table {
		for _, b := range bindings {
			for _, k := range b.Keys {
				got, ok := Lookup(scope, k)
				if !ok {
					t.Errorf("scope %q key %q not found", scope, k)
					continue
				}
				if got != b.Action {
					t.Errorf("scope %q key %q resolved to %q, want %q", scope, k, got, b.Action)
				}
			}
		}
	}
}

func TestLookupUnknownKey(t *testing.T) {
	if _, ok := Lookup(MainWorkflows, "zzz"); ok {
		t.Fatal("Lookup found an action for an unbound key")
	}
	if _, ok := Lookup(Scope("no.such.scope"), "j"); ok {
		t.Fatal("Lookup found an action in an unknown scope")
	}
}

// TestDocumentedKeysFromSpec pins the bindings the spec calls out by name, so a
// refactor that quietly drops one fails here rather than in the help overlay.
func TestDocumentedKeysFromSpec(t *testing.T) {
	cases := []struct {
		scope Scope
		key   string
		want  Action
	}{
		// 4.1 Workflows.
		{MainWorkflows, "j", ActionDown},
		{MainWorkflows, "k", ActionUp},
		{MainWorkflows, "down", ActionDown},
		{MainWorkflows, "up", ActionUp},
		{MainWorkflows, "enter", ActionOpen},
		{MainWorkflows, "r", ActionRefresh},
		{MainWorkflows, "ctrl+r", ActionRefresh},
		{MainWorkflows, "m", ActionFilterMine},
		{MainWorkflows, "f", ActionFilterType},
		{MainWorkflows, "w", ActionOpenBrowser},
		{MainWorkflows, "a", ActionApprove},
		{MainWorkflows, "x", ActionClearFilters},
		{MainWorkflows, "tab", ActionNextTab},
		{MainWorkflows, "shift+tab", ActionPrevTab},
		// 4.2 PRs.
		{MainPRs, "s", ActionCycleState},
		{MainPRs, "a", ActionFilterAuthor},
		{MainPRs, "i", ActionFilterAssignee},
		{MainPRs, "v", ActionFilterReviewer},
		// 4.3 Issues.
		{MainIssues, "s", ActionCycleState},
		{MainIssues, "a", ActionFilterAuthor},
		{MainIssues, "i", ActionFilterAssignee},
		// 4.4 Filter type overlay.
		{FilterType, "u", ActionFilterUser},
		{FilterType, "U", ActionFilterUser},
		{FilterType, "r", ActionFilterRepo},
		{FilterType, "R", ActionFilterRepo},
		{FilterType, "esc", ActionBack},
		// 4.7 PR/issue filter type.
		{PRFilterType, "a", ActionFilterAuthor},
		{PRFilterType, "i", ActionFilterAssignee},
		{PRFilterType, "r", ActionFilterReviewer},
		{PRFilterType, "c", ActionBack},
		{PRFilterType, "C", ActionBack},
		{IssueFilterType, "a", ActionFilterAuthor},
		{IssueFilterType, "i", ActionFilterAssignee},
		{IssueFilterType, "C", ActionBack},
		// 4.8 Jobs: the authoritative mapping from spec D3.
		{Jobs, "w", ActionOpenBrowser},
		{Jobs, "W", ActionOpenBrowser},
		{Jobs, "v", ActionOpen},
		{Jobs, "V", ActionOpen},
		{Jobs, "esc", ActionBack},
		{Jobs, "backspace", ActionBack},
		// PR details.
		{PRDetails, "m", ActionMerge},
		{PRDetails, "c", ActionComments},
		{PRDetails, "C", ActionClose},
		{PRDetails, "v", ActionCommits},
		{PRDetails, "D", ActionDiff},
		{PRDetails, "w", ActionOpenBrowser},
	}

	for _, tc := range cases {
		got, ok := Lookup(tc.scope, tc.key)
		if !ok {
			t.Errorf("scope %q: key %q is unbound, want %q", tc.scope, tc.key, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("scope %q: key %q resolved to %q, want %q", tc.scope, tc.key, got, tc.want)
		}
	}
}

// TestKeysForRendersDisplayForms checks the footer text a person reads.
func TestKeysForRendersDisplayForms(t *testing.T) {
	cases := []struct {
		b    Binding
		want string
	}{
		{Binding{Keys: []string{"j", "down"}}, "j/↓"},
		{Binding{Keys: []string{"shift+tab"}}, "Shift+Tab"},
		{Binding{Keys: []string{"w", "W"}}, "w/W"},
		{Binding{Keys: []string{}}, ""},
		{Binding{Keys: []string{"ctrl+r"}}, "Ctrl+R"},
	}
	for _, tc := range cases {
		if got := KeysFor(tc.b); got != tc.want {
			t.Errorf("KeysFor(%v) = %q, want %q", tc.b.Keys, got, tc.want)
		}
	}
}

// TestKeyStringLowerCase uses the plain path: a lowercase letter arrives as Text.
func TestKeyStringLowerCase(t *testing.T) {
	msg := tea.KeyPressMsg{Text: "a", Code: 'a'}
	if got := KeyString(msg); got != "a" {
		t.Fatalf("KeyString = %q, want %q", got, "a")
	}
}

// TestKeyStringUpperCaseFromText covers terminals that deliver the shifted
// character in Text.
func TestKeyStringUpperCaseFromText(t *testing.T) {
	msg := tea.KeyPressMsg{Text: "C", Code: 'c', ShiftedCode: 'C'}
	if got := KeyString(msg); got != "C" {
		t.Fatalf("KeyString = %q, want %q", got, "C")
	}
}

// TestKeyStringRecoversUppercaseFromShiftedCode is the regression test for the
// Kitty keyboard protocol case: Text is empty, so msg.String() would report "c"
// and a "C" binding would never fire.
func TestKeyStringRecoversUppercaseFromShiftedCode(t *testing.T) {
	for _, r := range []rune{'A', 'C', 'D', 'W', 'U', 'V', 'Y', 'N'} {
		msg := tea.KeyPressMsg{Code: r + ('a' - 'A'), ShiftedCode: r}
		if got := KeyString(msg); got != string(r) {
			t.Errorf("KeyString(ShiftedCode=%q) = %q, want %q", r, got, string(r))
		}
	}
}

// TestKeyStringPassesThroughSpecialKeys keeps normalisation from altering the
// canonical special-key spellings.
func TestKeyStringPassesThroughSpecialKeys(t *testing.T) {
	cases := []struct {
		msg  tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: tea.KeyEscape}, "esc"},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, "enter"},
		{tea.KeyPressMsg{Code: tea.KeyTab}, "tab"},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "shift+tab"},
		{tea.KeyPressMsg{Code: tea.KeyBackspace}, "backspace"},
		{tea.KeyPressMsg{Code: tea.KeyUp}, "up"},
		{tea.KeyPressMsg{Code: tea.KeyDown}, "down"},
		{tea.KeyPressMsg{Code: tea.KeyPgUp}, "pgup"},
		{tea.KeyPressMsg{Code: tea.KeyPgDown}, "pgdown"},
		{tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "ctrl+c"},
		{tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, "ctrl+r"},
	}
	for _, tc := range cases {
		if got := KeyString(tc.msg); got != tc.want {
			t.Errorf("KeyString(%v) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

// TestKeyStringIsIdempotentWithLookup ties normalisation to the table: for every
// documented key, feeding a synthetic press through KeyString then Lookup must
// resolve to the same action the literal key string does. This is what makes the
// uppercase bindings (C, D, W, U, V, Y, N) actually reachable.
func TestKeyStringIsIdempotentWithLookup(t *testing.T) {
	for scope, bindings := range table {
		for _, b := range bindings {
			for _, k := range b.Keys {
				msg, ok := syntheticPress(k)
				if !ok {
					continue // key string with no direct KeyPressMsg encoding
				}
				got := KeyString(msg)
				if got != k {
					t.Errorf("scope %q: KeyString of synthetic %q = %q", scope, k, got)
					continue
				}
				if a, found := Lookup(scope, got); !found || a != b.Action {
					t.Errorf("scope %q: round-trip of %q resolved to %q (found=%v), want %q",
						scope, k, a, found, b.Action)
				}
			}
		}
	}
}

// syntheticPress builds the KeyPressMsg a terminal would send for a bound key
// string, for the subset the test can construct deterministically.
func syntheticPress(key string) (tea.KeyPressMsg, bool) {
	switch key {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}, true
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, true
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}, true
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}, true
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}, true
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}, true
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}, true
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, true
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, true
	case "?":
		return tea.KeyPressMsg{Text: "?", Code: '?'}, true
	case "[":
		return tea.KeyPressMsg{Text: "[", Code: '['}, true
	case "]":
		return tea.KeyPressMsg{Text: "]", Code: ']'}, true
	}

	if len(key) != 1 {
		return tea.KeyPressMsg{}, false
	}
	r := rune(key[0])
	if r >= 'A' && r <= 'Z' {
		// Uppercase works two ways; test the harder one, Text empty.
		return tea.KeyPressMsg{Code: r + ('a' - 'A'), ShiftedCode: r}, true
	}
	return tea.KeyPressMsg{Text: key, Code: r}, true
}

// TestTitlesAreDistinct keeps breadcrumbs unambiguous.
func TestTitlesAreDistinct(t *testing.T) {
	seen := make(map[string]Scope)
	for scope, title := range titles {
		if prev, dup := seen[title]; dup {
			t.Errorf("title %q used by both %q and %q", title, prev, scope)
		}
		seen[title] = scope
	}
}

// TestAllActionsNonEmpty guards the helper tests rely on.
func TestAllActionsNonEmpty(t *testing.T) {
	actions := AllActions()
	if len(actions) == 0 {
		t.Fatal("AllActions returned nothing")
	}
	for a := range actions {
		if a == "" {
			t.Fatal("AllActions contains an empty action")
		}
	}
}

func BenchmarkLookup(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Lookup(MainWorkflows, "j")
	}
}

func TestDisplayUnknownKeyFallsBackToWireForm(t *testing.T) {
	if got := Display("zz"); got != "zz" {
		t.Fatalf("Display(zz) = %q, want %q", got, "zz")
	}
}
