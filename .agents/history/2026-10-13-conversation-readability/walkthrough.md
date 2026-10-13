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
- **Each card is washed in its author's shade.** The shades are derived, not
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

- New `TestWashesPaintEachContributionWithoutMovingIt` asserts that the same text
  sits in the same cells as a plain render, that every row naming an author opens
  in that author's shade, and that two authors of one thread do not share one.
- New `TestAuthorWashesStayInOneFamily`, `TestWashesFollowTheLogin` and
  `TestTintLinePaintsUnderTheText`, and `TestHeaderBandPaintsBehindEveryRun` for
  the band the cards share.
- `gofmt -l .` (silent), `go build ./...`, `go vet ./...`, `go test ./...` (every
  package ok), `golangci-lint run ./...` (0 issues), `govulncheck ./...` (no
  vulnerabilities).
- Goldens regenerated and reviewed: the three pull request conversation screens
  and the two issue conversation screens gain the air rows, the rules and the
  longer excerpt; nothing else moved.
- The fixture conversation was rendered in both palettes and printed through a
  real terminal (tmux, `capture-pane -e`): every card row carries its author's
  background from its first cell to the pane's edge, the paint is re-armed after
  each reset, the unpainted frame and the red/green diff lines stay distinct, and
  the header band now spans the whole row, account included.
- The environment sets `NO_COLOR=1`, which strips every colour: the runs that
  showed colour cleared it first, and the first run without that is why the
  missing header band was found.
- macOS and Windows are unverified; the checks above ran on Linux.

## Files

- `internal/ui/tints.go`, `internal/ui/tints_test.go` — new: the ramp, the login
  hash, the wash lookup.
- `internal/ui/chrome.go` — `band` and `paintLine`/`paintCode`.
- `internal/ui/theme.go` — the `AuthorTints` field and its two palettes.
- `internal/ui/conversation.go` — `cardLines`, `rootCard`, `replyCard`, the
  six-line excerpt.
- `internal/ui/prpanes.go` — the issue conversation's comment block uses the same
  card.
- `internal/ui/testdata/.../*.golden` — regenerated.
- `docs/spec.md`, `docs/architecture.md`, `README.md` — the washes, the spacing,
  the rules and the cost.
