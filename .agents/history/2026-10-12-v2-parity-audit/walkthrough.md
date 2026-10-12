# feat(tui): the parity audit and rendered markdown (Phase 6)

Phase 6 of the v2 rewrite, and the last one. `docs/spec.md` shipped with two
groups of boxes deliberately unticked — startup, context and global chrome
(§2.1–2.5) and the transport contract (§9) — because they were implemented and
partly tested but never read back against their own test. This phase is that
read. Every one of the document's 226 boxes is now ticked or marked
**Superseded** with the decision that replaced it, each row naming the test
behind it.

The phase also delivers the one capability the spec still listed as open:
pull request, issue, comment and commit bodies render as markdown. Reading the
boxes back turned up five behaviours that were implemented but wrong or
unobserved, four of them in the chrome, and all five are fixed.

Parent plan: `.agents/history/2026-10-12-v2-phase-0-spec-and-prep/implementation_plan.md`,
phase 6. Branch `feat/v2-parity-audit`, branched from `main` at `684de39` (the
squash of #23).

## What changed

| File | What it is |
| --- | --- |
| `internal/ui/markdown.go` | New. `Markdown` owns one `glamour/v2` renderer per `(palette, width)`, built by `Prepare` on a resize or a palette change and reused by every frame in between. The style is glamour's plain ASCII configuration with this palette's accent, body and muted colours on top; `WithEmoji` is on so a `:robot:` in a release note draws as 🤖. Two renderer behaviours are undone because they are not the pane's to draw: the pad every line is filled to the wrap width with, and the blank lines the document is wrapped in. Four call sites — the pull request description, the issue description, a comment body, a commit message. |
| `internal/ui/markdown_test.go` | New. 11 tests: the palette reaches every heading, link and code span; every RGB triple in a render is in the palette; five frames rebuild the renderer zero times; a body cannot smuggle an OSC 52 or a clear-screen through; a rendered line carries no pad; a palette without hyperlinks gets no OSC 8 tokens; a nil renderer falls back to plain wrapping; the description pane renders and scrolls over the rendered body; the commit message body appears, and a bodyless commit keeps the region. |
| `internal/ui/theme.go` | The three colours a rendered body draws with are named constants now, because the palette and glamour's style both need them and a literal repeated twice is a hue that can drift. `Theme` gains `Dark` (which side of the accent contrast) and `Colored()` (whether it paints at all). |
| `internal/ui/chrome.go` | The header's loading indicator is wired to the screen on top instead of a `false` constant; `statusTTL` is the spec's 3 s; the rate-limit band counts the window down on the API's clock; the footer's pinned global keys are chosen from what owns the keyboard (`hintMode`). |
| `internal/gh/client.go` | `RateLimitResetIn` reports how long the window has left on the API's clock, and `Now` finally has callers on both sides of the decision. `checkRateLimit` compares a server timestamp to the server's clock. Every decoded response and every error message goes through `sanitize.Texts`. |
| `internal/gql/gql.go`, `internal/logs/logs.go` | The same treatment at their own boundaries: a decoded query struct, a log line. `ansi.Strip` alone is not enough — it removes escape sequences and passes control characters through. |
| `internal/sanitize/` | New. `Text` and `Texts` (a reflection walk) strip escape sequences and control characters out of strings the API wrote. |
| `internal/overlay/{overlay,prompt}.go` | `TextEntry`/`Stack.TakesText`, so the root knows a box is taking characters. |
| `internal/ui/{model,route,mainroute}.go` and the six routes | `loadingRoute`, so the header can ask whether the screen on top is waiting for data; a quit that gives way to a text box; a spinner that runs while there is something to animate. |
| `cmd/ghspector/main.go` | `resolveToken` and `printAuthInstructions` are seams, so the no-credentials path is driven by a test rather than read. |
| `internal/ui/model_test.go`, `internal/ui/theme_test.go`, `cmd/ghspector/main_test.go`, `internal/gh/client_test.go` | New tests: the startup request order, the configured context, a rate-limited startup recovering, the footer on fifteen screens and on the three surfaces that differ, the header's spinner, the alternate screen, `q` inside a box, the documented headers, the server's clock, the XML log error and its raw-body fallback, the `defaultOwner` mapping. |
| `docs/spec.md` | The §2 and §9 walk, eight **Superseded** rows with their decisions, §7's last two items ticked, and §11 rewritten with the counts read back out of the document. |
| `docs/architecture.md` | Phase 6 build status; `internal/sanitize` in the layer table; the module-path decision and what it did not touch; what glamour adds to the module graph; three new conventions (12, 13); finding F6. |
| `README.md` | Markdown rendering and honest counts in the feature list; the "global keys" table split into tab keys (available on the three lists) and truly global keys, with a line about what a modal and a text box do with `q`. |

`go.mod` promotes `charm.land/glamour/v2` to a direct requirement and, for the
security fix below, raises `github.com/yuin/goldmark` to `v1.8.6`,
`golang.org/x/net` to `v0.61.0` and `golang.org/x/text` to `v0.43.0`. The module
path is `github.com/joangrigorov/ghspector` — see the deviation note at the end.

## How to read it

**The audit is the change.** The interesting part of this diff is not a feature;
it is that reading 226 boxes against their tests found five things the suite was
happy with. The header's `loading` argument was passed a literal `false`, so the
spinner added in Phase 1 had never drawn on a route. `statusTTL` said 5 s where
§2.3 says 3 s. `Client.Now` existed for the server offset and had no callers at
all, which is why the countdown was missing from the exhausted-budget band. A
`q` typed into a login filter quit the program, because the root answered quit
before any overlay saw the key. And the footer advertised `?`, `o` and quit over
a modal and over a text box, where those keys type characters or do nothing.
Four of the five are in the chrome, and each is the kind of defect a golden
render cannot see: the goldens were regenerating happily around all of it.

**The footer now says what works.** A route answers every global key, so all
three are pinned. A modal that is not a text box consumes the keys the route
would act on — the root still answers quit from anywhere, but `?` and `o` do
nothing — so quit alone is pinned. A text box takes characters, so `q`, `o` and
`?` are what somebody is typing and the footer offers `Ctrl+C` alone. This is
convention 13 in the architecture doc, the same rule as "do not document a key
that does nothing", applied to the one surface that documents keys dynamically.
It changed 12 goldens, which were read.

**Markdown is one renderer, not one render.** `glamour` is the largest thing this
rewrite has added, and the failure mode it could reintroduce is the baseline's:
the old markdown path called `lipgloss.HasDarkBackground()` inside the render
path, which is a blocking terminal probe per frame. Here the renderer is built by
`Prepare` — on construction, on a `WindowSizeMsg`, on a background-colour answer
— and a frame finds the one it needs. A test renders five frames at one width and
asserts the build counter stayed at 1. The style starts from glamour's plain
ASCII configuration rather than its dark one, because the standard dark style
paints top-level headings on a purple bar and this palette is not purple; a test
walks every RGB triple in a live render and fails if any is not one of the
theme's. The pads and blank lines glamour adds are removed at the pane boundary,
because the layout owns the region and the scroll bound must count rows the body
earned.

**A body is untrusted input drawn on a terminal.** That is why `internal/sanitize`
exists and why it is called at two boundaries: the transport (a decoded response,
an error message) and the last one before cells (`Markdown.Lines`, which can also
receive a route's own text). `ansi.Strip` is not sufficient — it removes escape
sequences and deliberately passes control characters through, so a BEL or a CR
survives it. A test in each package feeds a response carrying an OSC 52 clipboard
write and asserts it reaches the screen as text.

**The untested groups are tested now.** §2.1's last clause — "print instructions
to stderr, exit 1" — had no way to run: `run()` called the real token resolver and
the instructions went to the real stderr. The credential calls are package-level
variables now, so the failure path is driven. §9's six rows each got the test
they were waiting for; the XML log error had been asserted inside a test named
after three other endpoints, and it is now its own test with both halves of the
fallback.

**What "superseded" means here.** Eight boxes describe v1 behaviour the rewrite
changed on purpose, and they are ticked rather than left open, with the decision
in the row: box 43 (the default context came from matching the fetched owner
list; v2 selects the configured login and asks for it), box 45 (the splash
advertises no cancel, because it has one bounded request and one way out), boxes
71–72 (the error view also takes `r`, and a rate-limit window the client can wait
out is waited out inside the request rather than cleared by a timer), and the
three viewport-forwarding rows in §4 (scrolling is bound explicitly). Each has the
test that covers what v2 does instead.

## Verification

The seven checks, on the final commit of the branch:

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -race -count=1 ./...` | pass — 11 packages, 395 test functions |
| `golangci-lint run ./...` | pass — 0 issues, after fixing the four this phase introduced (a `time.Until`, an embedded-field selector, a De Morgan hint and an unused test field) rather than suppressing them |
| `govulncheck ./...` | 14 vulnerabilities, all in the Go standard library at `go1.26.5`, all fixed by a later patch of the same series (finding F5, unchanged and reproduced on `main`). The two module findings this phase would have added are closed — see below |
| `actionlint` | pass |
| `go mod tidy -diff` | pass (no change) |
| `gofmt -l .` | clean |

**CI.** The `Test and Lint` workflow passed on the branch's code commit
`35aafe2` in 2m06s (run
[38287725969](https://github.com/joangrigorov/ghspector/actions/runs/38287725969)) —
the same steps as the table above, plus the new formatting check, run by the
workflow itself rather than by hand.

**CI gain.** `.github/workflows/ci.yml` now checks formatting. `golangci-lint` is
configured here with an explicit linter list that contains no formatter, so
`gofmt` drift was invisible to CI — and the tree had drifted on `main` (struct
alignment in `internal/gh/types.go`, blank lines in `client.go`, trailing
whitespace in `client_test.go`). That drift is fixed in this branch and the new
step keeps it fixed.

**The dependency finding this phase introduced, and closed.** Adding glamour
brought `github.com/yuin/goldmark v1.7.8` (GO-2026-5320, an XSS in the HTML
renderer, reachable through `TerminalRenderer.Render`) and
`golang.org/x/text v0.24.0` (GO-2026-5970, an infinite loop on invalid input),
with `golang.org/x/net` alongside them. All three were raised inside their own
release line — goldmark `v1.8.6`, `x/net v0.61.0`, `x/text v0.43.0` — and
`govulncheck` reports no module findings afterwards. It is recorded as F6 in the
architecture doc. F5, the standard-library group, is unchanged and needs a
toolchain patch: the `go` directive names `1.26.5` and the fixes are in
`1.26.6`/`1.26.9`, so a maintainer on the patched toolchain builds with them.

### Run against GitHub, read-only, in a pty

The binary built from this branch was driven under `tmux` at 120×40 against the
real API, with the app's own credential path (the third source in the README's
hierarchy: `gh auth token`, since the session's environment has the CLI logged
in). Every call was a read. The walkthrough in one pass:

- **Splash → ready.** The cat logo and `g h s p e c t o r`, then the Pull Requests
  list with live data: `#22 chore(main): release 0.4.0` from the app's own
  repository, a dependabot dependency bump, a pull request from another
  repository, and a header reading
  `Rate limit: 4997/5000   Account: joangrigorov`.
- **The release pull request.** Enter on #22 drew the description as a document —
  `## 🤖 I have created a release beep boop`, `### ⚠ BREAKING CHANGES`, bullets
  with `•`, commit SHAs as links — beside the DETAILS/CHECKS sidebar, with the
  five-sub-tab strip above it. This is the case the emoji option exists for:
  release-please writes its notes in shortcodes.
- **Resize to 80×24 and then 60×16.** The header trail truncated with `…`, the
  details/checks sidebar dropped to the checks alone at 60 columns (the documented
  fallback), nothing clipped mid-row, and the frame stayed exactly the terminal
  size at each step.
- **The Issues tab.** Eleven open issues across two repositories, with the
  assignee column populated. `f` then `u` opened the login prompt; typing `q`
  into it typed a `q` and the program stayed up, with the footer reading
  `Ctrl+C:Quit  Enter:Confirm  Esc:Cancel` — the fix and the footer rule in one
  screenshot. Escape closed the prompt, Escape closed the list.
- **`?` and `o`.** The help overlay rendered its scopes from the keymap, and the
  context switcher listed the viewer first with `← active`, followed by the four
  organizations.
- **`q`.** The program exited and left the terminal usable.

No token was printed, written or passed anywhere it does not belong, and nothing
was merged, closed, approved or written back — including the config file, which
was read but never saved.

### What stands in for browser verification

A terminal UI has no browser to drive, so the substitute is as recorded in
previous phases: `teatest` runs through the real Bubble Tea runtime
(`TestFlowThroughTheProgram`, `TestFlowResizeWhileRunning`,
`TestFlowTooSmallThenRecovered`, `TestFlowFromTheRunListToTheLog`,
`TestFlowThroughThePullRequestScreens`, `TestFlowThroughTheIssueScreens`,
`TestFlowTabCyclingKeepsThePullRequestList`), 58 golden renders across the shells
at 80×24, 120×40, 60×16 and 40×15 with diffs read on every change, and scripted
tests that assert the requests a screen sent. This phase adds 33 test functions
to that set, and the live pty run above is the part none of it can cover: the
join between the real API, the real terminal and the real renderer.

### Not verified

- **macOS and Windows.** The build is portable Go and the tests are
  platform-neutral, but this session ran Linux only. The README's macOS
  instructions and the Windows config path are untouched by this phase.
- **A live light/dark scheme change.** The palette-follows-the-terminal path is
  covered by `TestBackgroundColorChangeMatchesAStartupInThatScheme` and the
  contrast test; `tmux` cannot answer an OSC 11 query the way a real terminal
  does, so the live run did not change schemes under itself.
- **Markdown typographic quality.** The tests pin which colours appear, that no
  renderer is rebuilt per frame, and that no pad or escape leaks; they cannot
  judge whether a table or a deeply nested list reads well. The two bodies that
  matter most for this project — a release-please PR and an issue — were read at
  120×40 and 60×16.

## Deviations from the plan for this phase

1. **The module path changed to `github.com/joangrigorov/ghspector`.** The plan
   said the module path keeps working unchanged, and this is the one thing in the
   rewrite that did not. The bare path `ghspector` is a module only a checkout at
   the repository root can build, so the README's `go install
   github.com/joangrigorov/ghspector/cmd/ghspector@latest` line named a module
   that does not exist on the proxy — it has been false since before the rewrite.
   The rename touches every import in the tree, and nothing else: the binary name,
   the entry point, the goreleaser ldflags target and the release configuration
   are unaffected. Flagged here because it is a deviation, not a detail.
2. **The footer no longer advertises `?` and `o` over a modal.** This is a
   user-visible change the audit asked for rather than one the review asked for.
   It is the honest reading of "renders the route's key hints" (spec §2.3), and it
   is the only presentation change in this phase that was not fixing a defect.
3. **`gofmt` is now a CI step.** Small, and outside the phase's stated scope. It
   exists because this phase is the one that fixed the drift, and a check is what
   keeps it fixed.
4. **Deferred, unchanged:** platform-specific `RepoStats`, automatic REST failure
   tracking, and multi-user caching (from Phase 5), and the toolchain patch that
   closes F5.

## For the review gate

- The release-please PR **#22 stays open and unmerged**, as instructed. This
  branch would sit on top of a `0.4.0` that has not been released; the rewrite's
  version stays `0.*` until you say otherwise.
- `docs/spec.md` is now at zero open boxes. If any tick reads as a claim you do
  not accept, the row names the test that backs it — the disagreement is a test
  to change, not a sentence to argue with.
- The one thing worth a hard look is the markdown dependency: it is the largest
  addition to the module graph in the rewrite, it is the only place the program
  formats text it did not write, and it is where F6 came from.
