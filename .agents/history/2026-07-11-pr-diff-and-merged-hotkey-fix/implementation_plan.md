# Implementation Plan - PR Diff View and Merged PR Action Hotkeys Fix

This plan details the implementation of a new PR Diff view accessed via the `D` (Shift+d) hotkey from the PR Details page, and a fix to hide the merge and close action hotkeys for already closed or merged PRs.

## Goals
1. Add a new view/page `viewPRDiff` displaying all changed files in the selected PR on the left, and the diff of the selected file on the right (similar to the commit details view).
2. Fetch the PR file diffs (patches) using `GetPullRequestFiles` when PR details are loaded.
3. Bind the `D` key on `viewPRDetails` to open the PR Diff view.
4. Hide and disable the Merge (`m`) and Close (`Shift+C`) actions/hotkeys on the PR Details view if the PR is already closed or merged.

## User Review Required
> [!NOTE]
> The PR Diff view is accessed via `D` (Shift+d) on the PR Details screen, as selected to avoid conflict with Vim-style description scrolling (`u` and `d`).

## Proposed Changes

### TUI Components

---

#### [MODIFY] internal/tui/model.go
- Add `viewPRDiff` to `viewState` const block.

#### [MODIFY] internal/tui/update.go
- Modify `fetchPRDetailsCmd` to fetch PR files changed concurrently using `m.client.GetPullRequestFiles`.
- Update `prDetailsLoadedMsg` handling to set `m.prFiles = msg.files`, set `m.selectedFileIdx = 0`, and call `m.updateDiffViewport()`.
- Add `updateDiffViewport()` helper function to initialize `diffViewport` and set its content formatted with `m.formatDiff` from `m.prFiles[m.selectedFileIdx].Patch`.
- Update `WindowSizeMsg` handler to set `m.diffViewport.Width = msg.Width - 44` to ensure it is aligned correctly with the left file list column (width 40 + spaces).
- Update `Update` message loop:
  - Inside `case viewPRDetails`, handle key `"D"` to change state to `viewPRDiff` and call `m.updateDiffViewport()`.
  - Add `case viewPRDiff` to handle key inputs for file navigation (`j/k`), diff scrolling (`u/d`), opening in browser (`w`), and exiting back to PR details (`esc` / `backspace`).
- Update `viewerCanMerge()` to return `false` if `m.selectedPull` is `nil` or `m.selectedPull.State != "open"`.

---

#### [MODIFY] internal/tui/view.go
- Update `View` switch to render `m.renderPRDiffView()` for `viewPRDiff`.
- Implement `renderPRDiffView()` displaying files changed on the left, vertical border, and diff viewport on the right (matching the layout of `renderCommitDetailsView`).
- Update the footer key lists in `renderPRDetailsView` to include `"D:Diff"` key for both open and non-mergeable PR states.

## Verification Plan

### Automated Tests
We will add a new test function `TestTUI_PRDiffViewAndMergePermissions(t *testing.T)` in `internal/tui/app_test.go` that:
1. Verifies that `D` (Shift+d) transitions the model state from `viewPRDetails` to `viewPRDiff`.
2. Verifies that the PR files changed navigation (`j/k`) and exit (`esc`/`backspace`) work correctly on the PR diff view.
3. Verifies that if a PR is closed or merged (i.e., `State != "open"`), `viewerCanMerge()` returns `false` and the merge (`m`) and close (`Shift+C`) actions/modals are disabled and not triggered.

Run unit tests with bypass sandbox:
```bash
go test ./...
```

### Manual Verification
1. Run `ghspector` locally:
   ```bash
   go run ./cmd/ghspector
   ```
2. Navigate to an **open** PR.
    - Verify the footer displays both `D:Diff` and actions `m:Merge` / `C:Close PR`.
    - Press `D` to open the PR Diff view. Verify you can navigate the list of files (`j/k`), scroll the diff content (`u/d`), open the files diff page in the browser (`w`), and return to PR details (`Esc`).
3. Navigate to a **merged** or **closed** PR.
    - Verify the footer displays `D:Diff` but does **not** show `m:Merge` or `C:Close PR`.
    - Verify pressing `m` or `Shift+C` has no effect.
