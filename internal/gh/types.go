package gh

import (
	"time"
)

// RateLimitInfo stores the parsed rate limit headers from the last response.
type RateLimitInfo struct {
	Limit     int
	Remaining int
	Reset     time.Time
}

// User represents a GitHub user account.
type User struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// Org represents a GitHub organization.
type Org struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// Repository represents a GitHub repository.
type Repository struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Owner         *User     `json:"owner"`
	PushedAt      time.Time `json:"pushed_at"`
	DefaultBranch string    `json:"default_branch"`
}

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID           int64       `json:"id"`
	Name         string      `json:"name"`
	RunNumber    int         `json:"run_number"`
	Event        string      `json:"event"`
	Status       string      `json:"status"`     // queued, in_progress, completed, etc.
	Conclusion   string      `json:"conclusion"` // success, failure, cancelled, skipped, etc.
	HeadBranch   string      `json:"head_branch"`
	HeadSHA      string      `json:"head_sha"`
	HTMLURL      string      `json:"html_url"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	Repository   Repository  `json:"repository"`
	DisplayTitle string      `json:"display_title"`
	Actor        *User       `json:"actor"`
}

// WorkflowJob represents a job in a GitHub Actions workflow run.
type WorkflowJob struct {
	ID          int64      `json:"id"`
	RunID       int64      `json:"run_id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`     // queued, in_progress, completed
	Conclusion  string     `json:"conclusion"` // success, failure, etc.
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt time.Time  `json:"completed_at"`
	Steps       []JobStep  `json:"steps"`
	HTMLURL     string     `json:"html_url"`
}

// JobStep represents a single step in a workflow job.
type JobStep struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`     // queued, in_progress, completed
	Conclusion  string    `json:"conclusion"` // success, failure, etc.
	Number      int       `json:"number"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// WorkflowRunsResponse represents the list workflow runs API response payload.
type WorkflowRunsResponse struct {
	TotalCount   int           `json:"total_count"`
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

// WorkflowJobsResponse represents the list workflow jobs API response payload.
type WorkflowJobsResponse struct {
	TotalCount int           `json:"total_count"`
	Jobs       []WorkflowJob `json:"jobs"`
}
