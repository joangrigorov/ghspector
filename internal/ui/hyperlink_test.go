package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ghspector/internal/layout"
)

// TestHyperlinkIsGatedOnTheTheme pins the contract that makes OSC 8 links safe
// to enable everywhere: a theme without the capability renders the text alone,
// byte for byte, so golden files and the plain-theme contract do not change.
func TestHyperlinkIsGatedOnTheTheme(t *testing.T) {
	if got := hyperlink(PlainTheme(), "fix the thing", "https://example.com/1"); got != "fix the thing" {
		t.Errorf("plain theme rendered %q, want the bare text", got)
	}

	linked := hyperlink(DarkTheme(), "fix the thing", "https://example.com/1")
	if !strings.Contains(linked, "https://example.com/1") {
		t.Errorf("linked text %q does not carry the URL", linked)
	}
	if !strings.HasPrefix(linked, ansi.SetHyperlink("https://example.com/1")) {
		t.Errorf("linked text %q does not open with the URL's OSC 8 sequence", linked)
	}
	if !strings.HasSuffix(linked, ansi.ResetHyperlink()) {
		t.Errorf("linked text %q does not close with a hyperlink reset", linked)
	}
}

// TestHyperlinkIsInvisibleToWidthAndClosedByTruncation is the reason links are
// applied to the text and not to a row: a link must not change the geometry of
// a table, and a row that is cut short must not leave the terminal linked.
func TestHyperlinkIsInvisibleToWidthAndClosedByTruncation(t *testing.T) {
	const url = "https://github.com/acme/api/pull/1234"
	text := "a fairly long pull request title"
	linked := hyperlink(DarkTheme(), text, url)

	if got, want := layout.Width(linked), len(text); got != want {
		t.Errorf("linked width = %d, want %d", got, want)
	}

	cut := layout.Truncate(linked, 10)
	if got := layout.Width(cut); got > 10 {
		t.Errorf("truncated link is %d cells wide, want at most 10", got)
	}
	if n := strings.Count(cut, "\x1b]8;;"); n%2 != 0 {
		t.Errorf("truncation left an unterminated hyperlink: %q", cut)
	}
	if !strings.HasSuffix(cut, ansi.ResetHyperlink()) {
		t.Errorf("truncated link ends with %q, want a hyperlink reset", cut)
	}
}
