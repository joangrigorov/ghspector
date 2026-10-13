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
5. The existing config file, CLI flags, release pipeline, and binary name keep working unchanged. The module path is the one exception, and it changed on purpose: it is now `github.com/joangrigorov/ghspector`, so the README's `go install` line resolves instead of naming a module only this checkout can build (see §9).

**Non-goals**

- Rewriting `internal/gh` (REST client) or `internal/auth`. They are correct and encode hard-won API behavior.
- Changing the binary name, entry point, or goreleaser output.
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

Dependency direction is strictly downward. `ui` never imports `gh` or `gql`; it consumes store types. `gh` and `gql` carry no application state — `gql.Client` remembers the last rate-limit report and nothing else — and know nothing about the UI or the store's cache.

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
| `internal/gql` | GraphQL queries, cursor pagination, and mapping a response into the domain types | Define its own parallel types for what it returns; reach the network outside the store's limiter |
| `internal/auth` | Token resolution, config load/save | Know about the UI |
| `internal/sanitize` | `Text`/`Texts`: strip escape sequences and control characters out of strings the API wrote | Know what a screen looks like |

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

**One content box.** `layout.Gutter` (2 cells) is the space every screen keeps clear at its left and right edge, and one screen's rows all share one box: a bar's text, a rule, a table row and a pane start at cell 2 and end at `width-3`. The chrome measures itself with `layout.ContentWidth`, a route is handed `layout.BoxWidth`, and the left gutter is the surface's own — a table row reserves it for the selection marker, a heading and a pane write it out, and `gutterBlock` shifts a pane block into it. `TestEveryScreenKeepsTheGutter` walks fifteen screens at three sizes and fails on a row that writes into either gutter, which is the check the defect it replaces needed: the description and the sidebar used to start in column zero, so the body ran into both edges while the bars above it were inset.

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
- Pagination is cursor-based for GraphQL and page-based for REST, behind the same `Page` types. A cursor API cannot jump to page N, so `store.searchScope` walks the pages before the one asked for; each walk step goes through the cache, and the page before the one being loaded is the page on screen, so a "Load More" costs one query.
- One rate limiter is shared by the REST and GraphQL transports. REST spends a request; GraphQL reserves the points its query cost, which the response reports after the fact, so the previous query's cost is what the next one reserves. The header renders whichever transport has less left, because either one can cut the app off.

**Enforced by:** store tests that run the same query against a REST-backed and a GraphQL-backed transport and assert equivalent domain objects, plus a test that the limiter depletes by the reported query cost.

---

## 4. Transport split

Which list belongs on which transport is a schema question, so the answer was read off the published schema (`https://docs.github.com/public/fpt/schema.docs.graphql`) rather than assumed.

**GraphQL (`internal/gql`, via `shurcooL/githubv4`)** serves the scope-wide pull request and issue lists. `Query.search` is the only connection that spans a scope for them: `Organization` exposes no issue or pull request connection at all, and `User.pullRequests` / `User.issues` are the authored-by connections, which answer a different question. Each list is therefore one cursor-paginated search query per page, with `state`, `repo`, `author`, `assignee` and `review-requested` as qualifiers the API applies across the whole scope, `HasMore` from the index's own match count, and the cursor the page ends with starting the next one. `Repository.pullRequests` and `Repository.issues` exist, but using them would rebuild the per-repository fan-out in a different dialect.

**REST (`internal/gh`)** serves everything else:

- workflow runs across a scope, which no transport can list: the schema's only run connection is `Workflow.runs`, which hangs off a single workflow file, and `Query.search` has no run index (`SearchType` is discussion, issue, repository and user). The runs list keeps its bounded fan-out, and `store.maxAggregateRepos` is that cap.
- the fallback for a scope the search index will not serve
- job logs (`/actions/jobs/{id}/logs`) — REST-only, and the error body is XML
- repository lists (the fan-out's targets and the context switcher)
- PR file patches and diffs
- check runs
- write operations already implemented and tested (merge, close, approve, permission checks)

The choice lives in `internal/store`: `Client.ScopePRs` and `Client.ScopeIssues` try the search query and fall back to the fan-out, so nothing above the store knows which transport answered. Both transports share the rate limiter and the error taxonomy (rate-limited, forbidden, not-found, generic).

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
| `charm.land/glamour/v2` | v2.0.1 (direct from Phase 6: markdown rendering) |
| `github.com/charmbracelet/x/ansi` | v0.11.9 |
| `github.com/charmbracelet/x/exp/teatest/v2` | v2.0.0-20261011004359-2acd7e345453 (untagged; pin the pseudo-version) |
| `github.com/charmbracelet/x/exp/golden` | v0.0.0-20251109135125-8916d276318f (test-only) |
| `github.com/shurcooL/githubv4` | v0.0.0-20260209031235-2402fdf4a9ed (untagged; pin the pseudo-version) |
| `github.com/shurcooL/graphql` | v0.0.0-20240915155400-7ee5256398cf (indirect, through `githubv4`) |
| `golang.org/x/time` | v0.16.0 |
| `golang.org/x/oauth2` | v0.37.0 (indirect requirement only — see below) |

**What glamour brings.** Markdown rendering is the one dependency this rewrite
added that is not Charm's own rendering stack, and it is the largest single
addition to the module graph. `go mod why` names the path for each: through
`charm.land/glamour/v2` come `yuin/goldmark` and `yuin/goldmark-emoji` (the
parser); through `glamour/v2/ansi` come `alecthomas/chroma/v2` (fenced code
blocks), `microcosm-cc/bluemonday` (sanitizing) and `golang.org/x/text`; and
`golang.org/x/net` arrives under bluemonday for its HTML parser. All of them are
indirect requirements. Two things follow. First, a body is untrusted input drawn
on a terminal, so it goes through `internal/sanitize` twice — at the transport
boundary and again in `Markdown.Lines` — and the renderer is built from glamour's
plain ASCII style with this palette's three colours on top, so nothing glamour
decides can introduce an escape sequence the chrome did not choose. Second, the
module graph is worth a `govulncheck` run at each phase: `golang.org/x/net
v0.39.0` is the version glamour pulls, and it is the one non-stdlib module in the
tree with advisories against nearby versions.

**No OAuth dependency.** The GraphQL transport authenticates with the same token
the REST transport uses, written into the `Authorization` header by a ~20-line
`authTransport` `http.RoundTripper`, so `githubv4.NewClient` receives a stock
`*http.Client`. `golang.org/x/oauth2` appears in `go.mod` as an indirect
requirement because `githubv4`'s own `example_test.go` imports it, and `go mod
tidy` records it for that reason. `go list -deps` over ghspector's packages does
not include it, so no `oauth2` code is compiled into the binary.

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
9. **No terminal query from a view.** The terminal is asked about its background
   and its focus at the root, once, and the answer arrives as a message that
   rebuilds the palette. A view renders; it does not sample. The baseline's
   markdown renderer called `lipgloss.HasDarkBackground()` inside the render path,
   which is one blocking probe per frame and is why the same version was both
   well rendered and unusable.
10. **One highlight treatment.** Every list marks its selected row the same way:
    the shared marker in the gutter, and the row's own text in the accent colour.
    A list draws no background bar, and no screen invents its own cursor style.
11. **Every theme colour is chosen against the surface it is drawn on**, and a
    test measures the contrast. A colour that assumes a dark terminal and is
    drawn on a light one is invisible, which is the defect this rule exists to
    prevent.
12. **Every string the API wrote is cleaned once at the boundary that receives it**,
    and again at the boundary that turns it into cells. `gh` and `gql` clean a
    decoded response, and `logs` cleans the raw log text; `Markdown.Lines`
    cleans whatever it is handed, because a body reaches it through code as well
    as through the transport. `ansi.Strip` alone is not enough: it removes escape
    sequences and passes control characters through, so `sanitize.Text` removes
    both. A test in each package asserts a response carrying an OSC 52 clipboard
    write reaches the screen as text.
13. **The footer advertises the keys that work on the surface it is drawn over.**
    A route gets the global keys pinned; a modal that consumes them gets quit
    alone; a text box gets Ctrl+C. Advertising a key that the surface in front of
    it swallows is the same defect as documenting a key that does nothing.
14. **One content box per screen.** Every row of a screen — a bar, a rule, a
    table row, a pane — starts at `layout.Gutter` and ends at `width-Gutter-1`.
    The chrome measures with `layout.ContentWidth`, a route is handed
    `layout.BoxWidth`, and a block that has to move asks for `paneRect` rather
    than adding spaces. A length that runs from the left gutter to the far edge
    is written as `width - layout.Gutter` by the surface that owns the indent,
    which is what keeps a rule the same length as the text above it.

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

- Entry point `cmd/ghspector/main.go`.
- `main.version`, `main.commit`, `main.date` remain `main`-package variables, injected by goreleaser `-X` ldflags.
- `release-please-config.json` and `.github/workflows/release.yml` stay as they are, plus `bump-minor-pre-major: true` so breaking changes move `0.x` to the next minor while the rewrite is unstable. Moving to `1.0.0` is a deliberate later act.
- `internal/auth.Config`'s YAML keys and the config path stay byte-compatible. A `version` key is added; nothing is removed or renamed.

Changed once, deliberately:

- **The module path is `github.com/joangrigorov/ghspector`.** It was the bare
  `ghspector` through Phase 4, which is a module path only a checkout at the
  repository root can build. The README has always advertised `go install
  github.com/joangrigorov/ghspector/cmd/ghspector@latest`, and that line was
  false: it named a module that does not exist on the proxy. The rename touches
  every import in the tree and nothing else — the binary name, the entry point,
  the ldflags target and the release configuration are unaffected, because
  goreleaser builds `cmd/ghspector` and `main`'s package is still `main`.
  `cmd/ghspector/main_test.go` and the goreleaser `-X` flags are the places to
  check after a rename like this; both are green.

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
| `internal/ui` | `Main.prsTab`, the `PRDetails` route with its five sub-tabs (Info, Commits, Files, Checks, Comments), the two-column Info row, the write gate, the merge and close overlays, and OSC 8 hyperlinks. |
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

### Phase 3 follow-up — palette, highlight, header (this change)

A read-through of the phase 3 build found five presentation defects. None of them
was a missing capability; all of them were the palette or the highlight being
applied differently on different screens.

- **A highlighted row vanished on a light terminal.** The switcher drew its
  cursor row as `theme.Selected` — the colour `#ffffff` — as a *foreground* with
  no background, so the selected account was white on white. The palette no
  longer has a `Selected` colour at all: `selectedRow` is the one highlight in the
  program, and it is a marker plus the accent colour.
- **The theme froze at startup.** `cmd/ghspector` sampled the terminal background
  with `lipgloss.HasDarkBackground(os.Stdin, os.Stdout)` once, before Bubble Tea
  had started, and nothing re-sampled it. Changing the OS scheme while the program
  ran therefore left the dark palette's greys on a light screen: the pull request
  detail values and the checks drew dark on dark. The model now asks the terminal
  itself (`tea.RequestBackgroundColor` in `Init`, again on `tea.FocusMsg` with
  `View.ReportFocus` on), and `withTheme` rebuilds the palette *and* the overlay
  chrome from the answer. A test renders the same screen twice — once after a
  background change, once from a startup in that scheme — and requires the two to
  be identical.
- **The highlight was not one treatment.** The log viewer's step list drew ` > `
  in a gutter the tables did not use, so the cursor looked different in the job
  view than in the account switcher. Every list now builds rows the same way:
  `tableIndent` on a plain row, `selectMarker` on the highlighted one.
- **The side of the Info row.** The details and the checks each had a column of
  their own beside the description, which left the description narrow and the
  details permanently short of rows. The sidebar now stacks the details over the
  checks, and the description keeps the width the third column would have taken.
  The details block gives way first on a short body, and it is drawn at 80×24 —
  the size the review was read at — which is why `detailsRows` is the block's own
  height rather than one row more.
- **The header carried a wordmark.** `ghspector  │  <trail>` spent seven columns
  of a narrow header saying what the splash and the window title already say. The
  header now starts with the trail.

The `Selected`/`SelectedBackground` pair is gone from `Theme`, so the class of
defect is gone with it: a colour that was used as a foreground on one screen and
as a background on another has no fields to live in. `internal/ui/theme_test.go`
measures every remaining colour's contrast against the surface it is drawn on
(3:1, the WCAG minimum for a glyph or large text), which is what `StatusWarn` on
white needed to be checked against rather than assumed.

Verification: see the walkthrough for the change. The same seven checks as the
phases pass; the palette is pinned by the contrast test and the background-change
equivalence test, the highlight by a unit test on `selectedRow` and by every list
golden, and the header by a test that reads the first line of a live render.

### Phase 4 — Issues vertical slice (this change)

The Issues tab, the issue detail route, and the conversation are real: an issue
list aggregated across the context's repositories with the pull requests the
endpoint returns alongside them dropped, a detail screen with the description
beside its metadata sidebar, and a comments view. The three top-level lists now
have the same shape — filters, pagination, polling, a Load More sentinel — and
the two description-beside-a-sidebar screens share one column split.

| Package | State |
| --- | --- |
| `internal/store` | `ScopeIssues` over the same `repoTargets` fan-out the runs and pull request lists use, `MergeIssues` and `SortIssues` (`updated_at` descending, then the higher number), `IssueQuery.{Repo,Author,Assignee}` applied over the merged page, and `HasMore` read from the raw page before pull requests are dropped. |
| `internal/gh` | `GetIssuesWithState` keeps its pull-request filter, and the filter now has a transport test of its own: the page's raw length decides `HasMore`. |
| `internal/ui` | `Main.issuesTab`, the `IssueDetails` route (description and sidebar with the Tab toggle), the `IssueComments` route, the issue filter menu, the login prompt and the two-role picker. |
| `internal/keymap` | `IssueComments` gains `u`/`d`, so a thread scrolls the way the pull request conversation does; `docs/keybindings.md` is regenerated. |
| `internal/auth` | `polling.issues_interval_seconds` (default 10 s); the legacy `polling_interval_seconds` now sets all three intervals. |

Fixed while building this:

- **The Issues tab owned no interval.** Spec §5.4 recorded the defect: the
  interval selector special-cased the Pull Requests scope, so the Issues tab rode
  the workflows cadence. `pollInterval` now selects on `keymap.MainIssues`, and
  the tab has a key of its own in the config file. A poll test releases the tick
  and asserts the interval the tab asked for.
- **The issue filters could not be reused as they were.** `milestoneTitle` and
  `labelNames` took a `store.PullRequest`, and the conversation renderer was a
  method on `PRDetails`, so neither could serve the issue screen. They are now
  `milestoneName`, `labelList` and `commentBlockLines`, and the pull request
  screens call the same functions. `infoColumns` became `detailColumns`, since
  two screens now split a row the same way.
- **The tab-strip placeholder had no reason to exist.** `Main`'s third tab drew a
  "this list arrives with the data layer" sentence; with the tab real, the
  `tabMeta.Empty` field and the fallback body were deleted rather than left
  unreachable.

Verification: see the walkthrough for the phase. The same seven checks as the
previous phases pass; the new screens are pinned by 16 goldens at three widths,
the filter and pagination paths by scripted tests that assert the queries the tab
sent, the exclusion of pull requests by a transport test and by a list test that
counts rows on screen, and the tab, its fetch and its return by a `teatest` run
through the runtime. The issue store gets its own tests for the fan-out, the
sort, the merge, the filters, `HasMore` and the partial-failure rule.

### Phase 5 — GraphQL aggregation (this change)

The scope-wide lists stop being a guess. Pull requests and issues are one
cursor-paginated GraphQL search query per page, with the API applying the
filters across the whole scope and reporting the match count the list ends on;
the REST fan-out stays underneath them as the fallback. The `store.Store`
interface did not change, and neither did a line of `internal/ui`.

The phase's exit criterion was "run/PR/issue lists come from GraphQL". Two of the
three could, and the third cannot, which is why this section records the schema
evidence as much as the delivery.

| Package | State |
| --- | --- |
| `internal/gql` | New. `Client` wraps `shurcooL/githubv4` around a stock `http.Client` (15 s timeout, `http.DefaultTransport`, the token added by a 20-line `authTransport` `RoundTripper`, so the token never reaches a URL, a query, or a message), plus a `Reserver` seam onto the store's limiter, the last `rateLimit` report, and an error mapper onto `gh`'s taxonomy. `SearchIssues`/`SearchPullRequests` build the qualifier query, page it with `after` cursors, request `rateLimit { limit cost remaining resetAt }` on every query, and map responses straight into `gh.Issue`/`gh.PullRequest`. |
| `internal/store/client.go` | New. `Client` composes `REST` with `gql`. `ScopePRs`/`ScopeIssues` run the search and fall back to the fan-out; `ScopeRuns` stays on `REST`. A 30-second breaker stops a query the API refused from being retried on every poll. `RateLimit()` reports whichever transport has less left. |
| `internal/store/search.go` | New. `searchScope`/`searchPager` walk the cursors before the page asked for through the cache, so a "Load More" costs one query and a poll that lands on the visible page costs none. `HasMore` comes from the index's match count, and the page is not post-filtered, because the API's qualifiers are exact and a second filter would desync the count from the rows. |
| `internal/gh` | `GetWorkflowRuns` returns the repository's `total_count` with the page, and `listOfTotal` turns it into `HasMore`: the runs list ends on a count the API reported rather than on a full page it guessed from. |
| `internal/ui` | Unchanged, which is the phase's proof that the seam held. The UI fixture server still serves REST and refuses `/graphql`, so the UI tests drive the documented fallback on purpose; the search path is covered by the `internal/gql` and `internal/store` tests. |

Fixed while building this:

- **A rate limit can only be read after the query it describes.** `do` first
  reserved the reported cost by passing `q.RateLimit.budget` — a bound method
  value, which evaluates its receiver when it is created. The client therefore
  reserved the zero-valued copy taken before the query ran, and every reservation
  was one point. It now passes a closure that reads the populated field, and a
  test asserts that the cost the API reports is the cost the next query reserves.
- **The decoder rejects a field the query never selected.** `shurcooL/graphql`
  errors on unknown JSON fields, so a stub that invented a `requestedReviewer`
  team arm failed with `struct field for "name" doesn't exist`. The fixture now
  sends a real non-`User` arm (empty, since none of the team fields are
  selected), which is what a query that only reads `login` would see.
- **A field tagged with a fragment cannot name a JSON key.** The query writer
  writes a `graphql` tag *in place of* the field's name, and the response decoder
  refuses to look up a key in a field whose tag begins with `...`. So a fragment
  tag belongs on a field *inside* the struct that carries the name — which is why
  `reviewRequests{nodes{requestedReviewer{... on User{login,databaseId}}}}` has
  the tag on `User` and not on `RequestedReviewer`. Putting it on the outer field
  produced both a malformed query and
  `struct field for "requestedReviewer" doesn't exist in any of 3 places`.

Found by running it against GitHub, which no stub in this repository can do:

- **`databaseId` is not a field of the `Actor` interface.** The first live query
  came back `Field 'databaseId' doesn't exist on type 'Actor'`, and every
  scope-wide list silently fell back to the fan-out — the app worked, without the
  feature. The id is now read through the fragments of the concrete types that
  carry one (`... on User{databaseId}`, `... on Bot{__typename,databaseId}`), the
  assignee connection and the review request union read it directly because their
  type is the concrete `User`, and `TestQueriesSelectAnActorsIDOnItsOwnType` pins
  both shapes for both queries. A stub server answers whatever it is asked, so
  nothing short of the real schema could have caught this; the check that did is
  recorded below.
- **An app's account has two spellings, and the index knows a third.** GraphQL
  reports a bot as `github-actions`, REST as `github-actions[bot]`, and the search
  index matches only `author:app/github-actions` — verified against the live
  index, where the bare slug returns nothing with the scope qualifier or without
  it. Two consequences, both fixed: the bot's login is restored to the REST
  spelling from the `__typename` the `Bot` fragment carries, so the two transports
  describe the same actor and the column reads the way it always did; and
  `appQualifier` turns a login ending in `[bot]` into `app/<slug>` before the
  filter reaches the index, so a filter built from what is on screen matches.

Deviations from the plan for this phase, to be reported at the review gate:

1. **Workflow runs keep the REST fan-out.** The plan asked for all three lists on
   GraphQL. `Workflow.runs` is the schema's only run connection and it hangs off
   a single workflow file; `Query.search` has no run index (`SearchType` is
   discussion, issue, repository, user). The list therefore keeps the fan-out and
   the cap, and `docs/spec.md` §5.3 records the same evidence.
2. **The `limit > 8` cap was consolidated, not deleted.** The four duplicated
   fan-out bodies in the previous `internal/tui/update.go` are gone: one
   `repoTargets` helper and one `maxAggregateRepos = 8` constant now serve the
   runs list and the fallback. The cap itself survives for the runs list, which
   is the one list that still fans out.
3. **`golang.org/x/oauth2` is not a code dependency.** The transport writes the
   bearer header itself. See §6 for why the module appears in `go.mod` anyway.
4. **A failed search is not retried immediately.** A scope the API refuses falls
   back to REST and the search stays off for 30 seconds, so a poll every five
   seconds does not turn one refusal into twelve.
5. **Deferred:** platform-specific `RepoStats` on `RepoRef`, automatic REST
   failure tracking, and multi-user caching. None is needed for the exit
   criteria, and each would add state without a caller.

Verification: see the walkthrough for the phase. Beyond the suite below, this
phase was the first with a path a stub could not stand in for, so it was run
against GitHub. That run is what found the `Actor` bug above; after the fix it
shows the scope-wide lists answering from the search index — eleven open issues
across two repositories where the eight-repository fan-out sees eight, three open
pull requests where the fan-out sees two — an app filter narrowing the list to
the one pull request the app opened, and the runs list unchanged on its fan-out.

The suite behind that: the same checks as the previous phases. The new transport
is pinned by 14 tests against a stubbed GraphQL server — query shape, domain
mapping, totals and cursors, cost reservation, the error taxonomy, the qualifier
builder and the app-login rule — and the store by a suite that runs the same
scope against a GraphQL server and a REST one and asserts equivalent domain
objects, plus the cursor walk, page caching, the fallback, the fallback's error
preference, the breaker, and the two-transport rate limit. There is no browser to
drive for a terminal UI, so the substitute stands as before: `teatest` runs
through the runtime, one golden per screen at 80×24, 120×40 and 40×15, and
scripted tests that assert the requests a screen sent, all of which pass
unchanged in this phase.

### Phase 6 — parity audit, stability, and 1.0 readiness (this change)

The rewrite's last phase is the one that reads the specification back against the
build. `docs/spec.md` had two groups of boxes deliberately unticked since Phase 1
— startup, context and global chrome (§2.1–2.5) and the transport contract (§9) —
on the grounds that they were implemented and partly tested but never read
against their own test. They now are, every row names the test behind it, and
every one of the document's 226 boxes is ticked or marked **Superseded** with the
decision that replaced it.

| Area | State |
| --- | --- |
| `internal/ui/markdown.go` | New. One `glamour/v2` renderer per (palette, width), built by `Prepare` on a resize or a palette change and reused by every frame in between. The style is glamour's plain ASCII configuration with this palette's accent, text and muted colours on top, and `WithEmoji` is on so a `:shortcode:` in a release note is drawn as the emoji it names. Four call sites: the pull request description, the issue description, a comment body, a commit message. |
| `internal/ui/chrome.go` | The header's loading indicator is wired to the screen on top rather than to a constant; the status message's TTL is the spec's 3 s; the rate-limit band counts the window down on the API's clock; the footer pins the global keys that the surface in front of it actually answers. |
| `internal/gh` | `RateLimitResetIn` and a corrected `Now` give the countdown and the wait decision a caller; both had none, which is how the clock offset went unnoticed. |
| `internal/keymap`, `internal/overlay` | `overlay.TextEntry`/`Stack.TakesText`, so a `q` typed into a box is a character. |
| `cmd/ghspector` | `resolveToken`/`printAuthInstructions` are seams, so the no-credentials path is driven rather than read. |

Fixed while running that pass — five defects, each found by reading a box against
its test rather than by a failing test:

- **The header's spinner was wired to `false`.** `renderHeader` took a `loading`
  argument and the call site passed a literal, so the header never animated while
  a screen was fetching. It now asks the screen on top (`loadingRoute`), and the
  spinner re-arms from the tick handler while there is something to animate.
  Four goldens changed by one glyph, which is the size of the defect.
- **A `q` typed into a filter box quit the program.** The root answered quit
  before any overlay saw the key, so a login beginning with `q` — or containing
  one — ended the session. Quit now gives way to a text box; `Ctrl+C` is the way
  out, which is what the footer says.
- **The footer advertised keys the surface in front of it swallows.** A modal
  eats `?` and `o`; a text box types them. The pinned globals are now chosen from
  what is on top (rule 13), which changed 12 goldens.
- **The status message lived 5 s where the spec says 3.** `statusTTL` was the
  only place the number appeared, which is why it drifted.
- **The rate-limit countdown had no caller.** `Client.Now` existed for the
  offset and nothing used it, so the header printed `0/5000` with no reset time
  and the wait decision compared a server timestamp to a local clock.

Verification: see the walkthrough for the phase. The same checks as the previous
phases pass, plus `golangci-lint` with the four issues this phase introduced
fixed rather than suppressed. There is no browser to drive for a terminal UI, so
the substitute stands: `teatest` runs through the real runtime, 60 golden
renders across the shells, and scripted tests that assert the requests a screen
sent. What a golden cannot prove is that the markdown is *good* — glamour's own
choices are pinned by the palette test instead, which asserts that every RGB
triple in a render is one of the theme's.

### Fix — the conversation is three lists (this change)

The Comments sub-tab read one endpoint. GitHub keeps a pull request's discussion
in three: conversation comments, reviews, and the inline comments a review
carries. A pull request that was reviewed and never discussed therefore had
nothing in the first list and everything in the other two, and the sub-tab drew
its empty sentence over the top of it.

The route now reads all three and merges them. `internal/ui/conversation.go` is
the whole of it: `newConversation` buckets the inline comments by the review that
owns them, drops a review the viewer has not submitted together with the drafts
it holds, keeps a thread whose review is not in the page as a contribution of its
own, and sorts the result by time with an unknown time last.
`conversationLines` renders one block per contribution with a rule between them.
A review's state is a badge the rest of the app already knows how to draw, and
its threads read as `path:line`, the last three lines of the hunk, and the
exchange.

| Package | State |
| --- | --- |
| `internal/gh` | `GetPullRequestReviews`, `GetPullRequestReviewComments`, and the `Review`/`ReviewComment` records they decode into. |
| `internal/store` | `Reviews` and `ReviewComments`, cached under their own keys and read at `defaultConversationPageSize` (100, the API's maximum): one page per endpoint is what keeps a review-heavy pull request a fixed cost, and `List.HasMore` says when the pane is showing part of it. |
| `internal/ui/conversation.go` | New. The merge, the ordering, and the rendering. |
| `internal/ui/prdetails.go`, `prpanes.go` | The Comments sub-tab loads three sections instead of one; `PRDetails.failed` remembers which sections failed, so a list that never arrived is not lost when a later one succeeds; `commentBlock` is the single-comment renderer the issue conversation and the pull request conversation now share. |
| `docs/spec.md` | The endpoint table gains the two routes, and §4.14 records the fix and its two deliberate edges. |

Fixed while building it:

- **One `err` could not describe three fetches.** `PRDetails.err` is set by
  whichever section failed last and cleared by whichever succeeded last, so a
  failed review list followed by a successful comment list left the tab unable to
  tell "still loading" from "never arrived". `PRDetails.failed` is per section,
  and `loadMessage` reads it. The same correction fixes the commits and files
  panes, which had the same latent bug behind a single-section tab.
- **A refresh kept the truncation note.** `conversationHasMore` was set and never
  cleared, so a pane that had once seen a full page said so for the rest of the
  session. It is reset with the sections the refresh drops.

Verification: the checks the other phases list, plus a live run of the built
binary. There is no browser to drive for a terminal UI; the substitutes are
`teatest` through the real runtime, three new goldens (the standard conversation
at two widths, and the review-heavy shape that was reported as broken), unit
tests for the merge, the ordering, the draft, the orphan thread, the hunk tail
and the truncation note, and scripted request assertions that the sub-tab reads
all three endpoints.

### Fix — the pane renders once per change (this change)

Two defects were reported together against the Comments sub-tab, and both were
reproduced before either was changed.

**The arrows did nothing.** `PRComments` and `IssueComments` bound `u`/`d` and
the page keys, and nothing else, so `j`, `k`, `Down` and `Up` — the keys every
other list in the app answers — fell through. The route already handled the
actions: `move`'s `prTabComments` case moves `commentScroll`, and
`IssueComments.Act` gained the two cases. The binding was the whole of the
missing piece. `TestPRCommentsScrollWithTheArrowKeys` and
`TestIssueCommentsScrollWithTheArrowKeys` drive the keys through the model, and
`TestDocumentedKeysFromSpec` pins them in both scopes.

**A keystroke rendered every body on screen.** `conversationLines` renders each
contribution's markdown from scratch, and the sub-tab called it twice per key:
once from `clamp`, to bound the scroll, and once from `commentsView`, to draw. On
the pull request this was reported from — 48 reviews and 41 inline remarks,
~44 KB of markdown — one render measured 163 ms
(`BenchmarkConversationRender`), so a keystroke paid something like 400 ms before
the frame was drawn, and a poll tick paid the render again.

`internal/ui/render.go` is the answer. `renderedLines[R]` holds the lines, keyed
on the width and the palette the markdown renderer answers for, plus whatever the
caller has to say what the content is: a counter a route bumps when data lands,
or the body text itself. `PRDetails.commentLines`, `PRDetails.descriptionLines`,
`IssueDetails.descriptionLines` and `IssueComments.lines` go through it. The
description joined the same pass because `clamp` measures it: every keystroke on
every sub-tab rendered the pull request body, which is 7 ms at 3 KB and grows
with the body.

The counts are pinned rather than described.
`TestPRCommentsRenderTheConversationOnce` and its issue-side twin assert that
seven keystrokes over the conversation cost zero markdown renders, that a refresh
re-renders it (the data the lines came from is about to be replaced), and that a
resize does too (every body re-wraps). `Markdown.renders` is the counter they
read, beside the `builds` counter that keeps renderer construction out of the
frame path. Measured on the same fixture, one key press went from ~400 ms to
0.6 ms.

What is not fixed: the render after data lands still happens synchronously on the
UI goroutine, so the first frame of a review-heavy conversation can still stall
briefly. One render per change is the cost the cache leaves; moving it off the
update loop is a larger change than this one.

Verification: the checks the conversation fix above lists, plus a live run of the
built binary (arrows scroll the reported pull request; the lag is gone).

### Fix — a thread is the comment it started from (this change)

The Comments sub-tab drew every inline remark as a thread of its own. The live
evidence, read off the pull request the report named: 41 inline comments, 25 of
them replies, and **every one of those 25 replies arrived in a later review than
the comment it answers** — GitHub posts a reply as part of a new review, so the reply carries
`pull_request_review_id` of the review that posted it while `in_reply_to_id`
names the remark it answers. 13 of the 16 threads spanned more than one review.
`groupThreads` was called once per review, so it could only merge a reply whose
parent happened to be in the same slice: 41 comments became 41 thread headers,
13 locations were drawn more than once, and one location was drawn four times.

`groupThreads` now takes the comments of the whole page and works out the thread
each belongs to by walking `in_reply_to_id` to the comment the exchange started
from. A thread is placed, and its location and hunk excerpt taken, from that
comment; a reply whose root is not on the page — the page was cut, or the token
cannot see it — is gathered under the remark it names rather than dropped or
drawn alone; and the remarks of a thread are ordered by time, whatever order the
API returned them in. `newConversation` then attributes each thread to the review
that owns its root comment, so a review that only replied reads as a heading with
nothing beneath it while the exchange stays where it was written.

The fixture made this defect invisible: it put the reply in the same review as
the comment it answered, a shape the API does not return. `fixtureInline` now
posts the reply from a second review, which is why `pr_comments_120x40` gained a
bare review block: the reply's review, holding none of the thread now drawn above
it. `TestConversationGroupsAThreadAcrossReviews`,
`TestConversationFollowsAChainOfReplies` and
`TestConversationGathersRepliesToAMissingComment` cover the grouping at the unit
level, and `TestPRCommentsKeepAThreadTogetherAcrossReviews` counts the location
on the screen.

### Feat — the conversation links what it names (this change)

The screen already linked the pull request title and the author of each
contribution. It now also links what the rest of its text names, all of it built
in `internal/ui/links.go` from what the API returned rather than from the screen's
own state, so a URL cannot point at something the row it is drawn from does not
belong to:

| Text | Opens |
| --- | --- |
| the repository in the heading | the repository page |
| `Source: x → Base: y` | the compare between the branches |
| a thread's `path:line` | the thread on GitHub |
| an issue conversation's heading | the issue |
| a contribution's name and time | that comment |

Two choices are deliberate. A thread's location opens the remark's permalink
rather than a blob URL built from the head SHA, because GitHub keeps a comment on
the line it was written against — the API reports that line as zero once the diff
has moved past it — so a blob URL would point at the wrong line the moment the
branch moves on, while the permalink always lands on the remark. And the name and
the time both carry the same permalink: the whole of what identifies a
contribution opens it, and neither link is the only way in.

The live run found the heading links dead. The REST payload carries the
repository only under `head.repo` and `base.repo`, so `Repository.FullName` at the
top level of the detail response is empty, and both the repository link and the
compare link were built from it. `repoFullName` now takes the name the payload
carried when it carried one and the reference the route was opened with
otherwise, and the test fixtures no longer inject a top-level `repository` object:
the injected object had hidden the defect, and the fixtures now mirror the shapes
GitHub returns. The fallback is what the heading already drew, so the text on
screen is unchanged and it opens the repository it names.

The links are OSC 8 sequences, so a terminal without support prints the text and
ignores them, and the plain palette the goldens render emits none: the link
decisions are pinned by `TestConversationLinksTheChromeAndTheThread` and
`TestIssueScreensLinkTheIssue` against a coloured palette, and the golden files
stay byte-for-byte free of escape sequences beyond the styling they always had.

Verification: the checks the conversation fix above lists, plus a live run of the
built binary against the reported pull request — 41 thread headers became 16, and
the raw pane carries the OSC 8 tokens for the repository, the compare, the thread
locations and the comment permalinks.

### Change — a thread is drawn in a frame (this change)

The conversation drew a thread as flat indentation: a location line, the hunk
under it, then a name-and-time line per remark with the body two cells further
in. Nothing marked where one remark ended and the next began, and a reply was
told from the remark it answered only by reading the two.

A thread is now a frame. `┌─ path:line` titles it, its content runs down a `│`
rail, and `└─` closes it. Each reply is a frame of its own nested one rail
inside, so nesting carries the relationship between a reply and the remark it
answers, and the frame's title is the reply's own author line. The remark the
thread started from carries a `●` out on the thread's rail; a reply carries no
mark, because the frame around it says what it is. The hunk is a frame inside the
thread's rail too, with its first line riding in the frame's corner: three lines
of diff cost four rows instead of five, which matters because `hunkLines` caps
the excerpt at three.

The frames are box characters and the header-rule colour, which is the whole
reason they survive the plain palette: goldens keep their structure with no
colour at all, and bold names stay the only escape sequence a plain render
carries. The frame is the widest thing on the line, so the width budget is
arithmetic: the title loses `frameCells` to the corner, and everything inside the
rail loses it twice.

Cost, and it is a real one: a framed thread is taller than the flat list was —
about six rows for a thread with a snippet and one reply. At 120x40 the standard
fixture's conversation no longer reaches the trailing body-less review, so the
golden for it shows less than it did; `pr_comments_reviews_only_120x40` still
shows that review, and `TestPRCommentsKeepAThreadTogetherAcrossReviews` reads the
same property at 120x56, where the whole conversation fits. `TestThreadsAreDrawnInFrames`
pins the frame's characters, the reply's nesting inside the thread's rail, the
width budget, and the absence of OSC 8 sequences.

### Security findings

| Finding | Status |
| --- | --- |
| F2 — the browser opener accepted any URL scheme | Closed. `ui.OpenBrowser` validates the scheme and host at the boundary; `ValidateBrowserURL` is unit-tested. |
| F4 — the credential source label printed the config file's absolute path | Closed. `auth` reports the file name only; a test asserts no path separator reaches the label. |
| F1 — `viewerCanMerge` fails open when the scope header is absent | **Closed in Phase 3.** The merge and close gate (`ui.decidePRWrite`) fails closed: repository write permission decides, an unreadable permission is a refusal, and the baseline's scope-header-only answer is gone. |
| F3 — `io.ReadAll` on job logs is unbounded | **Closed in Phase 2.** `gh.GetJobLogs` reads through a capped reader (`maxLogBytes`, 8 MiB) and appends a truncation note on a line boundary; error bodies are capped at 64 KiB. Tested against an oversized body and for line-boundary truncation. Multiplied by per-route polling, this was a memory-exhaustion vector rather than a vulnerability. |
| F5 — 14 stdlib advisories at `go1.26.5` | **Closed after Phase 6.** The reachable findings were all standard library, and the remedy was the toolchain patch this row predicted: the `go` directive names `1.27.2`, so the build uses a patched standard library. `govulncheck` reports no vulnerabilities at all, and the release workflows read the version from `go.mod`, so the shipped binaries carry the same fix. One consequence is worth recording: `golangci-lint` refuses a target newer than the Go it was built with, so a local binary older than `v2.14.0` (built with `go1.27.0`) fails with "the Go language version used to build golangci-lint is lower than the targeted Go version". CI downloads the current release itself and is unaffected; a local install needs `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`. |
| F6 — markdown rendering pulled in two modules with reachable advisories | **Closed in Phase 6.** Glamour's graph brought `github.com/yuin/goldmark v1.7.8` (GO-2026-5320, an XSS in the HTML renderer, reachable through `TerminalRenderer.Render`) and `golang.org/x/text v0.24.0` (GO-2026-5970, an infinite loop on invalid input), and `golang.org/x/net` with it. All three were upgraded inside the same release line — goldmark `v1.8.6`, `x/net v0.61.0`, `x/text v0.43.0` — and `govulncheck` reports no module findings afterwards. The advisory is in the HTML renderer while this program renders to a terminal, so the reachability is the weakest part of the report; the upgrade removes the need to argue about it. |

**Phase 5 transport invariants.** The GraphQL endpoint is a constant, so no
configuration value can redirect an authenticated request to another host, and
the endpoint parameter exists for tests alone (`TestNewClientPinsTheEndpointUnlessATestOverridesIt`
covers both halves). The client is a stock `http.Client` over
`http.DefaultTransport`, so the platform's own certificate verification
authenticates the server and no `InsecureSkipVerify` exists to misuse. The token
is written into the `Authorization` header of a cloned request and formatted into
no error, message, or log; the stub server asserts that it arrived in the header
and in no URL. The library's errors are matched on text rather than interpreted,
and the text is the API's own.

### Release hold

Do not merge a release-please release PR between the first and last rewrite
phase. The intermediate states are deliberately incomplete, and cutting a
release from one would publish a shell.
