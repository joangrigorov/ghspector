# ghspector v2 — architecture

This document defines the target architecture for the rewrite. Its purpose is to make the previous design's failure modes structurally impossible, so that a future contributor cannot accidentally reintroduce them.

Companion documents:

- `docs/spec.md` — the behavioral parity checklist that gates the rewrite.
- The phased plan (session artifact): phases, delivery, and per-phase exit criteria.

---

## 1. Goals and non-goals

**Goals**

1. Deterministic layout. Given `(width, height)` and a route, the rendered screen is fully determined, with no hand-tuned constants and no string slicing.
2. One source of truth per concern: one keymap, one overlay mechanism, one layout engine, one place that computes a view's dimensions.
3. Each screen is a small, independently testable unit with explicit inputs.
4. Scope-wide GitHub queries instead of per-repository fan-out, with real pagination.
5. The existing config file, CLI flags, release pipeline, and module path keep working unchanged.

**Non-goals**

- Rewriting `internal/gh` (REST client) or `internal/auth`. They are correct and encode hard-won API behavior.
- Changing the binary name, module path, entry point, or goreleaser output.
- Adding new user-facing capabilities before parity is reached, except where the architecture makes them free (scope-wide filtering).

---

## 2. Layers

```
cmd/ghspector        wiring only: flags, config, dependency construction, program start
      │
internal/ui          routes, components, theme, chrome          (presentation)
      │
internal/store       caching, pagination, aggregation, rate limiting  (application)
      │
internal/gh   internal/gql                                     (transport)
      │
      └── internal/auth, internal/config                        (infrastructure)
```

Dependency direction is strictly downward. `ui` never imports `gh` or `gql`; it consumes store types. `gh` and `gql` are stateless and know nothing about the UI or the store's cache.

### Package responsibilities

| Package | Responsibility | Must not |
| --- | --- | --- |
| `internal/layout` | `Rect`, `Frame`, splitting, placement, truncation, wrapping | Know about any screen |
| `internal/keymap` | Binding definitions, lookup, footer/help rendering from bindings | Know about any screen's state |
| `internal/overlay` | Modal stack, generic `Confirm`, compositing | Draw its own box characters |
| `internal/ui` | Root model, route stack, routes, chrome, theme | Do arithmetic on `width`/`height`; match raw key strings |
| `internal/ui/widgets` | Table, log view, breadcrumbs, status bar, spinner | Read the root model |
| `internal/store` | Cache, cursors, aggregation, single rate limiter | Format anything for display |
| `internal/gh` | REST endpoints, pagination helpers, error mapping | Hold mutable application state |
| `internal/gql` | GraphQL queries and mutations, cursor pagination | Know about `gh`'s types |
| `internal/auth` | Token resolution, config load/save | Know about the UI |

---

## 3. The five structural fixes

These are the direct answers to the failure modes in the v1 TUI. Each one is enforced by a test or a lint rule, not by convention alone.

### 3.1 Layout engine

**Problem it removes:** 47 hand-tuned constants such as `m.height - 12` and `m.width - 102`, differing per view, which is why scroll and clipping bugs appeared one view at a time.

```go
type Rect struct{ X, Y, W, H int }

type Frame struct {
    Header Rect
    Body   Rect
    Footer Rect
}

func Layout(width, height int) Frame
```

Rules:

- A view receives a `Rect` and renders within it. It never receives the raw terminal size.
- `Rect` gains only what is genuinely needed: `Split` (vertical/horizontal), `Inner` (inset by border/padding), and `Place` (center a smaller block).
- Truncation and wrapping go through `charmbracelet/x/ansi` so wide runes and ANSI sequences are handled by measurement, never by rune indexing.
- The frame is computed once per `WindowSizeMsg`, and a body too small to render produces one documented "terminal too small" view rather than a malformed screen.

**Enforced by:** a resize test that asserts every rendered line is exactly `width` cells and the line count is exactly `height`, across a range of sizes including very narrow and very wide.

### 3.2 Routes instead of `viewState`

**Problem it removes:** one enum serving as screens, modals, and routes at once, which made `View()` a combinatorial branch map and made `Esc` semantics implicit in a `prevState` field.

```go
type Route interface {
    Init() tea.Cmd
    Update(tea.Msg) (Route, tea.Cmd)
    View(r layout.Rect) tea.View
    Title() string
    Bindings() []keymap.Binding
    KeyScope() keymap.Scope
}

type Stack []Route
```

Navigation:

- `Push(route)` for drill-down; `Pop()` for `Esc`. `Esc` behavior is defined once, by the stack, instead of being re-implemented in each state's key switch.
- Breadcrumbs are `Join(stack titles)`. No screen builds its own breadcrumb string.
- Each pushed route carries its own identity — `(owner, repo, number)`, `(owner, repo, runID, attempt)` — instead of reading it back out of shared model fields. This removes the v1 pattern where `getRun()` patched missing repository fields by consulting the selected pull request.

Route inventory (from `docs/spec.md` §3):

```
Splash
Main{tab ∈ PRs | Workflows | Issues}
  Workflows: RunList → Jobs → Logs
  PRs:       PRList → PRDetails{sub-tab} → Commits → CommitDetails
                                        → Files (diff)
                                        → Comments
  Issues:    IssueList → IssueDetails → Comments
```

### 3.3 Overlay stack

**Problem it removes:** modals drawn as literal box-drawing strings with hardcoded widths, composited by stripping ANSI and slicing runes — the direct cause of unpredictable layout corrections.

```go
type Overlay interface {
    Update(tea.Msg) (Overlay, tea.Cmd)   // nil return asks the stack to pop
    View(width int) string               // returns content only, never a box
    Title() string                       // drawn above the body; empty means none
    Bindings() []keymap.Binding
}

type Stack struct{ /* ordered overlays plus the shared Chrome */ }
```

Navigation and stack control travel as messages rather than as methods on the
route interface, so a route never holds stack bookkeeping:

```go
type (PushMsg struct{ Route Route }; PopMsg struct{}; ReplaceMsg struct{ Route Route })
```

Rules:

- An overlay returns content. The chrome, border, padding, and centering come from the overlay stack.
- Composition uses `lipgloss/v2`'s `Compositor` with `NewLayer` and its `X`, `Y`, `Z` positions, which is cell-aware. No ANSI stripping, no rune slicing. A `Compositor` matters here and a bare `Canvas` does not: composing a layer directly hands it the whole canvas as its drawing area, and drawing a styled string clears the area it is given, so a bare layer wipes the frame underneath it and lands at the origin.
- One generic confirm covers merge, close, and run approval:

```go
type Confirm struct {
    Title    string
    Body     []string
    OnYes    tea.Cmd
    Destructive bool
}
```

- Overlays live in their own stack, so the route underneath is rendered once and never re-rendered because modal state changed.

**Enforced by:** a property test that composes random backgrounds with random overlay content — including emoji, CJK, and ANSI-styled text — and asserts the result preserves exact line count and display width, plus a test asserting the frame outside the box survives and a test pinning the box's centred position.

### 3.4 Keymap

**Problem it removes:** bindings declared in three unsynchronized places (the update switch, 16 footer call sites, and the help text), which let the README, the help screen, the footer, and the actual behavior diverge.

```go
type Scope string                     // e.g. "main.workflows", "pr.details", ...
type Action string                    // stable intent, e.g. "open-browser"
type Binding struct {
    Action      Action
    Keys        []string              // values reported by KeyString
    Help        string
    Destructive bool
}
func For(scope Scope) []Binding
func Lookup(scope Scope, key string) (Action, bool)
func KeyString(msg tea.KeyPressMsg) string
```

The keymap is data plus lookup, with no styling and no transport, so it stays
cheap to test and cannot import a screen.

`KeyString` exists because `msg.String()` reports an uppercase letter only when
the terminal supplies the shifted character as `Text`. A terminal using the
Kitty keyboard protocol sets `ShiftedCode` and leaves `Text` empty, in which
case `String()` lowercases the letter and a `C` binding would never fire.
`KeyString` recovers the uppercase form.

Rules:

- Dispatch is by action lookup in the route's scope, falling back to the global
  scope. There are no `switch msg.String()` key literals in route code.
- The footer renders from the same bindings, globals pinned first so a narrow
  footer drops scope hints rather than the keys that work everywhere.
- The help route renders from the same bindings, grouped by scope.
- A test asserts every scope in the table appears in the help order, every
  binding is reachable by lookup, and no scope binds one key to two actions.
- The README keybinding tables are generated from, or asserted against, the same bindings.

**Enforced by:** a test asserting every binding appears in help and is reachable, and that no key is handled outside the keymap.

### 3.5 Store

**Problem it removes:** per-repository goroutine fan-out duplicated across `fetchRunsCmd` and `pollRunsCmd`, an 8-repository cap, page-only filtering, and a hand-rolled rate-limit check.

```go
type Store interface {
    Contexts(ctx context.Context) ([]Context, error)          // user + orgs
    Repositories(ctx context.Context, c Context) ([]Repository, error)

    WorkflowRuns(ctx context.Context, q RunQuery) (RunPage, error)
    Jobs(ctx context.Context, run RunRef, attempt int) ([]Job, error)
    Logs(ctx context.Context, run RunRef, jobID int64) (string, error)

    PullRequests(ctx context.Context, q PRQuery) (PRPage, error)
    PullRequest(ctx context.Context, ref PRRef) (PRDetail, error)
    Issue(ctx context.Context, ref IssueRef) (IssueDetail, error)

    Merge(ctx context.Context, ref PRRef, method MergeMethod) error
    Close(ctx context.Context, ref PRRef) error
    ApproveRun(ctx context.Context, run RunRef) error
    ApproveDeployments(ctx context.Context, run RunRef, envIDs []int64) error
    CanWrite(ctx context.Context, repo RepoRef) (bool, error)
}
```

- Query types (`RunQuery`, `PRQuery`) carry scope, state, actor, author, assignee, reviewer, and cursor. Filtering is a property of the query, which is what lets v2 move it server-side.
- Caching and cursors live here, keyed by resource and scope. The UI holds presentation state only.
- Pagination is cursor-based for GraphQL and page-based for REST, behind the same `Page` types.
- One rate limiter is shared by the REST and GraphQL transports. REST feeds it from response headers; GraphQL feeds it from the `rateLimit { cost remaining resetAt }` field included in every query.

**Enforced by:** store tests that run the same query against a REST-backed and a GraphQL-backed transport and assert equivalent domain objects, plus a test that the limiter depletes by the reported query cost.

---

## 4. Transport split

**GraphQL (`internal/gql`, via `shurcooL/githubv4`)** handles listing and aggregation, where a single cursor-paginated query replaces N per-repository requests:

- workflow runs across a scope
- pull requests across a scope, including author/assignee/reviewer filters server-side
- issues across a scope, with the PR-exclusion handled by the API rather than by a client-side predicate
- repository lists (kept as REST fallback if GraphQL proves awkward for a scope)

**REST (`internal/gh`)** handles what GraphQL does not cover well:

- job logs (`/actions/jobs/{id}/logs`) — REST-only, and the error body is XML
- PR file patches and diffs
- check runs
- write operations already implemented and tested (merge, close, approve, permission checks)

Both transports share the rate limiter and the error taxonomy (rate-limited, forbidden, not-found, generic).

---

## 5. State ownership

| State | Owner |
| --- | --- |
| Terminal size → `Frame` | Root model |
| Route stack, overlay stack | Root model |
| Theme | Root model |
| Keymap scope resolution | Root model |
| Contexts, active context | Store-backed slice in the context switcher route; the active context lives in the root model |
| List data, cursors, selection, focus | The owning route (`RunList`, `PRList`, `IssueList`) |
| Page-level state (`RunPageData`) | The owning route |
| Caches | Store |
| `loading` / `status message` | Root model, since chrome renders both |

Target: on the order of 15 root-model fields, against 90 today. Detail identity (`owner`, `repo`, `number`, `runID`, `attempt`) lives in the route that was pushed, never in a shared field that a sibling route reads.

---

## 6. Dependencies

Pinned at the time of writing (verified against the module proxy):

| Module | Version |
| --- | --- |
| `charm.land/bubbletea/v2` | v2.1.0 |
| `charm.land/lipgloss/v2` | v2.0.6 |
| `charm.land/bubbles/v2` | v2.2.1 (added when the first widget needs it: the log viewport and the text inputs) |
| `charm.land/glamour/v2` | v2.0.1 (added with markdown rendering) |
| `github.com/charmbracelet/x/ansi` | v0.11.9 |
| `github.com/charmbracelet/x/exp/teatest/v2` | v2.0.0-20261011004359-2acd7e345453 (untagged; pin the pseudo-version) |
| `github.com/charmbracelet/x/exp/golden` | v0.0.0-20251109135125-8916d276318f (test-only) |
| `github.com/shurcooL/githubv4` | v0.0.0-20260209031235-2402fdf4a9ed (untagged; pin the pseudo-version) |
| `golang.org/x/time` | v0.16.0 |

Bubble Tea v2 contract changes to build against from the start:

- Charm's v2 modules moved to the `charm.land` vanity domain. The go-import meta tags there point back at `github.com/charmbracelet/<pkg>`, but the module paths declared in their `go.mod` files are `charm.land/...`, so imports must use `charm.land/...`. The two exceptions are `github.com/charmbracelet/x/ansi` (still on the GitHub path) and `github.com/charmbracelet/x/exp/teatest/v2` (declares the GitHub path; `charm.land/x/exp/teatest/v2` does not resolve).
- `View()` returns `tea.View`, constructed with `tea.NewView(content)`; alternate screen is a field on the view rather than a program option.
- Key messages are `KeyPressMsg` / `KeyReleaseMsg` carrying a typed `Key`. Exact signatures are confirmed against pkg.go.dev at the start of Phase 1 rather than assumed.

**Rule:** v1 and v2 Charm import paths must never coexist in the module. A CI check greps the tree for v1 import paths and fails if any are found.

---

## 7. Conventions

These are the rules that keep the design from regressing. Each is enforceable.

1. **No layout arithmetic in a view.** A view may read `rect.W` and `rect.H`; it may not do `height - N`.
2. **No `switch` on key strings in a route.** Keys resolve through the keymap.
3. **No box-drawing literals.** Borders come from `lipgloss` borders or the overlay stack.
4. **No measurement by rune index.** Width and truncation go through `x/ansi`.
5. **No route edits shared sibling state.** A route reads its own data and its own identity.
6. **No direct transport calls from `ui`.** Everything goes through the store.
7. **No v1 Charm imports.**
8. **Every screen has a golden test** at 80×24, 120×40, and 40×15.

---

## 8. Testing strategy

| Layer | Purpose | Location |
| --- | --- | --- |
| Golden renders | Catch layout, chrome, and resize regressions | `internal/ui/**/testdata/*.golden` |
| Property tests | Prove the compositor preserves dimensions with wide runes and ANSI | `internal/overlay` |
| Keymap invariants | Keep bindings, footer, help, and README consistent | `internal/keymap` |
| Scripted flows | Exercise real interactions against mock transports | route tests via teatest v2 |
| Store equivalence | Prove REST and GraphQL backends agree | `internal/store` |
| Client units | Rate limiting, error mapping, pagination, log parsing | `internal/gh`, `internal/gql` |
| Pure functions | Log segmentation, timestamp parsing, status ordering, dedup | alongside the widgets that use them |

Golden files are updated with a `-update` flag and reviewed on every diff. Because a golden file can only prove "unchanged", behavioral confidence comes from the scripted-flow and property tests.

The v1 `internal/tui/app_test.go` is deleted rather than ported: it asserts hardcoded layout arithmetic, which the layout engine makes meaningless.

---

## 9. Release and packaging constraints

Unchanged and load-bearing:

- Module path `ghspector`; entry point `cmd/ghspector/main.go`.
- `main.version`, `main.commit`, `main.date` remain `main`-package variables, injected by goreleaser `-X` ldflags.
- `release-please-config.json` and `.github/workflows/release.yml` stay as they are, plus `bump-minor-pre-major: true` so breaking changes move `0.x` to the next minor while the rewrite is unstable. Moving to `1.0.0` is a deliberate later act.
- `internal/auth.Config`'s YAML keys and the config path stay byte-compatible. A `version` key is added; nothing is removed or renamed.

---

## 10. Build status

### Phase 1 — foundation and shell (merged as #17)

Shipped:

| Package | State |
| --- | --- |
| `internal/layout` | `Rect`, `Frame`, `Layout`, `Fit`, `Truncate`, `Column`, `PadRight`, `FitColumns`. Swept over sizes and randomized wide-rune content. |
| `internal/keymap` | The binding table, `Lookup`, `KeyString`, footer and help rendering inputs, and the generated `docs/keybindings.md`. |
| `internal/overlay` | Modal stack, the generic `Confirm`, and a property test that proves composited frames keep exact geometry. |
| `internal/store` | The `Store` interface, a REST implementation, a keyed TTL cache with single-flight, and one shared `x/time/rate` limiter. |
| `internal/ui` | Root model, route stack, breadcrumbs, header with rate-limit indicator, footer from the keymap, transient status message, splash, error screen, too-small screen, tabbed main route, help route, context switcher. |

Deleted: the v1 `internal/tui` package and its `app_test.go`.

Deliberately not in this phase: list bodies. Phase 1 rendered three tabs with
live chrome, tabs, help, and key routing, but no rows. The data layer arrived
with the routes that need it.

### Phase 2 — Workflows vertical slice (merged as #18)

The Workflows tab, the Jobs route, and the Logs route are real: a run list
aggregated across the context's repositories, per-attempt job lists, the log
viewer with step segmentation and follow mode, and run approval with pending
deployments.

| Package | State |
| --- | --- |
| `internal/logs` | Log parsing, cleaning and segmentation, ported from v1's `update.go` as pure functions: `Parse`, `Clean`, `Segment`, `MatchScore`, `ParseTimestamp`. |
| `internal/store` | `ScopeRuns` (8-repository fan-out, status sort, merged poll results), `Jobs` per attempt, `JobLogs`, `PendingDeployments`, `ApproveRun`, `ApproveDeployments`, `RepoPermission`, `MissingScopes`. |
| `internal/gh` | Job-log reads are capped at 8 MiB with a truncation note, and error bodies at 64 KiB (finding F3). |
| `internal/overlay` | `Menu`, `List` and `Prompt` join `Confirm`; the stack composites through a `Compositor` (see the fix below); overlays are no longer `Title`-only. |
| `internal/keymap` | `MainWorkflows`, `Jobs`, `Logs`, `FilterType`, `RepoSelect`, `UserInput` and `ApproveRun` scopes carry every key the phase adds. |
| `internal/ui` | `Main.runsTab` (list, filters, pagination, approval answers), the `Jobs` route (attempt cycling, step counts, approval banner), the `Logs` route (steps sidebar, viewport, follow mode), the filter menu, repository picker, actor prompt and approval overlays, and a shared table renderer. |

Fixed while building this:

- **Modal compositing erased the frame behind it.** `Stack.View` composed bare
  `lipgloss` layers onto a `Canvas`. `Layer.Draw` ignores `X`/`Y` and a styled
  string clears the area it is given, so every modal landed at the origin and
  wiped the region it was composed into. The stack now builds a `Compositor`,
  which hands each layer its own rectangle. Two tests were added for the
  behaviours that were silently wrong: the frame around the box survives, and
  the box is centred.
- **The actor prompt rendered as `l`.** A `textinput` with no width set draws
  only the first rune of its placeholder, so an empty filter prompt showed `l`
  for `login` and offered no visible field. The input now has a width.

Verification: see the walkthrough for the phase. In short — `go build`, `go vet`,
`go test -race ./...`, `golangci-lint`, `govulncheck`, `actionlint` and
`go mod tidy -diff` all pass; screens are pinned by 24 golden renders, the poll
loop by a fake clock, and the list → jobs → logs path by a `teatest` run through
the real Bubble Tea runtime.

### Phase 3 — Pull requests vertical slice (this change)

The Pull Requests tab, the pull request detail route with its five sub-tabs, and
the merge and close flows are real: a pull request list aggregated across the
context's repositories, a detail screen whose checks resolve back to the workflow
runs that produced them, a commit list with commit detail, a files-and-diff view,
a comment list, and merge and close behind a write gate that fails closed.

| Package | State |
| --- | --- |
| `internal/store` | `PullRequests` through the same 8-repository fan-out and `repoTargets` helper the runs list uses, with `state` forwarded and poll results merged by ID; `PullRequest` detail, `PRCommits`, `PRFiles`, `Comments`, `Commit`, `CheckRuns`, `Merge`, `Close`, `RepoPermission`. |
| `internal/ui` | `Main.prsTab`, the `PRDetails` route with its five sub-tabs (Info, Commits, Files, Checks, Comments), the three-pane Info row, the write gate, the merge and close overlays, and OSC 8 hyperlinks. |
| `internal/overlay` | `method.go`: the merge-method select, built on the shared `Menu`, so the merge flow is overlays rather than a modal state machine. |
| `internal/keymap` | `MainPRs`, `PRDetails`, `PRCommits`, `CommitDetails`, `PRDiff`, `PRComments`, `PRFilterType`, `MergeMethod`, `MergeConfirm`, `CloseConfirm`. |
| `internal/auth` | `SetDefaultMergeMethod` edits the config file as a YAML document, so unknown keys survive and the token is never re-encoded. |
| `cmd/ghspector` | `run(args, stdout)` takes its arguments and version writer as parameters, so the flag handling is tested without a terminal. |

Fixed while building this:

- **The write gate failed open (finding F1, closed).** The baseline's
  `viewerCanMerge()` answered "yes" whenever the `X-OAuth-Scopes` header was
  absent, so a token the API said nothing about was advertised as able to write
  and then rejected at the API. The gate now asks the scope question first and,
  when the header is absent, asks the question that decides the matter:
  repository write permission for the authenticated user. An unreadable
  permission is a refusal. The refusals, the scoped path, a `read` permission and
  an unreadable permission all have tests in `internal/ui/prs_test.go`.
- **A chain of overlays drew as one box with a doubled border.** `Stack.View`
  centred every layer at the same place, so opening the repository picker from
  the filter menu produced two boxes on top of each other and the only evidence
  of a stack was a doubled border. Layers now cascade two cells up and to the
  left per level of depth, which is also what made the *already merged* Phase 2
  goldens show the defect; they were regenerated and read.
- **The Info row could draw an empty pane's separator.** At 60 columns the
  metadata pane was too narrow to hold a label and a value, so the row rendered
  `││` and a truncated answer. The pane is now dropped rather than squeezed, and
  the width it would have used returns to the description; a property test
  renders every width from 12 to 200 and asserts the row tiles the body exactly,
  with no doubled separator.
- **The test store throttled its own fixtures.** The shared limiter paces a
  person's requests one at a time, and a UI test drives a whole screen's fetch at
  once, so a golden rendered "Loading…" instead of the data. UI tests now lift
  the limit through one helper, and the limiter keeps its own tests in `store`.

Verification: see the walkthrough for the phase. In short — the same seven
checks as Phase 2 pass; screens are pinned by 18 pull request goldens and the
full golden set, the merge/close and filter flows by scripted tests, the pull
request → sub-tab → detail path and the tab-cycling path by `teatest` runs
through the real Bubble Tea runtime, and the pane geometry by property tests.

### Security findings

| Finding | Status |
| --- | --- |
| F2 — the browser opener accepted any URL scheme | Closed. `ui.OpenBrowser` validates the scheme and host at the boundary; `ValidateBrowserURL` is unit-tested. |
| F4 — the credential source label printed the config file's absolute path | Closed. `auth` reports the file name only; a test asserts no path separator reaches the label. |
| F1 — `viewerCanMerge` fails open when the scope header is absent | **Closed in Phase 3.** The merge and close gate (`ui.decidePRWrite`) fails closed: repository write permission decides, an unreadable permission is a refusal, and the baseline's scope-header-only answer is gone. |
| F3 — `io.ReadAll` on job logs is unbounded | **Closed in Phase 2.** `gh.GetJobLogs` reads through a capped reader (`maxLogBytes`, 8 MiB) and appends a truncation note on a line boundary; error bodies are capped at 64 KiB. Tested against an oversized body and for line-boundary truncation. Multiplied by per-route polling, this was a memory-exhaustion vector rather than a vulnerability. |
| F5 — 14 stdlib advisories at `go1.26.5` | Open, standalone. Fixed by the `go1.26.6` toolchain; reproduced identically on the previous `main`, so it is not introduced here. |

### Release hold

Do not merge a release-please release PR between the first and last rewrite
phase. The intermediate states are deliberately incomplete, and cutting a
release from one would publish a shell.
