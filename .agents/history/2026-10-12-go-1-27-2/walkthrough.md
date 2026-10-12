# fix(deps): upgrade to Go 1.27.2

The `go` directive names `1.27.2` instead of `1.26.5`. That is the whole
behavioural change, and it closes finding F5: every one of the 14 reachable
advisories in the phase-5 and phase-6 scans was standard library, and 1.27.2
carries the fixes for all of them.

Branch `build/go-1-27-2`, branched from `main` at `df51bf7` (the squash of #24).

## What changed

| File | What it is |
| --- | --- |
| `go.mod` | The `go` directive names `1.27.2`. Nothing else: no new dependency, no version moved, `go mod tidy` is a no-op. |
| `README.md` | The from-source line asks for Go 1.27.2 or later, so the documented requirement matches the module's. |
| `docs/architecture.md` | Finding F5 moves from Open to **Closed**, with the evidence and the one consequence for local tooling. |

## Why the toolchain is the fix

The advisories were never in a dependency. `govulncheck` attributed all 14 to
the standard library of the toolchain the module declared, and the advisory
database names patch releases in that series (`go1.26.6`, `go1.26.9`) rather
than anything in the module graph. Building on a patched standard library is
therefore the remedy, which is what this branch does.

The version matters beyond a local build. Both workflows read the Go version
from `go.mod` (`actions/setup-go` with `go-version-file`), and GoReleaser builds
the release archives and the `.deb` with that toolchain, so the fix reaches the
binaries people install rather than only the machine running the tests.

`govulncheck ./...` now reports nothing at all:

```
No vulnerabilities found.
```

## The one thing to know locally

`golangci-lint` refuses a Go target newer than the Go it was built with. A local
binary older than `v2.14.0` (which is built with `go1.27.0`) stops with:

```
Error: can't load config: the Go language version (go1.26) used to build
golangci-lint is lower than the targeted Go version (1.27.2)
```

CI is unaffected — `golangci-lint-action` downloads the current release itself,
and `v2.14.0` lints this tree at 0 issues. A local install needs:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
```

The `run.go` escape hatch in `.golangci.yml` was tried and rejected: pinning the
target language version to `1.26` makes `v2.12.2` panic inside its own analysis
runner, so it trades a clear error message for a crash.

## Verification

Every check CI runs was run locally with the 1.27.2 toolchain, and the same set
passed in a scratch worktree before anything was committed:

| Check | Result |
| --- | --- |
| `go mod tidy -diff` | clean, no diff |
| `go build ./...` | ok |
| `go vet ./...` | ok |
| `gofmt -l .` | clean |
| `go test -race ./...` | passes, all packages |
| `govulncheck ./...` | No vulnerabilities found |
| `golangci-lint run` (`v2.14.0`, built with `go1.27.0`) | 0 issues |
| `actionlint` | clean |

There is no browser to drive for a terminal UI, so the substitute stands where
it applies: `teatest` through the real runtime, the golden renders, and the
scripted request assertions all still pass unchanged, which is the point — a
toolchain bump must not move a single rendered cell. This branch moves none.
