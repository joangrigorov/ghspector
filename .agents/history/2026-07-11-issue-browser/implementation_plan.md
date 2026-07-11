# Implementation Plan: Issue Browser & Viewer

This plan outlines the design and implementation steps for adding a read-only Issue Browser/Viewer to `ghspector`. The implementation mirrors the existing Pull Requests browser, integrating seamlessly with the bubbletea framework.

---

## 1. Architecture & Data Structures

### A. API Wrapper Updates (`internal/gh`)

1. **`internal/gh/types.go`**:
   - Define the `Issue` struct:
     ```go
     type Issue struct {
         ID          int64      `json:"id"`
         Number      int        `json:"number"`
         Title       string     `json:"title"`
         Body        string     `json:"body"`
         State       string     `json:"state"` // "open" or "closed"
         HTMLURL     string     `json:"html_url"`
         CreatedAt   time.Time  `json:"created_at"`
         UpdatedAt   time.Time  `json:"updated_at"`
         ClosedAt    *time.Time `json:"closed_at"`
         User        *User      `json:"user"` // Creator
         Assignees   []User     `json:"assignees"`
         Labels      []Label    `json:"labels"`
         Milestone   *Milestone `json:"milestone"`
         // PullRequest is used to distinguish issues from PRs
         PullRequest *struct {
             URL string `json:"url"`
         } `json:"pull_request,omitempty"`
         Repository  Repository `json:"repository"`
     }
     ```

2. **`internal/gh/client.go`**:
   - Implement `GetIssuesWithState(ctx, owner, repo, state string, page, perPage int) ([]Issue, error)`:
     - Endpoint: `/repos/{owner}/{repo}/issues`
     - Query parameters: `state`, `page`, `per_page`
     - Filters out issues where `PullRequest != nil` (since the GitHub issues endpoint returns both issues and pull requests).

---

### B. Bubble Tea Model Updates (`internal/tui/model.go`)

1. **View State Extensions**:
   - Add view states:
     ```go
     viewIssueDetails
     viewIssueComments
     viewIssueFilterInput
     viewIssueFilterTypeSelect
     ```
2. **Main Tab Extensions**:
   - Update `mainTab` enum:
     ```go
     const (
         tabPRs mainTab = iota
         tabWorkflows
         tabIssues
     )
     ```
3. **Data Cache fields**:
   - Add fields for caching and tracking issues list and details:
     ```go
     issues             []gh.Issue
     selectedIssueIdx   int
     issueStartIndex    int
     issuePage          int
     hasMoreIssues      bool
     selectedIssue      *gh.Issue
     issueComments      []gh.IssueComment
     
     // Filter fields for issues
     filterIssueAuthor   string
     filterIssueAssignee string
     filterIssueState    string
     issueFilterUser     string
     ```
4. **Message Types**:
   - Add new tea message types:
     ```go
     type issuesLoadedMsg struct {
         issues []gh.Issue
         err    error
     }
     type issueDetailsLoadedMsg struct {
         issue        *gh.Issue
         comments     []gh.IssueComment
         renderedBody string
         err          error
     }
     ```

---

## 2. Controller & Command Updates (`internal/tui/update.go`)

### A. Data Fetching
- Implement `fetchIssuesCmd()`: fetches issues across accessible repositories concurrently, filtering out PRs, sorting by `UpdatedAt` descending, and returning `issuesLoadedMsg`.
- Implement `fetchIssueDetailsCmd(owner, repo string, number int)`: fetches full issue details and comments, then renders the issue body markdown via Glamour.
- Implement `fetchIssueCommentsCmd(owner, repo string, number int)`: fetches comments using the existing issue comments endpoint and returns them.

### B. Keyboard Shortcuts & Event Handling (`Update` function)
- **Tab/Shift+Tab cycling**: update modulo arithmetic to use `% 3`.
- **View Specific Keys in `viewMain` when `activeTab == tabIssues`**:
  - `j` / `down`: Scroll issues list down.
  - `k` / `up`: Scroll issues list up.
  - `Enter`: Fetch details and open `viewIssueDetails`.
  - `r`: Refresh issues.
  - `s`: Cycle issue state filter (`open` -> `closed` -> `all` -> `open`).
  - `a`: Filter issues authored by the current user.
  - `i`: Filter issues assigned to the current user.
  - `f`: Open custom user filter modal (`viewIssueFilterInput`).
  - `x`: Clear issue filters.
  - `w`: Open selected issue in browser.
- **Filter Inputs**:
  - Intercept keys in `viewIssueFilterInput` (typing username, `Enter` to select type, `Esc` to cancel).
  - Intercept keys in `viewIssueFilterTypeSelect` (`a` to filter by Author, `i` to filter by Assignee).
- **Issue Detail view (`viewIssueDetails`)**:
  - `Esc` / `Backspace`: Return to main tab.
  - `Tab` / `Shift+Tab`: Toggle focus between description viewport and metadata sidebar.
  - `j` / `down` / `k` / `up`: Scroll focused component.
  - `c`: Transition to `viewIssueComments`.
  - `w`: Open in browser.
  - `r`: Refresh details.
- **Issue Comments view (`viewIssueComments`)**:
  - `Esc` / `Backspace`: Return to Issue Details.
  - `j` / `down` / `k` / `up`: Scroll comments viewport.
  - `r`: Refresh comments.

---

## 3. UI & Views (`internal/tui/view.go`)

1. **Header Updates**:
   - Include `"Issues"` in the page title mapping if `activeTab == tabIssues`.
2. **List View (`renderIssuesView`)**:
   - Layout matching the Pull Requests table.
   - Column columns: `ST` (State status indicator dot: green for open, purple/gray for closed), `Issue #`, `Title`, `Author`, `Repository`, `Assignees`, and `Labels`.
   - Dynamic width estimation for Title based on screen size.
   - Show active filters and key shortcut help bar in the footer.
3. **Details View (`renderIssueDetailsView`)**:
   - Left column: Glamour-rendered markdown body viewport.
   - Right column: Sidebar showing:
     - State (Open/Closed)
     - Author
     - Repository
     - Milestone
     - Assignees
     - Labels
4. **Comments View (`renderIssueCommentsView`)**:
   - Unified viewport rendering Glamour-formatted issue comments.
5. **Modals**:
   - Render modals for `viewIssueFilterInput` and `viewIssueFilterTypeSelect`.

---

## 4. Implementation Steps

1. **Phase 1: API Changes**
   - Implement `Issue` types in `types.go`.
   - Implement client function `GetIssuesWithState` in `client.go`.
2. **Phase 2: Model & Navigation State Setup**
   - Expand `mainTab` and `viewState` enums.
   - Add cache fields and initialization values in `model.go`.
3. **Phase 3: Controller logic (Commands & Update loop)**
   - Add data fetching commands.
   - Wire up update loop events for list navigation, details view, comments view, and filtering.
4. **Phase 4: Views & Modals**
   - Add layout rendering functions in `view.go`.
   - Update header/footer logic.
5. **Phase 5: Verification & Tests**
   - Run tests (`go test ./internal/tui/...`).
   - Run ghspector locally to interactively test issue browsing.
