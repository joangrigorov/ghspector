package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"ghspector/internal/gh"
)

// View renders the TUI screen.
func (m Model) View() string {
	if m.err != nil {
		return m.renderErrorView()
	}

	switch m.state {
	case viewSplash:
		return RenderSplash(m.theme, m.loadingMsg, m.tickCount)
	case viewMain, viewPRFilterInput, viewPRFilterTypeSelect, viewIssueFilterInput, viewIssueFilterTypeSelect, viewFilterTypeSelect, viewRepoFilterSelect:
		switch m.activeTab {
		case tabWorkflows:
			viewStr := m.renderMainView()
			if m.state == viewFilterTypeSelect {
				modalStr := m.renderFilterTypeSelectModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			} else if m.state == viewRepoFilterSelect {
				modalStr := m.renderRepoFilterSelectModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			}
			return viewStr
		case tabPRs:
			viewStr := m.renderPullsView()
			if m.state == viewPRFilterInput {
				modalStr := m.renderPRFilterInputModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			} else if m.state == viewPRFilterTypeSelect {
				modalStr := m.renderPRFilterTypeSelectModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			} else if m.state == viewFilterTypeSelect {
				modalStr := m.renderFilterTypeSelectModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			} else if m.state == viewRepoFilterSelect {
				modalStr := m.renderRepoFilterSelectModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			}
			return viewStr
		case tabIssues:
			viewStr := m.renderIssuesView()
			if m.state == viewIssueFilterInput {
				modalStr := m.renderIssueFilterInputModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			} else if m.state == viewIssueFilterTypeSelect {
				modalStr := m.renderIssueFilterTypeSelectModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			} else if m.state == viewFilterTypeSelect {
				modalStr := m.renderFilterTypeSelectModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			} else if m.state == viewRepoFilterSelect {
				modalStr := m.renderRepoFilterSelectModal()
				viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
			}
			return viewStr
		default:
			return "Unknown tab"
		}
	case viewJobs:
		return m.renderJobsView()
	case viewLogs:
		return m.renderLogsView()
	case viewSwitcher:
		return m.renderSwitcherView()
	case viewHelp:
		return m.renderHelpView()
	case viewPRDetails:
		return m.renderPRDetailsView()
	case viewPRComments:
		return m.renderPRCommentsView()
	case viewPRCommits:
		return m.renderPRCommitsView()
	case viewPRDiff:
		return m.renderPRDiffView()
	case viewIssueDetails:
		return m.renderIssueDetailsView()
	case viewIssueComments:
		return m.renderIssueCommentsView()
	case viewCommitDetails:
		return m.renderCommitDetailsView()
	default:
		return "Unknown application state"
	}
}

// renderErrorView renders a full screen error block.
func (m Model) renderErrorView() string {
	var sb strings.Builder
	sb.WriteString("\n  " + m.theme.StatusFailed.Render("FATAL ERROR") + "\n\n")

	// Wrap error text to terminal width - 4
	w := m.width - 4
	if w < 20 {
		w = 20
	}
	errText := lipgloss.NewStyle().Width(w).Render(m.err.Error())
	sb.WriteString("  " + errText + "\n\n")

	// Check if this is a rate limit error
	if strings.Contains(strings.ToLower(m.err.Error()), "rate limit") {
		rl := m.client.GetRateLimit()
		if !rl.Reset.IsZero() {
			timeRemaining := time.Until(rl.Reset)
			if timeRemaining > 0 {
				timerStr := fmt.Sprintf("Rate Limit resets in: %s (at %s)", 
					formatDuration(timeRemaining), 
					rl.Reset.Format("15:04:05"),
				)
				sb.WriteString("  " + m.theme.StatusWaiting.Render(timerStr) + "\n\n")
				sb.WriteString("  The app will automatically reconnect once the rate limit resets.\n\n")
			} else {
				sb.WriteString("  " + m.theme.StatusSuccessful.Render("Rate limit reset. Reconnecting...") + "\n\n")
			}
		}
	}

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
	if len(m.targets) > 0 && m.selectedTargetIdx >= 0 && m.selectedTargetIdx < len(m.targets) {
		activeTarget = m.targets[m.selectedTargetIdx].Name
	}
	if activeTarget == "None" || activeTarget == "" {
		if m.selectedPull != nil && m.selectedPull.Repository.FullName != "" {
			activeTarget = m.selectedPull.Repository.FullName
		} else if run := m.getRun(); run.Repository.FullName != "" {
			activeTarget = run.Repository.FullName
		} else if m.selectedIssue != nil && m.selectedIssue.Repository.FullName != "" {
			activeTarget = m.selectedIssue.Repository.FullName
		}
	}
	if m.filterActor != "" {
		activeTarget += fmt.Sprintf(" (Filter: @%s)", m.filterActor)
	}

	rl := m.client.GetRateLimit()
	rlStr := ""
	if rl.Limit > 0 {
		rlStr = fmt.Sprintf("Rate Limit: %d/%d reqs", rl.Remaining, rl.Limit)
	}

	// Dynamic Page Name in Title
	pageName := "Dashboard"
	switch m.state {
	case viewMain, viewPRFilterInput, viewPRFilterTypeSelect, viewIssueFilterInput, viewIssueFilterTypeSelect, viewFilterTypeSelect, viewRepoFilterSelect:
		switch m.activeTab {
		case tabWorkflows:
			pageName = "Workflows"
		case tabPRs:
			pageName = "Pull Requests"
		case tabIssues:
			pageName = "Issues"
		}
	case viewJobs:
		pageName = "Workflow Jobs"
	case viewLogs:
		pageName = "Job Logs"
	case viewPRDetails:
		pageName = "PR Details"
	case viewPRComments:
		pageName = "PR Comments"
	case viewPRCommits:
		pageName = "PR Commits"
	case viewPRDiff:
		pageName = "PR Diff"
	case viewIssueDetails:
		pageName = "Issue Details"
	case viewIssueComments:
		pageName = "Issue Comments"
	case viewCommitDetails:
		pageName = "Commit Details"
	case viewSwitcher:
		pageName = "Account / Repository Switcher"
	case viewHelp:
		pageName = "Help & Keybindings"
	}

	headerBg := m.theme.HeaderBg
	titleStyle := m.theme.HeaderTitle
	contextStyle := m.theme.HeaderSubtitle
	bgStyle := m.theme.Header

	loadingInd := ""
	if m.isLoading {
		spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinnerChar := spinners[m.tickCount%len(spinners)]
		spinnerStyle := m.theme.StatusWaiting.Background(headerBg)
		loadingInd = bgStyle.Render(" ") + spinnerStyle.Render(spinnerChar)
	}

	title := titleStyle.Render("  ghspector | "+pageName) + loadingInd
	contextInfo := contextStyle.Render("Account/Org: " + activeTarget)

	var rlRendered string
	if rlStr != "" {
		var rlStyle lipgloss.Style
		if rl.Remaining < 200 {
			rlStyle = m.theme.StatusFailed.Background(headerBg)
		} else if rl.Remaining < 1000 {
			rlStyle = m.theme.StatusQueued.Background(headerBg)
		} else {
			rlStyle = m.theme.StatusSuccessful.Background(headerBg)
		}
		rlRendered = rlStyle.Render(rlStr) + bgStyle.Render("  ")
	}

	width := m.width
	if width < 40 {
		width = 40
	}

	leftStr := title + bgStyle.Render("   ") + contextInfo
	leftLen := lipgloss.Width(leftStr)

	leftStyle := lipgloss.NewStyle().Background(headerBg).Width(leftLen)
	rightStyle := lipgloss.NewStyle().Background(headerBg).Width(width - leftLen).Align(lipgloss.Right)

	headerLine := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftStyle.Render(leftStr),
		rightStyle.Render(rlRendered),
	)

	blankLine := bgStyle.Render(strings.Repeat(" ", width))
	hr := m.theme.Border.Render(strings.Repeat("─", width))

	return blankLine + "\n" + headerLine + "\n" + blankLine + "\n" + hr
}

// renderFooter renders the standard bottom bar.
func (m Model) renderFooter(keys []string) string {
	isMainView := m.state == viewMain || m.state == viewSplash
	backLabel := "Back"
	if isMainView {
		backLabel = "Exit"
	}
	if m.state == viewHelp {
		backLabel = "Close"
	}

	helpLabel := "Help"
	if m.state == viewHelp {
		helpLabel = "Close Help"
	}

	// Pinned Left Side Base: ?:Help  Esc:Exit (or Esc:Back)  o:Account  q:Quit
	leftStr := m.theme.HelpKey.Render("?") + m.theme.HelpDesc.Render(":"+helpLabel) +
		"  " + m.theme.HelpKey.Render("Esc") + m.theme.HelpDesc.Render(":"+backLabel) +
		"  " + m.theme.HelpKey.Render("o") + m.theme.HelpDesc.Render(":Account") +
		"  " + m.theme.HelpKey.Render("q") + m.theme.HelpDesc.Render(":Quit")

	// Filter out pinned keys (?, Esc, o, q) from rightKeys
	var rightKeys []string
	for _, k := range keys {
		parts := strings.SplitN(k, ":", 2)
		if len(parts) > 0 {
			keyStr := parts[0]
			if keyStr == "?" || keyStr == "Esc" || keyStr == "o" || keyStr == "q" {
				continue
			}
		}
		rightKeys = append(rightKeys, k)
	}

	width := m.width
	if width < 40 {
		width = 40
	}

	// BottomBar has Padding(0, 1), so inner printable width is width - 2
	innerWidth := width - 2
	leftLen := lipgloss.Width(leftStr)
	maxRightWidth := innerWidth - leftLen - 1
	if maxRightWidth < 0 {
		maxRightWidth = 0
	}

	// Format right keys while staying strictly within maxRightWidth
	var rightRendered []string
	currentRightWidth := 0

	for _, k := range rightKeys {
		parts := strings.SplitN(k, ":", 2)
		var item string
		if len(parts) == 2 {
			desc := parts[1]
			if desc == "Clear Filter" || desc == "Clear filter" {
				desc = "Clear"
			} else if desc == "Back to PRs" || desc == "Back to Details" || desc == "Back to PR" || desc == "Back to Issue" {
				desc = "Back"
			} else if desc == "Toggle Focus" {
				desc = "Focus"
			} else if desc == "Close PR" {
				desc = "Close"
			}
			item = m.theme.HelpKey.Render(parts[0]) + m.theme.HelpDesc.Render(":"+desc)
		} else {
			item = m.theme.HelpDesc.Render(k)
		}

		itemLen := lipgloss.Width(item)
		neededWidth := itemLen
		if len(rightRendered) > 0 {
			neededWidth += 2 // accounting for "  " separator
		}

		if currentRightWidth+neededWidth <= maxRightWidth {
			rightRendered = append(rightRendered, item)
			currentRightWidth += neededWidth
		} else {
			break
		}
	}

	rightStr := strings.Join(rightRendered, "  ")

	// Render Left (fixed) and Right (scalable right-aligned)
	leftStyle := lipgloss.NewStyle().Width(leftLen)
	rightStyle := lipgloss.NewStyle().Width(innerWidth - leftLen).Align(lipgloss.Right)

	bottomBarContent := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftStyle.Render(leftStr),
		rightStyle.Render(rightStr),
	)

	// Format status banner above bottom bar if active
	var statusBanner string
	if m.statusMsg != "" {
		lowerMsg := strings.ToLower(m.statusMsg)
		isError := strings.HasPrefix(lowerMsg, "error") || strings.Contains(lowerMsg, "failed") || strings.Contains(lowerMsg, "missing scopes") || strings.Contains(lowerMsg, "not yet available") || strings.Contains(lowerMsg, "invalid") || strings.Contains(lowerMsg, "cannot")

		cleanMsg := m.statusMsg
		if isError {
			cleanMsg = strings.TrimPrefix(cleanMsg, "Error: ")
			cleanMsg = strings.TrimPrefix(cleanMsg, "error: ")
			statusBanner = "  " + m.theme.StatusFailed.Render("✖ "+cleanMsg) + "\n"
		} else {
			statusBanner = "  " + m.theme.StatusWaiting.Render("ℹ "+cleanMsg) + "\n"
		}
	}

	return statusBanner + m.theme.BottomBar.Width(width).Render(bottomBarContent)
}

// renderMainView renders the Workflow Runs list with a scrolling window.
func (m Model) renderMainView() string {
	var sb strings.Builder

	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	// Display active filters
	var filterTexts []string
	if m.filterActor != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Actor: @%s", m.filterActor))
	}
	if m.filterRepo != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Repo: %s", m.filterRepo))
	}
	if len(filterTexts) > 0 {
		sb.WriteString("  " + m.theme.StatusWaiting.Render("Filter active: "+strings.Join(filterTexts, ", ")+" (Press 'x' to clear)") + "\n\n")
	}

	// Calculate dynamic workflow run column width
	// Widths of other columns (including margins and spaces):
	// ST: 3, REPOSITORY: 18, EVENT: 22, ACTOR: 24, DURATION: 12
	// Sum of other columns = 85
	runNameWidth := m.width - 85
	if runNameWidth < 10 {
		runNameWidth = 10
	}

	// Table header
	header := fmt.Sprintf("  %-3s %-18s %-*s %-22s %-24s %-12s", "ST", "REPOSITORY", runNameWidth, "WORKFLOW RUN", "EVENT", "ACTOR", "DURATION")
	sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

	renderedCount := 0
	if len(m.runs) == 0 {
		msg := "No recent workflow runs found."
		if m.isLoading {
			msg = "Loading workflow runs..."
		}
		sb.WriteString("\n  " + m.theme.HelpDesc.Render(msg) + "\n\n")
		renderedCount = 3
	} else {
		visibleRows := m.height - 12
		if m.showFilterInput {
			visibleRows -= 2
		}
		if len(filterTexts) > 0 {
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
				if len(runName) > runNameWidth {
					runName = runName[:runNameWidth-3] + "..."
				}

				runEvent := run.Event
				if len(runEvent) > 22 {
					runEvent = runEvent[:19] + "..."
				}

				actorName := "unknown"
				if run.Actor != nil && run.Actor.Login != "" {
					actorName = run.Actor.Login
				}
				if len(actorName) > 24 {
					actorName = actorName[:21] + "..."
				}

				paddedRunName := fmt.Sprintf("%-*s", runNameWidth, runName)

				rowText := fmt.Sprintf("  %-3s %-18s %s %-22s %-24s %-12s",
					statusInd,
					repoName,
					paddedRunName,
					runEvent,
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
	if len(filterTexts) > 0 {
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

	keys := []string{"?:Help", "Esc:Exit", "Tab:Tabs", "j/k:Navigate", "Enter:Jobs", "w:Browser", "f:Filter", "m:My Runs", "x:Clear Filter", "r:Refresh"}
	if m.selectedRunCanApprove() {
		keys = append(keys[:5], append([]string{"a:Approve"}, keys[5:]...)...)
	}
	sb.WriteString(m.renderFooter(keys))

	viewStr := sb.String()
	if m.runApprovalState > 0 {
		modalStr := m.renderApprovalModal()
		viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
	}

	return viewStr
}

// renderJobsView renders the list of jobs in a workflow run with a scrolling window.
func (m Model) renderJobsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	run := m.getRun()
	runName := run.Name
	if runName == "" {
		runName = "Workflow"
	}
	repoFullName := run.Repository.FullName
	if repoFullName == "" {
		repoFullName = "unknown"
	}
	workflowTitleText := renderHyperlink("Workflow: "+runName, run.HTMLURL)
	sb.WriteString("  " + m.theme.LogoText.Render(workflowTitleText) + "\n")

	attemptText := ""
	if run.RunAttempt > 1 {
		attemptText = fmt.Sprintf(" | Attempt %d of %d (use [ / ] to switch)", m.selectedAttempt, run.RunAttempt)
	}
	shaText := run.HeadSHA
	if len(shaText) > 7 {
		shaText = shaText[:7]
	} else if shaText == "" {
		shaText = "unknown"
	}
	divWidth := m.width - 4
	if divWidth < 10 {
		divWidth = 10
	}
	sb.WriteString("  " + m.theme.HelpDesc.Render(fmt.Sprintf("Repo: %s | Branch: %s | SHA: %s%s", repoFullName, run.HeadBranch, shaText, attemptText)) + "\n")
	sb.WriteString("  " + m.theme.Border.Render(strings.Repeat("─", divWidth)) + "\n\n")

	needsApproval := (run.Status == "waiting" || run.Conclusion == "action_required")
	renderedCount := 0

	if needsApproval {
		var banner strings.Builder
		bannerStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.theme.StatusWaiting.GetForeground()).
			Padding(1, 4).
			MarginLeft(4).
			Width(60)

		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(m.theme.StatusWaiting.GetForeground())

		isForkPR := (run.HeadRepository.FullName != "" && run.HeadRepository.FullName != run.Repository.FullName)

		banner.WriteString(titleStyle.Render(" Awaiting Approval Required ") + "\n\n")
		if run.Conclusion == "action_required" {
			if isForkPR {
				banner.WriteString("This workflow run was triggered by a pull request from a fork\n")
				banner.WriteString("and requires a maintainer's approval to execute.\n")
			} else {
				banner.WriteString("This workflow run was triggered by a local pull request\n")
				banner.WriteString("and requires approval to execute (due to bot trigger or policy).\n")
			}
		} else {
			banner.WriteString("This workflow run has completed initial checks but is waiting\n")
			banner.WriteString("for manual approval to deploy to a protected environment.\n")
		}
		
		banner.WriteString("\n")
		if m.selectedRunCanApprove() {
			banner.WriteString("Press " + m.theme.StatusSuccessful.Render("[a]") + " to approve and trigger this run now.\n")
		} else {
			if run.Conclusion == "action_required" && !isForkPR {
				banner.WriteString(m.theme.StatusFailed.Render("Cannot approve via API. Please approve via the GitHub UI.\n"))
			} else {
				banner.WriteString(m.theme.StatusFailed.Render("Your current access token does not have permissions to approve.\n"))
			}
		}
		
		sb.WriteString("\n" + bannerStyle.Render(banner.String()) + "\n")
		renderedCount = 9
	} else {
		jobNameWidth := m.width - 44
		if jobNameWidth < 10 {
			jobNameWidth = 10
		}

		header := fmt.Sprintf("  %-3s %-*s %-8s %-15s %-12s", "ST", jobNameWidth, "JOB NAME", "STEPS", "STARTED", "DURATION")
		sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

		if len(m.jobs) == 0 {
			msg := "No jobs found for this workflow run."
			if m.isLoading {
				msg = "Loading jobs..."
			}
			sb.WriteString("\n  " + m.theme.HelpDesc.Render(msg) + "\n\n")
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
				if len(jobName) > jobNameWidth {
					jobName = jobName[:jobNameWidth-3] + "..."
				}

				paddedJobName := fmt.Sprintf("%-*s", jobNameWidth, jobName)
				hyperlinkedJobName := renderHyperlink(paddedJobName, job.HTMLURL)

				// Calculate steps progress
				completedSteps := 0
				for _, step := range job.Steps {
					if step.Status == "completed" {
						completedSteps++
					}
				}
				stepsStr := fmt.Sprintf("%d/%d", completedSteps, len(job.Steps))

				rowText := fmt.Sprintf("  %-3s %s %-8s %-15s %-12s",
					statusInd,
					hyperlinkedJobName,
					stepsStr,
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
	}

	contentHeight := renderedCount + 14
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"j/k:Navigate", "Enter:Logs", "w:Job Browser", "v:Run Browser", "[/]:Attempts", "r:Refresh"}
	if m.selectedRunCanApprove() {
		keys = append(keys[:5], append([]string{"a:Approve"}, keys[5:]...)...)
	}
	sb.WriteString(m.renderFooter(keys))

	viewStr := sb.String()
	if m.runApprovalState > 0 {
		modalStr := m.renderApprovalModal()
		viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
	}

	return viewStr
}

// renderLogsView renders the logs viewer and steps list.
func (m Model) renderLogsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	job := m.jobs[m.selectedJobIdx]
	sb.WriteString("  " + m.theme.LogoText.Render("Job: "+job.Name) + "\n\n")

	leftWidth := 32
	h := m.logsViewport.Height

	viewContent := m.logsViewport.View()
	if m.logsLoading {
		viewContent = m.theme.HelpDesc.Render("Loading logs...")
	}

	// Render two columns side-by-side: Steps on the left, logs viewport on the right
	sideBySide := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderStepsSidebar(leftWidth, h),
		"   ", // separator gap
		m.theme.Border.Render(viewContent),
	)
	sb.WriteString(sideBySide + "\n")

	// Dynamic padding
	contentHeight := h + 10
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"j/k:Steps", "u/d:Scroll Logs", "w:Browser", "r:Refresh"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderStepsSidebar(width, height int) string {
	var lines []string
	headerStyle := m.theme.TableHeader
	lines = append(lines, headerStyle.Render(fmt.Sprintf(" %-*s", width-1, "STEPS")))

	job := m.jobs[m.selectedJobIdx]
	for i, step := range job.Steps {
		indicator := m.getStatusIndicator(step.Status, step.Conclusion)

		nameLimit := width - 8
		if nameLimit < 5 {
			nameLimit = 5
		}
		stepName := step.Name
		if len(stepName) > nameLimit {
			stepName = stepName[:nameLimit-3] + "..."
		}

		var rowText string
		if i == m.selectedStepIdx {
			rowText = fmt.Sprintf(" > %s %-*s", indicator, nameLimit, stepName)
			lines = append(lines, m.theme.TableSelected.Render(rowText))
		} else {
			rowText = fmt.Sprintf("   %s %-*s", indicator, nameLimit, stepName)
			lines = append(lines, m.theme.TableRow.Render(rowText))
		}
	}

	// Pad remaining height with empty lines so sidebar height matches viewport height
	for len(lines) < height+1 {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
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
	runRunning := m.theme.StatusRunning.Render("■") + " running"
	runSuccess := m.theme.StatusSuccessful.Render("■") + " success"
	runFailed := m.theme.StatusFailed.Render("■") + " failed"
	runQueued := m.theme.StatusQueued.Render("□") + " queued"
	runWaiting := m.theme.StatusWaiting.Render("◆") + " waiting"

	prOpen := m.theme.StatusSuccessful.Render("■") + " open"
	prDraft := m.theme.StatusNeutral.Render("■") + " draft"
	prMerged := m.theme.StatusWaiting.Render("■") + " merged"
	prClosed := m.theme.StatusFailed.Render("■") + " closed"

	var sb strings.Builder
	sb.WriteString("  " + m.theme.TableHeader.Render("LEGENDS") + "\n")
	if m.width < 70 {
		sb.WriteString("    Workflow Runs Status:\n")
		fmt.Fprintf(&sb, "      %s  %s  %s\n      %s  %s\n", runRunning, runSuccess, runFailed, runQueued, runWaiting)
		sb.WriteString("    Pull Requests Status:\n")
		fmt.Fprintf(&sb, "      %s  %s  %s  %s\n", prOpen, prDraft, prMerged, prClosed)
	} else {
		fmt.Fprintf(&sb, "    Workflow Runs: %s  %s  %s  %s  %s\n", runRunning, runSuccess, runFailed, runQueued, runWaiting)
		fmt.Fprintf(&sb, "    Pull Requests: %s  %s  %s  %s\n", prOpen, prDraft, prMerged, prClosed)
	}
	return sb.String()
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

	// Main screen shortcuts - Workflow runs
	sb.WriteString("  " + m.theme.TableHeader.Render("WORKFLOW RUNS (Main View)") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("j / k / Up / Down") + " " + m.theme.HelpDesc.Render("Navigate runs list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Enter") + "              " + m.theme.HelpDesc.Render("View Jobs of selected workflow run") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("a") + "                  " + m.theme.HelpDesc.Render("Approve waiting run / deployment (requires 'repo' & 'workflow' scopes)") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("r") + "                  " + m.theme.HelpDesc.Render("Refresh workflow runs list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("w") + "                  " + m.theme.HelpDesc.Render("Open selected workflow run in browser") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("m") + "                  " + m.theme.HelpDesc.Render("Toggle filtering by your own runs") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("f") + "                  " + m.theme.HelpDesc.Render("Filter by specific actor name") + "\n\n")

	// Main screen shortcuts - Pull Requests
	sb.WriteString("  " + m.theme.TableHeader.Render("PULL REQUESTS (Main View)") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("j / k / Up / Down") + " " + m.theme.HelpDesc.Render("Navigate pull requests list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Enter") + "              " + m.theme.HelpDesc.Render("View Details of selected PR") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("r") + "                  " + m.theme.HelpDesc.Render("Refresh pull requests list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("s") + "                  " + m.theme.HelpDesc.Render("Cycle state filter (Open / Closed / All)") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("a / i / v") + "          " + m.theme.HelpDesc.Render("Quick filter by authored / assigned / reviewed by me") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("f") + "                  " + m.theme.HelpDesc.Render("Flexible search filter (Author, Assignee, Reviewer)") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("x") + "                  " + m.theme.HelpDesc.Render("Clear active pull request filters") + "\n\n")

	// Pull Request Details
	sb.WriteString("  " + m.theme.TableHeader.Render("PULL REQUEST DETAILS") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Esc / Backspace") + "    " + m.theme.HelpDesc.Render("Go back to PR list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Tab") + "                " + m.theme.HelpDesc.Render("Toggle focus between description viewport and checks list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("j / k / Up / Down") + " " + m.theme.HelpDesc.Render("Scroll description or navigate checks list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Enter") + "              " + m.theme.HelpDesc.Render("Trigger check workflow jobs in-app or open browser link") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("w") + "                  " + m.theme.HelpDesc.Render("Open PR / check run in browser") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("m") + "                  " + m.theme.HelpDesc.Render("Merge PR (opens merge method selection popup)") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("c") + "                  " + m.theme.HelpDesc.Render("View PR comments list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("C") + "                  " + m.theme.HelpDesc.Render("Close PR (opens confirmation popup)") + "\n\n")

	// Jobs screen shortcuts
	sb.WriteString("  " + m.theme.TableHeader.Render("WORKFLOW JOBS") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("j / k / Up / Down") + " " + m.theme.HelpDesc.Render("Navigate jobs list") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("Enter") + "              " + m.theme.HelpDesc.Render("View Logs of selected job") + "\n")
	sb.WriteString("    " + m.theme.HelpKey.Render("a") + "                  " + m.theme.HelpDesc.Render("Approve waiting run / deployment (requires 'repo' & 'workflow' scopes)") + "\n")
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

	contentHeight := 45
	if m.width < 70 {
		contentHeight = 55
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


func (m Model) renderPullsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	// Display active filters
	var filterTexts []string
	if m.filterPRState != "" && m.filterPRState != "open" {
		filterTexts = append(filterTexts, fmt.Sprintf("State: %s", strings.ToUpper(m.filterPRState)))
	}
	if m.filterPRAuthor != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Author: @%s", m.filterPRAuthor))
	}
	if m.filterPRAssignee != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Assignee: @%s", m.filterPRAssignee))
	}
	if m.filterPRReviewer != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Reviewer: @%s", m.filterPRReviewer))
	}
	if m.filterRepo != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Repo: %s", m.filterRepo))
	}


	prTitleWidth := m.width - 102
	if prTitleWidth < 15 {
		prTitleWidth = 15
	}

	header := fmt.Sprintf("  %-3s %-6s %-*s %-24s %-20s %-12s %-12s %-16s", "ST", "PR #", prTitleWidth, "PULL REQUEST TITLE", "AUTHOR", "REPOSITORY", "ASSIGNEES", "REVIEWERS", "LABELS")
	sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

	renderedCount := 0
	hideList := m.state == viewPRFilterInput || m.state == viewPRFilterTypeSelect || m.state == viewFilterTypeSelect || m.state == viewRepoFilterSelect
	
	if hideList {
		visibleRows := m.height - 12
		if len(filterTexts) > 0 {
			visibleRows -= 2
		}
		if visibleRows < 5 {
			visibleRows = 5
		}
		for i := 0; i < visibleRows; i++ {
			sb.WriteString("\n")
		}
		renderedCount = visibleRows
	} else if len(m.pulls) == 0 {
		sb.WriteString("\n  " + m.theme.HelpDesc.Render("No open pull requests found.") + "\n\n")
		renderedCount = 3
	} else {
		visibleRows := m.height - 12
		if len(filterTexts) > 0 {
			visibleRows -= 2
		}
		if visibleRows < 5 {
			visibleRows = 5
		}

		endIdx := m.pullStartIndex + visibleRows
		totalRows := len(m.pulls)
		if m.hasMorePulls {
			totalRows++
		}
		if endIdx > totalRows {
			endIdx = totalRows
		}

		renderedCount = endIdx - m.pullStartIndex

		for i := m.pullStartIndex; i < endIdx; i++ {
			if i < len(m.pulls) {
				pr := m.pulls[i]
				
				statusInd := m.theme.StatusSuccessful.Render("■")
				if pr.Draft {
					statusInd = m.theme.StatusNeutral.Render("■")
				} else if pr.State == "closed" {
					if pr.MergedAt != nil {
						statusInd = m.theme.StatusWaiting.Render("■")
					} else {
						statusInd = m.theme.StatusFailed.Render("■")
					}
				}

				repoName := pr.Repository.Name
				if len(repoName) > 20 {
					repoName = repoName[:17] + "..."
				}

				prNumStr := fmt.Sprintf("#%d", pr.Number)
				
				prTitle := pr.Title
				if pr.Draft {
					prTitle = "[Draft] " + pr.Title
				}
				if len(prTitle) > prTitleWidth {
					prTitle = prTitle[:prTitleWidth-3] + "..."
				}

				authorName := "unknown"
				if pr.User != nil && pr.User.Login != "" {
					authorName = pr.User.Login
				}
				if len(authorName) > 24 {
					authorName = authorName[:21] + "..."
				}

				assigneesList := "None"
				if len(pr.Assignees) > 0 {
					var names []string
					for _, user := range pr.Assignees {
						names = append(names, user.Login)
					}
					assigneesList = strings.Join(names, ", ")
				}
				if len(assigneesList) > 12 {
					assigneesList = assigneesList[:9] + "..."
				}

				reviewersList := "None"
				if len(pr.RequestedReviewers) > 0 {
					var names []string
					for _, user := range pr.RequestedReviewers {
						names = append(names, user.Login)
					}
					reviewersList = strings.Join(names, ", ")
				}
				if len(reviewersList) > 12 {
					reviewersList = reviewersList[:9] + "..."
				}

				labelsList := "None"
				if len(pr.Labels) > 0 {
					var names []string
					for _, label := range pr.Labels {
						names = append(names, label.Name)
					}
					labelsList = strings.Join(names, ", ")
				}
				if len(labelsList) > 16 {
					labelsList = labelsList[:13] + "..."
				}

				paddedPRTitle := fmt.Sprintf("%-*s", prTitleWidth, prTitle)

				rowText := fmt.Sprintf("  %-3s %-6s %s %-24s %-20s %-12s %-12s %-16s",
					statusInd,
					prNumStr,
					paddedPRTitle,
					authorName,
					repoName,
					assigneesList,
					reviewersList,
					labelsList,
				)

				if i == m.selectedPullIdx {
					sb.WriteString(m.theme.TableSelected.Render(rowText) + "\n")
				} else {
					sb.WriteString(m.theme.TableRow.Render(rowText) + "\n")
				}
			} else if i == len(m.pulls) && m.hasMorePulls {
				loadText := "  [-- Load More Pull Requests... --]"
				if m.selectedPullIdx == len(m.pulls) {
					sb.WriteString(m.theme.TableSelected.Render(loadText) + "\n")
				} else {
					sb.WriteString(m.theme.Subtitle.Render(loadText) + "\n")
				}
			}
		}
	}

	contentHeight := renderedCount + 10
	if len(filterTexts) > 0 {
		contentHeight += 2
	}
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"?:Help", "Esc:Exit", "Tab:Tabs", "j/k:Navigate", "Enter:View PR", "w:Browser", "f:Filter", "s:State", "a:My PRs", "i:My Assigned", "v:My Reviewed", "x:Clear Filter"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderPRDetailsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	pr := m.selectedPull
	if pr == nil {
		sb.WriteString("  " + m.theme.HelpDesc.Render("Loading PR details...") + "\n")
		sb.WriteString(m.renderFooter([]string{"?:Help", "Esc:Back"}))
		return sb.String()
	}

	prStateStr := "OPEN"
	prStateStyle := m.theme.StatusSuccessful
	if pr.Draft {
		prStateStr = "DRAFT"
		prStateStyle = m.theme.StatusNeutral
	} else if pr.State == "closed" {
		if pr.MergedAt != nil {
			prStateStr = "MERGED"
			prStateStyle = m.theme.StatusWaiting
		} else {
			prStateStr = "CLOSED"
			prStateStyle = m.theme.StatusFailed
		}
	}

	authorLogin := "unknown"
	if pr.User != nil {
		authorLogin = pr.User.Login
	}

	divWidth := m.width - 4
	if divWidth < 10 {
		divWidth = 10
	}
	sb.WriteString("  " + prStateStyle.Render(fmt.Sprintf("[%s]", prStateStr)) + " " + m.theme.LogoText.Render(fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title)) + "\n")
	sb.WriteString("  " + m.theme.HelpDesc.Render(fmt.Sprintf("Repo: %s | Author: @%s | Source: %s → Base: %s", pr.Repository.FullName, authorLogin, pr.Head.Ref, pr.Base.Ref)) + "\n")
	sb.WriteString("  " + m.theme.Border.Render(strings.Repeat("─", divWidth)) + "\n\n")

	// Calculate widths dynamically
	sidebarWidth := m.width / 5
	if sidebarWidth < 40 {
		sidebarWidth = 40
	}
	h := m.prDescViewport.Height

	// Render two columns side-by-side: Description on the left, sidebar on the right
	middleView := m.prDescViewport.View()
	rightView := m.renderPRRightSidebar(sidebarWidth, h)

	sideBySide := lipgloss.JoinHorizontal(
		lipgloss.Top,
		middleView,
		"   ", // separator gap
		rightView,
	)

	sb.WriteString(sideBySide + "\n")

	// Dynamic padding
	contentHeight := h + 10
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"?:Help", "Esc:Back", "Tab:Focus", "j/k:Navigate", "Enter:Run/Browser", "Shift+D:Diff", "r:Refresh", "c:Comments", "v:Commits"}
	if m.viewerCanMerge() {
		keys = []string{"?:Help", "Esc:Back", "Tab:Focus", "Shift+D:Diff", "r:Refresh", "m:Merge", "c:Comments", "v:Commits", "Shift+C:Close"}
	}
	sb.WriteString(m.renderFooter(keys))

	viewStr := sb.String()

	if m.mergeState > 0 {
		modalStr := m.renderMergeModal()
		viewStr = overlayModal(viewStr, modalStr, m.width, m.height, 48)
	}

	return viewStr
}

func (m Model) renderPRCommentsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	titleText := " Pull Request Comments "
	if m.selectedPull != nil {
		titleText = fmt.Sprintf(" Pull Request #%d Comments ", m.selectedPull.Number)
	}
	sb.WriteString("  " + m.theme.LogoText.Render(titleText) + "\n\n")

	// Render the viewport with borders
	vpContent := m.commentsViewport.View()
	lines := strings.Split(vpContent, "\n")
	boxWidth := m.commentsViewport.Width + 4

	sb.WriteString("  " + m.theme.Border.Render("┌" + strings.Repeat("─", boxWidth-2) + "┐") + "\n")
	for _, line := range lines {
		lineLen := lipgloss.Width(line)
		pad := m.commentsViewport.Width - lineLen
		if pad < 0 {
			pad = 0
		}
		sb.WriteString("  " + m.theme.Border.Render("│ ") + line + strings.Repeat(" ", pad) + m.theme.Border.Render(" │") + "\n")
	}
	sb.WriteString("  " + m.theme.Border.Render("└" + strings.Repeat("─", boxWidth-2) + "┘") + "\n")

	// Dynamic padding
	contentHeight := m.commentsViewport.Height + 8
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"Esc:Back to PR Details", "r:Refresh", "j/k/Up/Down:Scroll", "q:Quit"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderPRCommitsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	titleText := " Pull Request Commits "
	if m.selectedPull != nil {
		titleText = fmt.Sprintf(" PR #%d Commits ", m.selectedPull.Number)
	}
	sb.WriteString("  " + m.theme.LogoText.Render(titleText) + "\n\n")

	h := m.height - 12
	if h < 5 {
		h = 5
	}
	
	sidebarWidth := m.width / 5
	if sidebarWidth < 40 {
		sidebarWidth = 40
	}
	commitsWidth := m.width - sidebarWidth - 4
	if commitsWidth < 20 {
		commitsWidth = 20
	}

	commitsView := m.renderCommitsListTable(commitsWidth, h)

	var checksView string
	if len(m.prCommits) > 0 && m.selectedCommitIdx < len(m.prCommits) {
		selectedCommit := m.prCommits[m.selectedCommitIdx]
		checksView = m.renderCommitChecksSidebar(selectedCommit.SHA, sidebarWidth, h)
	} else {
		checksView = m.renderCommitChecksSidebar("", sidebarWidth, h)
	}

	sideBySide := lipgloss.JoinHorizontal(
		lipgloss.Top,
		commitsView,
		"   ", // separator gap
		checksView,
	)

	sb.WriteString(sideBySide + "\n")

	contentHeight := h + 10
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"Esc:Back to Details", "j/k:Navigate", "Enter:View Commit Details", "q:Quit"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderCommitsListTable(width, height int) string {
	var sb strings.Builder

	shaLimit := 7
	checksLimit := 7
	authorLimit := 12
	dateLimit := 10

	msgWidth := width - shaLimit - checksLimit - authorLimit - dateLimit - 8
	if msgWidth < 10 {
		msgWidth = 10
	}

	header := fmt.Sprintf("  %-7s %-*s %-7s %-12s %-10s", "SHA", msgWidth, "COMMIT MESSAGE", "CHECKS", "AUTHOR", "DATE")
	sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

	renderedCount := 1
	if len(m.prCommits) == 0 {
		sb.WriteString("\n  No commits in this Pull Request.\n")
		renderedCount += 2
	} else {
		for i := 0; i < len(m.prCommits); i++ {
			if i >= height-1 {
				break
			}
			c := m.prCommits[i]
			shaStr := c.SHA
			if len(shaStr) > 7 {
				shaStr = shaStr[:7]
			}
			
			msgStr := c.Commit.Message
			if idx := strings.Index(msgStr, "\n"); idx != -1 {
				msgStr = msgStr[:idx]
			}
			if len(msgStr) > msgWidth {
				msgStr = msgStr[:msgWidth-3] + "..."
			}

			authorName := c.Commit.Author.Name
			if len(authorName) > authorLimit {
				authorName = authorName[:authorLimit-3] + "..."
			}

			dateStr := c.Commit.Author.Date.Format("2006-01-02")

			checks := m.prCommitChecks[c.SHA]
			checksStatus := ""
			if len(checks) > 0 {
				total := len(checks)
				successCount := 0
				hasFailure := false
				hasPending := false
				for _, ch := range checks {
					if ch.Conclusion == "success" {
						successCount++
					} else if ch.Conclusion == "failure" || ch.Conclusion == "action_required" || ch.Conclusion == "cancelled" {
						hasFailure = true
					} else if ch.Status == "in_progress" || ch.Status == "queued" {
						hasPending = true
					}
				}
				
				var badge string
				if hasFailure {
					badge = m.theme.StatusFailed.Render("✗")
				} else if hasPending {
					badge = m.theme.StatusWaiting.Render("⠋")
				} else {
					badge = m.theme.StatusSuccessful.Render("✓")
				}
				
				checksStatus = fmt.Sprintf("%s %d/%d", badge, successCount, total)
			} else {
				checksStatus = "-"
			}

			rowText := fmt.Sprintf("  %-7s %-*s %-7s %-12s %-10s",
				shaStr,
				msgWidth,
				msgStr,
				checksStatus,
				authorName,
				dateStr,
			)

			if i == m.selectedCommitIdx {
				sb.WriteString(m.theme.TableSelected.Render(rowText) + "\n")
			} else {
				sb.WriteString(m.theme.TableRow.Render(rowText) + "\n")
			}
			renderedCount++
		}
	}

	for i := renderedCount; i < height; i++ {
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m Model) renderCommitChecksSidebar(sha string, width, height int) string {
	var lines []string

	headerStyle := m.theme.TableHeader
	lines = append(lines, headerStyle.Render(fmt.Sprintf(" %-*s", width-1, "COMMIT CHECKS")))

	if sha == "" {
		lines = append(lines, m.theme.HelpDesc.Render("  No commit selected"))
	} else {
		checks := m.prCommitChecks[sha]
		if len(checks) == 0 {
			lines = append(lines, m.theme.HelpDesc.Render("  No checks for this commit"))
		} else {
			for _, check := range checks {
				statusInd := m.getStatusIndicator(check.Status, check.Conclusion)

				displayName := m.formatCheckName(check)

				nameLimit := width - 6
				if nameLimit < 3 {
					nameLimit = 3
				}
				name := displayName
				if len(name) > nameLimit {
					name = name[:nameLimit-3] + "..."
				}

				line := fmt.Sprintf("  %s %-*s", statusInd, nameLimit, name)
				lines = append(lines, m.theme.TableRow.Render(line))
			}
		}
	}

	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderPRRightSidebar(width, height int) string {
	var lines []string

	headerStyle := m.theme.TableHeader
	lines = append(lines, headerStyle.Render(fmt.Sprintf(" %-*s", width-1, "DETAILS")))

	pr := m.selectedPull
	if pr != nil {
		authorLogin := "unknown"
		if pr.User != nil {
			authorLogin = pr.User.Login
		}

		assigneesList := "None"
		if len(pr.Assignees) > 0 {
			var names []string
			for _, u := range pr.Assignees {
				names = append(names, "@"+u.Login)
			}
			assigneesList = strings.Join(names, ", ")
		}

		reviewersList := "None"
		if len(pr.RequestedReviewers) > 0 {
			var names []string
			for _, u := range pr.RequestedReviewers {
				names = append(names, "@"+u.Login)
			}
			reviewersList = strings.Join(names, ", ")
		}

		milestoneStr := "None"
		if pr.Milestone != nil {
			milestoneStr = pr.Milestone.Title
		}

		labelsList := "None"
		if len(pr.Labels) > 0 {
			var names []string
			for _, label := range pr.Labels {
				names = append(names, label.Name)
			}
			labelsList = strings.Join(names, ", ")
		}

		formatRow := func(label, val string) {
			lbl := m.theme.HelpDesc.Render(fmt.Sprintf("  %-11s ", label+":"))
			maxValLen := width - 15
			if maxValLen < 5 {
				maxValLen = 5
			}
			for len(val) > 0 {
				chunk := val
				if len(chunk) > maxValLen {
					chunk = chunk[:maxValLen]
					val = val[maxValLen:]
					lines = append(lines, lbl+m.theme.TableRow.Render(chunk))
					lbl = strings.Repeat(" ", 14)
				} else {
					lines = append(lines, lbl+m.theme.TableRow.Render(chunk))
					break
				}
			}
		}

		prStateStr := "OPEN"
		if pr.Draft {
			prStateStr = "DRAFT"
		} else if pr.State == "closed" {
			if pr.MergedAt != nil {
				prStateStr = "MERGED"
			} else {
				prStateStr = "CLOSED"
			}
		}

		formatRow("State", prStateStr)
		formatRow("Author", "@"+authorLogin)
		formatRow("Assignees", assigneesList)
		formatRow("Reviewers", reviewersList)
		formatRow("Milestone", milestoneStr)
		formatRow("Labels", labelsList)
	}

	lines = append(lines, "")

	// 2. Checks Section
	checksHeaderStyle := m.theme.TableHeader
	if !m.prDescFocused {
		checksHeaderStyle = m.theme.TableSelected
	}
	lines = append(lines, checksHeaderStyle.Render(fmt.Sprintf(" %-*s", width-1, "CHECKS")))

	if len(m.prChecks) == 0 {
		msg := "  No checks"
		if m.isLoading {
			msg = "  Loading checks..."
		}
		lines = append(lines, m.theme.HelpDesc.Render(msg))
	} else {
		for idx, check := range m.prChecks {
			statusInd := m.getStatusIndicator(check.Status, check.Conclusion)

			displayName := m.formatCheckName(check)

			nameLimit := width - 6
			if nameLimit < 3 {
				nameLimit = 3
			}
			name := displayName
			if len(name) > nameLimit {
				name = name[:nameLimit-3] + "..."
			}

			line := fmt.Sprintf("  %s %-*s", statusInd, nameLimit, name)

			if !m.prDescFocused && idx == m.selectedCheckIdx {
				lines = append(lines, m.theme.TableSelected.Render(line))
			} else {
				lines = append(lines, m.theme.TableRow.Render(line))
			}
		}
	}

	// Pad with empty lines to match height
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	return strings.Join(lines, "\n")
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(str string) string {
	return ansiRegexp.ReplaceAllString(str, "")
}

func overlayModal(background string, modal string, screenWidth, screenHeight int, modalWidth int) string {
	bgLines := strings.Split(background, "\n")
	modalLines := strings.Split(modal, "\n")
	modalHeight := len(modalLines)

	// Pad bgLines to screenHeight if it has fewer lines to ensure perfect centering and prevent scrolling
	for len(bgLines) < screenHeight {
		bgLines = append(bgLines, "")
	}

	// Calculate center position using screenHeight
	top := (screenHeight - modalHeight) / 2
	left := (screenWidth - modalWidth) / 2

	if top < 0 {
		top = 0
	}
	if left < 0 {
		left = 0
	}

	for i := 0; i < modalHeight && top+i < len(bgLines); i++ {
		bgLine := bgLines[top+i]
		modalLine := modalLines[i]

		// Strip ANSI formatting from background line to safely locate exact characters
		bgPlain := stripANSI(bgLine)
		bgRunes := []rune(bgPlain)
		
		if len(bgRunes) < screenWidth {
			padding := make([]rune, screenWidth-len(bgRunes))
			for p := range padding {
				padding[p] = ' '
			}
			bgRunes = append(bgRunes, padding...)
		}

		// Overlay the modal characters onto the plain runes
		
		// To preserve color coding on the modal itself, we keep modalLine as-is but we slice bgRunes.
		// So we construct: bgRunes[0:left] + modalLine + bgRunes[left+modalWidth:]
		leftPart := string(bgRunes[:left])
		rightStart := left + modalWidth
		if rightStart > len(bgRunes) {
			rightStart = len(bgRunes)
		}
		rightPart := string(bgRunes[rightStart:])

		// Replace the line in background (this also naturally grays-out/dims the bg lines touched by modal)
		bgLines[top+i] = leftPart + modalLine + rightPart
	}

	// Clamp to screenHeight if it has more lines (to prevent terminal scrolling)
	if len(bgLines) > screenHeight {
		bgLines = bgLines[:screenHeight]
	}

	return strings.Join(bgLines, "\n")
}

func (m Model) renderMergeModal() string {
	var modalText strings.Builder
	lineStyle := lipgloss.NewStyle().Width(46)
	
	switch m.mergeState {
	case 1: // choose method
		defMethod := "squash"
		if m.config != nil && m.config.DefaultMergeMethod != "" {
			defMethod = strings.ToLower(m.config.DefaultMergeMethod)
		}

		var squashLabel, mergeLabel, rebaseLabel string
		switch defMethod {
		case "merge":
			squashLabel = "Squash Merge"
			mergeLabel = "Regular Merge (Default)"
			rebaseLabel = "Rebase Merge"
		case "rebase":
			squashLabel = "Squash Merge"
			mergeLabel = "Regular Merge"
			rebaseLabel = "Rebase Merge (Default)"
		default:
			squashLabel = "Squash Merge (Default)"
			mergeLabel = "Regular Merge"
			rebaseLabel = "Rebase Merge"
		}

		modalText.WriteString("┌──────────────────────────────────────────────┐\n")
		modalText.WriteString("│                 MERGE METHOD                 │\n")
		modalText.WriteString("├──────────────────────────────────────────────┤\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("  Choose a merge method:") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.LogoText.Render("[S]")+" "+squashLabel) + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.LogoText.Render("[M]")+" "+mergeLabel) + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.LogoText.Render("[R]")+" "+rebaseLabel) + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("  Press "+m.theme.HelpKey.Render("Enter")+" to proceed with Default") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("  Press "+m.theme.HelpKey.Render("d")+" to cycle/change Default") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("  Press "+m.theme.HelpDesc.Render("Esc")+" or "+m.theme.HelpDesc.Render("c")+" to cancel") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("└──────────────────────────────────────────────┘")
	case 2: // confirm merge
		methodStr := "SQUASH"
		if m.mergeMethod == 1 {
			methodStr = "MERGE"
		} else if m.mergeMethod == 2 {
			methodStr = "REBASE"
		}
		
		modalText.WriteString("┌──────────────────────────────────────────────┐\n")
		modalText.WriteString("│                CONFIRM MERGE                 │\n")
		modalText.WriteString("├──────────────────────────────────────────────┤\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render(fmt.Sprintf("  Are you sure you want to merge PR #%d?", m.selectedPull.Number)) + "│\n")
		modalText.WriteString("│" + lineStyle.Render(fmt.Sprintf("  Method: %s", methodStr)) + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusSuccessful.Render("[Y]")+" Yes, merge now") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusFailed.Render("[N]")+" No, cancel") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("└──────────────────────────────────────────────┘")
	case 4: // confirm close
		modalText.WriteString("┌──────────────────────────────────────────────┐\n")
		modalText.WriteString("│                CONFIRM CLOSE                 │\n")
		modalText.WriteString("├──────────────────────────────────────────────┤\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render(fmt.Sprintf("  Are you sure you want to close PR #%d?", m.selectedPull.Number)) + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusSuccessful.Render("[Y]")+" Yes, close PR") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusFailed.Render("[N]")+" No, cancel") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("└──────────────────────────────────────────────┘")
	}

	return modalText.String()
}

func (m Model) renderApprovalModal() string {
	var modalText strings.Builder
	lineStyle := lipgloss.NewStyle().Width(46)
	run := m.getRun()
	isForkPR := (run.HeadRepository.FullName != "" && run.HeadRepository.FullName != run.Repository.FullName)
	isLocalPRApproval := (run.Conclusion == "action_required" && !isForkPR)
	
	modalText.WriteString("┌──────────────────────────────────────────────┐\n")
	modalText.WriteString("│              APPROVE WORKFLOW RUN            │\n")
	modalText.WriteString("├──────────────────────────────────────────────┤\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")

	if isLocalPRApproval {
		modalText.WriteString("│" + lineStyle.Render("  This internal run requires manual approval") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("  but cannot be approved via the GitHub API.") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("  Please approve it manually on GitHub.") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("  Press [w] to open browser to approve.") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusSuccessful.Render("[Y]")+" or "+m.theme.StatusSuccessful.Render("[w]")+" Yes, open browser") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusFailed.Render("[n]")+" No, cancel") + "│\n")
	} else {
		modalText.WriteString("│" + lineStyle.Render("  Are you sure you want to approve this run?") + "│\n")
		runName := run.Name
		if runName == "" && run.DisplayTitle != "" {
			runName = run.DisplayTitle
		}
		if len(runName) > 40 {
			runName = runName[:37] + "..."
		}
		modalText.WriteString("│" + lineStyle.Render(fmt.Sprintf("  Run: %s", runName)) + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusSuccessful.Render("[Y]")+" Yes, approve run") + "│\n")
		modalText.WriteString("│" + lineStyle.Render("    "+m.theme.StatusFailed.Render("[n]")+" No, cancel") + "│\n")
	}

	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("└──────────────────────────────────────────────┘")
	return modalText.String()
}

func (m Model) renderCommitDetailsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	c := m.viewingCommit
	if c == nil {
		return "No commit selected."
	}

	shaStr := c.SHA
	if len(shaStr) > 7 {
		shaStr = shaStr[:7]
	}
	
	msgLines := strings.Split(c.Commit.Message, "\n")
	titleLine := msgLines[0]
	if len(titleLine) > 60 {
		titleLine = titleLine[:57] + "..."
	}

	sb.WriteString("  " + m.theme.LogoText.Render(fmt.Sprintf("Commit %s: %s", shaStr, titleLine)) + "\n")
	sb.WriteString("  " + m.theme.HelpDesc.Render(fmt.Sprintf("Author: %s <%s> | Date: %s", c.Commit.Author.Name, c.Commit.Author.Email, c.Commit.Author.Date.Format("2006-01-02 15:04:05"))) + "\n\n")

	leftWidth := 40
	var fileLines []string

	// Pad header to leftWidth and split by newline to correctly format the bottom border
	headerText := fmt.Sprintf(" %-*s", leftWidth-1, "FILES CHANGED")
	headerRendered := m.theme.TableHeader.Render(headerText)
	fileLines = append(fileLines, strings.Split(headerRendered, "\n")...)

	visibleRowsFiles := m.height - 16
	if visibleRowsFiles < 5 {
		visibleRowsFiles = 5
	}
	endIdxFiles := m.commitFileStartIndex + visibleRowsFiles
	if endIdxFiles > len(m.commitFiles) {
		endIdxFiles = len(m.commitFiles)
	}

	for idx := m.commitFileStartIndex; idx < endIdxFiles; idx++ {
		file := m.commitFiles[idx]
		var statusIndicator string
		switch file.Status {
		case "added":
			statusIndicator = m.theme.StatusSuccessful.Render("[A]")
		case "removed", "deleted":
			statusIndicator = m.theme.StatusFailed.Render("[D]")
		default:
			statusIndicator = m.theme.StatusQueued.Render("[M]")
		}

		filename := file.Filename
		if len(filename) > leftWidth-6 {
			filename = "..." + filename[len(filename)-(leftWidth-9):]
		}
		
		lineText := fmt.Sprintf("  %s %s", statusIndicator, filename)
		visWidth := lipgloss.Width(lineText)
		if visWidth < leftWidth {
			lineText += strings.Repeat(" ", leftWidth-visWidth)
		}

		if idx == m.selectedCommitFileIdx {
			fileLines = append(fileLines, m.theme.TableSelected.Render(lineText))
		} else {
			fileLines = append(fileLines, m.theme.TableRow.Render(lineText))
		}
	}

	var selectedPath string
	if m.selectedCommitFileIdx < len(m.commitFiles) {
		selectedPath = m.commitFiles[m.selectedCommitFileIdx].Filename
	}

	diffHeader := fmt.Sprintf(" DIFF: %s", selectedPath)
	diffHeaderRendered := m.theme.TableHeader.Render(fmt.Sprintf("%-*s", m.commitDiffViewport.Width, diffHeader))
	diffHeaderLines := strings.Split(diffHeaderRendered, "\n")

	viewportLines := strings.Split(m.commitDiffViewport.View(), "\n")

	var rightLines []string
	rightLines = append(rightLines, diffHeaderLines...)
	rightLines = append(rightLines, viewportLines...)

	maxLines := len(fileLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	visibleRows := m.height - 14
	if visibleRows < 5 {
		visibleRows = 5
	}
	if maxLines > visibleRows {
		maxLines = visibleRows
	}

	for i := 0; i < maxLines; i++ {
		leftPart := ""
		if i < len(fileLines) {
			leftPart = fileLines[i]
		} else {
			leftPart = strings.Repeat(" ", leftWidth)
		}

		rightPart := ""
		if i < len(rightLines) {
			rightPart = rightLines[i]
		}
		sb.WriteString("  " + leftPart + " │ " + rightPart + "\n")
	}

	for i := 0; i < (m.height - 14 - maxLines); i++ {
		sb.WriteString("\n")
	}

	keys := []string{"Esc:Back to PR", "j/k:Navigate Files", "u/d:Scroll Diff", "w:Browser"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderPRDiffView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	pr := m.selectedPull
	if pr == nil {
		return "No PR selected."
	}

	titleText := fmt.Sprintf("PR #%d Diff: %s", pr.Number, pr.Title)
	sb.WriteString("  " + m.theme.LogoText.Render(titleText) + "\n")
	authorLogin := "unknown"
	if pr.User != nil {
		authorLogin = pr.User.Login
	}
	sb.WriteString("  " + m.theme.HelpDesc.Render(fmt.Sprintf("Repo: %s | Author: @%s | Source: %s → Base: %s", pr.Repository.FullName, authorLogin, pr.Head.Ref, pr.Base.Ref)) + "\n\n")

	leftWidth := 40
	var fileLines []string

	// Pad header to leftWidth and split by newline to correctly format the bottom border
	headerText := fmt.Sprintf(" %-*s", leftWidth-1, "FILES CHANGED")
	headerRendered := m.theme.TableHeader.Render(headerText)
	fileLines = append(fileLines, strings.Split(headerRendered, "\n")...)

	visibleRowsFiles := m.height - 16
	if visibleRowsFiles < 5 {
		visibleRowsFiles = 5
	}
	endIdxFiles := m.prFileStartIndex + visibleRowsFiles
	if endIdxFiles > len(m.prFiles) {
		endIdxFiles = len(m.prFiles)
	}

	for idx := m.prFileStartIndex; idx < endIdxFiles; idx++ {
		file := m.prFiles[idx]
		var statusIndicator string
		switch file.Status {
		case "added":
			statusIndicator = m.theme.StatusSuccessful.Render("[A]")
		case "removed", "deleted":
			statusIndicator = m.theme.StatusFailed.Render("[D]")
		default:
			statusIndicator = m.theme.StatusQueued.Render("[M]")
		}

		filename := file.Filename
		if len(filename) > leftWidth-6 {
			filename = "..." + filename[len(filename)-(leftWidth-9):]
		}
		
		lineText := fmt.Sprintf("  %s %s", statusIndicator, filename)
		visWidth := lipgloss.Width(lineText)
		if visWidth < leftWidth {
			lineText += strings.Repeat(" ", leftWidth-visWidth)
		}

		if idx == m.selectedFileIdx {
			fileLines = append(fileLines, m.theme.TableSelected.Render(lineText))
		} else {
			fileLines = append(fileLines, m.theme.TableRow.Render(lineText))
		}
	}

	var selectedPath string
	if m.selectedFileIdx < len(m.prFiles) {
		selectedPath = m.prFiles[m.selectedFileIdx].Filename
	}

	diffHeader := fmt.Sprintf(" DIFF: %s", selectedPath)
	diffHeaderRendered := m.theme.TableHeader.Render(fmt.Sprintf("%-*s", m.diffViewport.Width, diffHeader))
	diffHeaderLines := strings.Split(diffHeaderRendered, "\n")

	viewportLines := strings.Split(m.diffViewport.View(), "\n")

	var rightLines []string
	rightLines = append(rightLines, diffHeaderLines...)
	rightLines = append(rightLines, viewportLines...)

	maxLines := len(fileLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	visibleRows := m.height - 14
	if visibleRows < 5 {
		visibleRows = 5
	}
	if maxLines > visibleRows {
		maxLines = visibleRows
	}

	for i := 0; i < maxLines; i++ {
		leftPart := ""
		if i < len(fileLines) {
			leftPart = fileLines[i]
		} else {
			leftPart = strings.Repeat(" ", leftWidth)
		}

		rightPart := ""
		if i < len(rightLines) {
			rightPart = rightLines[i]
		}
		sb.WriteString("  " + leftPart + " │ " + rightPart + "\n")
	}

	for i := 0; i < (m.height - 14 - maxLines); i++ {
		sb.WriteString("\n")
	}

	keys := []string{"Esc:Back to PR", "j/k:Navigate Files", "u/d:Scroll Diff", "w:Browser"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}


func renderMarkdown(content string, width int) (string, error) {
	if content == "" {
		return "No description provided.", nil
	}
	style := "dark"
	if !lipgloss.HasDarkBackground() {
		style = "light"
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content, err
	}
	return r.Render(content)
}

func (m Model) renderPRFilterInputModal() string {
	var modalText strings.Builder
	modalText.WriteString("┌──────────────────────────────────────────────┐\n")
	modalText.WriteString("│                 FILTER USER                  │\n")
	modalText.WriteString("├──────────────────────────────────────────────┤\n")
	modalText.WriteString("│                                              │\n")
	modalText.WriteString("│  Enter GitHub username to filter by:         │\n")
	modalText.WriteString("│                                              │\n")
	
	// Create padded line using Lipgloss
	lineStyle := lipgloss.NewStyle().Width(46).PaddingLeft(4)
	inputLine := lineStyle.Render(m.textInput.View())
	modalText.WriteString("│" + inputLine + "│\n")
	
	modalText.WriteString("│                                              │\n")
	modalText.WriteString("│  Press " + m.theme.HelpKey.Render("Enter") + " to proceed, " + m.theme.HelpDesc.Render("Esc") + " to cancel       │\n")
	modalText.WriteString("└──────────────────────────────────────────────┘")
	return modalText.String()
}

func (m Model) renderPRFilterTypeSelectModal() string {
	var modalText strings.Builder
	username := m.prFilterUser
	if len(username) > 18 {
		username = username[:15] + "..."
	}
	
	modalText.WriteString("┌──────────────────────────────────────────────┐\n")
	modalText.WriteString("│                 FILTER TYPE                  │\n")
	modalText.WriteString("├──────────────────────────────────────────────┤\n")
	modalText.WriteString("│                                              │\n")
	
	// User line using Lipgloss
	userStyle := lipgloss.NewStyle().Width(46).PaddingLeft(4)
	userText := fmt.Sprintf("Filter user @%s by:", username)
	userLine := userStyle.Render(userText)
	modalText.WriteString("│" + userLine + "│\n")
	
	modalText.WriteString("│                                              │\n")
	modalText.WriteString("│    " + m.theme.HelpKey.Render("[A]") + " Author                                │\n")
	modalText.WriteString("│    " + m.theme.HelpKey.Render("[I]") + " Assignee                              │\n")
	modalText.WriteString("│    " + m.theme.HelpKey.Render("[R]") + " Reviewer                              │\n")
	modalText.WriteString("│                                              │\n")
	modalText.WriteString("│  Press " + m.theme.HelpDesc.Render("Esc") + " or " + m.theme.HelpDesc.Render("C") + " to cancel                    │\n")
	modalText.WriteString("└──────────────────────────────────────────────┘")
	return modalText.String()
}

// formatCheckName formats the check run name to "Workflow Name / Job Name" (aligning with GitHub Web UI).
func (m Model) formatCheckName(check gh.CheckRun) string {
	isActions := check.App != nil && (check.App.Slug == "github-actions" || strings.Contains(strings.ToLower(check.App.Name), "github actions") || strings.Contains(strings.ToLower(check.App.Slug), "action"))
	if !isActions {
		return check.Name
	}

	wfName := ""
	for _, r := range m.runs {
		if r.Name == check.Name || strings.Contains(strings.ToLower(check.Name), strings.ToLower(r.Name)) || strings.Contains(strings.ToLower(r.Name), strings.ToLower(check.Name)) {
			wfName = r.Name
			break
		}
	}
	if wfName == "" && m.viewingRun != nil {
		wfName = m.viewingRun.Name
	}
	if wfName == "" && check.HTMLURL != "" {
		runID := extractRunIDFromURL(check.HTMLURL)
		if runID > 0 {
			for _, r := range m.runs {
				if r.ID == runID {
					wfName = r.Name
					break
				}
			}
			if wfName == "" && m.viewingRun != nil && m.viewingRun.ID == runID {
				wfName = m.viewingRun.Name
			}
		}
	}

	if wfName == "" && len(m.runs) == 1 {
		wfName = m.runs[0].Name
	}

	if wfName != "" {
		if strings.Contains(check.Name, " / ") {
			return check.Name
		}
		if strings.HasPrefix(strings.ToLower(check.Name), strings.ToLower(wfName)) {
			cleaned := check.Name
			if strings.HasPrefix(strings.ToLower(cleaned), strings.ToLower(wfName)+":") {
				cleaned = cleaned[len(wfName)+1:]
				cleaned = strings.TrimSpace(cleaned)
			}
			return wfName + " / " + cleaned
		}
		return wfName + " / " + check.Name
	}

	return check.Name
}

func (m Model) renderIssueFilterInputModal() string {
	var modalText strings.Builder
	modalText.WriteString("┌──────────────────────────────────────────────┐\n")
	modalText.WriteString("│                 FILTER USER                  │\n")
	modalText.WriteString("├──────────────────────────────────────────────┤\n")
	modalText.WriteString("│                                              │\n")
	modalText.WriteString("│  Enter GitHub username to filter by:         │\n")
	modalText.WriteString("│                                              │\n")
	
	// Create padded line using Lipgloss
	lineStyle := lipgloss.NewStyle().Width(46).PaddingLeft(4)
	inputLine := lineStyle.Render(m.textInput.View())
	modalText.WriteString("│" + inputLine + "│\n")
	
	modalText.WriteString("│                                              │\n")
	modalText.WriteString("│  Press " + m.theme.HelpKey.Render("Enter") + " to proceed, " + m.theme.HelpDesc.Render("Esc") + " to cancel       │\n")
	modalText.WriteString("└──────────────────────────────────────────────┘")
	return modalText.String()
}

func (m Model) renderIssueFilterTypeSelectModal() string {
	var modalText strings.Builder
	username := m.issueFilterUser
	if len(username) > 18 {
		username = username[:15] + "..."
	}
	
	modalText.WriteString("┌──────────────────────────────────────────────┐\n")
	modalText.WriteString("│                 FILTER TYPE                  │\n")
	modalText.WriteString("├──────────────────────────────────────────────┤\n")
	modalText.WriteString("│                                              │\n")
	
	// User line using Lipgloss
	userStyle := lipgloss.NewStyle().Width(46).PaddingLeft(4)
	userText := fmt.Sprintf("Filter user @%s by:", username)
	userLine := userStyle.Render(userText)
	modalText.WriteString("│" + userLine + "│\n")
	
	modalText.WriteString("│                                              │\n")
	modalText.WriteString("│    " + m.theme.HelpKey.Render("[A]") + " Author                                │\n")
	modalText.WriteString("│    " + m.theme.HelpKey.Render("[I]") + " Assignee                              │\n")
	modalText.WriteString("│                                              │\n")
	modalText.WriteString("│  Press " + m.theme.HelpDesc.Render("Esc") + " or " + m.theme.HelpDesc.Render("C") + " to cancel                    │\n")
	modalText.WriteString("└──────────────────────────────────────────────┘")
	return modalText.String()
}

func (m Model) renderIssuesView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	// Display active filters
	var filterTexts []string
	if m.filterIssueState != "" && m.filterIssueState != "open" {
		filterTexts = append(filterTexts, fmt.Sprintf("State: %s", strings.ToUpper(m.filterIssueState)))
	}
	if m.filterIssueAuthor != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Author: @%s", m.filterIssueAuthor))
	}
	if m.filterIssueAssignee != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Assignee: @%s", m.filterIssueAssignee))
	}
	if m.filterRepo != "" {
		filterTexts = append(filterTexts, fmt.Sprintf("Repo: %s", m.filterRepo))
	}
	if len(filterTexts) > 0 {
		sb.WriteString("  " + m.theme.StatusWaiting.Render("Filter active: "+strings.Join(filterTexts, ", ")+" (Press 'x' to clear)") + "\n\n")
	}

	issueTitleWidth := m.width - 102
	if issueTitleWidth < 15 {
		issueTitleWidth = 15
	}

	header := fmt.Sprintf("  %-3s %-6s %-*s %-24s %-20s %-12s %-16s", "ST", "ISS #", issueTitleWidth, "ISSUE TITLE", "AUTHOR", "REPOSITORY", "ASSIGNEES", "LABELS")
	sb.WriteString(m.theme.TableHeader.Render(header) + "\n")

	renderedCount := 0
	hideList := m.state == viewIssueFilterInput || m.state == viewIssueFilterTypeSelect || m.state == viewFilterTypeSelect || m.state == viewRepoFilterSelect
	
	if hideList {
		visibleRows := m.height - 12
		if len(filterTexts) > 0 {
			visibleRows -= 2
		}
		if visibleRows < 5 {
			visibleRows = 5
		}
		for i := 0; i < visibleRows; i++ {
			sb.WriteString("\n")
		}
		renderedCount = visibleRows
	} else if len(m.issues) == 0 {
		msg := "No open issues found."
		if m.isLoading {
			msg = "Loading issues..."
		}
		sb.WriteString("\n  " + m.theme.HelpDesc.Render(msg) + "\n\n")
		renderedCount = 3
	} else {
		visibleRows := m.height - 12
		if len(filterTexts) > 0 {
			visibleRows -= 2
		}
		if visibleRows < 5 {
			visibleRows = 5
		}

		endIdx := m.issueStartIndex + visibleRows
		totalRows := len(m.issues)
		if m.hasMoreIssues {
			totalRows++
		}
		if endIdx > totalRows {
			endIdx = totalRows
		}

		renderedCount = endIdx - m.issueStartIndex

		for i := m.issueStartIndex; i < endIdx; i++ {
			if i < len(m.issues) {
				issue := m.issues[i]
				
				statusInd := m.theme.StatusSuccessful.Render("●")
				if issue.State == "closed" {
					statusInd = m.theme.StatusFailed.Render("○")
				}

				repoName := issue.Repository.Name
				if len(repoName) > 20 {
					repoName = repoName[:17] + "..."
				}

				issueNumStr := fmt.Sprintf("#%d", issue.Number)
				
				issueTitle := issue.Title
				if len(issueTitle) > issueTitleWidth {
					issueTitle = issueTitle[:issueTitleWidth-3] + "..."
				}

				authorName := "unknown"
				if issue.User != nil && issue.User.Login != "" {
					authorName = issue.User.Login
				}
				if len(authorName) > 24 {
					authorName = authorName[:21] + "..."
				}

				assigneesList := "None"
				if len(issue.Assignees) > 0 {
					var names []string
					for _, user := range issue.Assignees {
						names = append(names, user.Login)
					}
					assigneesList = strings.Join(names, ", ")
				}
				if len(assigneesList) > 12 {
					assigneesList = assigneesList[:9] + "..."
				}

				labelsList := "None"
				if len(issue.Labels) > 0 {
					var names []string
					for _, label := range issue.Labels {
						names = append(names, label.Name)
					}
					labelsList = strings.Join(names, ", ")
				}
				if len(labelsList) > 16 {
					labelsList = labelsList[:13] + "..."
				}

				paddedIssueTitle := fmt.Sprintf("%-*s", issueTitleWidth, issueTitle)

				rowText := fmt.Sprintf("  %-3s %-6s %s %-24s %-20s %-12s %-16s",
					statusInd,
					issueNumStr,
					paddedIssueTitle,
					authorName,
					repoName,
					assigneesList,
					labelsList,
				)

				if i == m.selectedIssueIdx {
					sb.WriteString(m.theme.TableSelected.Render(rowText) + "\n")
				} else {
					sb.WriteString(m.theme.TableRow.Render(rowText) + "\n")
				}
			} else if i == len(m.issues) && m.hasMoreIssues {
				loadText := "  [-- Load More Issues... --]"
				if m.selectedIssueIdx == len(m.issues) {
					sb.WriteString(m.theme.TableSelected.Render(loadText) + "\n")
				} else {
					sb.WriteString(m.theme.Subtitle.Render(loadText) + "\n")
				}
			}
		}
	}

	contentHeight := renderedCount + 10
	if len(filterTexts) > 0 {
		contentHeight += 2
	}
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"?:Help", "Esc:Exit", "Tab:Tabs", "j/k:Navigate", "Enter:View Issue", "w:Browser", "f:Filter", "s:State", "a:My Issues", "i:My Assigned", "x:Clear Filter"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderIssueDetailsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	issue := m.selectedIssue
	if issue == nil {
		return "No Issue selected."
	}

	issueStateStr := "OPEN"
	issueStateStyle := m.theme.StatusSuccessful
	if issue.State == "closed" {
		issueStateStr = "CLOSED"
		issueStateStyle = m.theme.StatusFailed
	}

	authorLogin := "unknown"
	if issue.User != nil {
		authorLogin = issue.User.Login
	}

	divWidth := m.width - 4
	if divWidth < 10 {
		divWidth = 10
	}
	sb.WriteString("  " + issueStateStyle.Render(fmt.Sprintf("[%s]", issueStateStr)) + " " + m.theme.LogoText.Render(fmt.Sprintf("Issue #%d: %s", issue.Number, issue.Title)) + "\n")
	sb.WriteString("  " + m.theme.HelpDesc.Render(fmt.Sprintf("Repo: %s | Author: @%s", issue.Repository.FullName, authorLogin)) + "\n")
	sb.WriteString("  " + m.theme.Border.Render(strings.Repeat("─", divWidth)) + "\n\n")

	// Calculate widths dynamically
	sidebarWidth := m.width / 5
	if sidebarWidth < 40 {
		sidebarWidth = 40
	}
	h := m.issueDescViewport.Height

	// Render two columns side-by-side: Description on the left, sidebar on the right
	middleView := m.issueDescViewport.View()
	rightView := m.renderIssueRightSidebar(sidebarWidth, h)

	sideBySide := lipgloss.JoinHorizontal(
		lipgloss.Top,
		middleView,
		"   ", // separator gap
		rightView,
	)

	sb.WriteString(sideBySide + "\n")

	// Dynamic padding
	contentHeight := h + 10
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"Esc:Back to Issues", "Tab:Toggle Focus", "j/k:Navigate", "w:Browser", "r:Refresh", "c:Comments", "q:Quit"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderIssueRightSidebar(width, height int) string {
	var sb strings.Builder
	issue := m.selectedIssue
	if issue == nil {
		return ""
	}

	styleVal := m.theme.LogoText
	styleLabel := m.theme.Subtitle

	sb.WriteString(m.theme.TableHeader.Render(fmt.Sprintf(" %-*s", width-2, "DETAILS")) + "\n")

	// State
	stateText := "Open"
	if issue.State == "closed" {
		stateText = "Closed"
	}
	fmt.Fprintf(&sb, " %-12s %s\n", styleLabel.Render("State:"), styleVal.Render(stateText))

	// Milestone
	milestoneText := "None"
	if issue.Milestone != nil {
		milestoneText = issue.Milestone.Title
	}
	if len(milestoneText) > width-15 {
		milestoneText = milestoneText[:width-18] + "..."
	}
	fmt.Fprintf(&sb, " %-12s %s\n", styleLabel.Render("Milestone:"), styleVal.Render(milestoneText))

	// Assignees
	assigneesText := "None"
	if len(issue.Assignees) > 0 {
		var names []string
		for _, u := range issue.Assignees {
			names = append(names, "@"+u.Login)
		}
		assigneesText = strings.Join(names, ", ")
	}
	if len(assigneesText) > width-15 {
		assigneesText = assigneesText[:width-18] + "..."
	}
	fmt.Fprintf(&sb, " %-12s %s\n", styleLabel.Render("Assignees:"), styleVal.Render(assigneesText))

	// Labels
	labelsText := "None"
	if len(issue.Labels) > 0 {
		var names []string
		for _, l := range issue.Labels {
			names = append(names, l.Name)
		}
		labelsText = strings.Join(names, ", ")
	}
	if len(labelsText) > width-15 {
		labelsText = labelsText[:width-18] + "..."
	}
	fmt.Fprintf(&sb, " %-12s %s\n", styleLabel.Render("Labels:"), styleVal.Render(labelsText))

	// Separator
	sb.WriteString("\n" + m.theme.Border.Render(strings.Repeat("─", width-2)) + "\n\n")

	// Comments count info
	commentsCount := fmt.Sprintf("%d comments", len(m.issueComments))
	sb.WriteString("  " + m.theme.HelpKey.Render("Press 'c' to view comments") + "\n")
	sb.WriteString("  " + m.theme.HelpDesc.Render("("+commentsCount+")") + "\n")

	// Pad to height
	lines := strings.Split(sb.String(), "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderIssueCommentsView() string {
	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	titleText := " Issue Comments "
	if m.selectedIssue != nil {
		titleText = fmt.Sprintf(" Issue #%d Comments ", m.selectedIssue.Number)
	}
	sb.WriteString("  " + m.theme.LogoText.Render(titleText) + "\n\n")

	// Render the viewport with borders
	vpContent := m.commentsViewport.View()
	lines := strings.Split(vpContent, "\n")
	boxWidth := m.commentsViewport.Width + 4

	sb.WriteString("  " + m.theme.Border.Render("┌" + strings.Repeat("─", boxWidth-2) + "┐") + "\n")
	for _, line := range lines {
		lineLen := lipgloss.Width(line)
		pad := m.commentsViewport.Width - lineLen
		if pad < 0 {
			pad = 0
		}
		sb.WriteString("  " + m.theme.Border.Render("│ ") + line + strings.Repeat(" ", pad) + m.theme.Border.Render(" │") + "\n")
	}
	sb.WriteString("  " + m.theme.Border.Render("└" + strings.Repeat("─", boxWidth-2) + "┘") + "\n")

	// Dynamic padding
	contentHeight := m.commentsViewport.Height + 8
	padding := m.height - contentHeight
	if padding < 0 {
		padding = 0
	}
	for i := 0; i < padding; i++ {
		sb.WriteString("\n")
	}

	keys := []string{"Esc:Back to Issue", "j/k:Scroll Comments", "r:Refresh", "q:Quit"}
	sb.WriteString(m.renderFooter(keys))

	return sb.String()
}

func (m Model) renderFilterTypeSelectModal() string {
	var modalText strings.Builder
	lineStyle := lipgloss.NewStyle().Width(46)

	modalText.WriteString("┌──────────────────────────────────────────────┐\n")
	modalText.WriteString("│                 FILTER OPTIONS               │\n")
	modalText.WriteString("├──────────────────────────────────────────────┤\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("  Choose a filter target:") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("    "+m.theme.LogoText.Render("[U]")+" Filter by User") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("    "+m.theme.LogoText.Render("[R]")+" Filter by Repository") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("  Press "+m.theme.HelpDesc.Render("Esc")+" to cancel") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("└──────────────────────────────────────────────┘")
	return modalText.String()
}

func (m Model) renderRepoFilterSelectModal() string {
	var modalText strings.Builder
	lineStyle := lipgloss.NewStyle().Width(46)

	modalText.WriteString("┌──────────────────────────────────────────────┐\n")
	modalText.WriteString("│              SELECT REPOSITORY               │\n")
	modalText.WriteString("├──────────────────────────────────────────────┤\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")

	if len(m.repos) == 0 {
		msg := "  No repositories found."
		if m.isLoading {
			msg = "  Loading repositories..."
		}
		modalText.WriteString("│" + lineStyle.Render(msg) + "│\n")
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	} else {
		visibleRows := 8
		if visibleRows > len(m.repos) {
			visibleRows = len(m.repos)
		}

		if m.selectedRepoIdx < 0 {
			m.selectedRepoIdx = 0
		}
		if m.selectedRepoIdx >= len(m.repos) {
			m.selectedRepoIdx = len(m.repos) - 1
		}
		if m.selectedRepoIdx < m.repoStartIndex {
			m.repoStartIndex = m.selectedRepoIdx
		}
		if m.selectedRepoIdx >= m.repoStartIndex+visibleRows {
			m.repoStartIndex = m.selectedRepoIdx - visibleRows + 1
		}

		endIdx := m.repoStartIndex + visibleRows
		for i := m.repoStartIndex; i < endIdx; i++ {
			repo := m.repos[i]
			displayName := repo.Name
			if len(displayName) > 36 {
				displayName = displayName[:33] + "..."
			}

			var lineContent string
			if i == m.selectedRepoIdx {
				lineContent = "  " + m.theme.TableSelected.Render(" > "+displayName+" ")
			} else {
				lineContent = "    " + displayName
			}

			modalText.WriteString("│" + lineStyle.Render(lineContent) + "│\n")
		}
		modalText.WriteString("│" + lineStyle.Render("") + "│\n")
		scrollerText := fmt.Sprintf("  Row %d of %d (j/k or arrows)", m.selectedRepoIdx+1, len(m.repos))
		modalText.WriteString("│" + lineStyle.Render(m.theme.HelpDesc.Render(scrollerText)) + "│\n")
	}

	modalText.WriteString("│" + lineStyle.Render("  Press "+m.theme.HelpKey.Render("Enter")+" to select, "+m.theme.HelpDesc.Render("Esc")+" to back") + "│\n")
	modalText.WriteString("│" + lineStyle.Render("") + "│\n")
	modalText.WriteString("└──────────────────────────────────────────────┘")
	return modalText.String()
}

