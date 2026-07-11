# Walkthrough - PR Diff View and Merged PR Action Hotkeys Fix

I have successfully implemented the PR Diff page feature, added the associated key bindings, and fixed the bug showing action hotkeys on merged or closed pull requests.

## Changes Made

### TUI Components

#### [model.go](internal/tui/model.go)
- Added `viewPRDiff` to the `viewState` enum.

#### [update.go](internal/tui/update.go)
- Updated `fetchPRDetailsCmd` to concurrently fetch PR files using the API client.
- Populated `m.prFiles` and initialized the diff viewport in `prDetailsLoadedMsg` handling.
- Implemented `updateDiffViewport()` helper to setup and display the patch content of the selected file.
- Adjusted diff viewport heights (`Height - 13`) in the resize handler and creation helpers to accommodate the file path header.
- Added keyboard shortcuts in the main loop:
  - From PR details, pressing `D` (Shift+d) transitions to the PR Diff view.
  - In `viewPRDiff`, navigating with `j/k` shifts the selected file and updates the diff viewport, `u/d` scrolls the viewport, `w` opens the PR files page in the browser, and `esc` / `backspace` goes back to PR details.
- Fixed `viewerCanMerge()` to return `false` if `m.selectedPull` is `nil` or the PR status/state is not `"open"`, disabling the actions and hiding them from the footer.

#### [view.go](internal/tui/view.go)
- Wired `viewPRDiff` state inside the `View` render switch.
- Implemented `renderPRDiffView()` displaying files changed on the left and the file patch diff on the right.
- Added a styled header (`DIFF: <full_file_path>`) at the top of the right column inside both the Commit Details and PR Diff views.
- Aligned footer key definitions in `renderPRDetailsView` to display `C:Close PR` instead of `Shift+C:Close PR`.

---

### Tests

#### [app_test.go](internal/tui/app_test.go)
- Added `TestTUI_PRDiffViewAndMergePermissions` to fully verify the `viewPRDiff` transitions, navigation, browser hook, and the disabled state of merge/close actions on closed/merged PRs.

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
# ok  	ghspector/internal/tui	0.064s
```
