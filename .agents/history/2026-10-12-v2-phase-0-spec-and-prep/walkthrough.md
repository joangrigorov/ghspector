# chore(v2): Phase 0 — spec freeze and release prep

First phase of the v2 TUI rewrite. This phase changes no TUI code: it makes parity a checklist, records the target architecture, and prepares the release pipeline to keep producing `0.x` while the rewrite is unstable.

Parent plan: the phased plan for the v2 rewrite (Phase 0 of 7).

## What changed

### `docs/spec.md` (new)

The behavioral specification and parity checklist for the rewrite, read from the code at `3e64be8` rather than from the README.

- Route map: all 19 `viewState` values mapped to their v2 routes, including the two modal machines hidden in integer fields (`mergeState`, `runApprovalState`) and the one input route hidden in a bool (`showFilterInput`).
- Per-route keybinding tables for all 20 route/overlay combinations, transcribed from the actual handlers.
- Filter semantics, split by where each filter is applied (server-side vs client-side after fetch).
- Pagination table (repos 100/15, runs 8, PRs 8, issues 50) and the Load More sentinel rule.
- Polling model, including the interval-selection quirk that puts the Issues tab on the workflows interval.
- CLI and config surface, token resolution order, and scope gating.
- Full REST endpoint map (24 endpoints) with which transport (REST vs GraphQL) takes over in v2.
- A "Fix in v2" backlog, a "Parallel anchors" list of pure functions worth porting, and the API behaviors to preserve.

### `docs/architecture.md` (new)

The target design for v2, written so the failure modes cannot be reintroduced: the layout engine, the route stack, the overlay stack, the keymap, and the store, each paired with the v1 problem it removes and the test that enforces it. Also records layer boundaries, the transport split (GraphQL for listing/aggregation, REST for logs, diffs, checks, and writes), dependency versions, conventions, and the release constraints that must not move.

### `internal/auth/auth.go`

Adds a `version` key to the config schema, additively.

- `Config.Version` with `yaml:"version,omitempty"`, and `CurrentConfigVersion = 2`.
- `LoadConfig` reports a config with no `version` key as version 1, so existing user configs load unchanged and are identifiably legacy.
- `SaveConfig` stamps the current version on write.
- No key is renamed or removed; the config path is unchanged.

### `internal/auth/auth_test.go`

`TestConfigVersion` covers both directions: a saved config round-trips as the current version, and a hand-written legacy config (no `version` key) loads as version 1 with its existing keys intact.

### `.gitignore`

Adds `/ghspector` (anchored, so it matches only the root binary). The phased plan builds a runnable binary at the project root after every phase so it can be launched immediately during review; without this rule that binary appears as untracked and can be swept into a commit by a broad `git add`.

### `release-please-config.json`

Adds `"bump-minor-pre-major": true`. A breaking commit (`feat!`) now produces `0.4.0` instead of `1.0.0`, so the rewrite stays on `0.x` until parity and stability justify the flip. Moving to `1.0.0` later is a deliberate act: remove this flag and land a breaking change, or use a `Release-As: 1.0.0` footer.

## Verification

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -race ./...` | pass (`auth`, `gh`, `tui` all ok) |
| `golangci-lint run` | 0 issues |
| `actionlint` | pass |
| `release-please-config.json` parses | pass |

Correctness of the spec was established by reading the handlers rather than by inference. Every keybinding row in `docs/spec.md` is transcribed from a `case` arm in `internal/tui/update.go`, and every endpoint from a path literal in `internal/gh/client.go`.

## Findings recorded for review

Eight places where the README and the code disagree are documented as `D1`–`D8` in `docs/spec.md` with a proposed decision each. The two that affect user-facing docs most:

- **D3, a real keybinding bug.** In the Jobs view the code opens the **selected job** on `w` and the **parent run** on `v`. Both the README and the in-app help claim the opposite. Proposed: keep the code's mapping, fix the docs, and lock it with the keymap consistency test.
- **D1, a documented view that does not exist.** The README documents a Dashboard tab as the first tab; the code has three tabs (PRs, Workflows, Issues) with no dashboard. Dead remnants (`dashboardPRsCount`, `dashboardWorkflowsCount`, `dashboardStatsLoadedMsg`) are noted for removal.

Also recorded: `viewPRCommits` has an unreachable `q` handler (the global handler runs first), and `mergeState == 3` is declared in a comment but never used.

Two of the eight are now decided, and Phase 1 records them in the spec:

- **D3 — keep the code.** `w` opens the highlighted job, `v` opens the parent run. The README and the help overlay are corrected to match, and the keymap consistency test locks it, so existing muscle memory holds.
- **D1 — the Dashboard stays removed.** The dead counters and `dashboardStatsLoadedMsg` go away with Phase 1's deletion of `internal/tui`; the README's global-keys table is corrected in Phase 6.

## Running this branch

This phase changes no TUI code, so it behaves as 0.3.0. A binary is built at the project root for review:

```bash
go build -o ghspector -ldflags "-X main.version=0.3.0-dev -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/ghspector
./ghspector -v
```

It reports the commit it was built from, so the build on disk is unambiguous. It needs a real terminal and GitHub credentials (`GH_TOKEN`, a config file, or `gh auth login`).

Build output verified: `ghspector 0.3.0-dev`, `commit: fb2fd85`. Without a TTY it exits cleanly with `could not open a new TTY` rather than panicking.

## Note on `govulncheck`

A local `govulncheck ./...` reports 14 advisories, all in the Go standard library at `go1.26.5`, all fixed in `go1.26.6` (which is available on the proxy). The finding is **pre-existing and not introduced here**: an identical run against a clean worktree of unmodified `main` produces the same 14 advisories and exit code 3, and CI was green on `main` at `3e64be8`.

This phase does not change the toolchain, since the plan scope is docs and release prep and CI is currently green. If you want the scan clean locally, the fix is a `go` directive bump to `1.26.6` — a separate, one-line change.

## Risk

Low. Two new documents, one additive config field with tests, and one release-please flag. The only behavioral change is that newly written config files gain a `version: 2` line, which older binaries ignore.

## Next phase

Phase 1 (`feat/v2-foundation-shell`) builds the foundation on Bubble Tea v2 and Lip Gloss v2: the layout engine, route stack, overlay stack, keymap, and the REST-backed store skeleton — then deletes `internal/tui`, including its 2,093-line test file. Its exit criteria are the golden renders, the compositor property test, and the keymap consistency test described in `docs/architecture.md` §8.
