# Walkthrough - PR Diff View, Details Refresh, Running Job Logs Graceful Warnings, and Files List Scrolling

I have successfully implemented all features, added the associated key bindings, fixed the bug showing action hotkeys on merged or closed pull requests, added graceful warnings for in-progress job logs, and implemented list view scrolling for the FILES CHANGED panel.

## Changes Made

### TUI Components

#### [model.go](internal/tui/model.go)
- Added `viewPRDiff` to the `viewState` enum.
- Added `prFileStartIndex` and `commitFileStartIndex` to the `Model` struct to enable scrolling on the FILES CHANGED lists.

#### [update.go](internal/tui/update.go)
- Updated `fetchPRDetailsCmd` to concurrently fetch PR files using the API client.
- Populated `m.prFiles` and initialized the diff viewport in `prDetailsLoadedMsg` handling.
- Implemented `updateDiffViewport()` helper to setup and display the patch content of the selected file.
- Implemented `scrollPRFiles()` and `scrollCommitFiles()` to dynamically update list viewport scroll boundaries.
- Adjusted diff viewport heights (`Height - 13`) in the resize handler and creation helpers to accommodate the file path header.
- Added keyboard shortcuts in the main loop:
  - From PR details, pressing `D` (Shift+d) transitions to the PR Diff view.
  - In `viewPRDiff`, navigating with `j/k` shifts the selected file, scrolls the list via `scrollPRFiles()`, and updates the diff viewport. `u/d` scrolls the viewport, `w` opens the PR files page in the browser, and `esc` / `backspace` goes back to PR details.
  - In `viewCommitDetails`, navigating with `j/k` scrolls the list via `scrollCommitFiles()`.
  - In `viewPRDetails`, pressing `r` / `ctrl+r` refreshes the PR details (fetching commits, files, comments, checks, and runs again).
- Fixed `viewerCanMerge()` to return `false` if `m.selectedPull` is `nil` or the PR status/state is not `"open"`, disabling the actions and hiding them from the footer.
- Added check for job status (`in_progress` or `queued`) on entering logs or refreshing logs, displaying a friendly warning `"Logs are not yet available for running jobs. Please wait for completion."` instead of attempting the API call.
- Updated `logsLoadedMsg` handling to intercept `404` or `BlobNotFound` errors, mapping them to the same friendly warning.

#### [view.go](internal/tui/view.go)
- Wired `viewPRDiff` state inside the `View` render switch.
- Implemented `renderPRDiffView()` displaying files changed on the left (sliced by `prFileStartIndex`) and the file patch diff on the right.
- Updated `renderCommitDetailsView()` to display files changed sliced by `commitFileStartIndex`.
- Added a styled header (`DIFF: <full_file_path>`) at the top of the right column inside both the Commit Details and PR Diff views.
- Aligned footer key definitions in `renderPRDetailsView` to display `C:Close PR` instead of `Shift+C:Close PR`, and added `r:Refresh`.

---

### Tests

#### [app_test.go](internal/tui/app_test.go)
- Added `TestTUI_PRDiffViewAndMergePermissions` to verify the `viewPRDiff` transitions, navigation, browser hook, and the disabled state of merge/close actions on closed/merged PRs. Also added test assertions checking the dynamic adjustment of `prFileStartIndex` when scrolling beyond the visible height limit.
- Added `TestTUI_RunningJobLogsAndPRDetailsRefresh` to verify the PR details refresh hotkey, the job status check before fetching logs, and the friendly error mapping for `logsLoadedMsg` failures on running jobs.

---

## Verification Results

### Automated Tests
Ran the full test suite and all tests passed successfully:
```bash
go test ./...
# Output:
# ?   	ghspector/cmd/ghspector	[no test files]
# ok  	ghspector/internal/auth	(cached)
# ok  	ghspector/internal/gh	(cached)
# ok  	ghspector/internal/tui	0.023s
```
