package ui

import (
	"strings"
	"testing"
)

// TestValidateBrowserURL is the guard added for the URL opener: only the two
// schemes a GitHub link can legitimately use may reach the system handler.
func TestValidateBrowserURL(t *testing.T) {
	valid := []string{
		"https://github.com/owner/repo/pull/16",
		"http://localhost:8080/x",
		"HTTPS://GITHUB.COM/owner/repo",
		"https://github.com/owner/repo/actions/runs/1/job/2?pr=3#step:4:5",
	}
	for _, raw := range valid {
		if err := ValidateBrowserURL(raw); err != nil {
			t.Errorf("ValidateBrowserURL(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"ftp://example.com/x",
		"vscode://file/etc/passwd",
		"mailto:someone@example.com",
		"/etc/passwd",
		"http://",
		"https://",
		"github.com/owner/repo",
		"",
		"://not a url",
	}
	for _, raw := range invalid {
		if err := ValidateBrowserURL(raw); err == nil {
			t.Errorf("ValidateBrowserURL(%q) = nil, want an error", raw)
		}
	}
}

// TestValidateBrowserURLErrorsAreDescriptive keeps the message useful, because
// it ends up in the footer.
func TestValidateBrowserURLErrorsAreDescriptive(t *testing.T) {
	err := ValidateBrowserURL("file:///etc/passwd")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "file") {
		t.Errorf("error %q does not name the rejected scheme", err)
	}
}
