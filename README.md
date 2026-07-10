# ghspector

```text
      /\___/\
     / ⌐■_■ \
    (  = ~ = )
     \______/

    g h s p e c t o r
```

`ghspector` is a sleek, terminal-native user interface (TUI) for browsing, monitoring, and debugging GitHub Actions workflow runs, jobs, and logs in real-time. Built in Go using the Bubble Tea framework, it provides developer-friendly navigation, quick filters, historical run attempt browsing, and cross-platform browser integration.

## Table of Contents
- [Features](#features)
- [Installation](#installation)
- [Configuration](#configuration)
  - [Authentication Methods](#authentication-methods)
  - [Configuration File Options](#configuration-file-options)
- [Usage & Keybindings](#usage--keybindings)
  - [Global Keys](#global-keys)
  - [Workflow Runs View](#workflow-runs-view)
  - [Workflow Jobs View](#workflow-jobs-view)
  - [Logs Viewer](#logs-viewer)
  - [Context Switcher](#context-switcher)

---

## Features
- **Real-Time Polling**: Active background status updates for runs, jobs, and log tails.
- **Cross-Platform Links**: Native browser integration (via keypresses and clickable terminal OSC 8 hyperlinks).
- **Historical Attempts**: Easily browse and cycle through previous attempts of multi-run workflows.
- **Actor Filtering**: Filter runs by your own account or a specific GitHub username with instant matching.
- **Context Switcher**: Swap between user accounts and different organization scopes on the fly.
- **Keyboard-First Interface**: Complete and responsive shortcut navigation with a dedicated overlay help guide.

---

## Installation

> [!NOTE]
> Detailed installation guides and pre-built packages will be available soon.

*TODO: Add installation instructions (Go installer, homebrew, binary downloads, etc.).*

---

## Configuration

`ghspector` automatically loads configuration options upon startup.

### Authentication Methods

The application resolves your GitHub credentials using the following hierarchy:

1. **Environment Variables**:
   * Sets `GH_TOKEN` or `GITHUB_TOKEN` to your personal access token (PAT).
     ```bash
     export GH_TOKEN="ghp_yourpersonalaccesstokenhere"
     ```
2. **Configuration File**:
   * Reads from `config.yaml` in your standard user configuration directory (see options below).
3. **GitHub CLI fallback**:
   * If no token is found in the environment or file, `ghspector` integrates with the official GitHub CLI by querying `gh auth token` automatically.

### Configuration File Options

You can create a configuration file at:
* **Linux/macOS**: `~/.config/ghspector/config.yaml`
* **Windows**: `%APPDATA%\ghspector\config.yaml`

The following options are available:

```yaml
# ~/.config/ghspector/config.yaml

# Your GitHub Personal Access Token (PAT) with repo & workflow scopes
github_token: "ghp_yourpersonalaccesskeyhere"

# (Optional) Default organization context to open on startup
default_org: "my-organization-name"

# (Optional) Default user account context to open on startup
default_account: "my-github-username"

# (Optional) Background polling interval in seconds
polling_interval_seconds: 5
```

---

## Usage & Keybindings

Below is the complete keybindings map for navigating the application. Press `?` in any view to display the help overlay.

### Global Keys
| Key | Action |
| --- | --- |
| `?` | Toggle Help overlay screen |
| `o` | Open Context Switcher to swap account/organization scope |
| `q` or `Ctrl+C` | Quit `ghspector` |

### Workflow Runs View
| Key | Action |
| --- | --- |
| `j` / `Down` | Move selection down |
| `k` / `Up` | Move selection up |
| `Enter` | Select and fetch jobs for the highlighted run |
| `r` | Refresh the workflow runs list |
| `w` | Open the highlighted workflow run in your default browser |
| `m` | Toggle filtering by your own runs (current user) |
| `f` | Open text input to filter by specific actor username (type name and press `Enter`; `Esc` clears) |

### Workflow Jobs View
| Key | Action |
| --- | --- |
| `j` / `Down` | Move selection down |
| `k` / `Up` | Move selection up |
| `Enter` | View logs for the highlighted job |
| `[` | Cycle to the **previous** workflow run attempt (if multiple exist) |
| `]` | Cycle to the **next** workflow run attempt |
| `w` | Open the parent workflow run in your default browser |
| `v` | Open the highlighted job page in your default browser |
| `Esc` / `Backspace` | Return to the Workflow Runs list |
| `r` | Refresh the jobs list for the active attempt |

### Logs Viewer
| Key | Action |
| --- | --- |
| `u` | Scroll logs Up |
| `d` | Scroll logs Down |
| `r` | Refresh logs (tails active jobs in real-time) |
| `Esc` / `Backspace` | Return to the jobs list |

### Context Switcher
| Key | Action |
| --- | --- |
| `j` / `Down` | Navigate down the accounts/organizations list |
| `k` / `Up` | Navigate up the list |
| `Enter` | Confirm context switch and load runs for the selected scope |
| `Esc` | Close context switcher |
