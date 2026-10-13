# Thread frames, and a block for the code a remark was written against

The conversation drew a thread as flat indentation: a location line, the hunk
under it, then a name-and-time line per remark with the body two cells further
in. Nothing marked where one remark ended and the next began, and a reply was
told from the remark it answered only by reading the two names. The excerpt of
code had no container either.

## What a thread looks like now

```
    ┌─ internal/ui/table.go:12
    │  ┌─ -    return 40
    │  │  +    return tableWidth
    │  └─
    │
    │  ● carol  2024-05-01 10:03
    │    This width needs a name.
    │
    │  ┌─ octocat  2024-05-01 10:04
    │  │    Named it tableWidth.
    │  └─
    └─
```

- A thread is a frame titled by `path:line`, with its content down a `│` rail and
  a `└─` closing it. The title is still the link to the thread on GitHub.
- The remark the thread started from carries a `●` out on the thread's rail. Each
  reply is a frame of its own, nested one rail inside, titled by its own author
  line — so nesting says which remark an answer belongs to, without the reader
  comparing names. A reply needs no mark of its own.
- The excerpt of code is a frame inside the thread's rail, with its first line
  riding in the frame's corner. That keeps a three-line excerpt at four rows
  instead of five. (`hunkLines` already caps the excerpt at the three lines above
  the commented line; the frame changes how they are drawn, not how many.)
- The frames are box characters and the header-rule colour, which is what lets
  them survive the zero-colour palette the goldens render: bold names stay the
  only escape sequence a plain render carries.

The width budget is arithmetic: the title loses three cells to the corner,
anything inside the rail loses three more, and a reply body loses six. A test
asserts no rendered line exceeds the pane.

## Cost, stated plainly

A framed thread is about six rows taller than the flat list was — the snippet
frame, the reply frame and the rail separators. At 120x40 the standard fixture's
conversation no longer reaches its trailing body-less review, so that golden
shows a little less than it did. `pr_comments_reviews_only_120x40` still shows
that review, and `TestPRCommentsKeepAThreadTogetherAcrossReviews` now reads the
same property at 120x56, where the whole conversation fits.

## Verification

A TUI has no browser, so the checks are the test suite, the recorded golden
screens, and live runs of the built binary in tmux.

- New `TestThreadsAreDrawnInFrames` pins the frame's characters, the reply's
  frame nested inside the thread's rail, the reply body two rails in, the content
  width, and the absence of OSC 8 sequences in a render without hyperlinks.
- `gofmt -l .` (silent), `go build ./...`, `go vet ./...`, `go test -race ./...`
  (every package ok), `golangci-lint run` (0 issues), `govulncheck ./...` (no
  vulnerabilities), `actionlint` (clean).
- Goldens regenerated and reviewed: `pr_comments_120x40` and
  `pr_comments_reviews_only_120x40` gain the frame; nothing else moved.
- Live in tmux with the built binary, against public pull requests that carry
  real inline threads (`cli/cli#14568`, 18 inline comments and 9 replies): the
  thread frame, the snippet frame, the `●` mark and a reply frame nested inside
  the parent's rail all render, with the location, the names and the times still
  carrying their OSC 8 links.
- macOS and Windows are unverified; the checks above ran on Linux.

## Coming next, by your ordering

The body-less review lines stay as they are for now, deliberately: you want to
look at that conversation in depth. The resolved-thread collapsing and the
focus navigation are the following piece of work, since they need a GraphQL
query for `reviewThreads.isResolved` that the REST payload does not carry.

## Files

- `internal/ui/conversation.go` — the frame constants, `threadLines`,
  `snippetLines`, `replyLines`, `bodyLines`, and `authorHeading`.
- `internal/ui/conversation_test.go` — the frame test.
- `internal/ui/prs_test.go` — the behaviour test reads at 120x56.
- `internal/ui/testdata/.../pr_comments*.golden` — regenerated.
- `docs/spec.md`, `docs/architecture.md`, `README.md` — the frame, its cost and
  the reason the goldens changed.
