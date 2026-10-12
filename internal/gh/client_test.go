package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClient_GetUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		user := User{Login: "test-user", ID: 1}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		json.NewEncoder(w).Encode(user)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	user, err := client.GetUser(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Login != "test-user" || user.ID != 1 {
		t.Errorf("unexpected user data: %+v", user)
	}

	rl := client.GetRateLimit()
	if rl.Limit != 5000 || rl.Remaining != 4999 {
		t.Errorf("unexpected rate limit: %+v", rl)
	}
}

func TestClient_RateLimitBackup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "2000000000") // way in the future
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	// Initial request to populate rate limits
	_, err := client.GetUser(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}

	// Next request should fail immediately or back off. Since reset is way in the future, it should return rate limited err.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = client.GetUser(ctx)
	if err == nil || !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected immediate ErrRateLimited due to local client rate limit block, got: %v", err)
	}
}

func TestClient_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	_, err := client.GetOrgs(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestClient_GetWorkflowRunsAndJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/owner/repo/actions/runs" {
			runsResp := WorkflowRunsResponse{
				TotalCount: 1,
				WorkflowRuns: []WorkflowRun{
					{ID: 101, Name: "Build", RunNumber: 5, Status: "completed", Conclusion: "success"},
				},
			}
			json.NewEncoder(w).Encode(runsResp)
			return
		}
		if r.URL.Path == "/repos/owner/repo/actions/runs/101/jobs" {
			jobsResp := WorkflowJobsResponse{
				TotalCount: 1,
				Jobs: []WorkflowJob{
					{ID: 201, RunID: 101, Name: "test-job", Status: "completed", Conclusion: "success"},
				},
			}
			json.NewEncoder(w).Encode(jobsResp)
			return
		}
		if r.URL.Path == "/repos/owner/repo/actions/jobs/201/logs" {
			_, _ = w.Write([]byte("Job logs line 1\nJob logs line 2"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)

	runs, total, err := client.GetWorkflowRuns(context.Background(), "owner", "repo", 1, 10, "")
	if err != nil {
		t.Fatalf("unexpected error fetching runs: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(runs) != 1 || runs[0].ID != 101 {
		t.Errorf("expected run ID 101, got: %+v", runs)
	}

	jobs, err := client.GetWorkflowRunJobs(context.Background(), "owner", "repo", 101)
	if err != nil {
		t.Fatalf("unexpected error fetching jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != 201 {
		t.Errorf("expected job ID 201, got: %+v", jobs)
	}

	logs, err := client.GetJobLogs(context.Background(), "owner", "repo", 201)
	if err != nil {
		t.Fatalf("unexpected error fetching logs: %v", err)
	}
	if logs != "Job logs line 1\nJob logs line 2" {
		t.Errorf("unexpected logs: %s", logs)
	}
}

// TestClient_JobLogErrorsAreParsedAsXML pins the one endpoint that answers with
// XML: the log download is served by a blob store, whose error body carries a
// Code and a Message rather than the API's JSON. The message is read to its
// first line, because the rest of it is a request id and a timestamp. A body
// that is not that document falls back to the raw text, so an error is never
// swallowed into an empty message.
func TestClient_JobLogErrorsAreParsedAsXML(t *testing.T) {
	cases := []struct {
		jobID  int64
		status int
		body   string
		want   string
	}{
		{
			jobID: 404, status: http.StatusNotFound,
			body: `<?xml version="1.0" encoding="utf-8"?><Error><Code>BlobNotFound</Code><Message>The specified blob does not exist.
RequestId:f907b3df-401e-0001-0fa1-0fc4e0000000
Time:2026-07-09T12:51:16.9150158Z</Message></Error>`,
			want: "github api logs error (status 404): BlobNotFound: The specified blob does not exist.",
		},
		{
			jobID: 502, status: http.StatusBadGateway,
			body: `<html><body>Bad gateway</body></html>`,
			want: "github api logs error (status 502): <html><body>Bad gateway</body></html>",
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, tc := range cases {
			if strings.HasSuffix(r.URL.Path, fmt.Sprintf("/jobs/%d/logs", tc.jobID)) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client := NewClient("test-token", server.URL)
	for _, tc := range cases {
		_, err := client.GetJobLogs(context.Background(), "owner", "repo", tc.jobID)
		if err == nil {
			t.Fatalf("job %d: error = nil, want %q", tc.jobID, tc.want)
		}
		if err.Error() != tc.want {
			t.Errorf("job %d: error = %q, want %q", tc.jobID, err.Error(), tc.want)
		}
	}
}

func TestClient_GetWorkflowRuns_ActorFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := r.URL.Query().Get("actor")
		if actor != "octocat" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		runsResp := WorkflowRunsResponse{
			TotalCount: 1,
			WorkflowRuns: []WorkflowRun{
				{ID: 102, Name: "Filtered Build", RunAttempt: 2},
			},
		}
		json.NewEncoder(w).Encode(runsResp)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	runs, _, err := client.GetWorkflowRuns(context.Background(), "owner", "repo", 1, 10, "octocat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != 102 || runs[0].RunAttempt != 2 {
		t.Errorf("unexpected runs response: %+v", runs)
	}
}

func TestClient_GetWorkflowRunAttemptJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/actions/runs/101/attempts/2/jobs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		jobsResp := WorkflowJobsResponse{
			TotalCount: 1,
			Jobs: []WorkflowJob{
				{ID: 301, RunID: 101, Name: "attempt-2-job", Status: "completed", Conclusion: "success"},
			},
		}
		json.NewEncoder(w).Encode(jobsResp)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	jobs, err := client.GetWorkflowRunAttemptJobs(context.Background(), "owner", "repo", 101, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != 301 || jobs[0].Name != "attempt-2-job" {
		t.Errorf("unexpected jobs: %+v", jobs)
	}
}

func TestClient_GetCheckRunsDuplicateFiltering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/commits/sha123/check-runs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		t1 := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2026, 7, 10, 10, 30, 0, 0, time.UTC)

		resp := CheckRunsResponse{
			TotalCount: 4,
			CheckRuns: []CheckRun{
				{ID: 1, Name: "semantic-pr", Status: "completed", Conclusion: "failure", StartedAt: t1},
				{ID: 2, Name: "semantic-pr", Status: "completed", Conclusion: "success", StartedAt: t2},
				{ID: 3, Name: "run-tests", Status: "completed", Conclusion: "success", StartedAt: t1},
				{ID: 4, Name: "run-tests", Status: "completed", Conclusion: "failure", StartedAt: t1}, // duplicate same started_at but higher ID
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	runs, err := client.GetCheckRuns(context.Background(), "owner", "repo", "sha123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runs) != 2 {
		t.Fatalf("expected 2 filtered check runs, got: %d", len(runs))
	}

	// First should be semantic-pr (success, which is the latest started_at)
	if runs[0].Name != "semantic-pr" || runs[0].Conclusion != "success" || runs[0].ID != 2 {
		t.Errorf("expected latest semantic-pr success, got: %+v", runs[0])
	}

	// Second should be run-tests (failure, which is the higher ID with same started_at)
	if runs[1].Name != "run-tests" || runs[1].Conclusion != "failure" || runs[1].ID != 4 {
		t.Errorf("expected latest run-tests failure, got: %+v", runs[1])
	}
}

// TestClient_GetIssuesWithStateDropsPullRequests pins the rule the issues view
// depends on: the GitHub issues endpoint returns pull requests as well, and only
// the issues are returned. HasMore is reported from the raw page, before the
// drop, so a full page keeps the Load More row even when the filtered list is
// short.
func TestClient_GetIssuesWithStateDropsPullRequests(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/issues" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Issue{
			{ID: 1, Number: 1, Title: "an issue"},
			{ID: 2, Number: 2, Title: "a pull request", PullRequest: &struct {
				URL string `json:"url"`
			}{URL: "https://api.github.com/repos/owner/repo/pulls/2"}},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	issues, hasMore, err := client.GetIssuesWithState(context.Background(), "owner", "repo", "open", 1, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 1 || issues[0].Number != 1 {
		t.Fatalf("expected just the issue, got: %+v", issues)
	}
	if !hasMore {
		t.Error("HasMore = false for a page that filled the requested size")
	}
	for _, want := range []string{"state=open", "page=1", "per_page=2"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("the request asked %q, missing %q", gotQuery, want)
		}
	}
}

func TestClient_GetRepoPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/collaborators/test-user/permission" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := RepoPermissionResponse{Permission: "write"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	perm, err := client.GetRepoPermission(context.Background(), "owner", "repo", "test-user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perm != "write" {
		t.Errorf("expected write, got: %s", perm)
	}
}

func TestClient_GetPendingDeployments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/actions/runs/123/pending_deployments" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := []PendingDeployment{
			{
				CurrentUserCanApprove: true,
			},
		}
		resp[0].Environment.ID = 456
		resp[0].Environment.Name = "production"
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	deployments, err := client.GetPendingDeployments(context.Background(), "owner", "repo", 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deployments) != 1 || deployments[0].Environment.ID != 456 || !deployments[0].CurrentUserCanApprove {
		t.Errorf("unexpected deployments: %+v", deployments)
	}
}

func TestClient_ApproveWorkflowRun(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/repos/owner/repo/actions/runs/123/approve" {
			called = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	err := client.ApproveWorkflowRun(context.Background(), "owner", "repo", 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected endpoint to be called")
	}
}

func TestClient_ApprovePendingDeployments(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/repos/owner/repo/actions/runs/123/pending_deployments" {
			var body environmentApprovalRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(body.EnvironmentIDs) == 1 && body.EnvironmentIDs[0] == 456 && body.State == "approved" {
				called = true
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	err := client.ApprovePendingDeployments(context.Background(), "owner", "repo", 123, []int64{456}, "Approved via ghspector")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected endpoint to be called with correct body")
	}
}

// TestClient_CleansTerminalInstructionsFromAResponse pins the transport
// boundary: a title, a body, a branch name and a label are written by whoever
// opened the pull request, and every one of them reaches a screen as cells.
func TestClient_CleansTerminalInstructionsFromAResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"number": 1,
			"title": "a\u001b]52;c;cGF5bG9hZA==\u0007title",
			"body": "one\u001b[2Jtwo",
			"state": "open",
			"head": {"ref": "feat/\u001b[31mx"},
			"labels": [{"name": "bug\u0007fix", "color": "ff0000"}]
		}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	pr, err := client.GetPullRequest(context.Background(), "owner", "repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Title != "atitle" {
		t.Errorf("title = %q, want the instruction removed", pr.Title)
	}
	if pr.Body != "onetwo" {
		t.Errorf("body = %q, want the instruction removed", pr.Body)
	}
	if pr.Head.Ref != "feat/x" {
		t.Errorf("branch = %q, want the instruction removed", pr.Head.Ref)
	}
	if len(pr.Labels) != 1 || pr.Labels[0].Name != "bugfix" {
		t.Errorf("labels = %+v, want the instruction removed", pr.Labels)
	}
}

// TestClient_CleansTerminalInstructionsFromAnError covers the other string the
// API writes: the message the app puts in a status line.
func TestClient_CleansTerminalInstructionsFromAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden \u001b[31mnow\u001b[0m"}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	_, err := client.GetUser(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if strings.Contains(err.Error(), "\x1b") {
		t.Errorf("the error carries an escape sequence: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "forbidden now") {
		t.Errorf("error = %q, want the message text", err.Error())
	}
}

// TestClient_RateLimitResetUsesTheServersClock covers the one use of the Date
// header: the countdown to a window's end. The reset moment is the server's
// absolute time, so a local clock that is off must not move it.
func TestClient_RateLimitResetUsesTheServersClock(t *testing.T) {
	const skew = 90 * time.Second
	reset := time.Now().Add(10 * time.Minute).Truncate(time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.Header().Set("Date", time.Now().Add(skew).UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"login":"octocat","id":1}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	if _, err := client.GetUser(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	left, ok := client.RateLimitResetIn()
	if !ok {
		t.Fatal("the window's end is not reported")
	}
	want := reset.Sub(time.Now().Add(skew))
	if diff := left - want; diff > 2*time.Second || diff < -2*time.Second {
		t.Errorf("countdown = %v, want about %v", left, want)
	}
	if naive := time.Until(reset); left > naive-skew/2 {
		t.Errorf("countdown = %v, which is the local clock's answer (%v) rather than the server's", left, naive)
	}
}

// TestClient_SendsTheDocumentedHeaders covers the request contract from the spec
// on every path that builds one: a plain read, a read that writes a body, and the
// job log fetch, which does not go through doRequest.
func TestClient_SendsTheDocumentedHeaders(t *testing.T) {
	seen := make(chan http.Header, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/logs") {
			_, _ = w.Write([]byte("log output"))
			return
		}
		_, _ = w.Write([]byte(`{"number":1}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	ctx := context.Background()
	if _, err := client.GetPullRequest(ctx, "owner", "repo", 1); err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if err := client.ClosePullRequest(ctx, "owner", "repo", 1); err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	if _, err := client.GetJobLogs(ctx, "owner", "repo", 1); err != nil {
		t.Fatalf("GetJobLogs: %v", err)
	}

	for i := 0; i < 3; i++ {
		h := <-seen
		if got := h.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("request %d: Accept = %q", i, got)
		}
		if got := h.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("request %d: X-GitHub-Api-Version = %q", i, got)
		}
		if got := h.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("request %d: Authorization = %q", i, got)
		}
	}
}

// TestClient_WaitsOutAShortRateLimit covers the second half of the rule: a window
// that resets within 30 s is waited for rather than turned into an error, and the
// wait ends when the request's context does.
func TestClient_WaitsOutAShortRateLimit(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		reset := time.Now().Add(1500 * time.Millisecond).Truncate(time.Second)
		if calls > 1 {
			reset = time.Now().Add(10 * time.Minute)
		}
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"octocat","id":1}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	// The first request learns the budget; it is not waiting for anything yet.
	if _, err := client.GetUser(context.Background()); err != nil {
		t.Fatalf("priming request: %v", err)
	}

	start := time.Now()
	if _, err := client.GetUser(context.Background()); err != nil {
		t.Fatalf("the client did not wait out a short window: %v", err)
	}
	if waited := time.Since(start); waited < 300*time.Millisecond {
		t.Errorf("waited %v, want the reset to be honoured", waited)
	}

	// The same wait, abandoned when the caller's context ends first. The window
	// is set by hand so the case does not depend on how long the last request
	// took.
	client.SetRateLimit(RateLimitInfo{Limit: 5000, Remaining: 0, Reset: time.Now().Add(5 * time.Second)})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start = time.Now()
	if _, err := client.GetUser(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the context's own deadline", err)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("waited %v, want it abandoned with the context", waited)
	}
	if calls != 2 {
		t.Errorf("the server saw %d requests, want the third abandoned before it was sent", calls)
	}
}

// TestClient_MapsAnUnlistedStatus covers the catch-all: a status that is neither
// a rate limit nor a permission failure still carries the API's message.
func TestClient_MapsAnUnlistedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"Server Error"}`))
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)
	_, err := client.GetUser(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "Server Error") {
		t.Errorf("error = %q, want the status and the API's message", err.Error())
	}
	if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrForbidden) {
		t.Errorf("error = %v, want neither the rate-limit nor the forbidden classification", err)
	}
}
