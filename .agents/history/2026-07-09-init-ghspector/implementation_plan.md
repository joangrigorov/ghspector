# Implementation Plan: `ghspector` (Go TUI for GitHub Actions)

`ghspector` is a terminal user interface (TUI) written in Go using the Bubble Tea framework. It allows developers to browse, monitor, and inspect GitHub Actions workflow runs, jobs, steps, and logs across accessible organizations and accounts.

---

## 1. System Architecture

```mermaid
graph TD
    A[main.go] --> B[internal/auth]
    A --> C[internal/tui]
    C --> D[internal/gh]
    
    subgraph UI Components (Bubble Tea / Lipgloss)
        C --> C1[Splash Screen / ASCII Logo]
        C --> C2[Main Workflow Run Table]
        C --> C3[Jobs List View]
        C --> C4[Step & Log Browser]
        C --> C5[Org/Account Switcher Popup]
        C --> C6[Status/Bottom Bar]
    end
    
    subgraph GitHub API Interaction
        D --> D1[Auth Token Manager]
        D --> D2[Rate Limiting & Polling Engine]
        D --> D3[GitHub HTTP Client]
    end
```

---

## 2. Authentication & Configuration Flow

`ghspector` handles authentication using a 3-step fallback sequence. If no authenticated token is found, it exits with clear setup instructions.

### Auth Order of Precedence:
1. **Environment Variables**: `GH_TOKEN` or `GITHUB_TOKEN`.
2. **Config File**: `~/.config/ghspector/config.yaml` (by default on Unix/Linux).
   - Resolved using Go's `os.UserConfigDir()` to support multi-platform config directories:
     - **Linux/Unix**: `~/.config/ghspector/config.yaml` (or `$XDG_CONFIG_HOME/ghspector/config.yaml`)
     - **macOS**: `~/Library/Application Support/ghspector/config.yaml`
     - **Windows**: `%APPDATA%\ghspector\config.yaml` (typically `C:\Users\<Username>\AppData\Roaming\ghspector\config.yaml`)
   - The directory and file will be created with restricted permissions (`0600` on Unix/macOS) to protect the stored token.
3. **GitHub CLI Fallback**: Run `gh auth token` to retrieve an active token.

### Config File Structure (`config.yaml`):
```yaml
github_token: "ghp_..."
default_org: "my-org"
default_account: "my-user"
polling_interval_seconds: 5
```

### Unauthenticated Instruction Output:
If all fallback options fail, the app will output:
```
Error: GitHub token not found.

To authenticate ghspector, please perform one of the following steps:

1. Authenticate via GitHub CLI (recommended):
   $ gh auth login --scopes "repo,workflow,read:org"
   (ghspector will automatically pick up your credentials)

2. Set the GH_TOKEN environment variable:
   $ export GH_TOKEN=your_personal_access_token

3. Create a configuration file at ~/.config/ghspector/config.yaml:
   github_token: "your_personal_access_token"
```

---

## 3. UI, Navigation, and Keybindings

The TUI uses **Bubble Tea** for model-view-update architecture, **Lipgloss** for responsive design and light/dark theme adaptation, and **Bubbles** for viewport and table components.

### Screen Layout:
- **Header**: App title `ghspector` + currently active org/account and repo context.
- **Content Area**: Dynamic depending on the active state.
- **Footer**: Standard keymap helper status bar.

### Screen Flow:
1. **Splash Screen**: Display a colorful ASCII art of a cat wearing glasses and "ghspector" logo while initial fetching occurs.
2. **Main View (Workflow Runs)**:
   - A list/table of recent runs across all accessible repos or the selected org/account.
   - Shows repo name, workflow name, event type, status, and execution time.
   - "Load More" indicator/trigger at the bottom when scrolled to the end.
3. **Jobs View**:
   - Lists all jobs in the selected workflow run, showing job name, repository, and job status.
4. **Log Browser View**:
   - Shows steps inside the selected job.
   - Embeds a viewport to view live/concluded logs with scrolling capability.

### Navigation Keymap (Vim-style):
- `q` / `Ctrl+C`: Quit application.
- `Esc` / `Backspace`: Return to previous view (Logs -> Jobs -> Main).
- `j` / `k` / Up / Down: Move selection down/up.
- `u` / `d` / PageUp / PageDown: Scroll logs page up/down.
- `Enter`: Select / drills down into item.
- `o`: Open organization/account switcher dropdown/popup.
- `Ctrl+R` / `r`: Force manual refresh.

---

## 4. Rate-Limit Safe Concurrency & Polling

To display live job execution status without exhausting GitHub's API rate limits, `ghspector` will implement an active polling coordinator:

1. **Only Poll Visible Runs/Jobs**:
   - The UI coordinator only initiates polling goroutines for items that are currently active (running/queued) and visible in the viewport.
2. **Inspect Rate Limit Headers**:
   - Every GitHub API response will be inspected for headers:
     - `X-RateLimit-Limit`
     - `X-RateLimit-Remaining`
     - `X-RateLimit-Reset`
   - If `X-RateLimit-Remaining` drops below 10% of `X-RateLimit-Limit`, the app will double the polling interval or pause polling until the reset time is reached, notifying the user in the status bar.
3. **Goroutine-safe state**:
   - Updates are sent back to the main Bubble Tea event loop via bubble tea messages (`tea.Cmd` / `tea.Msg`), preventing race conditions.

---

## 5. Visual Indicators & Theme Compatibility

We strictly avoid emojis and use ANSI/Lipgloss styling to draw status symbols.

| Status | Indicator Symbol | Default Dark Style | Default Light Style |
| :--- | :--- | :--- | :--- |
| **Running** | `■` (Filled Block) | Orange (`#ff8700`) | Orange (`#d75f00`) |
| **Successful** | `■` (Filled Block) | Green (`#00af00`) | Green (`#008700`) |
| **Failed** | `■` (Filled Block) | Red (`#df0000`) | Red (`#af0000`) |
| **Queued** | `□` (Hollow Block) | Yellow (`#d7af00`) | Yellow (`#af8700`) |

### Responsive & Theme Adaptation:
- Colors are calculated dynamically using Lipgloss depending on the terminal's theme background query or using ANSI color escapes that translate properly across default terminal schemes.
- Layout widths/heights will listen to `tea.WindowSizeMsg` and resize tables and viewports dynamically.

---

## 6. Testing Strategy

Integration tests are implemented using Go's `net/http/httptest` package.

### Test Coverage Areas:
- **API Responses**: Serving mock JSON files for workflows, jobs, and log downloads.
- **Authentication Handlers**: Simulating 401/403 HTTP errors and verifying appropriate TUI transition or standard error prints.
- **Pagination**: Verifying "Load More" behaves correctly under standard GitHub pagination links (`Link` headers).
- **Rate Limit Headers**: Simulating `X-RateLimit-Remaining` being low or exhausted (429 response) and asserting that the client backs off.
- **No Results**: Clean state handling if the account or org has no recent runs.

---

## 7. Multi-Platform Build Configuration (`.goreleaser.yaml`)

Configure GoReleaser to compile statically-linked binaries for:
- **Linux**: amd64, arm64
- **macOS**: amd64, arm64
- **Windows**: amd64, arm64

---

## 8. Phased Implementation Plan

### Phase 1: Module Initialisation & Authentication Manager
- Initialize Go module, pull dependencies (`bubbletea`, `lipgloss`, `bubbles`, `go-github`, `yaml.v3`).
- Implement credentials provider sequence (`internal/auth/auth.go`).
- Write unit tests for auth resolution.

### Phase 2: GitHub API Integration & Polling Controller
- Implement API client (`internal/gh/client.go`) wrapping `go-github` with headers inspection and rate-limit aware tracking.
- Add dynamic list fetching with pagination (`Link` header parser).
- Write `httptest.Server`-based integration tests.

### Phase 3: Splash Screen & Theme Core
- Create Lipgloss color scheme definitions supporting Light/Dark terminals.
- Draw the colorful splash screen ASCII cat wearing glasses.
- Test terminal size adjustments.

### Phase 4: Bubble Tea Core TUI Model
- Create views:
  - Main workflow runs table (including Load More button).
  - Selected run jobs list.
  - Job step and log scrollable viewport.
- Link views via keyboard shortcuts.
- Implement polling loop for active visible items.

### Phase 5: Goreleaser & Integration Testing
- Configure `.goreleaser.yaml`.
- Refine error and rate limit mock tests.
- Verify multi-platform compilation.
