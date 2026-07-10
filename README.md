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

`ghspector` offers pre-built binaries and native packages for multiple platforms.

### Pre-Built Binaries

Download the appropriate archive for your system (Linux, macOS, or Windows) from the [Releases](https://github.com/joangrigorov/ghspector/releases) page, extract it, and move the binary to a directory in your `PATH`.

### Linux Package Installation

We provide native packages for popular package managers. Download the package for your architecture (e.g., `amd64`, `arm64`) from the [Releases](https://github.com/joangrigorov/ghspector/releases) page and install it:

#### Debian / Ubuntu (`.deb`)
```bash
sudo dpkg -i ghspector_<version>_linux_<arch>.deb
```

#### Fedora / RHEL (`.rpm`)
```bash
sudo rpm -ivh ghspector_<version>_linux_<arch>.rpm
```

#### Alpine Linux (`.apk`)
```bash
sudo apk add --allow-untrusted ghspector_<version>_linux_<arch>.apk
```

### From Source

If you have Go 1.26.5 or later installed, you can build and install the latest release directly:
```bash
go install github.com/joangrigorov/ghspector/cmd/ghspector@latest
```

---

## Configuration

`ghspector` automatically loads configuration options upon startup.

### Authentication Methods

The application resolves your GitHub credentials using the following hierarchy:

1. **Environment Variables**:
   * Checks if `GH_TOKEN` or `GITHUB_TOKEN` is set in your environment:
     ```bash
     export GH_TOKEN="ghp_yourpersonalaccesstokenhere"
     ```
     *Note: To merge Pull Requests, this token must have the `repo` scope.*
2. **Configuration File**:
   * Reads from `config.yaml` in your standard user configuration directory (see options below).
3. **GitHub CLI fallback**:
   * If no token is found in the environment or file, `ghspector` integrates with the official GitHub CLI by querying `gh auth token` automatically.
   * If you are already logged in but need write permissions (for merging PRs), refresh your scopes:
     ```bash
     gh auth refresh -s repo
     ```

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

# (Optional) Background polling intervals in seconds (contextualized per tab/use-case)
polling:
  workflows_interval_seconds: 5
  prs_interval_seconds: 10
```

---

## Usage & Keybindings

Below is the complete keybindings map for navigating the application. Press `?` in any view to display the help overlay.

### Global Keys
| Key | Action |
| --- | --- |
| `Tab` | Switch to the **next** main tab (Dashboard → Workflows → Pull Requests) |
| `Shift+Tab` | Switch to the **previous** main tab |
| `?` | Toggle Help overlay screen |
| `o` | Open Context Switcher to swap account/organization scope |
| `q` or `Ctrl+C` | Quit `ghspector` |

### Dashboard View
Displays your active GitHub context, API rate limit consumption status, aggregated pull request and workflow run metrics, and navigation shortcuts.

### Workflow Runs View
| Key | Action |
| --- | --- |
| `j` / `Down` | Move selection down |
| `k` / `Up` | Move selection up |
| `Enter` | Select and fetch jobs for the highlighted run |
| `r` | Refresh the workflow runs list |
| `w` | Open the highlighted workflow run in your default browser |
| `m` | Toggle filtering by your own runs (current user) |
| `f` | Open text input to filter by specific actor username |

### Pull Requests View
| Key | Action |
| --- | --- |
| `j` / `Down` | Move selection down |
| `k` / `Up` | Move selection up |
| `Enter` | Open the detailed workspace for the highlighted pull request |
| `r` | Refresh the pull requests list |
| `w` | Open the highlighted pull request in your default browser |

### Pull Request Details View
Symmetric three-column layout (Checks sidebar, scrollable PR description body, Metadata details sidebar).
| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Toggle interactive focus between Checks sidebar and PR description viewport |
| `j` / `k` (or `Down` / `Up`) | Navigate highlighted Check items, or scroll PR description body |
| `u` / `d` | Scroll PR description body up/down |
| `Enter` | Open selected Actions check run job in-app, or open non-actions check in browser |
| `w` | Open the pull request (or selected check) page in your default browser |
| `m` | Initiate Merge process (available only with repository write permissions) |
| `c` | Initiate Close process (available only with repository write permissions) |
| `Esc` / `Backspace` | Return to the Pull Requests list |

### PR Merging & Closing Overlays
Available when repository write scopes exist.
| Key | Action |
| --- | --- |
| **Merge Selection (`m`):** | |
| `s` / `S` | Select Squash Merge (default) and go to confirmation screen |
| `m` / `M` | Select Normal Merge (create merge commit) and go to confirmation screen |
| `r` / `R` | Select Rebase Merge and go to confirmation screen |
| `esc` / `c` / `C` | Cancel merge process |
| **Confirm Action:** | |
| `y` / `Y` | Confirm merge or close execution |
| `n` / `N` / `Esc` | Cancel confirmation and return |

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
| `Esc` / `Backspace` | Return to the parent list (runs or PR checks) |
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
