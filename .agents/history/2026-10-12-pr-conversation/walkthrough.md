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

Branch `feat/pr-conversation`, branched from `main` at `2e5b9a9` (the squash of
#25). It is deliberately not stacked on #26 (`fix/ui-content-gutter`): the two
branches sit on the same commit, so neither diff carries the other. They do touch
the same goldens — see the note for the review gate at the end.

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
| `internal/ui/prs_test.go` | The fixture server serves `/pulls/{n}/reviews` and `/pulls/{n}/comments`, with review and inline-comment specs, an empty comment list, and options to fail the review list or all three. Five new UI tests: the three lists on screen in one conversation with the three requests asserted, the partial failure, the total failure, the skipped draft, and the full page with its note reached by scrolling. |
| `internal/store/rest_test.go` | The paged-children test covers the two new calls, and a new test pins the page size the review lists ask for. |
| `internal/ui/golden_test.go` and `testdata/TestGoldenPullRequestScreens/` | The Comments golden regenerated, plus `pr_comments_80x24` and `pr_comments_reviews_only_120x40` — the reported shape: nothing in the conversation comments, everything in the reviews, one of them with no summary. |
| `docs/spec.md` | The endpoint table gains the two routes; §4.14 records the fix, the three-part shape, and its two deliberate edges. |
| `docs/architecture.md` | The build-status section for this change, including the two defects found while building it. |
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
- `go test -race ./...` — 632 tests pass across 12 packages (629 before this
  change; the new ones are the 10 conversation unit tests, 5 UI tests, 1 store
  test, plus the extended paged-children test).
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
- At 80×24 the location line truncated with an ellipsis rather than wrapping, and
  the bodies wrapped at the pane width.
- The report this PR answers was read off the same pull request: the tab said
  "No comments" while the API held the reviews. The run above is the same token,
  the same pull request, and the tab reading them.

### What stands in for browser verification

A TUI has no browser to drive, so the substitutes are, as in every previous
phase: `teatest` through the real runtime, goldens at four sizes, scripted tests
that assert the requests a screen sent, and the live pty run above. The goldens
that change here are the Comments tab's; a golden can prove the pane reads a
review and its thread in the right order, and cannot prove the markdown is good,
which `internal/ui/markdown_test.go` pins against the palette instead.

### Not verified

- macOS and Windows: not run, as in every previous phase.
- A pull request whose discussion exceeds one page of 100 entries: the note is
  covered by a test that builds a full page, and the live pull request is under
  the limit, so the note has not been seen against a real truncated page.
- A conversation comment and a review landing on the same second: the sort is
  stable, so the comments keep the order the API returned them in, but this has
  not been observed live.

## For the review gate

`#26` (`fix/ui-content-gutter`) and this branch both regenerate goldens under
`internal/ui/testdata/`: 54 files there, three here, and `pr_comments_120x40` is
in both sets, because #26's gutter moves every line of it. They also both touch the
same neighbourhood of `internal/ui/` — `#26` the pane insets in `prdetails.go`
and `prpanes.go`, this branch the Comments pane and the comment block they share.
Whichever of the two merges second
needs a rebase onto the new `main` and `go test ./internal/ui -run 'TestGolden' -update`,
after which the golden diff of the second one should be read rather than trusted.
