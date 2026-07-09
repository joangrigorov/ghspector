package gh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
			w.Write([]byte("Job logs line 1\nJob logs line 2"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)

	runs, err := client.GetWorkflowRuns(context.Background(), "owner", "repo", 1, 10)
	if err != nil {
		t.Fatalf("unexpected error fetching runs: %v", err)
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
