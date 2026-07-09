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
			c.serverOffset = serverTime.Sub(time.Now())
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

// GetWorkflowRuns fetches the workflow runs for a specific repository.
func (c *Client) GetWorkflowRuns(ctx context.Context, owner, repo string, page, perPage int) ([]WorkflowRun, error) {
	var wrResp WorkflowRunsResponse
	q := params{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
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
		if err := xml.Unmarshal(body, &xErr); err == nil && xErr.Code != "" {
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
