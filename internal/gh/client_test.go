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
		if r.URL.Path == "/repos/owner/repo/actions/jobs/404/logs" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><Error><Code>BlobNotFound</Code><Message>The specified blob does not exist.
RequestId:f907b3df-401e-0001-0fa1-0fc4e0000000
Time:2026-07-09T12:51:16.9150158Z</Message></Error>`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient("test-token", server.URL)

	runs, err := client.GetWorkflowRuns(context.Background(), "owner", "repo", 1, 10, "")
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

	// Test XML BlobNotFound error parsing
	_, err = client.GetJobLogs(context.Background(), "owner", "repo", 404)
	if err == nil {
		t.Fatal("expected error for job 404, got nil")
	}
	expectedErr := "github api logs error (status 404): BlobNotFound: The specified blob does not exist."
	if err.Error() != expectedErr {
		t.Errorf("expected error message %q, got %q", expectedErr, err.Error())
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
	runs, err := client.GetWorkflowRuns(context.Background(), "owner", "repo", 1, 10, "octocat")
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
