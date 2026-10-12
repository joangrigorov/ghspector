package gh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_GetJobLogsIsBounded(t *testing.T) {
	// A log larger than the cap: the reader must not hold all of it.
	var body strings.Builder
	line := strings.Repeat("x", 99) + "\n"
	for body.Len() < int(maxLogBytes)+(2<<20) {
		body.WriteString(line)
	}
	oversized := body.String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(oversized))
	}))
	defer server.Close()

	client := NewClient("token", server.URL)
	got, err := client.GetJobLogs(context.Background(), "acme", "api", 42)
	if err != nil {
		t.Fatalf("GetJobLogs: %v", err)
	}

	if len(got) > int(maxLogBytes)+len(truncationNote) {
		t.Errorf("read %d bytes, want at most the cap plus the note (%d)", len(got), maxLogBytes+len(truncationNote))
	}
	if !strings.HasSuffix(got, truncationNote) {
		t.Error("a truncated log was not marked as truncated")
	}
	if !strings.HasSuffix(strings.TrimSuffix(got, truncationNote), "\n") {
		t.Error("the truncation did not land on a line boundary")
	}
}

func TestClient_GetJobLogsLeavesAShortLogAlone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("2024-05-01T10:00:00Z hello\n"))
	}))
	defer server.Close()

	client := NewClient("token", server.URL)
	got, err := client.GetJobLogs(context.Background(), "acme", "api", 42)
	if err != nil {
		t.Fatalf("GetJobLogs: %v", err)
	}
	want := "2024-05-01T10:00:00Z hello\n"
	if got != want {
		t.Errorf("GetJobLogs = %q, want %q", got, want)
	}
}

func TestReadCapped(t *testing.T) {
	cases := []struct {
		name  string
		input string
		limit int64
		want  string
	}{
		{"below the cap", "one\ntwo\n", 100, "one\ntwo\n"},
		{"exactly the cap", "12345", 5, "12345"},
		{"cut at a line boundary", "aaa\nbbb\n", 5, "aaa\n" + truncationNote},
		{"no boundary to cut on", "aaaaaa", 3, "aaa" + truncationNote},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readCapped(strings.NewReader(tc.input), tc.limit)
			if err != nil {
				t.Fatalf("readCapped: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("readCapped(%q, %d) = %q, want %q", tc.input, tc.limit, got, tc.want)
			}
		})
	}
}
