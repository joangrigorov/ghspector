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

	rl := m.client.GetRateLimit()
	rlStr := "Rate Limit: N/A"
	if rl.Limit > 0 {
		rlStr = fmt.Sprintf("API: %d/%d", rl.Remaining, rl.Limit)
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

	// Right-align rate limit
	width := m.width
	if width < 40 {
		width = 40
	}
	rightSpace := width - lipgloss.Width(title) - lipgloss.Width(contextInfo) - 4
	if rightSpace < 2 {
		rightSpace = 2
	}
	spaces := strings.Repeat(" ", rightSpace)

	return "\n " + title + "  " + contextInfo + spaces + rlStr + "\n"
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
		status = m.theme.Subtitle.Render(" | msg: " + m.statusMsg)
	}

	content := strings.Join(formatted, "  ") + status
	return "\n" + m.theme.BottomBar.Render(content) + "\n"
}

// renderMainView renders the Workflow Runs list.
func (m Model) renderMainView() string {
	var sb strings.Builder

	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	// Table header
	header := fmt.Sprintf("  %-3s %-20s %-30s %-12s %-12s", "ST", "REPOSITORY", "WORKFLOW RUN", "EVENT", "DURATION")
	sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

	if len(m.runs) == 0 {
		sb.WriteString("\n  " + m.theme.HelpDesc.Render("No recent workflow runs found.") + "\n\n")
	} else {
		for i, run := range m.runs {
			statusInd := m.getStatusIndicator(run.Status, run.Conclusion)

			// Calculate Duration / Age
			durStr := ""
			if run.Status == "in_progress" {
				durStr = formatDuration(time.Since(run.CreatedAt))
				durStr = m.theme.StatusRunning.Render(durStr)
			} else if run.Status == "queued" {
				durStr = "queued"
				durStr = m.theme.StatusQueued.Render(durStr)
			} else {
				durStr = formatDuration(run.UpdatedAt.Sub(run.CreatedAt))
			}

			repoName := run.Repository.Name
			if len(repoName) > 18 {
				repoName = repoName[:15] + "..."
			}

			runName := run.Name
			if runName == "" && run.DisplayTitle != "" {
				runName = run.DisplayTitle
			}
			if len(runName) > 28 {
				runName = runName[:25] + "..."
			}

			rowText := fmt.Sprintf("  %-3s %-20s %-30s %-12s %-12s",
				statusInd,
				repoName,
				runName,
				run.Event,
				durStr,
			)

			if i == m.selectedRunIdx {
				sb.WriteString(m.theme.TableSelected.Render(rowText) + "\n")
			} else {
				sb.WriteString(m.theme.TableRow.Render(rowText) + "\n")
			}
		}

		// Draw "Load More" row
		if m.hasMoreRuns {
			loadText := "  [-- Load More Workflow Runs... --]"
			if m.selectedRunIdx == len(m.runs) {
				sb.WriteString(m.theme.TableSelected.Render(loadText) + "\n")
			} else {
				sb.WriteString(m.theme.Subtitle.Render(loadText) + "\n")
			}
		}
	}

	// Dynamic sizing pads
	contentHeight := len(m.runs) + 6
	if m.hasMoreRuns {
		contentHeight++
	}
	padding := m.height - contentHeight - 5
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"j/k:Navigate", "Enter:View Jobs", "o:Switch Context", "r:Refresh", "q:Quit"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

// renderJobsView renders the list of jobs in a workflow run.
func (m Model) renderJobsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	run := m.runs[m.selectedRunIdx]
	sb.WriteString("  " + m.theme.LogoText.Render("Workflow: "+run.Name) + "\n")
	sb.WriteString("  " + m.theme.HelpDesc.Render(fmt.Sprintf("Repo: %s | Branch: %s | SHA: %s", run.Repository.FullName, run.HeadBranch, run.HeadSHA[:7])) + "\n\n")

	header := fmt.Sprintf("  %-3s %-40s %-15s %-12s", "ST", "JOB NAME", "STARTED", "DURATION")
	sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

	if len(m.jobs) == 0 {
		sb.WriteString("\n  " + m.theme.HelpDesc.Render("No jobs found for this workflow run.") + "\n\n")
	} else {
		for i, job := range m.jobs {
			statusInd := m.getStatusIndicator(job.Status, job.Conclusion)

			startedStr := job.StartedAt.Format("15:04:05")
			if job.StartedAt.IsZero() {
				startedStr = "N/A"
			}

			durStr := ""
			if job.Status == "in_progress" {
				durStr = formatDuration(time.Since(job.StartedAt))
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

			rowText := fmt.Sprintf("  %-3s %-40s %-15s %-12s",
				statusInd,
				jobName,
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

	contentHeight := len(m.jobs) + 9
	padding := m.height - contentHeight - 4
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"j/k:Navigate", "Enter:View Logs", "Esc:Back", "r:Refresh", "q:Quit"}
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

	keys := []string{"u/d:Scroll Up/Down", "Esc:Back to Jobs", "r:Refresh logs", "q:Quit"}
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

	keys := []string{"j/k:Navigate", "Enter:Confirm", "Esc/o:Close Switcher", "q:Quit"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}
