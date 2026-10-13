# A wash per commenter, air under the name, a rule under the remark

The conversation was readable but flat: the author line sat directly on the body
it introduced, only the top-level contributions were separated by anything, so a
thread's remarks ran together, the hunk excerpt stopped at three lines, and the
whole pane was one background.

## What changed

- **Every contribution is a card.** `cardLines` draws a heading, a row of air,
  then the body. The air row carries the rail the contribution is drawn inside,
  so a framed card does not break its own frame. A contribution with no body is
  a heading and nothing else, which is what keeps a review that only carries a
  verdict one line tall.
- **A rule closes each remark.** A thread's remark draws its own rule inside the
  thread's rail, and a reply's frame foot is a rule, so the two remarks of a
  thread are separated the way two top-level contributions are.
- **Each remark's body is washed in its author's shade.** The shades are derived, not
  chosen: `authorTints` mixes the palette's accent into the palette's background
  in six even steps, so they are one family by construction and each palette
  gets washes on its own side of the background — `#05090a` to `#20343d` on the
  dark palette, `#f7fafb` to `#d9e7ed` on the light one. `authorTint` picks a
  shade with an FNV-1a hash of the login, so a person keeps theirs across
  refreshes, screens and conversations.
- **Contrast is checked, not assumed.** The body colour stays above 10:1 on
  every shade of either palette and the muted colour, which draws the timestamps,
  above 4:1. `TestAuthorWashesStayInOneFamily` asserts both, and the property
  that makes the shades one hue: each takes the same share of the accent in every
  channel, each takes more than the last, and none takes more than a third.
- **The excerpt grew to six lines** (`hunkTail` 3 → 6). The frame around it is
  what made it affordable.
- **The header's band was broken and is fixed.** A plain lipgloss background does
  not survive its own contents: every styled run ends with `\x1b[m`, and a reset
  clears the background along with the colour. The paint is re-armed after every
  reset by `paintLine`, which the header band and the cards now share.

## What a conversation looks like now

```
    alice  2024-05-01 10:00
    ␣
    Comment 1 looks good to me.

  ────────────────────────────────────────────────────

    carol  [approved]  2024-05-01 10:03
    ␣
    Ship it once the checks pass.

      ┌─ internal/ui/table.go:12
      │  ┌─ @@ -10,4 +10,5 @@
      │  │   func width() int {
      │  │  -    return 40
      │  │  +    return tableWidth
      │  │   }
      │  └─
      │
      │  ● carol  2024-05-01 10:03
      │  ␣
      │    This width needs a name.
      │  ─────────────────────────────────────────────────
      │
      │  ┌─ octocat  2024-05-01 10:04
      │  │  ␣
      │  │    Named it tableWidth.
      │  └─────────────────────────────────────────────────
      └─
```

The `␣` rows are air: blank inside the card, carrying the rail inside a frame.

## Follow-up: the wash is the remark's own box

The first cut painted every row of a card — the author line, the row of air, the
body and the rules — from the pane's first cell to its last. In a live pane that
read as greedy: the colour ran behind the name and across the whole width, so it
marked "a review happened here" rather than "this is the remark".

The wash is now the box the body's own text occupies. `cardLines` paints the body
and nothing else, and `bodyBox` measures the block before painting it: the box
opens at the column the card puts its text in and ends on the last cell of the
block's widest line, with every line of the block carried out to it, so a block is
one colour rather than one highlight per line. The heading, the row of air, the
rules and the pane's content past the box keep the terminal's own background, and
a folded thread's summary row — a summary rather than a remark — carries none at
all.

That needed a second helper: `paintSpan` paints behind a span of a row instead of
the whole of it. Measuring the result rather than looking at it is what caught the
defect the first version had: a markdown run ends with `\x1b[0m`, and the re-arm
only knew about `\x1b[m`, so the padding that carries a short line out to the box
fell outside the wash and the box came apart into per-line highlights.

Two commits: the box attempt that followed the first review (`fd0f63f`, a
six-cell inset that was too subtle to see in a pane) was reverted whole
(`47a8871`), which put the render, the rules and the seven conversation goldens
back on `a4ae7a9` byte for byte, and the box above replaced it.

## Cost, stated plainly

A card is two rows taller than the lines it replaced — the air row and the rule —
on top of the frame's own rows, and each snippet grew by three lines. At 120x40
the standard fixture's conversation now reaches neither the reply in its thread
nor the trailing body-less review, so those goldens show less than they did; the
same conversation fits at 120x56, where `TestPRCommentsShowsTheThreeListsInOneConversation`
and `TestPRCommentsKeepAThreadTogetherAcrossReviews` read it whole.

## Verification

A TUI has no browser, so the checks are the test suite, the recorded golden
screens, and a render of the real screen shown through a real terminal.

- New `TestWashesPaintTheCommentBodyAndNothingElse` asserts that the same text
  sits in the same cells as a plain render, that each body wears its author's
  shade and no row which is not a body wears one, and that two authors of one
  thread do not share one. `TestAWashIsTheCommentsOwnBox` reads the extent of the
  wash off a block whose lines are of two different lengths.
- New `TestAuthorWashesStayInOneFamily`, `TestWashesFollowTheLogin`,
  `TestTintLinePaintsUnderTheText`, `TestSpanPaintsTheBoxAndNothingElse` and
  `TestHeaderBandPaintsBehindEveryRun` for the band the cards share.
- `gofmt -l .` (silent), `go build ./...`, `go vet ./...`, `go test ./...` (every
  package ok), `golangci-lint run ./...` (0 issues), `govulncheck ./...` (no
  vulnerabilities).
- Goldens regenerated and reviewed: the three pull request conversation screens
  and the two issue conversation screens gain the air rows, the rules and the
  longer excerpt; nothing else moved.
- The fixture conversation was rendered in both palettes through the app's own
  screen composition and the raw output read as an image: the wash is one box
  behind each remark's own text, and the author line, the rules, the frame and
  the pane's margin carry no colour. The header band still spans the whole row,
  account included.
- The environment sets `NO_COLOR=1`, which strips every colour: the runs that
  showed colour cleared it first, and the first run without that is why the
  missing header band was found.
- macOS and Windows are unverified; the checks above ran on Linux.

## Files

- `internal/ui/tints.go`, `internal/ui/tints_test.go` — new: the ramp, the login
  hash, the wash lookup.
- `internal/ui/chrome.go` — `band`, `paintLine`/`paintCode` for the header, and
  `paintSpan` for a remark's box.
- `internal/ui/theme.go` — the `AuthorTints` field and its two palettes.
- `internal/ui/conversation.go` — `cardLines`, `bodyBox`, `rootCard`, `replyCard`,
  the six-line excerpt.
- `internal/ui/prpanes.go` — the issue conversation's comment block uses the same
  card.
- `internal/ui/testdata/.../*.golden` — regenerated.
- `docs/spec.md`, `docs/architecture.md`, `README.md` — the washes, the spacing,
  the rules and the cost.
