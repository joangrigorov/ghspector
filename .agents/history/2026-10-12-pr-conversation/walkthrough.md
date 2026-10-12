# feat(prs): read the whole conversation on the Comments tab

The Comments sub-tab read one endpoint, `GET /issues/{n}/comments`, and GitHub
keeps a pull request's discussion in three. A pull request that was reviewed and
never discussed therefore reported nothing at all: the tab drew "No comments"
over the top of an API that was holding the reviews, exactly as reported on
`getpup/backend#1536` — 0 conversation comments, 30 reviews, 30 inline review
comments at the time of the report, 48 and 41 at the time of this writing.

The sub-tab now reads the three lists and merges them into the conversation the
web UI shows: a comment as it was, a review as its author, its verdict, its
submission time and its summary, and each inline thread underneath the review it
belongs to as `path:line`, the last three lines of the hunk it was written
against, and the exchange.

The last commit on the branch answers two defects reported against the same
sub-tab — the arrows did not scroll it, and a keystroke re-rendered every
markdown body on screen. See "The lag and the missing key" below.

Branch `feat/pr-conversation`. It was opened off `main` at `2e5b9a9` (the squash
of #25), and was rebased onto `8368a20` (the squash of #26) after that one
merged, so the diff below sits on the content box #26 introduced; the three
Comments goldens were regenerated during the rebase and read afterwards.

## What changed

| File | What it is |
| --- | --- |
| `internal/ui/conversation.go` | New. `newConversation` buckets the inline comments by the review that owns them, drops an unsubmitted review together with the drafts it holds, keeps a thread whose review is not in the page as a contribution of its own, and sorts by time with an unknown time last. `conversationLines` renders one block per contribution with a rule between them; a thread renders its location, its hunk through the same `diffStyled` the diff pane uses, then each comment's author line and markdown body. |
| `internal/ui/conversation_test.go` | New. 10 unit tests over the merge and the rendering: the interleave, the unknown time, the dropped draft, the orphan thread, replies grouping under their parent (including a reply whose parent is not on the page), the `original_line` fallback, the hunk tail and its two empty shapes, the truncation note, and the four verdict badges. |
| `internal/gh/types.go` | `Review` (state, body, URL, submission time) and `ReviewComment` (review id, path, line, `original_line`, `diff_hunk`, `in_reply_to_id`), decoded straight from the two endpoints. |
| `internal/gh/client.go` | `GetPullRequestReviews` and `GetPullRequestReviewComments`, matching the shape of the comment call beside them. |
| `internal/store/{store,rest}.go` | `Reviews` and `ReviewComments`, cached under their own keys, read at `defaultConversationPageSize` — 100, the API's maximum, so a review-heavy pull request is one request per endpoint rather than several. `List.HasMore` says when a page came back full. |
| `internal/ui/prdetails.go` | The Comments sub-tab loads and refreshes three sections instead of one; `prDataMsg` carries the two new lists; `conversationHasMore` is reset by a refresh rather than sticking for the session. `PRDetails.failed` is new: see the first item below. |
| `internal/ui/prpanes.go` | `commentsView` draws the assembled conversation, and `commentBlock` is the single-comment renderer the issue conversation and this one now share. |
| `internal/ui/prs_test.go` | The fixture server serves `/pulls/{n}/reviews` and `/pulls/{n}/comments`, with review and inline-comment specs, an empty comment list, and options to fail the review list or all three. Six new UI tests: the three lists on screen in one conversation with the three requests asserted, the partial failure, the total failure, the skipped draft, the full page with its note reached by scrolling, and the arrow keys scrolling the pane. |
| `internal/store/rest_test.go` | The paged-children test covers the two new calls, and a new test pins the page size the review lists ask for. |
| `internal/ui/golden_test.go` and `testdata/TestGoldenPullRequestScreens/` | The Comments golden regenerated, plus `pr_comments_80x24` and `pr_comments_reviews_only_120x40` — the reported shape: nothing in the conversation comments, everything in the reviews, one of them with no summary. |
| `docs/spec.md` | The endpoint table gains the two routes; §4.14 records the fix, the three-part shape, its two deliberate edges, and what a keystroke now costs. |
| `docs/architecture.md` | The build-status section for this change, including the two defects found while building it and the render cache the report added. |
| `README.md` | The conversation joins the feature list. |

## Two things found while building it

**One `err` cannot describe three fetches.** `PRDetails.err` is set by whichever
section failed last and cleared by whichever succeeded last, so a failed review
list followed by a successful comment list left the tab unable to tell "still
loading" from "never arrived" — it would have said "Loading..." for the rest of
the session. `PRDetails.failed` remembers per section, and `loadMessage` reads it.
The commits and files panes had the same latent bug behind a single-section tab,
where a poll of the checks could clear a commit failure's record; it is fixed for
them too, which is why `loadMessage` changed rather than gaining a sibling.

**A refresh kept the truncation note.** `conversationHasMore` was set and never
cleared, so a pane that had once seen a full page said so for the rest of the
session. It is now reset with the sections the refresh drops.

## The lag and the missing key

Two defects were reported together against the same sub-tab, and both were
reproduced before either was changed.

**The arrows did nothing.** `j`, `k`, `Down` and `Up` are the keys every other
list in the app answers; the two conversation scopes bound only `u`/`d` and the
page keys, so the arrows fell through. The routes already handled the actions —
`move` moves `commentScroll` on the Comments sub-tab, and `IssueComments.Act`
gained the two cases — so the binding was the whole of the missing piece.

**A keystroke rendered every markdown body on screen.** `conversationLines`
renders each contribution from scratch, and the sub-tab called it twice per key:
once from `clamp`, to bound the scroll, and once from `commentsView`, to draw.
The reported pull request carries 48 reviews and 41 inline remarks, ~44 KB of
markdown; one render of it measured 163 ms (`BenchmarkConversationRender`), so a
keystroke paid something like 400 ms of work before the frame was drawn, and
every poll tick paid the render again.

| File | What it is |
| --- | --- |
| `internal/ui/render.go` | New. `renderedLines[R]` holds content rendered to lines until something that decides what it looks like changes: whatever the caller has to say what the content is (a counter a route bumps when data lands, or the body text itself), the width it wraps to, and the palette it draws in. |
| `internal/ui/prpanes.go`, `prdetails.go` | `commentLines` and `descriptionLines` go through the cache; `convVersion` counts the changes to the three conversation lists, and `descriptionLines` keys on the body, so a keystroke on any sub-tab renders nothing. |
| `internal/ui/issuedetails.go` | The same for the issue conversation (`version`/`rendered`) and the issue description (`descLines`), plus the `ActionDown`/`ActionUp` cases the scope was missing. |
| `internal/ui/markdown.go` | `Markdown.renders` counts documents rendered, beside the `builds` counter, so a test can prove a frame renders nothing. |
| `internal/keymap/keymap.go` | `PRComments` and `IssueComments` bind `j`/`Down` and `k`/`Up`. |
| `internal/ui/render_test.go` | New. The cache itself, and the counts: seven keystrokes over the conversation cost zero markdown renders, a refresh renders it again, a resize renders it again at the new width, and the issue thread and the description behave the same. |
| `internal/ui/render_bench_test.go` | New. `BenchmarkConversationRender` — the reported shape as a benchmark, so the 163 ms is reproducible rather than quoted. |
| `internal/ui/issues_test.go`, `prs_test.go` | `TestIssueCommentsScrollWithTheArrowKeys` and `TestPRCommentsScrollWithTheArrowKeys` drive the keys through the model and assert both the offset and the frame. |
| `internal/keymap/keymap_test.go` | Both scopes pinned in `TestDocumentedKeysFromSpec`. |
| `docs/keybindings.md` | Generated; the two scopes gained the two rows. |

### Measured live

The pre-fix binary was built from the branch head (`2c19db0`) and run beside the
fixed one in tmux at 140×45 against `getpup/backend#1536`:

| | three Down presses | CPU per keystroke | idle, Comments tab |
| --- | --- | --- | --- |
| before | the screen did not move | 76–90 ms | — |
| this change | the pane scrolled | 2–6 ms | 0.75% of a core, the same as the Info tab |

What is *not* fixed: the render after data lands still happens synchronously on
the UI goroutine, so the first frame of a review-heavy conversation can still
stall briefly. One render per change is the cost the cache leaves.

## Deliberate edges

- **A draft review is not part of the discussion.** State `PENDING`, or a review
  with no submission time, is a review its author has not submitted; it and the
  inline remarks it is holding are left out rather than drawn as a contribution
  with no time to place it by. `TestConversationDropsAPendingReview` and
  `TestPRCommentsSkipsAReviewDraft` hold that.
- **A thread whose review is not in the page is still drawn.** The review list is
  one page; an inline comment can name a review that was cut, or that the token
  cannot see. Silently dropping a remark somebody wrote is worse than drawing it
  without its parent.
- **An inline thread shows the tail of its hunk, three lines.** That is the
  context GitHub's own view shows around a commented line, which is what makes
  "this needs a name" mean something. A hunk that is only its `@@` header has no
  context and draws none.
- **A full page is reported.** Read at `per_page=100`, so a discussion longer
  than that says so at the end instead of showing a fraction as the whole.

## Verification

The checks every phase lists, on this branch:

- `go build ./...`, `go vet ./...`, `gofmt -l .` — clean.
- `go test -race ./...` — 420 test functions, all passing across 11 test
  packages (414 before this change; 690 runs counting subtests).
- `golangci-lint run` — no issues, with the local binary at `v2.14.0` built with
  `go1.27.2`, the version this `go.mod` needs.
- `govulncheck ./...` — no vulnerabilities found.
- `actionlint` — clean.

### Run against GitHub, read-only, in a pty

The binary was built from this branch and driven in a tmux session at 140×45 and
80×24 against the pull request the report named, `getpup/backend#1536`, in the
`getpup` organisation context. The API holds 0 conversation comments, 48 reviews
and 41 inline review comments for it.

- The tab drew the reviews, with `getpup-reviewer[bot]` and `joangrigorov` as
  authors, `[reviewed]` against each, the markdown summaries wrapped to the
  pane, and the inline threads as `internal/component/user/infra/search/user_search.go:84`
  with the hunk excerpt above the remark and the reply under it.
- `d` scrolled through the conversation to the later reviews, which is where the
  long summaries and the later threads live.
- The arrows scroll it now, and the pre-fix build is the control: the same three
  `Down` presses left its screen byte for byte unchanged, while the fixed build
  moved the pane. A `k` after three `Down` presses lands two lines in, which is
  the line-at-a-time step the test asserts.
- Idle CPU on the Comments tab is the same as on the Info tab (0.75% of a core
  over a four-second window), so the screen no longer costs anything to sit on.
- At 80×24 the location line truncated with an ellipsis rather than wrapping, and
  the bodies wrapped at the pane width.

### What stands in for browser verification

A TUI has no browser to drive, so the substitutes are, as in every previous
phase: `teatest` through the real runtime, goldens at four sizes, scripted tests
that assert the requests a screen sent, and the live pty run above. The goldens
that change here are the Comments tab's and the issue conversation's, and the
regression tests for the lag count markdown renders rather than describing them.
A golden can prove the pane reads a review and its thread in the right order, and
cannot prove the markdown is good, which `internal/ui/markdown_test.go` pins
against the palette instead.

### Not verified

- macOS and Windows: not run, as in every previous phase.
- A pull request whose discussion exceeds one page of 100 entries: the note is
  covered by a test that builds a full page, and the live pull request is under
  the limit, so the note has not been seen against a real truncated page.
- A conversation comment and a review landing on the same second: the sort is
  stable, so the comments keep the order the API returned them in, but this has
  not been observed live.
- One full verbose run of the suite reported a single unnamed failure that eight
  later runs — including one under CPU load — did not reproduce. The test driver
  gives a command 200 ms to answer before treating it as a timer, which a loaded
  machine can exceed; no test of this change is timing-dependent.

## Merge state

#26 (`fix/ui-content-gutter`) is merged as `8368a20`, and this branch is rebased
onto it, so both halves of the golden overlap are already resolved here: the
three Comments goldens were regenerated during the rebase and read afterwards
(the content carries #26's two-cell gutter, the footers carry the two new key
hints). Nothing in this branch needs #26 again, and nothing is stacked.
