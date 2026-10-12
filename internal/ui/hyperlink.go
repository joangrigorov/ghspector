package ui

import (
	"github.com/charmbracelet/x/ansi"
)

// hyperlink renders text as an OSC 8 hyperlink pointing at url.
//
// The sequence is invisible to a terminal that does not support OSC 8 — it
// prints the text and ignores the escape — and to every display-width
// calculation, because x/ansi skips it. A theme without Hyperlinks returns the
// text unchanged, which is what keeps a plain render free of escape sequences.
//
// The link is applied to the text only, never to a whole row, so a truncated
// row cannot leave the terminal in a linked state that bleeds into the next
// one. x/ansi's Truncate closes an open hyperlink, and a test pins that.
func hyperlink(theme Theme, text, url string) string {
	if !theme.Hyperlinks || url == "" || text == "" {
		return text
	}
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}
