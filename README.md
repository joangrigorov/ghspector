# ghspector

```text
      /\___/\
     / ⌐■_■ \
    (  = ~ = )
     \______/

    g h s p e c t o r
```

`ghspector` is a sleek, terminal-native developer dashboard (TUI) for universal GitHub operations. Built in Go using the Bubble Tea framework, it provides a unified interface to browse, monitor, and manage your workflows, pull requests and issues across entire user or organization scopes in real-time.

With `ghspector`, you can tail workflow logs, cycle through run attempts, inspect pull request checks and metadata, and merge or close pull requests directly from your terminal.

### The Problem It Solves

Monitoring GitHub Actions workflows across multiple active repositories typically requires opening dozens of browser tabs, navigating back and forth, and constantly reloading pages. `ghspector` solves this friction by aggregating workflow runs and pull requests across a user account or organization into a single, cohesive dashboard. It eliminates tab clutter and context switching, bringing real-time observability and GitHub operations directly into your terminal.

## Table of Contents
- [Features](#features)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage & Keybindings](#usage--keybindings)
  - [Global Keys](#global-keys)
  - [Full Keybinding Reference](#full-keybinding-reference)

---

## Features
- **Unified GitHub Operations**: A single, clean terminal interface to manage CI/CD workflow runs, pull requests and issues.
- **Aggregated Scopes**: View combined workflows, pull requests and issues aggregated across a user account or an entire organization. Pull requests and issues are one cursor-paginated GraphQL search query per page, so their filters and their count cover the whole scope; workflow runs cover the repositories pushed to most recently, because GitHub's GraphQL schema exposes no scope-wide run list.
- **Real-Time Polling**: Active background status updates for runs, jobs, logs, pull requests and issues.
- **Workflow & Log Viewer**: Cycle through historical attempts of multi-run workflows and tail active jobs in real-time.
- **Keyboard-Driven PR Management**: Read PR descriptions, inspect Action checks/comments, and merge (Squash, Normal, Rebase) or close PRs directly.
- **The Whole Conversation**: A pull request's discussion is read from the three lists GitHub keeps it in — comments, reviews, and the inline comments a review carries — merged in the order they were written, with each review's verdict, summary and file threads (`path:line`, the hunk, the replies) under the review that opened them. A thread is the comment it started from, so a reply posted from a later review stays under the remark it answers.
- **Issue Browser**: Read issue descriptions and metadata, and follow a thread's comments, with author/assignee filters.
- **Rendered Markdown**: Pull request and issue descriptions, comments and commit messages are rendered as documents — headings, lists, quotes, inline code — in the same palette as the rest of the screen, in either light or dark mode.
- **Actor Filtering**: Quick text filtering to isolate workflow runs, PRs or issues by specific users.
- **Context Switcher**: Swap between user profiles and organization scopes on the fly.
- **Terminal Hyperlinks**: Support for clickable OSC 8 links and quick keys to open runs, jobs, and PRs in your default browser. On the conversation screens the repository, the branch compare, every thread's location, and the name and time of every contribution are links too.
- **Honest Counts**: A list says "Load More" while the API reports matches left, rather than when a page happened to come back full.

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

### macOS Installation

You can download and install the pre-built macOS binary directly via your terminal. 

Select the commands corresponding to your Mac's architecture:

#### Apple Silicon (M-series)
```bash
# Download the archive
curl -Lo ghspector.tar.gz https://github.com/joangrigorov/ghspector/releases/latest/download/ghspector_darwin_arm64.tar.gz

# Extract the binary
tar -xzf ghspector.tar.gz ghspector

# Move the binary to your PATH
sudo mv ghspector /usr/local/bin/

# Clean up
rm ghspector.tar.gz
```

#### Intel
```bash
# Download the archive
curl -Lo ghspector.tar.gz https://github.com/joangrigorov/ghspector/releases/latest/download/ghspector_darwin_x86_64.tar.gz

# Extract the binary
tar -xzf ghspector.tar.gz ghspector

# Move the binary to your PATH
sudo mv ghspector /usr/local/bin/

# Clean up
rm ghspector.tar.gz
```

### From Source

If you have Go 1.27.2 or later installed, you can build and install the latest release directly:
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
     *Note: To merge Pull Requests and approve workflow runs/deployments, this token must have the `repo` and `workflow` scopes.*
2. **Configuration File**:
   * Reads from `config.yaml` in your standard user configuration directory (see options below).
3. **GitHub CLI fallback**:
   * If no token is found in the environment or file, `ghspector` integrates with the official GitHub CLI by querying `gh auth token` automatically.
   * If you are already logged in but need write permissions (for merging PRs or approving runs), refresh your scopes:
     ```bash
     gh auth refresh -s repo -s workflow
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

# (Optional) Default merge method to use when merging PRs (squash, merge, or rebase)
default_merge_method: "squash"

# (Optional) Background polling intervals in seconds (contextualized per tab/use-case)
polling:
  workflows_interval_seconds: 5
  prs_interval_seconds: 10
  issues_interval_seconds: 10
```

---

## Usage & Keybindings

Below is the keybindings map for navigating the application. Press `?` in any view to display the help overlay.

### Switching Tabs
Available on the three top-level lists (`Tab` and `Shift+Tab` cycle the tabs, and a tab fetches when it is first opened):

| Key | Action |
| --- | --- |
| `Tab` | Switch to the **next** main tab (Pull Requests → Workflows → Issues) |
| `Shift+Tab` | Switch to the **previous** main tab |

The tabs are **Pull Requests**, **Workflows**, **Issues**, and the app opens on
Pull Requests.

### Global Keys
Available on every screen, including inside the detail views:

| Key | Action |
| --- | --- |
| `?` | Toggle the help screen |
| `o` | Open the context switcher to swap account/organization scope |
| `q` or `Ctrl+C` | Quit `ghspector` |

Over a modal, `q`/`Ctrl+C` still quits; over a text box, `Ctrl+C` does, and `q` is a character you are typing.

### Full Keybinding Reference
Every key, grouped by the view it applies in, is in
[docs/keybindings.md](docs/keybindings.md). That file is generated from the
keymap table the app itself dispatches from, so the reference and the behaviour
cannot drift apart. Press `?` in the app for the same content.
