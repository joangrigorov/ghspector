# Conversation threads, grouped by the comment they started from — and links on the conversation screens

Two problems, one branch. The Comments tab drew one thread header per inline
comment, and the conversation screens named the repository, the branches, the
thread locations and the contributors without letting you open any of them.

## The thread defect, from the live data

The review-comments endpoint returns 41 inline comments, 25 of
them replies, and **all 25 replies were posted through a review later than the
comment they answer** — none shared a review with its parent. `in_reply_to_id`
names the comment answered; `pull_request_review_id` names the review that posted
the reply.

`groupThreads` ran once per review, so a thread whose reply arrived later was
drawn twice: once with the root under its own review, once with the reply under
its review. That produced **41 thread headers for 41 comments**, 13 locations
drawn more than once, and one location drawn four times.

Threads are now gathered across the whole page: each comment walks to its root
through `in_reply_to_id`, the comments that share a root form one exchange, and
the exchange is drawn where its root's review is. The review that carried a reply
keeps its own place in the timeline and shows nothing of its own when every
comment it posted belongs to a thread drawn elsewhere. A reply whose root is not
in the page — the review was submitted after the page was fetched, or the token
cannot see it — is drawn on its own rather than dropped, and replies to the same
missing comment read as one exchange.

The fixture had hidden this: it put the reply in the same review as the comment it
answered, so the tests exercised a shape GitHub does not return. The PR and issue
detail fixtures now mirror the live payloads.

## The links

Every link is built in `internal/ui/links.go` from what the API returned, so a URL
cannot point at a row it does not belong to:

| Text | Opens |
| --- | --- |
| the repository in the heading | the repository page |
| `Source: x → Base: y` | the compare between the branches |
| a thread's `path:line` | the thread on GitHub |
| an issue conversation's heading | the issue |
| a contribution's name and time | that comment |

A thread's location opens the remark's permalink. A blob URL built from the head
SHA would point at the wrong line as soon as the branch moves, because GitHub
keeps a comment on the line it was written against while the API reports that line
as zero once the diff has moved on.

The links are OSC 8 sequences, so a terminal without support prints the text and
ignores them, and the zero-colour palette the goldens render emits none — the
golden files stay free of escape sequences.

### The heading links were dead, and the live run is what found it

The REST detail payload names the repository only under `head.repo` and
`base.repo`; `Repository.FullName` at the top level is empty, so both the
repository link and the compare link were built from an empty string and the
heading named things without opening them. `repoFullName` now takes the name the
payload carried when it has one and the repository reference the route was opened
with otherwise. The heading text is unchanged.

## Verification

A TUI has no browser, so the checks are the test suite, the recorded golden
screens, and live runs of the built binary in tmux.

- `gofmt -l .` (silent), `go build ./...`, `go vet ./...`, `go test -race ./...`
  (every package ok), `golangci-lint run` (0 issues), `govulncheck ./...` (no
  vulnerabilities), `actionlint` (clean).
- New unit tests: a reply in a later review lands under its root's review; a
  three-deep chain passed in shuffled order comes out in time order and takes its
  location from the root; two replies naming an absent comment form one thread.
- New screen test: `TestPRCommentsKeepAThreadTogetherAcrossReviews` counts the
  thread location once on screen and finds the reply under the root. New link
  tests pin every URL builder, the empty-input cases, and the OSC 8 targets
  against a coloured palette.
- Golden regenerated: `pr_comments_120x40` — the reply is now drawn under the
  thread it answers, and the review that carried it reads bare (2 lines changed).
- Live run against the reported pull request in tmux: a dense sweep found **15
  distinct thread-header strings covering the 16 threads the API holds** (two
  threads share one location), down from 41 headers; the pane stops moving only at the
  end of the conversation, so the scroll keeps up. The raw pane stream carries OSC
  8 targets for the repository, the compare, every thread location and every
  contribution's name and time. The issue conversation's heading opens its issue.

## Limits

- macOS and Windows are unverified; the checks above ran on Linux.
- A live issue with comments was not reachable in the visible page of the issue
  list, so the issue-comment author line is covered by the shared code path and
  the unit tests rather than by a live sample.

## Files

- `internal/ui/conversation.go` — grouping by root, attribution to the root's
  review, ties in the timeline.
- `internal/ui/links.go` (new) — the URL builders and `repoFullName`.
- `internal/ui/prdetails.go`, `internal/ui/issuedetails.go` — the headings.
- `internal/ui/*_test.go`, `internal/ui/testdata/…/pr_comments_120x40.golden` —
  coverage and the regenerated screen.
- `docs/spec.md`, `docs/architecture.md`, `README.md` — the behaviour, the defect
  record and the user-facing description.
