# fix(ui): one content box for every screen

Two spacing defects, one cause. The body of a detail screen — the description
and the DETAILS/CHECKS sidebar — ran from column zero to the last column, while
the bars above and below it, the heading, the tab strip and every table were
inset. On top of that, the sidebar's own labels sat on the rule that divides the
columns: `│DETAILS`, `│CHECKS`.

Both are fixed by naming the inset and giving it one owner. `layout.Gutter` is
two cells; one screen's rows now share one box, from cell 2 to `width-3`, and
`TestEveryScreenKeepsTheGutter` walks fifteen screens at three sizes and fails on
a row that writes into either gutter.

Branch `fix/ui-content-gutter`, branched from `main` at `2e5b9a9` (the squash of
#25).

## What changed

| File | What it is |
| --- | --- |
| `internal/layout/layout.go` | `Gutter` (2 cells), `ContentWidth` (the text area of a screen: `width-2*Gutter`) and `BoxWidth` (the width of a block that spans the screen and carries its own left gutter: `width-Gutter`). |
| `internal/ui/model.go` | A route renders into `layout.BoxWidth` of the terminal rather than all of it, so the shell's `Fit` fills the right gutter. The left gutter stays with the surface, because a list draws its selection marker in it. |
| `internal/ui/chrome.go` | The header and the footer measure with `layout.ContentWidth` and draw through `gutterBlock`, so the bars and the rules sit in the same box as the content. `paneRect` and `gutterRows` are the pair a route uses to move a block of panes into that box without doing the arithmetic itself. |
| `internal/ui/prdetails.go` | `body` renders every sub-tab into `paneRect(r)` and shifts the block, which puts the panes on the heading's left edge. The sidebar's panes are inset by `sidebarPad`, so `DETAILS` and `CHECKS` clear the column rule. `bodyWidth` is the pane width now rather than the terminal's, and the description clamps against `descriptionWidth`, the width the pane is actually drawn at. |
| `internal/ui/issuedetails.go` | The same two changes for the issue screen, the same width fix for its description and for the issue conversation. |
| `internal/ui/prpanes.go` | The commit screen's message and file panes are one block, so they move together; its heading rule is measured from the gutter. |
| `internal/ui/help.go` | The reference is one block and moves as one, so a section heading starts where the bars' text does. |
| `internal/ui/jobs.go` | The job heading's rule is measured from the gutter instead of a literal 4. |
| `internal/ui/gutter_test.go` | New. The invariant, plus the pane geometry: a block rendered into a rect one gutter too narrow leaves the sidebar two cells short and clips it, which only a test that measures the column rule would catch. |
| `internal/layout/layout_test.go` | `BoxWidth` and `ContentWidth` on the sizes the goldens use, and the two stay `Gutter` apart at every width. |
| `docs/architecture.md` | The content box in §3.1, and convention 14. |

54 golden files are re-rendered. Every line of a screen changed, because a row is
padded to the terminal's width and the box moved: the bars' text ends two cells
earlier, the rules are two cells shorter at each end, the tables' last column is
two cells narrower, and the panes are two cells to the right. The splash and the
too-small screens are untouched, which is what their unchanged goldens show.

## How to read it

**The bug was an ownership question.** The left margin existed in five places and
was missing in a sixth: headings wrote `"  "`, tables reserved `tableIndent`,
lists reserved it for the selection marker, the bars wrote `"  "`, the sidebar
wrote `"  "` inside itself — and a pane block wrote nothing, because the code
that composed it assumed its rect started at the screen's edge. The right margin
existed in one place only, and by accident: the heading truncated its detail line
to `width-4` and drew a rule of the same length, which is why the heading looked
inset on both sides while everything under it did not.

Naming the gutter and giving the right edge one owner removes the question. A
route is handed the box; a surface that has to move part of itself asks
`paneRect`; the shell fills the rest. The heading's literal 4 became
`width - layout.Gutter`, which is the same number with a reason attached.

**What the invariant catches.** `TestEveryScreenKeepsTheGutter` renders fifteen
screens at 120×40, 80×24 and 60×16, and for every row checks that the first two
cells are blank or hold the selection marker, and the last two are blank. It
found the help screen, whose section headings were the last thing starting in
column zero. `TestDescriptionAndSidebarTileTheBox` pins the column rule's cell and
the row's width against `detailColumns`, which is the check that catches the
mistake this change made on its way in: insetting the pane rect by two gutters
instead of one, which left the sidebar four cells off the right edge and clipped
it, invisible in a screenshot of the left edge.

## Verification

| Check | Result |
| --- | --- |
| `go build ./...`, `go vet ./...`, `gofmt -l .` | clean |
| `go test -race ./...` | passes |
| `golangci-lint run` (v2.14.0) | 0 issues |
| `govulncheck ./...` | No vulnerabilities found |
| `actionlint` | clean |
| 54 goldens re-rendered | reviewed line by line; splash and too-small unchanged |

There is no browser for a terminal UI, so the substitute stands and was used:
`teatest` through the real runtime, the golden renders, the scripted request
assertions, and the built binary driven by keys in a tmux pane at 140×45 and
80×24 against the live API. The pane geometry was measured in those captures
rather than eyeballed: at 140 columns the column rule sits in cell 97 with the
description 95 cells wide and the sidebar 40, which is 136 cells — the box
exactly. At 80 columns the sidebar takes 25 cells and a long label wraps onto
continuation lines under it.

Measured before the change, for the record, at 140 columns: the bars' text and
the tables started at column 2 and ended at 139; the heading ended at 137; the
description, the panes and the sidebar started at column 0, and the sidebar's
labels began immediately after the rule at column 99.
