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
	RunAttempt   int         `json:"run_attempt"`
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

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	ID                 int64          `json:"id"`
	Number             int            `json:"number"`
	Title              string         `json:"title"`
	Body               string         `json:"body"`
	State              string         `json:"state"` // open, closed
	Draft              bool           `json:"draft"`
	HTMLURL            string         `json:"html_url"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	MergedAt           *time.Time     `json:"merged_at"`
	User               *User          `json:"user"` // PR author
	Assignees          []User         `json:"assignees"`
	RequestedReviewers []User         `json:"requested_reviewers"`
	Labels             []Label        `json:"labels"`
	Milestone          *Milestone     `json:"milestone"`
	Head               PullRequestRef `json:"head"`
	Base               PullRequestRef `json:"base"`
	Repository         Repository     `json:"repository"`
}

// Label represents a GitHub label.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// Milestone represents a GitHub milestone.
type Milestone struct {
	Title string `json:"title"`
}

// PullRequestRef represents a git ref in a pull request (head or base).
type PullRequestRef struct {
	Label string      `json:"label"`
	Ref   string      `json:"ref"`
	SHA   string      `json:"sha"`
	Repo  *Repository `json:"repo"`
}

// CommitFile represents a file changed in a PR or commit.
type CommitFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"` // added, modified, deleted
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch"`
}

// RepositoryCommit represents a commit in a repository.
type RepositoryCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name  string    `json:"name"`
			Email string    `json:"email"`
			Date  time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Author *User `json:"author"`
}

// CheckRun represents a single check run (PR check).
type CheckRun struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`     // queued, in_progress, completed
	Conclusion  string    `json:"conclusion"` // success, failure, etc.
	HTMLURL     string    `json:"html_url"`
	App         *CheckApp `json:"app"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

type CheckApp struct {
	Slug string `json:"slug"` // "github-actions" for Actions workflows
	Name string `json:"name"`
}

type CheckRunsResponse struct {
	TotalCount int        `json:"total_count"`
	CheckRuns  []CheckRun `json:"check_runs"`
}

// IssueComment represents a comment on a PR (issue comment).
type IssueComment struct {
	ID        int64     `json:"id"`
	HTMLURL   string    `json:"html_url"`
	Body      string    `json:"body"`
	User      *User     `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

