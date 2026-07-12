package gh

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrRateLimited = errors.New("github api rate limit reached")
	ErrForbidden   = errors.New("github api access forbidden")
)

// Client handles communication with the GitHub API.
type Client struct {
	httpClient *http.Client
	token      string
	baseURL    string

	mu           sync.RWMutex
	rateLimit    RateLimitInfo
	serverOffset time.Duration
	scopes       []string
}

// Now returns the current time synchronized with the GitHub API server clock.
func (c *Client) Now() time.Time {
	c.mu.RLock()
	offset := c.serverOffset
	c.mu.RUnlock()
	return time.Now().Add(offset)
}

// NewClient returns a new GitHub API client.
func NewClient(token, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		token:      token,
		baseURL:    baseURL,
	}
}

// GetRateLimit returns the last recorded rate limit information.
func (c *Client) GetRateLimit() RateLimitInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rateLimit
}

// GetScopes returns the scopes of the API token.
func (c *Client) GetScopes() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.scopes
}

func (c *Client) updateScopes(header http.Header) {
	if scopesStr := header.Get("X-OAuth-Scopes"); scopesStr != "" {
		c.mu.Lock()
		c.scopes = nil
		for _, s := range strings.Split(scopesStr, ",") {
			if s = strings.TrimSpace(s); s != "" {
				c.scopes = append(c.scopes, s)
			}
		}
		c.mu.Unlock()
	}
}

func (c *Client) updateRateLimit(header http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if limitStr := header.Get("X-RateLimit-Limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			c.rateLimit.Limit = limit
		}
	}
	if remainingStr := header.Get("X-RateLimit-Remaining"); remainingStr != "" {
		if remaining, err := strconv.Atoi(remainingStr); err == nil {
			c.rateLimit.Remaining = remaining
		}
	}
	if resetStr := header.Get("X-RateLimit-Reset"); resetStr != "" {
		if resetUnix, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			c.rateLimit.Reset = time.Unix(resetUnix, 0)
		}
	}
}

func (c *Client) updateTimeOffset(header http.Header) {
	if dateStr := header.Get("Date"); dateStr != "" {
		if serverTime, err := http.ParseTime(dateStr); err == nil {
			c.mu.Lock()
			c.serverOffset = time.Until(serverTime)
			c.mu.Unlock()
		}
	}
}

func (c *Client) checkRateLimit(ctx context.Context) error {
	c.mu.RLock()
	remaining := c.rateLimit.Remaining
	reset := c.rateLimit.Reset
	c.mu.RUnlock()

	// If rate limit is fully depleted, back off until reset
	if remaining == 0 && !reset.IsZero() && time.Now().Before(reset) {
		waitDur := time.Until(reset)
		if waitDur > 30*time.Second {
			// Don't sleep for too long inside the TUI, return rate limited error so UI knows
			return fmt.Errorf("%w: resets at %v (wait %v)", ErrRateLimited, reset, waitDur.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDur):
		}
	}
	return nil
}

func (c *Client) doRequest(ctx context.Context, method, apiPath string, query params, responseVal any) error {
	if err := c.checkRateLimit(ctx); err != nil {
		return err
	}

	u, err := url.Parse(c.baseURL + apiPath)
	if err != nil {
		return err
	}

	if query != nil {
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	c.updateRateLimit(resp.Header)
	c.updateTimeOffset(resp.Header)
	c.updateScopes(resp.Header)

	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return ErrForbidden
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error (status %d): %s", resp.StatusCode, string(body))
	}

	if responseVal != nil {
		dec := json.NewDecoder(resp.Body)
		return dec.Decode(responseVal)
	}
	return nil
}

type params map[string]string

// GetUser fetches details of the authenticated user.
func (c *Client) GetUser(ctx context.Context) (*User, error) {
	var user User
	err := c.doRequest(ctx, "GET", "/user", nil, &user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetOrgs fetches organizations the authenticated user belongs to.
func (c *Client) GetOrgs(ctx context.Context) ([]Org, error) {
	var orgs []Org
	err := c.doRequest(ctx, "GET", "/user/orgs", nil, &orgs)
	if err != nil {
		return nil, err
	}
	return orgs, nil
}

// GetRepos fetches repositories for an organization or user, sorted by pushed_at descending.
func (c *Client) GetRepos(ctx context.Context, ownerType, owner string, page, perPage int) ([]Repository, error) {
	var repos []Repository
	path := "/users/" + owner + "/repos"
	if ownerType == "org" {
		path = "/orgs/" + owner + "/repos"
	}

	q := params{
		"sort":      "pushed",
		"direction": "desc",
		"page":      strconv.Itoa(page),
		"per_page":  strconv.Itoa(perPage),
	}

	err := c.doRequest(ctx, "GET", path, q, &repos)
	if err != nil {
		return nil, err
	}
	return repos, nil
}

// GetWorkflowRuns fetches the workflow runs for a specific repository, optionally filtered by actor.
func (c *Client) GetWorkflowRuns(ctx context.Context, owner, repo string, page, perPage int, actor string) ([]WorkflowRun, error) {
	var wrResp WorkflowRunsResponse
	q := params{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if actor != "" {
		q["actor"] = actor
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs", owner, repo)
	err := c.doRequest(ctx, "GET", path, q, &wrResp)
	if err != nil {
		return nil, err
	}
	return wrResp.WorkflowRuns, nil
}

// GetWorkflowRun fetches a single workflow run by ID.
func (c *Client) GetWorkflowRun(ctx context.Context, owner, repo string, runID int64) (*WorkflowRun, error) {
	var run WorkflowRun
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", owner, repo, runID)
	err := c.doRequest(ctx, "GET", path, nil, &run)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// GetWorkflowRunJobs fetches the jobs of a workflow run.
func (c *Client) GetWorkflowRunJobs(ctx context.Context, owner, repo string, runID int64) ([]WorkflowJob, error) {
	var jResp WorkflowJobsResponse
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs", owner, repo, runID)
	err := c.doRequest(ctx, "GET", path, nil, &jResp)
	if err != nil {
		return nil, err
	}
	return jResp.Jobs, nil
}

// GetWorkflowRunAttemptJobs fetches the jobs of a workflow run for a specific run attempt.
func (c *Client) GetWorkflowRunAttemptJobs(ctx context.Context, owner, repo string, runID int64, attempt int) ([]WorkflowJob, error) {
	var jResp WorkflowJobsResponse
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/attempts/%d/jobs", owner, repo, runID, attempt)
	err := c.doRequest(ctx, "GET", path, nil, &jResp)
	if err != nil {
		return nil, err
	}
	return jResp.Jobs, nil
}

// GetWorkflowJob fetches a single workflow job by ID.
func (c *Client) GetWorkflowJob(ctx context.Context, owner, repo string, jobID int64) (*WorkflowJob, error) {
	var job WorkflowJob
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d", owner, repo, jobID)
	err := c.doRequest(ctx, "GET", path, nil, &job)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// GetJobLogs fetches logs for a specific job. Returns them as a string.
func (c *Client) GetJobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID)
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	c.updateRateLimit(resp.Header)
	c.updateTimeOffset(resp.Header)

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", ErrRateLimited
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return "", ErrForbidden
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		var xErr xmlError
		err = xml.Unmarshal(body, &xErr)
		if err == nil && xErr.Code != "" {
			msg := xErr.Message
			if idx := strings.Index(msg, "\n"); idx != -1 {
				msg = strings.TrimSpace(msg[:idx])
			}
			return "", fmt.Errorf("github api logs error (status %d): %s: %s", resp.StatusCode, xErr.Code, msg)
		}
		return "", fmt.Errorf("github api logs error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

type xmlError struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func (c *Client) doRequestWithBody(ctx context.Context, method, apiPath string, query params, bodyVal any, responseVal any) error {
	if err := c.checkRateLimit(ctx); err != nil {
		return err
	}

	u, err := url.Parse(c.baseURL + apiPath)
	if err != nil {
		return err
	}

	if query != nil {
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	var bodyReader io.Reader
	if bodyVal != nil {
		var jsonBytes []byte
		jsonBytes, err = json.Marshal(bodyVal)
		if err != nil {
			return err
		}
		bodyReader = strings.NewReader(string(jsonBytes))
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if bodyVal != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	c.updateRateLimit(resp.Header)
	c.updateTimeOffset(resp.Header)
	c.updateScopes(resp.Header)

	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return ErrForbidden
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error (status %d): %s", resp.StatusCode, string(body))
	}

	if responseVal != nil {
		dec := json.NewDecoder(resp.Body)
		return dec.Decode(responseVal)
	}
	return nil
}

// GetPullRequests fetches pull requests for a specific repository.
func (c *Client) GetPullRequests(ctx context.Context, owner, repo string, page, perPage int) ([]PullRequest, error) {
	return c.GetPullRequestsWithState(ctx, owner, repo, "open", page, perPage)
}

// GetPullRequestsWithState fetches pull requests with a custom state (open, closed, all) for a specific repository.
func (c *Client) GetPullRequestsWithState(ctx context.Context, owner, repo, state string, page, perPage int) ([]PullRequest, error) {
	var prs []PullRequest
	q := params{
		"state":    state,
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	err := c.doRequest(ctx, "GET", path, q, &prs)
	if err != nil {
		return nil, err
	}
	return prs, nil
}

// GetIssuesWithState fetches issues with a custom state (open, closed, all) for a specific repository.
func (c *Client) GetIssuesWithState(ctx context.Context, owner, repo, state string, page, perPage int) ([]Issue, error) {
	var allIssues []Issue
	q := params{
		"state":    state,
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	err := c.doRequest(ctx, "GET", path, q, &allIssues)
	if err != nil {
		return nil, err
	}

	// Filter out PRs because the GitHub issues API returns both issues and pull requests.
	var issues []Issue
	for _, issue := range allIssues {
		if issue.PullRequest == nil {
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

// GetIssue fetches a single issue by number.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	var issue Issue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	err := c.doRequest(ctx, "GET", path, nil, &issue)
	if err != nil {
		return nil, err
	}
	return &issue, nil
}


// GetPullRequest fetches a single pull request by number.
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error) {
	var pr PullRequest
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	err := c.doRequest(ctx, "GET", path, nil, &pr)
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

// GetPullRequestComments fetches comments for a pull request (issue comments).
func (c *Client) GetPullRequestComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	var comments []IssueComment
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	err := c.doRequest(ctx, "GET", path, nil, &comments)
	if err != nil {
		return nil, err
	}
	return comments, nil
}

// GetPullRequestCommits fetches commits for a pull request.
func (c *Client) GetPullRequestCommits(ctx context.Context, owner, repo string, number int) ([]RepositoryCommit, error) {
	var commits []RepositoryCommit
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/commits", owner, repo, number)
	err := c.doRequest(ctx, "GET", path, nil, &commits)
	if err != nil {
		return nil, err
	}
	return commits, nil
}

// GetPullRequestFiles fetches files changed in a pull request.
func (c *Client) GetPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]CommitFile, error) {
	var files []CommitFile
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files", owner, repo, number)
	// We want to request up to 100 files for a reasonable limit
	q := params{
		"per_page": "100",
	}
	err := c.doRequest(ctx, "GET", path, q, &files)
	if err != nil {
		return nil, err
	}
	return files, nil
}

// GetCommit fetches a single commit details, including its files.
func (c *Client) GetCommit(ctx context.Context, owner, repo, sha string) (*RepositoryCommit, []CommitFile, error) {
	var fullCommit struct {
		RepositoryCommit
		Files []CommitFile `json:"files"`
	}
	path := fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, sha)
	err := c.doRequest(ctx, "GET", path, nil, &fullCommit)
	if err != nil {
		return nil, nil, err
	}
	return &fullCommit.RepositoryCommit, fullCommit.Files, nil
}

// filterLatestCheckRuns keeps only the latest check run for each unique check name (matching the GitHub web UI behavior).
func filterLatestCheckRuns(runs []CheckRun) []CheckRun {
	latest := make(map[string]CheckRun)
	var namesOrder []string

	for _, r := range runs {
		key := r.Name
		if existing, exists := latest[key]; exists {
			// Keep the check run with the later StartedAt timestamp. If identical, keep the one with higher ID.
			if r.StartedAt.After(existing.StartedAt) || (r.StartedAt.Equal(existing.StartedAt) && r.ID > existing.ID) {
				latest[key] = r
			}
		} else {
			latest[key] = r
			namesOrder = append(namesOrder, key)
		}
	}

	var result []CheckRun
	for _, name := range namesOrder {
		result = append(result, latest[name])
	}
	return result
}

// GetCheckRuns fetches check runs for a specific commit reference (SHA/branch) and filters duplicates.
func (c *Client) GetCheckRuns(ctx context.Context, owner, repo, ref string) ([]CheckRun, error) {
	var checkRunsResp CheckRunsResponse
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", owner, repo, ref)
	err := c.doRequest(ctx, "GET", path, nil, &checkRunsResp)
	if err != nil {
		return nil, err
	}
	return filterLatestCheckRuns(checkRunsResp.CheckRuns), nil
}

type mergeRequest struct {
	CommitTitle   string `json:"commit_title,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
	MergeMethod   string `json:"merge_method"` // merge, squash, rebase
}

// MergePullRequest merges a pull request with the specified method.
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, number int, commitTitle, commitMessage, mergeMethod string) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, repo, number)
	body := mergeRequest{
		CommitTitle:   commitTitle,
		CommitMessage: commitMessage,
		MergeMethod:   mergeMethod,
	}
	// PUT request, expecting 200 OK or failure
	var response any
	return c.doRequestWithBody(ctx, "PUT", path, nil, body, &response)
}

// ClosePullRequest closes a pull request without merging.
func (c *Client) ClosePullRequest(ctx context.Context, owner, repo string, number int) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	body := map[string]string{
		"state": "closed",
	}
	var response any
	return c.doRequestWithBody(ctx, "PATCH", path, nil, body, &response)
}

// GetRepoPermission checks a user's permission for a repository.
func (c *Client) GetRepoPermission(ctx context.Context, owner, repo, username string) (string, error) {
	var resp RepoPermissionResponse
	path := fmt.Sprintf("/repos/%s/%s/collaborators/%s/permission", owner, repo, username)
	err := c.doRequest(ctx, "GET", path, nil, &resp)
	if err != nil {
		return "", err
	}
	return resp.Permission, nil
}

// GetPendingDeployments fetches pending deployments for a workflow run.
func (c *Client) GetPendingDeployments(ctx context.Context, owner, repo string, runID int64) ([]PendingDeployment, error) {
	var resp []PendingDeployment
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/pending_deployments", owner, repo, runID)
	err := c.doRequest(ctx, "GET", path, nil, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ApproveWorkflowRun approves a workflow run from a fork.
func (c *Client) ApproveWorkflowRun(ctx context.Context, owner, repo string, runID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/approve", owner, repo, runID)
	return c.doRequestWithBody(ctx, "POST", path, nil, nil, nil)
}

type environmentApprovalRequest struct {
	EnvironmentIDs []int64 `json:"environment_ids"`
	State          string  `json:"state"` // approved or rejected
	Comment        string  `json:"comment"`
}

// ApprovePendingDeployments approves pending deployments for a workflow run.
func (c *Client) ApprovePendingDeployments(ctx context.Context, owner, repo string, runID int64, envIDs []int64, comment string) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/pending_deployments", owner, repo, runID)
	body := environmentApprovalRequest{
		EnvironmentIDs: envIDs,
		State:          "approved",
		Comment:        comment,
	}
	return c.doRequestWithBody(ctx, "POST", path, nil, body, nil)
}


