# ghspector v2 — total TUI rewrite (phased plan)

## Why we're rewriting (diagnosis, one paragraph)

`internal/tui` (~8,900 lines incl. tests) has four structural faults that make every fix unstable:

1. `viewState` (19 constants) conflates screens, modals, routes, and a splash screen; two more modal machines live as magic ints in `mergeState` / `runApprovalState`. `View()` branches combinatorially (`view.go:24-71`).
2. `Update` is one 1,975-line function (`update.go:23-1997`) with 128 key cases and 13 view cases.
3. Modals are hand-drawn ASCII boxes with hardcoded widths and literal box-drawing characters (`renderMergeModal`, `view.go:1716`), composited by `overlayModal` (`view.go:1656`) which strips ANSI and slices runes — the source of the "non-deterministic" layout breakage.
4. Layout is 47 hand-tuned constants (`m.height - 12/14/15/16/8`, `m.width - 44/85/102`), and keybindings are declared in three unsynchronized places (Update switch, 16 `renderFooter` sites, `renderHelpView`).

`internal/gh` (~945 lines) and `internal/auth` (~163 lines) are clean and carry real API knowledge. They stay.

---

## Locked decisions

| Decision | Choice | Verified version |
| --- | --- | --- |
| TUI framework | Bubble Tea v2 | `github.com/charmbracelet/bubbletea/v2` **v2.1.0** |
| Styling + overlay compositing | Lip Gloss v2 (Canvas/Layer) | `github.com/charmbracelet/lipgloss/v2` **v2.0.6** |
| Widgets | Bubbles v2 | `github.com/charmbracelet/bubbles/v2` **v2.2.1** |
| Markdown rendering | Glamour v2 | `github.com/charmbracelet/glamour/v2` **v2.0.1** |
| Cross-scope aggregation | GitHub GraphQL v4 | `github.com/shurcooL/githubv4` **v0.0.0-20260209031235-2402fdf4a9ed** |
| Rate limiting | `golang.org/x/time/rate` | **v0.16.0** |
| TUI integration tests | teatest v2 (untagged, pin pseudo-version) | `github.com/charmbracelet/x/exp/teatest/v2` **v2.0.0-20261011004359-2acd7e345453** |
| Version series | Stay `0.x`; `1.0.0` only when parity + stability proven | release-please |

Notes on these choices:

- **Never mix v1 and v2 import paths.** `bubbles/v2` pairs with `bubbletea/v2` and `lipgloss/v2`. The old v1 imports disappear from the tree entirely.
- Bubble Tea v2 changes the model contract: `View()` returns `tea.View` (`tea.NewView(content)`, with `AltScreen` as a field) instead of `string`, and key messages are reworked (`KeyPressMsg`/`KeyReleaseMsg` with a typed `Key`). The keymap layer is written against v2's key types from day one; exact API signatures get confirmed against pkg.go.dev during Phase 1 rather than assumed.
- Lip Gloss v2 `Canvas`/`Layer` (`NewCanvas`, `NewLayer`, `.X/.Y/.Z`, `.Compose`, `.Render`) is cell-aware, which structurally replaces `overlayModal` and kills the wide-rune/ANSI drift bug class.
- REST stays for endpoints GraphQL cannot serve: **job logs** (`/actions/jobs/{id}/logs`, REST-only, returns XML on error), PR diff/file patches, and check runs. GraphQL takes over cross-repo *aggregation and listing*.
- GraphQL rate budget is separate from REST and returned per query as `rateLimit { cost remaining resetAt }`; include it in every aggregation query so the limiter stays accurate.

### Versioning mechanics (so 0.x holds, and 1.0.0 stays reachable)

Add to `release-please-config.json` under the `"."` package:

```json
"bump-minor-pre-major": true
```

With this, a `feat!:` breaking commit yields `0.4.0` instead of `1.0.0`. When the rewrite reaches parity and proves stable, cut `1.0.0` deliberately by one of:

- a commit with footer `Release-As: 1.0.0`, or
- removing `bump-minor-pre-major` and landing one breaking change.

### Hard constraints that must not move

- Module path stays `ghspector`; entry point stays `cmd/ghspector/main.go`.
- Goreleaser builds `main: ./cmd/ghspector/main.go` and injects `-X main.version` / `-X main.commit` / `-X main.date`. Those variable names and that path stay, or releases and `-v` break silently.
- `auth.Config` YAML keys stay byte-identical: `github_token`, `default_org`, `default_account`, `polling_interval_seconds`, `polling.workflows_interval_seconds`, `polling.prs_interval_seconds`, `default_merge_method`. Config path stays `<UserConfigDir>/ghspector/config.yaml`. A `version:` key is *added*; nothing is removed or renamed.
- Token precedence stays env → config → `gh auth token`.
- `ci.yml` / `release.yml` need no changes (they are structure-agnostic). One possible addition: a `-update` flag workflow for golden files, only if we decide to add it.

---

## Target architecture

### Package layout

```
cmd/ghspector/main.go          # wiring only (v2 program, AltScreen via View)
internal/auth/                 # unchanged (+ version field in Config)
internal/gh/                   # REST client, unchanged (+ pagination helper)
internal/gql/                  # NEW: GraphQL client (shurcooL/githubv4)
internal/store/                # NEW: cache + pagination + aggregation, REST/GraphQL behind one API
internal/keymap/               # NEW: single source of truth for bindings, per route
internal/layout/               # NEW: Rect, Frame, Layout(width,height); no view does math
internal/overlay/              # NEW: modal/overlay stack built on lipgloss v2 Canvas/Layer
internal/ui/                   # NEW: routes, components, theme, splash
internal/ui/widgets/           # NEW: table, logview, breadcrumbs, statusbar, confirm modal
```

### The five structural fixes

**1. Layout engine.** `layout.Frame{Header, Body, Footer Rect}` computed once from `(width, height)`. Every component renders into an explicit `Rect`. The 47 magic constants are deleted by construction.

**2. Route stack instead of `viewState`.**

```go
type Route interface {
    Init() tea.Cmd
    Update(tea.Msg) (Route, tea.Cmd)
    View(r layout.Rect) tea.View
    Bindings() []keymap.Binding
    Title() string
}
type Stack []Route   // Enter pushes, Esc pops; breadcrumbs = join(stack titles)
```

Routes: `Splash`, `Main{Tabs: PRs|Workflows|Issues}` → Workflows `RunList→JobList→Logs`, PRs `PRList→PRDetail{Info|Commits|Files|Checks|Comments}→Diff|CommitDetail`, Issues `IssueList→IssueDetail→Comments`.

**3. Overlay stack.** One generic `Modal{Title, Body, Hints}` sized from content and centered by the v2 compositor; `Confirm(title, body, onYes, onNo)` covers merge/close/approve. Overlays live in a separate stack, so backgrounds never re-render against modal state.

**4. Keymap.** `keymap.Binding` per action; the dispatcher matches on bindings; footer and help render *from the same bindings*. A test asserts keymap ↔ README ↔ help agreement.

**5. Store.** `internal/store` owns caching, cursor pagination, and scope-wide aggregation behind typed methods. `internal/gh` and `internal/gql` stay stateless. Rate limiting moves to one shared `x/time/rate` limiter fed by both REST headers and GraphQL `rateLimit` payloads. The "top 8 repos" fan-out cap disappears when GraphQL lands.

State splits per domain (`workflowState`, `prState`, `issueState`) and lives inside the owning route. Root model holds route stack, overlay stack, keymap, store, theme, dimensions — target ~15 fields against today's 90.

---

## Phase 0 — Spec freeze and meta prep

**Goal:** Make parity a checklist instead of a memory. Zero TUI code changes.

**Work:**
- Write `docs/spec.md`: every route, every keybinding per route, every write action, every filter, every polling behavior — extracted from `README.md` plus the actual `update.go`/`view.go` behavior (resolve any README↔code disagreements explicitly, and record the decision).
- Write `docs/architecture.md`: the design above, so future contributors don't reintroduce `viewState`.
- Add `version: 2` to the config struct (additive, ignored by old binaries).
- `release-please-config.json`: add `bump-minor-pre-major: true`.
- Record baseline: `go test ./...` and `go build ./...` both green today (verified).

**Exit criteria:** `docs/spec.md` enumerates ≥100 testable items (19 view states → route map, ~128 key cases → bindings); architecture doc merged; config + release-please changes are additive only.

**Verification:** `go build ./...`, `go test ./...`, and a manual pass reading the config file into the new struct to confirm round-trip.

**Risks:** Discovering README drift. Mitigation: the spec records *actual* behavior as the baseline and flags intended corrections separately, so parity work has a single reference.

**Status:** complete. Delivered as PR #16 (`chore/v2-phase-0-spec-and-prep`), CI green. `docs/spec.md` records eight README-versus-code disagreements as D1–D8 with a proposed decision each.

**Resolved drift decisions:**

- **D3 — decided: keep the code.** `w` opens the highlighted job, `v` opens the parent run. The README and the help overlay are corrected to match, and the keymap consistency test locks it. This preserves the muscle memory of anyone already on 0.3.0.
- **D1 — decided: the Dashboard stays removed.** The dead counters and `dashboardStatsLoadedMsg` disappear when Phase 1 deletes `internal/tui`; the README's global-keys table is corrected in Phase 6.

The spec's D3 entry is updated to record the decision as part of Phase 1's PR.

**Binary convention starts here:** the first action after this plan is approved is to build `./ghspector` from the current tree, so there is a runnable binary on disk from the outset. Phase 0 changes no TUI code, so that binary behaves as 0.3.0.

---

## Phase 1 — Foundation shell

**Goal:** A binary that runs on Bubble Tea v2, lays out deterministically, and navigates the route/overlay/keymap skeleton with empty screens. This phase retires the whole class of layout bugs.

**Work:**
- Swap deps to v2 across the board; delete v1 imports. `View() tea.View` with `AltScreen` set in the root view.
- `internal/layout`: `Rect`, `Frame`, `Layout(width, height)`, plus helpers (`Rect.Split`, `Place`, truncate/wrap via `charmbracelet/x/ansi`).
- `internal/ui`: root model, route stack, breadcrumbs from stack titles, header (context + rate-limit indicator), footer driven by `Bindings()`, status message with TTL, splash, error view, help route, context switcher route.
- `internal/overlay`: modal stack on `lipgloss.Canvas`/`Layer`; generic `Confirm`.
- `internal/keymap`: bindings + dispatcher + `RenderFooter`/`RenderHelp` from the same data.
- `internal/store`: interface + REST-backed skeleton (list/cache/poll plumbing, shared `x/time/rate` limiter).
- Security hardening carried in from the guardrail audit: validate the URL scheme in the browser opener (require `http`/`https`) before handing it to the OS, and test it as a property (finding F2); render only the basename of the config path in the `TokenSource` message rather than the absolute path (finding F4).
- Add the REST/GraphQL transports' shared rule to the store: never format the token or the `Authorization` header into an error, log line, or status message.
- Add `/ghspector` to `.gitignore` (anchored) so the local review binary is never committed.
- Record the resolved drift decisions in `docs/spec.md`: D3 keeps the code's mapping (`w` = job, `v` = run) with the README and help corrected to match; D1 keeps the Dashboard removed. The dead dashboard fields and message disappear with this phase's deletion of `internal/tui`; the README's global-keys table is corrected in Phase 6.
- Delete `internal/tui/*` and `internal/tui/app_test.go` at the end of this phase, once the shell carries the equivalent skeleton.

**Exit criteria:** binary builds and runs; splash → main; `Tab`/`Shift+Tab` switch tabs; `o` context switcher works against a mock server; `?` help renders from the keymap; `q`/`Ctrl+C` exit; all three tabs render empty/loading/error states; `Esc` pops routes; a "terminal too small" state replaces malformed output below the minimum width.

**Note on what this phase's binary is worth:** it is runnable but shows no GitHub data, because the lists arrive in Phases 2–4. Do not merge a release-please PR while this is the state of `main` (see [Release hold](#release-hold-while-main-is-incomplete)).

**Verification:**
- Golden render tests for the shell at 80×24, 120×40, 40×15 (header/footer/body/empty state/error state).
- Compositor property test: for random background/modal pairs including emoji, CJK, and ANSI-styled content, assert overlay preserves exact line count and display width.
- Keymap test: every binding appears in help and footer; no footer key lacks a binding or handler.
- Resize test: drive `tea.WindowSizeMsg` across sizes; assert no line exceeds width and line count equals height.
- Scripted flow test (teatest v2 with pipes) for context switching.

**Risks:** v2 API differences (key messages, `tea.View`) and teatest v2 being untagged. Mitigation: pin the verified pseudo-version; if teatest proves unstable, fall back to driving `Update` directly over pipes — golden tests do not depend on it either way.

---

## Phase 2 — Workflows vertical slice

**Goal:** Full workflows capability on the new foundation.

**Work:** run list (status sort, actor filter, repo filter, pagination) → jobs (`[`/`]` attempt cycling, step progress) → logs (follow mode, step segmentation); run approval + pending deployments; open-in-browser via OSC 8; all polls cancellable per route. Cap the job-log read (finding F3): the current `io.ReadAll` on the logs endpoint is unbounded, and this phase introduces repeated polling reads of the same body.

**Exit criteria:** every Workflows/Jobs/Logs row in `docs/spec.md` checked off; write actions (approve run, approve deployments) work against the mock; fork-PR approval semantics preserved (internal runs surface "approve on GitHub" instead of calling the API); log reads are bounded and tested against an oversized body.

**This is the first phase whose binary is useful** — the Workflows tab is real, and PRs/Issues remain empty placeholders.

**Verification:** golden tests per screen; teatest flows for list→jobs→logs, attempt cycling, approve confirm; poll tests with a fake clock; log-segmentation unit tests ported from `update.go` (`segmentLogs`/`calculateMatchScore` logic is behavior worth keeping as pure functions).

**Risks:** poll/refresh churn. Mitigation: one cancellable poller per route, `ctx` torn down on pop.

---

## Phase 3 — Pull requests vertical slice

**Goal:** Full PR capability, including the three-column detail layout that is fragile today.

**Work:** PR list → detail with `Info|Commits|Files|Checks|Comments` tabs → commit detail → diff view; merge (squash/merge/rebase, default persisted to config) and close via generic `Confirm`; check-run → in-app job navigation; OSC 8 links. Replace the merge/close gate with the same server-side permission check the run-approval path already uses, and fail closed (finding F1): today `viewerCanMerge()` returns `true` when the `X-OAuth-Scopes` header is absent and checks a coarse scope rather than repository permission, so the UI advertises merge and close to users who cannot use them. GitHub remains the authority on every write either way, so this is a UX and correctness fix, not an authorization hole.

**Exit criteria:** every PR row in `docs/spec.md` checked; merge/close writes work; `default_merge_method` persists and reloads; the old implicit coupling (`getRun()` back-filling repo fields from `selectedPull`) is gone — routes carry explicit `(owner, repo, number)`; merge and close are offered only when `GetRepoPermission` reports `admin`, `write`, or `maintain`, and are withheld when the permission cannot be determined.

**Verification:** golden tests for detail layout at 3 sizes including narrow (the layout currently assumes ~100+ columns); property test asserting sidebar + body widths always sum to the body rect; teatest flow for tab cycling and merge confirm → mock merge call.

**Risks:** narrow-terminal layout. Mitigation: spec a documented minimum width with a graceful "terminal too small" state, tested.

---

## Phase 4 — Issues vertical slice

**Goal:** Issues list → detail → comments, plus all filters (author, assignee, state, label).

**Work:** split issues from PRs (the REST issues endpoint returns PRs, filtered today by a `PullRequest` field); author/assignee/state filter modals via the new overlay stack; pagination.

**Exit criteria:** every Issues row in `docs/spec.md` checked; issue filter type-select and repo filter behave identically to workflows/PRs because they share the overlay + keymap code.

**This is the first phase that reaches functional parity with 0.3.0** — all three tabs do real work again. GraphQL aggregation (Phase 5) is the remaining gap, and it is an improvement rather than a regression.

**Verification:** golden tests; teatest filter flows; unit tests asserting the PR-exclusion predicate.

---

## Phase 5 — GraphQL aggregation upgrade

**Goal:** Replace per-repo REST fan-out with scope-wide GraphQL queries behind the unchanged `store` interface. This is the phase the locked GraphQL decision pays for.

**Work:**
- `internal/gql` using `shurcooL/githubv4` with an `oauth2.StaticTokenSource` HTTP client.
- Workflow runs across a user/org in one query; PRs and issues across a scope likewise; cursor pagination via `PageInfo`.
- Request `rateLimit { cost remaining resetAt }` in every query and feed it into the shared limiter.
- Delete the `limit > 8` cap and the duplicated fan-out bodies in `fetchRunsCmd`/`pollRunsCmd`.
- Keep REST for job logs, diff patches, and check runs; keep the "top-N active repos for polling" heuristic only if it still earns its keep (decide from measured cost).
- Security invariants for the new transport: use a stock `http.Client` with default TLS (no custom transport, no verification bypass); build the GraphQL endpoint from a hardcoded constant rather than anything read from config, and keep `gh.NewClient`'s `baseURL` a test-only seam so a config edit can never redirect the authenticated client (SSRF); never format the token or `Authorization` header into an error or log.
- Pin exact versions for the new modules, including replacing the "latest" placeholder for `charmbracelet/x/ansi` in `docs/architecture.md`.

**Security note:** this phase is the largest supply-chain addition in the rewrite — four Charm v2 modules, `shurcooL/githubv4`, and `golang.org/x/time`. All eight module paths were verified against their canonical upstream repositories during the guardrail audit, and the untagged `teatest/v2` and `githubv4` pseudo-versions are content-addressed by commit and covered by `go.sum`. The existing `govulncheck` CI step and the grouped `gomod` Dependabot updates stay in place to catch advisories afterwards.

**Exit criteria:** run/PR/issue lists come from GraphQL with real `total_count`; polls still update statuses; no TUI package changes (proves the interface held); REST fallback path for a scope GraphQL rejects (e.g. permissions) is tested.

**Verification:** `internal/gql` tests against a stubbed GraphQL server (query shape + error mapping); `store` tests asserting GraphQL and REST backends return equivalent domain objects for the same fixture; recorded-cost test asserting the limiter depletes per query cost.

**Risks:** schema/auth surprises (org visibility, SSO). Mitigation: keep the REST path as a runtime fallback and test it.

---

## Phase 6 — Parity audit, stability, 1.0.0 readiness

**Work:**
- Walk `docs/spec.md` end to end against the binary; every unchecked item either lands or is explicitly deferred with a reason.
- Manual smoke matrix on Linux + macOS terminals (and one Windows terminal) for resize, light/dark, narrow width.
- README refresh: keybinding tables and features now generated/validated against the keymap; installation and config sections stay as-is.
- Archive session artifacts to `.agents/history/` per project convention.
- Delete leftover v1 scaffolding; confirm `go.mod` has no v1 Charm modules.

**Exit criteria:** 100% of `docs/spec.md`; `go test -race ./...`, `golangci-lint`, `govulncheck`, `actionlint` green; a clean `release-please` PR producing `0.x`.

**Then:** flip to `1.0.0` deliberately (remove `bump-minor-pre-major` or use `Release-As: 1.0.0`) once it has shipped as `0.x` and held steady.

---

## Security guardrails

Audit performed against the `security-guardrails` rules, covering PR #16 and every planned phase. Everything below is evidenced by a command, not by inspection from memory.

### PR #16 verdict: clean

| Guardrail | Evidence | Result |
| --- | --- | --- |
| No upload to public services | Only local `/tmp` scratch files, all removed; comparison worktree removed | pass |
| No environment dumping | No `printenv`/`env`/`set`; `go env` scoped to named keys only | pass |
| No local identifiers in artifacts | Commit diff scanned for home paths, usernames, IPs, MAC addresses — no matches | pass |
| No secrets committed | Pattern scan over the diff and all tracked files; matches only the README's two documentation placeholders | pass |
| No credentials staged | Exactly five files staged; `walkthrough.md` untracked; `config.yaml` and `.env` gitignored | pass |
| No TLS or certificate bypass | No `InsecureSkipVerify`, `TLSClientConfig`, or transport overrides in the tree; `GOPROXY=proxy.golang.org,direct`, `GOSUMDB=sum.golang.org`, no `GOFLAGS` | pass |
| No arbitrary script execution | No `curl \| bash`; network use limited to verified tooling | pass |
| No privilege escalation | No `sudo`, no permission changes; config writes stay `0600` file / `0700` dir | pass |
| Supply chain | All eight proposed module paths verified against canonical upstream repositories via the GitHub API; none misspelled, forked, or archived | pass |
| Token leakage | The three `Authorization` assignments set a header and nothing logs it; only token-adjacent output is instructional text with a literal placeholder | pass |

### Findings

All five are pre-existing. None is introduced by this rewrite, and none is exploitable as-is.

| ID | Finding | Severity | Handled in |
| --- | --- | --- | --- |
| F1 | The merge/close gate (`viewerCanMerge`, `update.go:2421`) returns `true` when the `X-OAuth-Scopes` header is absent and checks a coarse scope rather than repository permission, so the UI offers write actions the user cannot complete. GitHub enforces authorization server-side on every write endpoint, so this is a UX and correctness issue, not an authorization hole. The run-approval path already does it correctly via `GetRepoPermission` and `current_user_can_approve`. | low | Phase 3 |
| F2 | `openBrowser` passes a URL to `exec.Command` with no scheme validation. All 13 call sites use API-provided `HTMLURL` values or constructed `https://` strings, so no user-controlled input reaches it. Residual risk: a leading `-` would be read as a flag by the OS opener. | low (hardening) | Phase 1 |
| F3 | `io.ReadAll` on the job-logs endpoint (`client.go:375`) is unbounded. Logs reach tens of megabytes and the design polls them repeatedly. A memory-exhaustion vector rather than a vulnerability. | low | Phase 2 |
| F4 | `TokenSource` is set to `"config file: " + path` and rendered in the run-approval error (`update.go:3054`, `update.go:3080`), putting the user's home directory on screen. | low (hygiene) | Phase 1 |
| F5 | Fourteen standard-library advisories at `go1.26.5`, all fixed in `go1.26.6`. Reproduces identically on unmodified `main`, and CI is green on `main`, so the pipeline is unaffected. | informational | standalone one-line `go` directive bump |

### Invariants held for the rest of the rewrite

1. The token never appears in an error, status message, log line, or artifact.
2. `gh.NewClient`'s `baseURL` stays a test-only seam. Wiring it from config would let a config edit redirect the authenticated client's traffic.
3. The GraphQL client uses a stock `http.Client` with default TLS.
4. All four write actions keep server-side permission checks: merge, close, approve run, approve deployments. This is why the spec's endpoint map keeps `GetRepoPermission` and `GetPendingDeployments` on the REST side after GraphQL takes over reads.
5. Config file stays `0600`, its directory `0700`, and `config.yaml` stays gitignored.
6. Archived `walkthrough.md` files are scrubbed of absolute paths before they land in `.agents/history/`.

---

## Testable binary per phase

Answering directly: the phases as originally written delivered code, tests, and a PR — **not** a binary you could run. Every phase ends with a binary that builds and runs, since each one is independently mergeable and `main` stays shippable, but nothing published that binary: `ci.yml` builds and tests without uploading, and goreleaser only runs when release-please cuts a release. You would have had to fetch the branch and build it yourself.

What each phase's binary is actually worth:

| Phase | Builds and runs | Does real GitHub work |
| --- | --- | --- |
| 0 | yes (unchanged 0.3.0 plus docs and a config key) | yes — nothing was removed |
| 1 | yes | no — shell, layout, navigation, overlays; empty tabs |
| 2 | yes | partially — Workflows/Jobs/Logs real; PRs and Issues empty |
| 3 | yes | mostly — adds PRs; Issues empty |
| 4 | yes | yes — parity with 0.3.0 restored |
| 5 | yes | yes — scope-wide aggregation replaces the 8-repo cap |
| 6 | yes | yes — parity complete and stable |

So the honest summary: a runnable binary from Phase 1, and the first one worth your time from Phase 2. Phases 1 through 3 are each strictly less capable than 0.3.0.

### Delivery: a local binary at the project root

No artifacts, no release plumbing, no CI changes. At the end of every phase — and after every fix or change cycle during a review gate — I build the binary into the project root as `./ghspector`, gitignored, ready to run:

```
go build -o ghspector ./cmd/ghspector
```

Step 1 of Phase 1 adds `/ghspector` to `.gitignore` (anchored, so it cannot shadow anything else). The existing `bin/` and `dist/` entries stay.

To make it self-identifying, build with the same ldflags goreleaser uses:

```
go build -o ghspector -ldflags "-X main.version=0.4.0-dev -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/ghspector
```

Then `./ghspector -v` reports the commit the binary was built from, so which build is on disk is never in question. The plain `go build` form above is always acceptable; this one is just better for keeping track.

Each review-gate report states the commit the binary was built from and the command to run it, so the thing under review is unambiguous.

**Caveat:** building writes `./ghspector` over any previous phase's binary, so only the most recent phase's build is on disk. Ask and I will rebuild any earlier phase's branch on request.

### Release hold while main is incomplete

Phase 1 deletes `internal/tui`, so from Phase 1 until Phase 4, `main`'s binary is progressively less capable than the 0.3.0 release. Functionality returns in this order: Workflows (Phase 2), PRs (Phase 3), Issues (Phase 4).

Therefore:

- Do **not** merge a release-please release PR between Phase 1 and Phase 4. Otherwise a `0.x` release goes out with a gutted binary, and users on `latest` silently lose every capability.
- Held releases accumulate into one release, which is acceptable. At Phase 4 decide whether to cut `0.4.0` for the whole rewrite or resume per-phase numbering.
- Validate the `bump-minor-pre-major` behaviour on the first release PR by reading its diff before merging, rather than trusting the config.
- If you ever want public betas during the rewrite, release-please prerelease mode is the option; it is out of scope unless you ask for it.

---

## Cross-cutting verification strategy

The browser rule does not apply to a TUI, so verification is four layers:

| Layer | What it catches | How |
| --- | --- | --- |
| Golden render | Layout regressions, footer/header drift, resize breakage | Render each screen at 80×24 / 120×40 / 40×15; diff against committed goldens |
| Property (compositor) | Wide-rune and ANSI drift, overlay clipping | Randomized overlay inputs; assert exact line count and display width |
| Scripted flows | Behavior, not appearance | teatest v2 with mock HTTP/GraphQL servers; reuse the existing httptest mock from `app_test.go` |
| Invariant | Keybinding/doc drift | Assert keymap ↔ README ↔ help are mutually consistent |

`app_test.go` (2,093 lines) is **deleted, not ported** — it pins the hardcoded layout constants (`// visibleRows is m.height - 8 = 2`). Expect a real coverage dip in Phases 1–2 while the golden suite ramps; that is the intended trade.

---

## Risk register

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Mixing v1/v2 Charm imports | Build breaks, subtle runtime bugs | One dependency-swap commit in Phase 1; CI grep asserting no v1 import paths |
| teatest v2 untagged | Test tooling instability | Pin the verified pseudo-version; fall back to direct `Update` + pipes (goldens independent) |
| Layout engine rewritten twice | Wasted phase | Freeze `layout.Rect`/`Frame` in Phase 1 after the compositor property test passes |
| GraphQL permissions/SSO gaps | Missing data for some scopes | REST fallback retained and tested in Phase 5 |
| Golden tests becoming a rubber stamp | False confidence | Property tests and teatest carry behavioral weight; goldens are reviewed on every diff |
| Long-lived rewrite branch | Merge pain, lost momentum | Each phase is independently mergeable; `main` stays shippable throughout |
| Coverage dip after deleting `app_test.go` | Regressions slip through | Per-phase exit criteria gate on the new suites, not on line coverage |
| `main` is functionally less capable than 0.3.0 from Phase 1 to Phase 4 | A release ships a gutted binary and users lose capabilities | Release hold (do not merge the release PR until Phase 4) |
| A phase cannot be tried by hand | Review gates become code review only, and integration problems surface late | `./ghspector` is rebuilt at the project root after every phase and every fix cycle, with the build commit reported; each PR body carries a smoke checklist |
| Only the newest build is on disk | Reviewing an earlier phase, or comparing two phases, is awkward | Any earlier phase's binary is rebuilt on request from its branch |
| Release-please config change produces an unintended release | A premature or mislabelled version goes out | Read the first release PR's diff before merging it instead of trusting the config |
| A guardrail invariant is dropped during the rewrite | Token or path leakage reintroduced, or a write gate weakened | Invariants listed in [Security guardrails](#security-guardrails) are checked per phase; Phase 3 exit criteria name the permission gate explicitly |

---

## Deliverable strategy — distinct PR per phase

Each phase ships as its own branch and pull request. The next phase branches only after the previous PR is squash-merged into `main`, so `main` stays shippable and every phase is independently reviewable and revertable.

| Phase | Branch | Commit type | Release impact |
| --- | --- | --- | --- |
| 0 | `chore/v2-phase-0-spec-and-prep` | `docs` / `chore` | none (additive config + docs); `bump-minor-pre-major` lands here |
| 1 | `feat/v2-foundation-shell` | `feat(tui)!` | `0.3.0 → 0.4.0` (breaking, minor-pre-major) |
| 2 | `feat/v2-workflows` | `feat(tui)` | `0.5.0` |
| 3 | `feat/v2-pull-requests` | `feat(tui)` | `0.6.0` |
| 4 | `feat/v2-issues` | `feat(tui)` | `0.7.0` |
| 5 | `feat/v2-graphql-aggregation` | `feat(store)` | `0.8.0` |
| 6 | `chore/v2-parity-audit` | `chore` / `docs` | `0.9.0`, holding for a deliberate `1.0.0` |

Release numbering only applies once releases resume. Phases 1 through 4 must not produce a stable release (see [Release hold](#release-hold-while-main-is-incomplete)).

### Per-phase procedure

1. Confirm the previous phase's PR is merged; if not, squash-merge it with the GitHub MCP tool, then update local `main` from `origin/main`.
2. Branch from updated `origin/main` using the name above.
3. Implement, then run the mandatory checks: `go build ./...`, `go test -race ./...`, `golangci-lint run`, `govulncheck ./...`, `actionlint`.
4. Commit with a conventional message (multiple `-m` flags for the body; each git command run individually, never chained).
5. Push, then open the PR against `main` using `walkthrough.md` as the body (the file itself stays uncommitted; archive it to `.agents/history/` after merge).
6. Build the binary into the project root (`./ghspector`, ldflags stamped with the commit) and run it by hand in a real terminal against the phase's smoke checklist. This is a step, not an afterthought — a phase is not finished until its binary has been run by hand, because a TUI's real defects (resize, wide runes, focus order) do not show up in tests alone.
7. **Review gate — stop.** Report what landed, the PR link, the commit the local `./ghspector` was built from, a short "what to look at" list, a note of what is deliberately missing at this stage, and a preview of the next phase. Then wait for approval.
8. **Fix cycle.** If the review produces changes, implement them, re-run the checks, rebuild `./ghspector` from the updated branch, and report again with the new commit. The binary on disk always matches the newest review state.

Each phase's PR body carries three things the reviewer needs and nothing else has provided so far: the build command, the smoke checklist, and an explicit statement of the capabilities this phase does **not** yet have.

Phase 1 is the largest review by design; Phases 2–4 are single-domain and review as self-contained vertical slices. If Phase 1's PR grows past comfortable review size, it can be split without changing the architecture: shell + layout first, then overlay/keymap/store in a follow-up branch.
