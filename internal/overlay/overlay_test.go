package overlay

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ghspector/internal/keymap"
	"ghspector/internal/layout"
)

// stubOverlay is a minimal Overlay for exercising the stack.
type stubOverlay struct {
	title    string
	body     string
	scope    keymap.Scope
	lastMsg  string
	replies  func(msg tea.Msg) (Overlay, tea.Cmd)
	viewCall int
}

func (s *stubOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		s.lastMsg = keymap.KeyString(k)
	}
	if s.replies != nil {
		return s.replies(msg)
	}
	return s, nil
}

func (s *stubOverlay) View(width int) string {
	s.viewCall++
	return s.body
}

func (s *stubOverlay) Title() string              { return s.title }
func (s *stubOverlay) Bindings() []keymap.Binding { return keymap.For(s.scope) }

// plainChrome is the deterministic chrome used in tests: no colours, fixed
// padding, so goldens do not depend on terminal colour support.
func plainChrome() Chrome {
	c := DefaultChrome()
	c.BorderColor = nil
	c.TitleColor = nil
	c.BodyColor = nil
	c.Background = nil
	return c
}

// wideSamples are strings whose display width differs from their rune length,
// which is exactly where the v1 rune-slicing compositor went wrong.
var wideSamples = []string{
	"plain ascii",
	"emoji 🚀🌍🔥 pad",
	"cjk 日本語テキスト",
	"combining ééé e\u0301",
	"zwj family 👨‍👩‍👧‍👦",
	"flag 🇯🇵 region",
	"\x1b[31mred text\x1b[0m",
	"\x1b[1;32mbold green\x1b[0m with tail",
	"tab\there",
	"",
	"a",
}

// renderBackground builds a deterministic width×height frame from the sample
// pool so the property test covers wide runes and ANSI in the background too.
func renderBackground(rng *rand.Rand, w, h int) string {
	lines := make([]string, h)
	for y := range lines {
		var sb strings.Builder
		col := 0
		for col < w {
			s := wideSamples[rng.Intn(len(wideSamples))]
			sb.WriteString(s)
			col += lipgloss.Width(s) + 1
			sb.WriteString(" ")
		}
		lines[y] = sb.String()
	}
	return strings.Join(lines, "\n")
}

// TestViewPreservesGeometryOnWideContent is the property that motivates the
// whole package: whatever the background and the overlay contain, the
// composited frame is exactly width×height display cells.
func TestViewPreservesGeometryOnWideContent(t *testing.T) {
	rng := rand.New(rand.NewSource(20261012))
	sizes := [][2]int{{80, 24}, {120, 40}, {40, 15}, {50, 12}, {200, 60}, {20, 8}}

	for _, size := range sizes {
		w, h := size[0], size[1]
		for i := 0; i < 60; i++ {
			bg := renderBackground(rng, w, h)
			o := &stubOverlay{
				title: wideSamples[rng.Intn(len(wideSamples))],
				body:  renderBackground(rng, min(w/2+1, 100), 1+rng.Intn(6)),
			}
			st := NewStack(plainChrome()).Push(o)
			out := st.View(bg, w, h)

			if got := lipgloss.Height(out); got != h {
				t.Fatalf("size %dx%d iter %d: height = %d, want %d", w, h, i, got, h)
			}
			for n, line := range strings.Split(out, "\n") {
				if got := lipgloss.Width(line); got != w {
					t.Fatalf("size %dx%d iter %d line %d: width = %d, want %d\nbg=%q\nout=%q",
						w, h, i, n, got, w, bg, out)
				}
			}
		}
	}
}

// TestViewWithoutOverlaysIsIdentity keeps the fast path honest: an empty stack
// must not touch the frame, so the common case costs nothing.
func TestViewWithoutOverlaysIsIdentity(t *testing.T) {
	bg := "line one\nline two\nline three"
	if got := NewStack(plainChrome()).View(bg, 20, 3); got != bg {
		t.Fatalf("empty stack altered the frame:\n%q", got)
	}
}

// TestViewKeepsTheFrameAroundTheBox is the reason the stack draws through a
// compositor: a modal covers the middle of the screen, and the frame it does not
// cover has to survive. Drawing a styled string clears the area it is given, so
// a box given the whole region erases everything behind it.
func TestViewKeepsTheFrameAroundTheBox(t *testing.T) {
	const w, h = 40, 12

	// A background where every line is identifiable, so a wiped frame is a
	// failing assertion rather than a subtle visual difference.
	lines := make([]string, h)
	for i := range lines {
		lines[i] = fmt.Sprintf("row %02d %s", i, strings.Repeat("-", w))
	}
	bg := layout.Fit(strings.Join(lines, "\n"), w, h)

	o := &stubOverlay{title: "T", body: "body"}
	out := NewStack(plainChrome()).Push(o).View(bg, w, h)

	outLines := strings.Split(out, "\n")
	if len(outLines) != h {
		t.Fatalf("height = %d, want %d", len(outLines), h)
	}
	// The box is centred, so the first and last rows are never covered.
	if !strings.Contains(outLines[0], "row 00") {
		t.Errorf("the modal erased the row above it:\n%s", out)
	}
	if !strings.Contains(outLines[h-1], fmt.Sprintf("row %02d", h-1)) {
		t.Errorf("the modal erased the row below it:\n%s", out)
	}
	// The box itself is still drawn.
	if !strings.Contains(out, "T") {
		t.Errorf("the modal is missing:\n%s", out)
	}
}

// TestViewCentresTheBox pins the position, not just the size: a box drawn at the
// origin looks identical in a geometry assertion and wrong on a screen.
func TestViewCentresTheBox(t *testing.T) {
	const w, h = 40, 10
	o := &stubOverlay{title: "T", body: "body"}
	st := NewStack(plainChrome()).Push(o)

	box := st.renderBox(o, w)
	wantX := (w - lipgloss.Width(box)) / 2
	wantY := (h - lipgloss.Height(box)) / 2

	blank := layout.Fit("", w, h)
	out := strings.Split(st.View(blank, w, h), "\n")
	top := -1
	for i, line := range out {
		if strings.Contains(line, "╭") {
			top = i
			break
		}
	}
	if top != wantY {
		t.Fatalf("box top row = %d, want %d\n%s", top, wantY, strings.Join(out, "\n"))
	}
	if got := strings.Index(out[top], "╭"); got != wantX {
		t.Errorf("box left column = %d, want %d\n%q", got, wantX, out[top])
	}
}

// TestViewDrawsTheTopmostOverlayLast keeps a chain readable: the overlay opened
// last is the one the user is answering.
func TestViewDrawsTheTopmostOverlayLast(t *testing.T) {
	const w, h = 40, 12
	base := &stubOverlay{title: "first", body: "behind"}
	top := &stubOverlay{title: "second", body: "in front"}

	out := NewStack(plainChrome()).Push(base).Push(top).View(renderBackground(rand.New(rand.NewSource(2)), w, h), w, h)
	if !strings.Contains(out, "in front") {
		t.Fatalf("the topmost overlay is not on screen:\n%s", out)
	}
	if strings.Contains(out, "behind") {
		t.Errorf("a deeper overlay was drawn over the topmost one:\n%s", out)
	}
}

// TestViewOffsetsAChainSoBothRead is the fix for a chain that rendered as one
// box with a doubled border: each overlay below the topmost is stepped up and
// to the left, so its corner shows and the chain reads as a stack.
func TestViewOffsetsAChainSoBothRead(t *testing.T) {
	const w, h = 80, 20
	base := &stubOverlay{title: "first", body: "underneath"}
	top := &stubOverlay{title: "second", body: "on top"}

	out := NewStack(plainChrome()).Push(base).Push(top).View(renderBackground(rand.New(rand.NewSource(3)), w, h), w, h)
	lines := strings.Split(out, "\n")

	// Two boxes, drawn from the deepest first, so the corners come in order.
	// The column is measured in display cells, because the border runes are
	// wider than one byte.
	var corners [][2]int
	for row, line := range lines {
		if at := strings.Index(line, "╭"); at >= 0 {
			corners = append(corners, [2]int{row, layout.Width(line[:at])})
		}
	}
	if len(corners) != 2 {
		t.Fatalf("found %d box corners, want both boxes on screen:\n%s", len(corners), out)
	}
	if got := corners[1][0] - corners[0][0]; got != 2 {
		t.Errorf("the chain steps down by %d rows, want 2", got)
	}
	if got := corners[1][1] - corners[0][1]; got != 2 {
		t.Errorf("the chain steps right by %d columns, want 2", got)
	}

	// Each box is drawn whole, and the overlay in front is the one on top: the
	// box behind contributes its corner and nothing over the box in front.
	if got := strings.Count(out, "╰"); got != 2 {
		t.Errorf("found %d bottom borders, want one per box:\n%s", got, out)
	}
	if !strings.Contains(out, "on top") {
		t.Errorf("the topmost overlay is not drawn:\n%s", out)
	}
	if strings.Contains(out, "underneath") {
		t.Errorf("the box behind was drawn over the one in front:\n%s", out)
	}
}

// TestViewOnTinyTerminalDoesNotPanic covers the degenerate sizes that arrive
// while a terminal is being resized.
func TestViewOnTinyTerminalDoesNotPanic(t *testing.T) {
	o := &stubOverlay{title: "Confirm", body: "A long paragraph that will not fit."}
	for _, size := range [][2]int{{1, 1}, {2, 2}, {5, 3}, {10, 4}, {0, 0}, {-1, 5}} {
		w, h := size[0], size[1]
		if h < 1 {
			continue
		}
		st := NewStack(plainChrome()).Push(o)
		out := st.View(strings.Repeat("x", max(w, 0)), w, h)
		if w > 0 && h > 0 {
			if got := lipgloss.Height(out); got != h {
				t.Errorf("size %dx%d: height = %d, want %d", w, h, got, h)
			}
		}
	}
}

// TestNarrowBoxStaysWithinFrame checks that the modal never overflows the
// region it was given, which is how v1 modals ended up wrapping the terminal.
func TestNarrowBoxStaysWithinFrame(t *testing.T) {
	o := &stubOverlay{
		title: "Confirm merge",
		body:  "This will squash-merge the pull request and delete the branch.",
	}
	for w := 20; w <= 200; w += 3 {
		st := NewStack(plainChrome()).Push(o)
		out := st.View(renderBackground(rand.New(rand.NewSource(1)), w, 24), w, 24)
		for _, line := range strings.Split(out, "\n") {
			if got := lipgloss.Width(line); got != w {
				t.Fatalf("width %d: line width %d\n%q", w, got, out)
			}
		}
	}
}

func TestStackPushPopIsLIFO(t *testing.T) {
	a := &stubOverlay{title: "a"}
	b := &stubOverlay{title: "b"}
	st := NewStack(plainChrome())

	if !st.Empty() || st.Len() != 0 || st.Top() != nil {
		t.Fatal("new stack is not empty")
	}

	st1 := st.Push(a)
	st2 := st1.Push(b)

	if st.Len() != 0 {
		t.Errorf("Push mutated the receiver: len = %d", st.Len())
	}
	if st1.Len() != 1 || st2.Len() != 2 {
		t.Fatalf("lens = %d, %d, want 1, 2", st1.Len(), st2.Len())
	}
	if st2.Top() != Overlay(b) {
		t.Error("Top did not return the most recently pushed overlay")
	}

	popped := st2.Pop()
	if popped.Len() != 1 || popped.Top() != Overlay(a) {
		t.Error("Pop did not remove the topmost overlay")
	}
	if st2.Len() != 2 {
		t.Errorf("Pop mutated the receiver: len = %d", st2.Len())
	}

	if got := st.Pop().Len(); got != 0 {
		t.Errorf("Pop on empty stack changed len to %d", got)
	}
}

func TestPushNilIsIgnored(t *testing.T) {
	st := NewStack(plainChrome()).Push(nil)
	if !st.Empty() {
		t.Fatal("Push(nil) added an overlay")
	}
}

// TestUpdateRoutesOnlyToTopmost is the input-isolation invariant: the route
// underneath must not observe keystrokes meant for the modal.
func TestUpdateRoutesOnlyToTopmost(t *testing.T) {
	bottom := &stubOverlay{title: "bottom"}
	top := &stubOverlay{title: "top", scope: keymap.MergeConfirm}
	st := NewStack(plainChrome()).Push(bottom).Push(top)

	_, _ = st.Update(tea.KeyPressMsg{Text: "y", Code: 'y'})
	if top.lastMsg != "y" {
		t.Errorf("topmost saw %q, want y", top.lastMsg)
	}
	if bottom.lastMsg != "" {
		t.Errorf("lower overlay saw %q, want nothing", bottom.lastMsg)
	}
}

func TestReturningNilPops(t *testing.T) {
	o := &stubOverlay{replies: func(tea.Msg) (Overlay, tea.Cmd) { return nil, nil }}
	st := NewStack(plainChrome()).Push(&stubOverlay{}).Push(o)

	next, cmd := st.Update(tea.KeyPressMsg{Text: "y", Code: 'y'})
	if next.Len() != 1 {
		t.Fatalf("len after nil return = %d, want 1", next.Len())
	}
	if cmd != nil {
		t.Error("nil return produced a non-nil command")
	}
}

func TestReturningReplacementSwapsInPlace(t *testing.T) {
	replacement := &stubOverlay{title: "replacement"}
	o := &stubOverlay{replies: func(tea.Msg) (Overlay, tea.Cmd) { return replacement, nil }}
	st := NewStack(plainChrome()).Push(&stubOverlay{}).Push(o)

	next, _ := st.Update(tea.KeyPressMsg{Text: "y", Code: 'y'})
	if next.Len() != 2 {
		t.Fatalf("len after replacement = %d, want 2", next.Len())
	}
	if next.Top() != Overlay(replacement) {
		t.Error("replacement overlay is not on top")
	}
}

func TestStackControlMessages(t *testing.T) {
	a := &stubOverlay{title: "a"}
	b := &stubOverlay{title: "b"}

	st := NewStack(plainChrome()).Push(a)

	st, _ = st.Update(PushMsg{Overlay: b})
	if st.Len() != 2 || st.Top() != Overlay(b) {
		t.Fatal("PushMsg did not push")
	}

	c := &stubOverlay{title: "c"}
	st, _ = st.Update(ReplaceMsg{Overlay: c})
	if st.Len() != 2 || st.Top() != Overlay(c) {
		t.Fatal("ReplaceMsg did not replace")
	}

	st, _ = st.Update(CloseMsg{})
	if st.Len() != 1 || st.Top() != Overlay(a) {
		t.Fatal("CloseMsg did not pop")
	}
}

func TestBindingsReflectTopmost(t *testing.T) {
	if got := NewStack(plainChrome()).Bindings(); got != nil {
		t.Errorf("empty stack bindings = %v, want nil", got)
	}
	st := NewStack(plainChrome()).Push(&stubOverlay{scope: keymap.MergeConfirm})
	got := st.Bindings()
	want := keymap.For(keymap.MergeConfirm)
	if len(got) != len(want) {
		t.Fatalf("bindings len = %d, want %d", len(got), len(want))
	}
}

// TestConfirmDispatchFromKeymap drives every key the confirm's scope declares,
// so the modal and its footer can never disagree.
func TestConfirmDispatchFromKeymap(t *testing.T) {
	for _, scope := range []keymap.Scope{
		keymap.MergeConfirm,
		keymap.CloseConfirm,
		keymap.ApproveRun,
	} {
		for _, b := range keymap.For(scope) {
			if b.Action != keymap.ActionConfirm &&
				b.Action != keymap.ActionBack &&
				b.Action != keymap.ActionOpenBrowser {
				continue
			}
			for _, k := range b.Keys {
				yes, no, open := false, false, false
				c := Confirm{
					Scope:  scope,
					OnYes:  func() tea.Msg { yes = true; return nil },
					OnNo:   func() tea.Msg { no = true; return nil },
					OnOpen: func() tea.Msg { open = true; return nil },
				}
				msg, ok := pressFor(k)
				if !ok {
					continue
				}
				next, cmd := c.Update(msg)
				if next != nil {
					t.Errorf("scope %q key %q: overlay did not close", scope, k)
				}
				if cmd != nil {
					cmd()
				}
				switch b.Action {
				case keymap.ActionConfirm:
					if !yes || no || open {
						t.Errorf("scope %q key %q: yes=%v no=%v open=%v, want yes", scope, k, yes, no, open)
					}
				case keymap.ActionBack:
					if yes || !no || open {
						t.Errorf("scope %q key %q: yes=%v no=%v open=%v, want no", scope, k, yes, no, open)
					}
				case keymap.ActionOpenBrowser:
					if yes || no || !open {
						t.Errorf("scope %q key %q: yes=%v no=%v open=%v, want open", scope, k, yes, no, open)
					}
				}
			}
		}
	}
}

// pressFor builds the KeyPressMsg for a bound key string.
func pressFor(key string) (tea.KeyPressMsg, bool) {
	switch key {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}, true
	}
	if len(key) != 1 {
		return tea.KeyPressMsg{}, false
	}
	r := rune(key[0])
	if r >= 'A' && r <= 'Z' {
		return tea.KeyPressMsg{Code: r + ('a' - 'A'), ShiftedCode: r}, true
	}
	return tea.KeyPressMsg{Text: key, Code: r}, true
}

// TestConfirmIgnoresUnboundKeys keeps stray input from dismissing a modal that
// guards a destructive action.
func TestConfirmIgnoresUnboundKeys(t *testing.T) {
	c := Confirm{Scope: keymap.MergeConfirm}
	for _, k := range []tea.KeyPressMsg{
		{Text: "j", Code: 'j'},
		{Text: "q", Code: 'q'},
		{Code: tea.KeyUp},
	} {
		next, cmd := c.Update(k)
		if next == nil {
			t.Fatalf("key %v dismissed the modal", k)
		}
		if cmd != nil {
			t.Errorf("key %v produced a command", k)
		}
	}
}

// TestConfirmViewWrapsToWidth checks the body respects the text column.
func TestConfirmViewWrapsToWidth(t *testing.T) {
	c := Confirm{
		Heading: "Confirm merge",
		Body:    []string{"This will squash-merge pull request #16 and delete the branch."},
	}
	for _, w := range []int{20, 40, 82} {
		out := c.View(w)
		for _, line := range strings.Split(out, "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: line width %d (%q)", w, got, line)
			}
		}
	}
	if got := c.View(0); got == "" {
		t.Error("View(0) dropped the body")
	}
}

func TestConfirmWithNoBodyRendersEmpty(t *testing.T) {
	c := Confirm{Heading: "Confirm"}
	if got := c.View(40); got != "" {
		t.Errorf("View = %q, want empty", got)
	}
}

// TestChromeZeroValueIsUsable guards against a nil border panicking when a
// caller constructs Chrome{} directly.
func TestChromeZeroValueIsUsable(t *testing.T) {
	st := NewStack(Chrome{}).Push(&stubOverlay{title: "t", body: "b"})
	out := st.View("bg", 40, 10)
	if out == "" {
		t.Fatal("zero-value chrome produced nothing")
	}
	if got := lipgloss.Height(out); got != 10 {
		t.Errorf("height = %d, want 10", got)
	}
}

func TestInnerWidthClamping(t *testing.T) {
	c := DefaultChrome()
	c.MaxInner, c.MinInner = 40, 10

	cases := []struct{ avail, want int }{
		{200, 40},
		{46, 40},
		{44, 38},
		{20, 14},
		{10, 10},
		{4, 10},
		{1, 10},
	}
	for _, tc := range cases {
		if got := c.InnerWidth(tc.avail); got != tc.want {
			t.Errorf("InnerWidth(%d) = %d, want %d", tc.avail, got, tc.want)
		}
	}
}

func TestEmptyTitleAddsNoHeading(t *testing.T) {
	if out := NewStack(plainChrome()).View("", 40, 8); out != "" {
		t.Fatalf("empty stack view = %q", out)
	}
}

// TestTitleAddsExactlyOneHeadingAndBlank measures the box directly, because a
// configured stack always returns a fixed-size canvas.
func TestTitleAddsExactlyOneHeadingAndBlank(t *testing.T) {
	body := "just a body"
	st := NewStack(plainChrome())

	noTitle := st.renderBox(&stubOverlay{body: body}, 60)
	withTitle := st.renderBox(&stubOverlay{title: "T", body: body}, 60)

	if got := lipgloss.Height(withTitle) - lipgloss.Height(noTitle); got != 2 {
		t.Errorf("heading added %d lines, want 2 (heading + blank)\nno title:\n%s\nwith title:\n%s",
			got, noTitle, withTitle)
	}
	if withTitle == noTitle {
		t.Error("title had no effect on the rendered box")
	}
}

func TestRenderBoxUsesDeclaredTitle(t *testing.T) {
	o := &stubOverlay{title: "Heading Here", body: "body"}
	st := NewStack(plainChrome()).Push(o)
	out := st.View("", 60, 12)
	if !strings.Contains(out, "Heading Here") {
		t.Errorf("title missing from:\n%s", out)
	}
	if o.viewCall == 0 {
		t.Error("View was never called")
	}
}

func TestFitLinesEquivalentThroughCanvas(t *testing.T) {
	// A body with a very long line and a very short one must still produce a
	// rectangular interior.
	o := &stubOverlay{
		title: "T",
		body:  "short\n" + strings.Repeat("long ", 40),
	}
	st := NewStack(plainChrome()).Push(o)
	out := st.View("", 80, 30)
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if got := lipgloss.Width(line); got != 80 {
			t.Fatalf("line %d width = %d\n%s", i, got, out)
		}
	}
}
