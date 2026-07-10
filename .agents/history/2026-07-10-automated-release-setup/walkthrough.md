# Task Walkthrough - Automated Release & CI/CD Setup

We have set up a modern, automated CI/CD and release workflow for `ghspector` that handles testing, linting, dependency grouping, versioning, package building, and release generation.

## Technical Details of Changes

### 1. Version Flag Support
- Modified [main.go](cmd/ghspector/main.go) to declare build variables (`version`, `commit`, `date`) and check for `-v` or `-version` flags:
```go
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// In main():
versionFlag := flag.Bool("v", false, "Print version information and exit")
versionLongFlag := flag.Bool("version", false, "Print version information and exit")
// ...
if *versionFlag || *versionLongFlag {
	fmt.Printf("ghspector %s\n", version)
	fmt.Printf("commit: %s\n", commit)
	fmt.Printf("build date: %s\n", date)
	os.Exit(0)
}
```

### 2. Dependabot Configuration
- Added [.github/dependabot.yml](.github/dependabot.yml) to keep GitHub Actions and Go module dependencies updated.
- Updates are scheduled weekly, grouped, and commits are prefixed with `fix(deps)`.

### 3. CI Workflow
- Added [.github/workflows/ci.yml](.github/workflows/ci.yml) to run on pushes and pull requests.
- Steps include running tests, security check (`govulncheck`), linter (`golangci-lint`), and workflow linter (`actionlint`).
- All actions are pinned to their exact commit SHAs for maximum security.

### 4. Release Automation Workflow
- Added [.github/workflows/release.yml](.github/workflows/release.yml) to automate release creation.
- When pushed to `main`, `release-please` runs.
- If a release is created:
  1. Checks out the tagged commit.
  2. Fetches the git tags.
  3. Runs GoReleaser to build artifacts for all platforms and architectures.
  4. Generates `.deb`, `.rpm`, and `.apk` linux packages.
  5. Uploads all binaries and packages directly to the newly created GitHub Release.
- Avoids spawning secondary workflows, requiring zero custom PATs or GitHub Apps (uses only the default `secrets.GITHUB_TOKEN`).

### 5. GoReleaser Configuration Update
- Modified [.goreleaser.yaml](.goreleaser.yaml) to use the GoReleaser v2 specification format.
- Added `nfpms` block for packaging:
```yaml
nfpms:
  - file_name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}{{ if .Arm }}v{{ .Arm }}{{ end }}"
    homepage: https://github.com/joangrigorov/ghspector
    maintainer: Yoan-Alexander Grigorov
    description: "Sleek, terminal-native TUI for browsing, monitoring, and debugging GitHub Actions workflows."
    license: MIT
    formats:
      - deb
      - rpm
      - apk
```

### 6. Vulnerability Mitigations
- Bumped Go version inside [go.mod](go.mod) from `1.26.1` to `1.26.5` to pick up security fixes in the standard library (resolving standard library TLS and textproto vulnerability reports).
- Upgraded `golang.org/x/net` to `v0.57.0` and `github.com/yuin/goldmark` to `v1.7.17`.
- Verified `govulncheck` locally; 0 vulnerabilities remain.

### 7. Documentation
- Updated [README.md](README.md) to add an Installation section with instructions for pre-built binaries, `.deb`/`.rpm`/`.apk` packages, and `go install`.

### 8. Linter Quick Fixes (Iteration 2)
- Resolved `staticcheck` warnings in `internal/tui/view.go` under check `QF1012`. Replaced performance-inefficient `sb.WriteString(fmt.Sprintf(...))` calls with standard `fmt.Fprintf(&sb, ...)` statements.

### 9. Workflow Optimization & Deprecation Fixes (Iteration 3)
- Swapped deprecated `google-github-actions/release-please-action` for `googleapis/release-please-action` in `.github/workflows/release.yml`.
- Reordered CI steps in `.github/workflows/ci.yml` so `govulncheck` runs before test suite execution.
- Added `.idea/` to `.gitignore` to prevent committing JetBrains IDE metadata.

## Verification Results

- **Unit and Integration Tests**: All passed successfully (`go test ./...`).
- **golangci-lint**: 0 issues found.
- **govulncheck**: 0 vulnerabilities found.
- **actionlint**: 0 issues found.
- **goreleaser check**: Config successfully validated.
