package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the TUI screen.
func (m Model) View() string {
	if m.err != nil {
		return m.renderErrorView()
	}

	switch m.state {
	case viewSplash:
		return RenderSplash(m.theme, m.loadingMsg, m.tickCount)
	case viewMain:
		return m.renderMainView()
	case viewJobs:
		return m.renderJobsView()
	case viewLogs:
		return m.renderLogsView()
	case viewSwitcher:
		return m.renderSwitcherView()
	case viewHelp:
		return m.renderHelpView()
	default:
		return "Unknown application state"
	}
}

// renderErrorView renders a full screen error block.
func (m Model) renderErrorView() string {
	var sb strings.Builder
	sb.WriteString("\n  " + m.theme.StatusFailed.Render("FATAL ERROR") + "\n\n")
	sb.WriteString("  " + m.theme.TableRow.Render(m.err.Error()) + "\n\n")
	sb.WriteString("  Press q or Ctrl+C to exit.\n")
	return sb.String()
}

// Helper to format duration
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func (m Model) getStatusIndicator(status, conclusion string) string {
	if status == "waiting" || conclusion == "action_required" {
		return m.theme.StatusWaiting.Render("◆")
	}
	switch status {
	case "queued":
		return m.theme.StatusQueued.Render("□")
	case "in_progress":
		return m.theme.StatusRunning.Render("■")
	case "completed":
		switch conclusion {
		case "success":
			return m.theme.StatusSuccessful.Render("■")
		case "failure", "timed_out":
			return m.theme.StatusFailed.Render("■")
		default:
			return m.theme.StatusNeutral.Render("■")
		}
	default:
		return m.theme.StatusNeutral.Render("■")
	}
}

// renderHeader renders the standard top bar.
func (m Model) renderHeader() string {
	activeTarget := "None"
	if len(m.targets) > 0 && m.selectedTargetIdx < len(m.targets) {
		activeTarget = m.targets[m.selectedTargetIdx].Name
	}
	if m.filterActor != "" {
		activeTarget += fmt.Sprintf(" (Filter: @%s)", m.filterActor)
	}

	rl := m.client.GetRateLimit()
	rlStr := ""
	if rl.Limit > 0 {
		rlStr = fmt.Sprintf("Rate Limit: %d/%d reqs", rl.Remaining, rl.Limit)
		// Warning color if rate limit is low
		if rl.Remaining < 200 {
			rlStr = m.theme.StatusFailed.Render(rlStr)
		} else if rl.Remaining < 1000 {
			rlStr = m.theme.StatusQueued.Render(rlStr)
		} else {
			rlStr = m.theme.StatusSuccessful.Render(rlStr)
		}
	}

	title := m.theme.Title.Render("ghspector")
	contextInfo := m.theme.Subtitle.Render("Account/Org: " + activeTarget)

	// Clamp/protect layout dimensions
	width := m.width
	if width < 40 {
		width = 40
	}

	neededWidth := lipgloss.Width(title) + lipgloss.Width(contextInfo) + 6
	if rlStr != "" {
		neededWidth += lipgloss.Width(rlStr)
	}

	if width < neededWidth {
		// Hide rate limit first
		rlStr = ""
		neededWidth = lipgloss.Width(title) + lipgloss.Width(contextInfo) + 6
		if width < neededWidth {
			// Hide context info too
			contextInfo = ""
		}
	}

	rightSpace := width - lipgloss.Width(title) - 4
	if contextInfo != "" {
		rightSpace -= lipgloss.Width(contextInfo)
	}
	if rlStr != "" {
		rightSpace -= lipgloss.Width(rlStr)
	}

	if rightSpace < 1 {
		rightSpace = 1
	}
	spaces := strings.Repeat(" ", rightSpace)

	res := "\n " + title + "  "
	if contextInfo != "" {
		res += contextInfo
	}
	res += spaces
	if rlStr != "" {
		res += rlStr
	}
	return res + "\n"
}

// renderFooter renders the standard bottom bar.
func (m Model) renderFooter(keys []string) string {
	var formatted []string
	for _, k := range keys {
		parts := strings.SplitN(k, ":", 2)
		if len(parts) == 2 {
			formatted = append(formatted, m.theme.HelpKey.Render(parts[0])+m.theme.HelpDesc.Render(":"+parts[1]))
		} else {
			formatted = append(formatted, m.theme.HelpDesc.Render(k))
		}
	}

	status := ""
	if m.statusMsg != "" {
		lowerMsg := strings.ToLower(m.statusMsg)
		if strings.HasPrefix(lowerMsg, "error") {
			cleanErr := m.statusMsg
			cleanErr = strings.TrimPrefix(cleanErr, "Error: ")
			cleanErr = strings.TrimPrefix(cleanErr, "error: ")
			status = " | " + m.theme.StatusFailed.Render("error: "+cleanErr)
		} else {
			status = m.theme.Subtitle.Render(" | msg: " + m.statusMsg)
		}
	}

	content := strings.Join(formatted, "  ") + status
	return "\n" + m.theme.BottomBar.Render(content) + "\n"
}

func shortenEvent(event string) string {
	switch event {
	case "pull_request":
		return "pull_req"
	case "pull_request_target":
		return "pr_target"
	case "workflow_dispatch":
		return "dispatch"
	case "workflow_run":
		return "wf_run"
	default:
		if len(event) > 12 {
			return event[:9] + "..."
		}
		return event
	}
}

// renderMainView renders the Workflow Runs list with a scrolling window.
func (m Model) renderMainView() string {
	var sb strings.Builder

	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	// Table header
	header := fmt.Sprintf("  %-3s %-18s %-26s %-12s %-12s %-12s", "ST", "REPOSITORY", "WORKFLOW RUN", "EVENT", "ACTOR", "DURATION")
	sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

	renderedCount := 0
	if len(m.runs) == 0 {
		sb.WriteString("\n  " + m.theme.HelpDesc.Render("No recent workflow runs found.") + "\n\n")
		renderedCount = 3
	} else {
		visibleRows := m.height - 12
		if m.showFilterInput {
			visibleRows -= 2
		}
		if visibleRows < 5 {
			visibleRows = 5
		}

		endIdx := m.runStartIndex + visibleRows
		totalRows := len(m.runs)
		if m.hasMoreRuns {
			totalRows++
		}
		if endIdx > totalRows {
			endIdx = totalRows
		}

		renderedCount = endIdx - m.runStartIndex

		for i := m.runStartIndex; i < endIdx; i++ {
			if i < len(m.runs) {
				run := m.runs[i]
				statusInd := m.getStatusIndicator(run.Status, run.Conclusion)

				// Calculate Duration / Age
				durStr := ""
				if run.Status == "in_progress" {
					durStr = formatDuration(m.client.Now().Sub(run.CreatedAt))
					durStr = m.theme.StatusRunning.Render(durStr)
				} else if run.Status == "queued" {
					durStr = "queued"
					durStr = m.theme.StatusQueued.Render(durStr)
				} else {
					durStr = formatDuration(run.UpdatedAt.Sub(run.CreatedAt))
				}

				repoName := run.Repository.Name
				if len(repoName) > 16 {
					repoName = repoName[:13] + "..."
				}

				runName := run.Name
				if runName == "" && run.DisplayTitle != "" {
					runName = run.DisplayTitle
				}
				if len(runName) > 24 {
					runName = runName[:21] + "..."
				}

				actorName := "unknown"
				if run.Actor != nil && run.Actor.Login != "" {
					actorName = run.Actor.Login
				}
				if len(actorName) > 12 {
					actorName = actorName[:9] + "..."
				}

				paddedRunName := fmt.Sprintf("%-26s", runName)
				hyperlinkedRunName := renderHyperlink(paddedRunName, run.HTMLURL)

				rowText := fmt.Sprintf("  %-3s %-18s %s %-12s %-12s %-12s",
					statusInd,
					repoName,
					hyperlinkedRunName,
					shortenEvent(run.Event),
					actorName,
					durStr,
				)

				if i == m.selectedRunIdx {
					sb.WriteString(m.theme.TableSelected.Render(rowText) + "\n")
				} else {
					sb.WriteString(m.theme.TableRow.Render(rowText) + "\n")
				}
			} else if i == len(m.runs) && m.hasMoreRuns {
				loadText := "  [-- Load More Workflow Runs... --]"
				if m.selectedRunIdx == len(m.runs) {
					sb.WriteString(m.theme.TableSelected.Render(loadText) + "\n")
				} else {
					sb.WriteString(m.theme.Subtitle.Render(loadText) + "\n")
				}
			}
		}
	}

	// Dynamic sizing pads
	contentHeight := renderedCount + 10
	if m.showFilterInput {
		contentHeight += 2
	}
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	if m.showFilterInput {
		sb.WriteString("  Filter by actor: " + m.textInput.View() + "\n")
	}

	keys := []string{"?:Help", "j/k:Navigate", "Enter:Jobs", "w:Browser", "r:Refresh"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

// renderJobsView renders the list of jobs in a workflow run with a scrolling window.
func (m Model) renderJobsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	run := m.runs[m.selectedRunIdx]
	workflowTitleText := renderHyperlink("Workflow: "+run.Name, run.HTMLURL)
	sb.WriteString("  " + m.theme.LogoText.Render(workflowTitleText) + "\n")

	attemptText := ""
	if run.RunAttempt > 1 {
		attemptText = fmt.Sprintf(" | Attempt %d of %d (use [ / ] to switch)", m.selectedAttempt, run.RunAttempt)
	}
	sb.WriteString("  " + m.theme.HelpDesc.Render(fmt.Sprintf("Repo: %s | Branch: %s | SHA: %s%s", run.Repository.FullName, run.HeadBranch, run.HeadSHA[:7], attemptText)) + "\n\n")

	header := fmt.Sprintf("  %-3s %-40s %-15s %-12s", "ST", "JOB NAME", "STARTED", "DURATION")
	sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

	renderedCount := 0
	if len(m.jobs) == 0 {
		sb.WriteString("\n  " + m.theme.HelpDesc.Render("No jobs found for this workflow run.") + "\n\n")
		renderedCount = 3
	} else {
		visibleRows := m.height - 15
		if visibleRows < 5 {
			visibleRows = 5
		}

		endIdx := m.jobStartIndex + visibleRows
		if endIdx > len(m.jobs) {
			endIdx = len(m.jobs)
		}

		renderedCount = endIdx - m.jobStartIndex

		for i := m.jobStartIndex; i < endIdx; i++ {
			job := m.jobs[i]
			statusInd := m.getStatusIndicator(job.Status, job.Conclusion)

			startedStr := job.StartedAt.Format("15:04:05")
			if job.StartedAt.IsZero() {
				startedStr = "N/A"
			}

			durStr := ""
			if job.Status == "in_progress" {
				durStr = formatDuration(m.client.Now().Sub(job.StartedAt))
				durStr = m.theme.StatusRunning.Render(durStr)
			} else if job.Status == "queued" {
				durStr = "queued"
				durStr = m.theme.StatusQueued.Render(durStr)
			} else if !job.CompletedAt.IsZero() && !job.StartedAt.IsZero() {
				durStr = formatDuration(job.CompletedAt.Sub(job.StartedAt))
			} else {
				durStr = "N/A"
			}

			jobName := job.Name
			if len(jobName) > 38 {
				jobName = jobName[:35] + "..."
			}

			paddedJobName := fmt.Sprintf("%-40s", jobName)
			hyperlinkedJobName := renderHyperlink(paddedJobName, job.HTMLURL)

			rowText := fmt.Sprintf("  %-3s %s %-15s %-12s",
				statusInd,
				hyperlinkedJobName,
				startedStr,
				durStr,
			)

			if i == m.selectedJobIdx {
				sb.WriteString(m.theme.TableSelected.Render(rowText) + "\n")
			} else {
				sb.WriteString(m.theme.TableRow.Render(rowText) + "\n")
			}
		}
	}

	contentHeight := renderedCount + 14
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"?:Help", "j/k:Navigate", "Enter:Logs", "Esc:Back", "w/v:Browser", "[/]:Attempts"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

// renderLogsView renders the logs viewer and steps list.
func (m Model) renderLogsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	job := m.jobs[m.selectedJobIdx]
	sb.WriteString("  " + m.theme.LogoText.Render("Job: "+job.Name) + "\n")

	// Render steps summaries
	var stepsSummary []string
	for _, step := range job.Steps {
		indicator := m.getStatusIndicator(step.Status, step.Conclusion)
		stepsSummary = append(stepsSummary, fmt.Sprintf("%s %s", indicator, step.Name))
	}

	if len(stepsSummary) > 0 {
		sb.WriteString("  Steps: " + strings.Join(stepsSummary, " → ") + "\n")
	}
	sb.WriteString("\n")

	// Draw viewport
	sb.WriteString("  " + m.theme.Border.Render(m.logsViewport.View()) + "\n")

	keys := []string{"?:Help", "u/d:Scroll", "Esc:Back", "r:Refresh"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

// renderSwitcherView renders the context selection dialog overlay.
func (m Model) renderSwitcherView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	sb.WriteString("  " + m.theme.LogoText.Render("Switch GitHub Account/Organization Context") + "\n\n")

	for i, target := range m.targets {
		prefix := "  "
		typeStr := "[User]"
		if target.IsOrg {
			typeStr = "[Org] "
		}

		rowText := fmt.Sprintf("    %s %s", typeStr, target.Name)
		if i == m.selectedTargetIdx {
			sb.WriteString("  " + m.theme.TableSelected.Render(rowText) + "\n")
		} else {
			sb.WriteString(prefix + m.theme.TableRow.Render(rowText) + "\n")
		}
	}

	contentHeight := len(m.targets) + 8
	padding := m.height - contentHeight - 4
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"?:Help", "j/k:Navigate", "Enter:Confirm", "Esc:Close"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderLegend() string {
	running := m.theme.StatusRunning.Render("■") + " running"
	success := m.theme.StatusSuccessful.Render("■") + " success"
	failed := m.theme.StatusFailed.Render("■") + " failed"
	queued := m.theme.StatusQueued.Render("□") + " queued"
	waiting := m.theme.StatusWaiting.Render("◆") + " waiting"

	if m.width < 70 {
		return fmt.Sprintf("  Legend:\n    %s  %s\n    %s  %s\n    %s\n", running, success, failed, queued, waiting)
	}
	return fmt.Sprintf("  Legend: %s  %s  %s  %s  %s\n", running, success, failed, queued, waiting)
}

// renderHelpView renders the full keyboard shortcuts help list and status legend.
func (m Model) renderHelpView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	sb.WriteString("  " + m.theme.LogoText.Render("Keyboard Shortcuts & Help") + "\n\n")

	// Global / Navigation shortcuts
	sb.WriteString("  " + m.theme.TableHeader.Render("GLOBAL KEYS") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("?") + "                " + m.theme.HelpDesc.Render("Toggle this Help screen") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("o") + "                " + m.theme.HelpDesc.Render("Switch GitHub Account/Org Context") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("q / Ctrl+C") + "       " + m.theme.HelpDesc.Render("Quit application") + "\n\n")

	// Main screen shortcuts
	sb.WriteString("  " + m.theme.TableHeader.Render("WORKFLOW RUNS (Main View)") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("j / k / Up / Down") + " " + m.theme.HelpDesc.Render("Navigate runs list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Enter") + "              " + m.theme.HelpDesc.Render("View Jobs of selected workflow run") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("r") + "                  " + m.theme.HelpDesc.Render("Refresh workflow runs list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("w") + "                  " + m.theme.HelpDesc.Render("Open selected workflow run in browser") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("m") + "                  " + m.theme.HelpDesc.Render("Toggle filtering by your own runs") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("f") + "                  " + m.theme.HelpDesc.Render("Filter by specific actor name") + "\n\n")

	// Jobs screen shortcuts
	sb.WriteString("  " + m.theme.TableHeader.Render("WORKFLOW JOBS") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("j / k / Up / Down") + " " + m.theme.HelpDesc.Render("Navigate jobs list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Enter") + "              " + m.theme.HelpDesc.Render("View Logs of selected job") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("[ / ]") + "              " + m.theme.HelpDesc.Render("Cycle through previous workflow run attempts") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("w") + "                  " + m.theme.HelpDesc.Render("Open workflow run in browser") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("v") + "                  " + m.theme.HelpDesc.Render("Open selected job in browser") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Esc / Backspace") + "    " + m.theme.HelpDesc.Render("Go back to workflow runs list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("r") + "                  " + m.theme.HelpDesc.Render("Refresh jobs list") + "\n\n")

	// Logs screen shortcuts
	sb.WriteString("  " + m.theme.TableHeader.Render("LOGS VIEWER") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("u / d") + "              " + m.theme.HelpDesc.Render("Scroll logs up/down") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Esc / Backspace") + "    " + m.theme.HelpDesc.Render("Go back to jobs list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("r") + "                  " + m.theme.HelpDesc.Render("Refresh logs") + "\n\n")

	// Legend
	sb.WriteString(m.renderLegend())
	sb.WriteString("\n")

	contentHeight := 28
	if m.width < 70 {
		contentHeight = 31
	}
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"Esc:Close Help", "q:Quit"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func renderHyperlink(text, url string) string {
	if url == "" {
		return text
	}
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}
