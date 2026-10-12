# feat(tui)!: Phase 1 — foundation shell on Bubble Tea v2

Second phase of the v2 TUI rewrite. This phase replaces the v1 TUI wholesale: the layout engine, keymap, overlay stack, store skeleton, and every screen are new code, and `internal/tui` is deleted. `main`'s binary keeps working but shows no list data until Phases 2–4 restore the vertical slices.

Parent plan: the phased plan for the v2 rewrite (Phase 1 of 7).

## Why the TUI was replaced rather than repaired

The diagnosis, with the specific structures that made the old TUI fragile:

- **One 19-value `viewState`** conflated screens, modals, and input modes. Two modal machines hid in plain integers (`mergeState`, `runApprovalState`) and the filter input hid in a `bool`.
- **One 1,975-line `Update`** with 128 key cases and no per-screen boundary.
- **An overlay compositor that stripped ANSI and sliced runes**, which is exactly the class of change that produced non-deterministic fixes.
- **47 hand-tuned layout constants**, pinned by a test whose comment read `// visibleRows is m.height - 8 = 2`.
- **Keybindings defined in three unsynchronised places**: the handler, the in-app help, and the README.

## What changed

| Package | What it is |
| --- | --- |
| `internal/layout` | `Rect`, `Frame`, `Layout(width, height)`, `Fit`, `Truncate`, ANSI-aware `Width`. `Fit` truncates to width and pads to an exact cell count, so any screen can be forced to exact geometry. |
| `internal/keymap` | The single binding table: 24 scopes, 135 bindings, `Lookup`, `KeyString`, `Display`. Footer and help render from this table. |
| `internal/overlay` | A modal stack composited on a Lip Gloss v2 `Canvas`, with a generic `Confirm`. Overlays own the keyboard while they are up. |
| `internal/store` | The `Store` interface, a REST implementation over `internal/gh`, a keyed TTL cache with single-flight, and one shared `x/time/rate` limiter. |
| `internal/ui` | Root model, route stack with breadcrumbs, header with the rate-limit indicator, footer from the keymap, transient status message, splash, error screen, too-small screen, three-tab main route, help route, context switcher. |
| `internal/gh/client.go` | The three paginated getters now take `page`/`per_page` and forward them, so the store can page instead of guessing. |
| `docs` | `docs/keybindings.md` is generated from the keymap; `spec.md` gains a recorded-resolutions table for D1–D8; `architecture.md` §10 records the shipped state; the README's per-view key tables now point at the generated file. |

Deleted: `internal/tui` in full — 9 files and 9,601 lines, including the 2,093-line `app_test.go` that pinned the old hardcoded layout constants, and `testdata/raw_logs.txt`, a captured CI log that should not have been in the repo.

Also staged here: `.agents/history/2026-10-12-v2-phase-0-spec-and-prep/`, Phase 0's archived plan and walkthrough. Untracked by design when Phase 0 merged.

## Structural fixes, each enforced by a test

1. **Layout determinism.** `layout.Fit` is the only way a screen returns a frame, and `TestFitAlwaysProducesExactGeometry` plus `TestEveryScreenHasExactGeometry` assert exact cell widths. Fourteen golden renders cover the shell at 80×24, 120×40, 60×16, and 40×15; `TestGoldenResizeDoesNotLeaveArtifacts` drives resizes and asserts no stale lines survive.
2. **Keyboard as data.** `TestKeybindingsDocIsCurrent` regenerates `docs/keybindings.md` and fails when it is stale, `TestEveryScopeAppearsInTheDoc` and `TestDocumentedKeysFromSpec` tie the help text to the table, and `TestNoDuplicateKeysInScope` catches collisions. Adding a key in one place is now the only way to add it.
3. **Overlay compositing on a canvas.** `TestViewPreservesGeometryOnWideContent` runs randomized background/modal pairs containing emoji, CJK, and ANSI-styled text and asserts the composited frame keeps exact line count and display width. `TestUpdateRoutesOnlyToTopmost` and `TestModalOwnsTheKeyboard` pin focus.
4. **One store, one limiter.** 26 tests cover the TTL cache, single-flight collapses (`TestGetOrLoadCollapsesConcurrentLoads`), failed and errored loads not being cached, write invalidation, paging, and the shared limiter. The token is never formatted into an error or status message.
5. **Navigation as a stack.** Breadcrumbs come from stack titles (`TestBreadcrumbsJoinTheStack`); `ActionBack` pops only above the root; `ActionQuit` resolves before the overlay stack, so `q` and `Ctrl+C` exit from behind a modal (`TestCtrlCQuitsFromBehindAModal`). A context switch broadcasts `AccountChangedMsg` to every route instead of each screen watching the account itself.

One real bug was found and fixed while building this: the overlay stack compared overlay values for equality to decide whether to pop, which panics when an overlay holds a slice — `overlay.Confirm` holds callbacks. The stack now treats a `nil` return as a pop and anything else as a replacement, and never compares.

## Security

Both Phase 1 hardening findings are closed, with tests:

- **F2** — `ui.OpenBrowser` now validates scheme and host at the boundary before handing a URL to the OS opener (`TestValidateBrowserURL`, `TestValidateBrowserURLErrorsAreDescriptive`).
- **F4** — the credential-source label reports the config file's base name only; `TestConfigFileTokenSourceHidesThePath` asserts no path separator reaches the label. Confirmed live: the splash shows `config: config.yaml` and no home directory.

Still open and unchanged by this phase: **F1** (the merge gate fails open; Phase 3) and **F3** (unbounded `io.ReadAll` on job logs; Phase 2).

## Verification

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -race ./...` | pass — 138 test cases in the 7 packages that have tests |
| `golangci-lint run` | 0 issues |
| `actionlint` | pass |
| `go mod tidy -diff` | clean |
| Deleted v1 Charm imports | none remain; `go.mod` carries only `charm.land/*` v2 modules |

The browser-verification rule does not apply to a TUI. The substitute is four layers, as the plan specified: golden renders, a compositor property test, scripted `teatest` flows driving the real program with mock HTTP servers (`TestFlowThroughTheProgram`, `TestFlowResizeWhileRunning`, `TestFlowTooSmallThenRecovered`), and keymap/document invariants. On top of those, the built binary was driven by hand in a pty at 120×32 and the following was observed end to end: splash → `Account: joangrigorov` with a live rate-limit counter; `Tab` and `Shift+Tab` move the tab strip; `?` opens the help screen and `esc` returns to the list; `o` opens the context switcher showing `joangrigorov (account) ← active` and `eventsalsa (organization)`; `q` exits with status 0.

An earlier run of that harness reported the program as "still running" after `q`. That was the harness: it treated the pty `EIO` raised when the child exits as a timeout and never reaped the child, and it sent the key before draining the splash. Fixed harness, same binary: clean exit, status 0.

## Running this branch

```bash
go build -o ghspector -ldflags "-X main.version=0.4.0-dev -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/ghspector
./ghspector -v
```

`./ghspector` is gitignored (anchored `/ghspector`) and already sits at the project root, built from this branch's commit. It needs a real terminal and GitHub credentials (`GH_TOKEN`, a config file, or `gh auth login`).

Smoke checklist for review:

1. Splash appears, then the chrome with your account and a rate-limit counter.
2. `Tab` / `Shift+Tab` cycle the three tabs; the header title and footer hints follow.
3. `?` opens help; `j`/`k` scroll it; `esc` or `?` closes it.
4. `o` opens the context switcher, `j`/`k` move, `enter` switches context, `esc` cancels.
5. Resize the terminal while running: no line overflows, nothing stale at the bottom.
6. Shrink below 50×12: the screen is replaced by a "too small" notice with the current title.
7. `q` and `Ctrl+C` exit; `Ctrl+C` also exits while a modal is open.

## Deliberately missing at this stage

- **No list bodies.** The three tabs render live chrome, tabs, empty states, and key routing, but no rows, because the routes that own rows arrive with their data in Phases 2–4. This is why the binary is runnable but not yet useful.
- **No release.** Per the plan's release hold, do not merge a release-please release PR until Phase 4. This commit is a breaking change (`feat(tui)!`), and with `bump-minor-pre-major` it would cut `0.4.0` — a version whose binary would be less capable than 0.3.0.

## Note on `govulncheck`

Unchanged from Phase 0 and not introduced here: a local `govulncheck ./...` reports 14 advisories, all in the Go standard library at `go1.26.5` and all fixed in `go1.26.6`. The traces point into `internal/gh/client.go` and `cmd/ghspector/main.go`, i.e. stdlib code paths this binary uses. CI's `govulncheck` step was green on `main` at `fd91ca7`, because that step resolves a newer Go patch than the local toolchain. The fix remains the one-line `go` directive bump recorded as F5.

## Next phase

Phase 2 (`feat/v2-workflows`) turns the Workflows tab into a real vertical slice: run list with status sort, actor and repo filters and pagination; jobs with `[`/`]` attempt cycling and step progress; logs with follow mode and step segmentation; run approval and pending deployments; open-in-browser. It also closes F3 by bounding the job-log read.
