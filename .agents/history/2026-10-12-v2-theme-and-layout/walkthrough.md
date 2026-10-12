# fix(tui): one highlight, one palette, no wordmark

The fix cycle that follows the Phase 3 review (#19). The review found no missing
capability; it found the palette and the highlight applied differently on
different screens, one layout that did not need its columns, and a header that
said nothing. This change closes all of it, and answers the markdown question by
scheduling it rather than folding it in.

Parent plan: the phased plan for the v2 rewrite. Markdown rendering stays a
separate change, with the reason recorded in `docs/spec.md` §7.

## What changed

| File | What it is |
| --- | --- |
| `internal/ui/theme.go` | The palette. `Theme.Selected` and `Theme.SelectedBackground` are gone — the pair that caused the light-on-light row. Both themes now carry a sky-blue accent (`#87d7ff` dark, `#005f87` light), and the muted greys are chosen against each theme's own background. `ThemeFor(dark)` is the one place the choice lives. |
| `internal/ui/chrome.go` | The one highlight: `selectedRow` puts the shared marker (`› `) in the gutter and the row's text in the accent colour and bold, re-opening the accent after a nested span so a state marker keeps its own colour. The header lost its wordmark, and `overlayChrome` moved here so the palette and the modal chrome are derived from the same answer. |
| `internal/ui/model.go` | The palette follows the terminal: `tea.RequestBackgroundColor` in `Init`, `tea.BackgroundColorMsg` → `withTheme`, the query repeated on `tea.FocusMsg` with `View.ReportFocus` on. `withTheme` rebuilds the theme *and* the overlay chrome. |
| `internal/overlay/overlay.go` | `Stack.SetChrome`, so a modal already on the stack changes with the screen behind it. |
| `cmd/ghspector/main.go` | The startup probe is gone: `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)` used to decide the palette before Bubble Tea started, which is why a scheme change mid-session was never noticed. `ui.DarkTheme()` is now only the first frame's palette. |
| `internal/ui/prdetails.go` | The Info sub-tab is two columns: the description, and a sidebar with the details block above the checks. `detailsRows` is the block's own height, so it is drawn at 80×24. Check rows leave their text unstyled for the shared highlight. |
| `internal/ui/logs.go` | The step list builds its rows the way every table does — `tableIndent` on a plain row, `selectMarker` on the selected one — instead of its own ` > ` gutter. |
| `internal/ui/{mainroute,switcher,prs,jobs,runs,runs,prpanes}.go` | Their own cursor styling is gone; each row is handed to `selectedRow`. |
| `internal/ui/theme_test.go` | New. The contrast measurement, the `selectedRow` unit tests, the background-change equivalence test, and the header test. |
| `docs/{spec,architecture}.md` | §4.10 records the two-column Info row, §7 records markdown as its own change with its rules, and the architecture conventions gain rules 9–11. |

## The five defects the review found

Each is a defect this build had, and each is pinned by a test that fails on the
old code.

1. **A highlighted row vanished on a light terminal.** The switcher drew its
   cursor row as `theme.Selected` — `#ffffff` — as a *foreground* with no
   background, so the selected account was white on white. The fix is not a new
   white: it is the removal of the field. There is now one highlight in the
   program (`selectedRow`), it is a marker plus the accent colour, and the accent
   is the same hue in both palettes at a contrast each background can carry.
2. **The theme froze at startup, so a scheme change broke the screen it found.**
   `cmd/ghspector` sampled the background once, before the runtime started, and
   nothing re-sampled it: switching the OS to dark while the program ran left the
   light palette's near-black text on a dark terminal — the "PR details values and
   checks are invisible" report. The model now asks the terminal (`Init`, and
   again on focus), and rebuilds the palette and the modal chrome from the answer.
   Exiting and restarting was correct before because the probe ran again; now both
   paths agree, and a test asserts it: the screen after a background change is
   required to be *identical* to a startup in that scheme.
3. **The highlight was not one treatment.** The log viewer's step list drew ` > `
   in a gutter the tables did not use, so the cursor looked different in the job
   view than in the account switcher — and it carried the only background bar in
   the program. Every list now builds rows the same way.
4. **The Info row gave the details and the checks a column each.** Three columns
   left the description narrow and the details short of rows at every size. The
   sidebar stacks them now, and the description keeps the width the third column
   would have taken. While fixing it, `detailsRows` was found to be one row too
   tall for 80×24 — the details were dropped on the size the review was read at —
   so the block is drawn at its own height and is visible there.
5. **The header carried a wordmark.** `ghspector  │  <trail>` spent seven columns
   of the header saying what the splash and the window title already say. The
   header starts with the trail now; the account and the rate limit keep the room.

## The two tweaks from the second pass

A second, smaller read of the same screens produced two layout notes and one
fragile test.

1. **The checks heading was glued to the details block.** The sidebar stacked the
   two blocks with nothing between them, so `CHECKS` sat immediately under the
   last value above it — and under a value that wraps, immediately under its
   second line. The column now holds a blank row between the blocks
   (`sideGapRows`), and `checksMinRows` came down from five to four so that
   80×24 still fits all three parts: ten rows of details, the blank, four rows of
   checks. Fifteen is exactly the Info body at 80×24, which is why the two
   constants and the gap have to be read together.
2. **The account moved to the right-hand edge.** `Account: <owner>` sat directly
   after the trail, where its position moved whenever the trail grew. It is now
   the last thing on the band, with the rate limit written before it — so the
   account stays put as the trail changes length, and the count's own width
   changes (a remaining request crossing a power of ten, or the reset time
   appearing) move the count instead of the name.
3. **A flow test's marker arrived split.** With the sidebar changed, the merge
   method overlay's body partly coincided with the screen behind it, so the
   renderer wrote `Squash and merge` in two runs separated by a cursor move and
   the test's literal match timed out. The product was correct — the box and its
   text are both in the stream — but the marker was. The three copies of the wait
   closure are now one `waitForText` helper that matches against the stream with
   its escape sequences removed, which is what the file's own comment said the
   markers needed and what the diff renderer makes unreliable.

Named plainly: the third item is a test fix. It was found by this change, and it
would have failed eventually on any screen whose patches lined up that way.

## A note on the accent and on "no colour is the only signal"

The purple is gone with `Selected`. Both palettes now use one accent — a sky blue
— for the header trail, the pane that owns the keyboard, key hints and the
highlighted row. The status colours (green, amber, red) keep their own hues, and
they are never the only signal: a status also draws a glyph, and the highlighted
row also draws a marker. That rule is written into `theme.go` and into the
architecture conventions, because it is what makes a palette swap safe.

`internal/ui/theme_test.go` measures every colour against the surface it is drawn
on: 3:1, the WCAG minimum for a glyph or large text. Amber on white sits at 3.3:1,
which is why the threshold is not 4.5:1 — a stricter ratio would rule out the
colour of "waiting". The rules and borders are checked for presence rather than
for contrast, since a separator that matches its surface is a separator that is
not there.

## Markdown

Not in this change. It is the one place the program formats text it did not
write, so it gets its own change with its own review. `charm.land/glamour/v2`
v2.0.1 is available, is v2-clean, and depends only on the v2 Charm modules. The
rules are recorded in `docs/spec.md` §7 and architecture convention 9: one
renderer per (scheme, width), a style fixed at construction, the renderer built
when the scheme or the width changes and reused, and no terminal query and no
renderer construction inside a view. The baseline's markdown renderer called
`lipgloss.HasDarkBackground()` inside the render path — a blocking probe per frame
— which is how one version rendered markdown well and was unusable at the same
time. That is the defect this schedule exists to not repeat, and it is the same
defect this change removed from the palette.

## Verification

This is a TUI, so browser verification does not apply and has no equivalent here.
The substitute is the same one the phases used, stated plainly: scripted
`teatest` runs through the real Bubble Tea runtime, golden renders of every
screen, property tests for the geometry, and the unit tests below — plus, for
this change specifically, renders of the real screens with the real palettes,
read as escape sequences, because "is this colour visible" is precisely the
question a plain golden cannot answer. What that leaves unverified is a human's
impression of the two palettes on a real terminal and the feel of the new
highlight, which is the reader's step.

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -race -count=1 ./...` | pass — 293 test functions, 423 leaf runs, in the 9 packages that have tests |
| `golangci-lint run` | 0 issues |
| `govulncheck ./...` | 14 findings, all standard library (F5, pre-existing; identical on `main`) |
| `actionlint` | pass |
| `go mod tidy -diff` | clean |
| `gofmt -l` | clean except `internal/gh/{client,client_test,types}.go`, whose drift predates the rewrite and is left alone |
| `./ghspector -v` | prints `ghspector <version>`, `commit: <sha>`, `build date: <date>` and exits 0 |
| CI `Test and Lint` on the PR | pass on every commit of the branch: 1m00s on `e6845c0`, 1m04s on `87c905b` |

Screen and flow coverage for this change:

- **The palette**: every colour measured against the surface it is drawn on, in
  both themes.
- **The background change**: the screen after a `tea.BackgroundColorMsg` is
  required to be identical to a startup in that scheme, both for the main screen
  and for a modal opened over it — which is what covers the overlay stack, the
  part built from the theme at construction.
- **The highlight**: `selectedRow` is asserted to open with the marker in the
  accent, to draw the row's text in accent and bold after a nested span, to keep
  the state marker's own colour, and to leave the row's width unchanged. A plain
  theme is asserted to emit bold and no colour at all, which is what keeps the
  goldens readable.
- **The header**: the first line of a live render names the screen and the
  account and does not name the program.
- **The Info row**: DETAILS, State and CHECKS are all present at 80×24, the row
  above the CHECKS heading is blank in the sidebar, and the existing property
  test still tiles every width from 12 to 200 and every height from 6 to 20 with
  no doubled separator.
- **The header band**: the trail is the left-hand end, the account is the
  right-hand end, and with a rate limit on the band (a fixture that sends
  `X-RateLimit-*`, which the shared fixtures do not) the count is written before
  the account. The placement assertion was checked against the old layout, where
  it fails.

The renders read during this change, with the real palettes:

| Screen | Light | Dark |
| --- | --- | --- |
| Account switcher | `› octocat` in `#005f87` bold on white; `(account)` muted; `← active` green | `› octocat` in `#87d7ff` bold |
| Job list | marker `› ` accent, the failing `■` in `#af0000`, the row text accent and bold | same shape, `■` in `#df5f5f` |
| Log viewer steps | selected step `› ■ Run tests`, the only background in the program is the header band | same |
| PR Info | values `#262626` on white, checks `#5fd75f` in the dark scheme | values `#e4e4e4`, checks `#5fd75f` |
| Header, 100 wide with `Rate limit: 4999/5000` | trail left, count, then `Account: octocat` at the edge | same |

All 38 goldens were regenerated, and every changed line was read: the wordmark
gone from the header and the account moved to its right-hand edge, the `› ` marker
in place of the old gutters, the Info row two columns wide with a blank row before
CHECKS, and the log step gutter aligned with the tables.

## Notes for the review

- `./ghspector` at the project root is the binary for this commit, built locally
  and gitignored. `./ghspector -v` reports the commit it was built from.
- PR #19 was merged before this branch started, as requested.
- Not merged, by design: PR #15 (`chore(main): release 0.4.0`).
- The theme change is the one part a golden cannot pin, because goldens render
  with the plain theme. It is covered by the contrast test, the equivalence test
  and the byte-level reads above.
