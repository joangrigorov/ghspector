# Resolved threads fold to one row, and the arrows walk their buttons

You asked for collapsible resolved threads: still visible, still there, folded
away until `[show]` is hit, with the arrows moving from one button to the next
and the scroll keys and the wheel left to scrolling. This is that, plus the one
thing the earlier round could not do without it: the resolution state is not in
the REST payload, so it took a GraphQL query.

## What changed

- **The resolution state comes from the schema.** `internal/gql/threads.go` asks
  `reviewThreads(first: 100) { isResolved comments(first: 100) { databaseId } }`
  in one query per pull request, validated against the live schema on a pull
  request with nine resolved threads.
- **The store keeps the seam.** `store.Client.ReviewThreads` goes through the
  same `fetch` helper as every other read, so the query is cached, limited and
  keyed like the rest, and `ReviewThread` is aliased into the store's domain
  types. Nothing above the store knows a second transport answered.
- **A resolved thread is one row.** `[show]`, where it was written (the link to
  the thread on GitHub), who opened it, how many remarks it holds, `resolved`.
  The button leads the row, so the controls of a conversation line up in one
  column. Opening it restores the frame it was, with `[hide]` on the title row.
- **The folded row wears the wash it folded.** A thread keeps its author's shade
  while it is out of the way, and unfolds back into it.
- **The arrows move the focus, the scroll stays the scroll.** `j`/`k`/`↑`/`↓`
  walk from button to button and hand the movement back to the scroll at either
  end, so one pair of keys does both and the reader is never stuck on a button.
  `u`/`d` and the wheel always scroll. `Enter` toggles the focused thread and
  scrolls it into view.
- **A failing query costs the fold and nothing else.** The threads section is
  deliberately outside `conversationSections`, so a refused query does not make
  the pane claim the whole conversation failed, and the error is not surfaced:
  the conversation is drawn in full, exactly as before the feature.

## What a folded thread looks like

```
  ──────────────────────────────────────────────────────────

    carol  2024-05-01 11:44
    ␣
    Left one question about the ordering guarantee.

  ›   [show] internal/ui/table.go:12 · carol · 2 remarks · resolved
      [show] internal/ui/table.go:80 · carol · 2 remarks · resolved
```

`Enter` on the first one:

```
  ›   ┌─ internal/ui/table.go:12  [hide]
      │  ┌─ @@ -10,4 +10,5 @@
      │  │  ...
      │  └─
      │
      │  ● carol  2024-05-01 11:44
      │    If the ordering is guaranteed, document it here.
      └─
```

## Two defects the live pane found

Both were in how the focus is drawn, and neither was caught by the tests that
existed when I started:

- the marked row was written back into the slice the render cache returned, which
  it shares with the cache's array. The marker therefore stayed on the row and
  every later render added another one: the screen read `› › › › › [show]`. The
  row is now drawn as it is handed to the pane, and the cached lines are never
  written to.
- the marker takes its cells from the indent the row already carries, and the
  indent of a washed row sits behind the escape that starts the paint, so the
  trim missed it and the row shifted right by the indent's width. `dropIndent`
  cuts the indent where it stands and keeps the paint, and the marker's padding
  is measured in cells rather than bytes, so a control keeps its column when the
  focus lands on it.

`TestTheFocusMarkerIsNotBakedIntoTheCache` now renders the same state twice on
the washed palette and asserts the two screens are identical, that no row carries
the marker twice, and that the focused control sits in the same column as the
others. It fails on both defects.

## Verification

A TUI has no browser, so the checks are the test suite, the recorded golden
screens, and live runs of the built binary.

- `gofmt -l .` (silent), `go build ./...`, `go test ./...` (every package ok),
  `golangci-lint run ./...` (0 issues).
- Goldens: `pr_comments_resolved_120x40` and
  `pr_comments_resolved_opened_120x40` are new, and the five conversation goldens
  from before are unchanged.
- New tests: the fold and its reopening, the arrow walk between the buttons, the
  arrows scrolling when nothing is resolvable, the wheel scrolling the route, the
  marker not sticking in the cache, the folded row's wash in both palettes.
- Live in a tmux pane against `cli/cli#14567` (four of its seven threads are
  resolved): the three folded rows sit at one column, `Enter` opens the focused
  one, `Down` moves the marker to the next control, and a wheel sequence written
  into the pane scrolls the conversation.
- Rendered again under a pty that answers the terminal's background query, once
  with a light background and once with a dark one: the folded rows wear the same
  shade as the remark they folded — `rgb(27,43,51)` against `rgb(11,17,20)` on
  the dark run, `rgb(223,235,240)` against `rgb(241,246,249)` on the light one —
  and the washes are one family with the background in both.
- macOS and Windows are unverified; the checks above ran on Linux.

## Files

- `internal/gql/threads.go`, `internal/gql/threads_test.go` — the resolution
  query and its tests.
- `internal/store/threads.go`, `internal/store/threads_test.go`,
  `internal/store/store.go` — the cached read, the domain alias, the interface.
- `internal/ui/conversation.go` — the fold, the folded row, the summary, the
  controls the render reports.
- `internal/ui/prdetails.go` — the threads section, the resolution state, the
  expansion state, the focus, `Enter`.
- `internal/ui/prpanes.go` — the focused row, `dropIndent`, the viewport.
- `internal/ui/model.go` — the wheel and mouse reporting.
- `internal/keymap/keymap.go`, `docs/keybindings.md` — the new bindings and the
  help text.
- `docs/spec.md`, `docs/architecture.md`, `README.md` — the fold, the transport
  decision and the two defects.
