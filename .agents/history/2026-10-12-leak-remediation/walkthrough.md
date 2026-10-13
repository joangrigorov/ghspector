# Removing a private project's identifiers from the repository history

## What leaked

A private project's name, a private repository path, a bot account login, an
internal source path, and a quoted excerpt of a private code review had reached
the public repository in this session's work:

- `internal/ui/render_bench_test.go` — a benchmark fixture quoting a private
  review body, using a private bot login and a private file path;
- `docs/spec.md` and `docs/architecture.md` — the private repository reference
  in the description of the reported case;
- the archived walkthroughs under `.agents/history/`;
- the commit messages of two commits;
- the descriptions of pull requests #27 and #28 on GitHub.

Older and already released: `internal/tui/app_test.go` carried a URL with the
private organisation and repository from v0.0.1 through v0.3.0. The file was
removed by the v2 rewrite, so it is absent from `main`, but it remains in those
release tags, their tarballs and the Go module proxy cache.

## What was done

1. A deterministic sanitizer (exact string replacements driven to a fixed point)
   replaces each identifier with a generic placeholder: `the reported pull
   request`, `example/api`, `review-bot[bot]`, `internal/service/search/handler.go`,
   and rewrites the quoted review body into neutral review prose.
2. `git filter-branch --tree-filter` applied it to every commit in
   `v0.3.0..HEAD`, with `--msg-filter` for commit messages.
3. `main` and the feature branch were force-pushed; the pull request bodies for
   #27 and #28 were replaced with sanitized text; release-please recreated its
   own release pull request from the scrubbed `main`.
4. Locally: the filter-branch backup refs were deleted, reflogs expired, and
   `git gc --prune=now` dropped the pre-scrub objects. A bundle of the pre-scrub
   state is kept outside the repository, at `/tmp/ghspector-pre-scrub.bundle`,
   in case anything has to be recovered.

## Verification

- Every commit not reachable from `v0.3.0` (all unreleased work) was scanned for
  the identifiers, in content and in commit messages: none left.
- The tips of `main` and the feature branch are clean, as is their published
  copy on `origin`.
- The rewrite changed exactly the expected files: three in `main`'s tree (the
  benchmark fixture, `docs/spec.md`, one archived walkthrough) and the branch's
  own work besides.
- `gofmt -l .`, `go build ./...`, `go vet ./...`, `go test -race ./...`,
   `golangci-lint run`, `govulncheck ./...` and `actionlint` are all clean on the
  rewritten branch.

## What a force-push cannot remove

- `refs/pull/27/head` and `refs/pull/28/head` keep the pre-scrub commits
  fetchable by SHA; the commit-by-SHA API still serves them.
- The "Files changed" view of the merged pull requests renders the leaked lines
  as they were merged.
- CI logs of the runs against the pre-scrub commits are retained by GitHub.
- The released tags were left in place by decision: rewriting a published Go
  module tag breaks checksum verification for anyone who fetches it, and the
  proxy cache and release tarballs keep serving the old copy regardless.
