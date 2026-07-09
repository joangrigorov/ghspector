package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTokenWithCliGetter(t *testing.T) {
	// Clean env
	os.Unsetenv("GH_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")

	// Helper function for mock CLI getter
	mockCliSuccess := func() (string, error) {
		return "cli_token_123", nil
	}
	mockCliFail := func() (string, error) {
		return "", errors.New("gh cli not logged in")
	}

	t.Run("Env Variable GH_TOKEN takes highest priority", func(t *testing.T) {
		os.Setenv("GH_TOKEN", "env_token_gh")
		defer os.Unsetenv("GH_TOKEN")

		token, _, err := ResolveTokenWithCliGetter(mockCliSuccess)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "env_token_gh" {
			t.Errorf("expected env_token_gh, got %s", token)
		}
	})

	t.Run("Env Variable GITHUB_TOKEN takes priority over config/cli", func(t *testing.T) {
		os.Setenv("GITHUB_TOKEN", "env_token_github")
		defer os.Unsetenv("GITHUB_TOKEN")

		token, _, err := ResolveTokenWithCliGetter(mockCliSuccess)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "env_token_github" {
			t.Errorf("expected env_token_github, got %s", token)
		}
	})

	t.Run("Config file fallback", func(t *testing.T) {
		// We mock config path by creating a temporary config file
		tmpDir, err := os.MkdirTemp("", "ghspector-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Set config home env to make os.UserConfigDir point to our temp dir
		// On unix UserConfigDir uses XDG_CONFIG_HOME
		os.Setenv("XDG_CONFIG_HOME", tmpDir)
		defer os.Unsetenv("XDG_CONFIG_HOME")
		// On windows it uses APPDATA
		os.Setenv("APPDATA", tmpDir)
		defer os.Unsetenv("APPDATA")

		cfg := &Config{
			GitHubToken: "config_token_abc",
		}
		err = SaveConfig(cfg)
		if err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		token, _, err := ResolveTokenWithCliGetter(mockCliFail)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "config_token_abc" {
			t.Errorf("expected config_token_abc, got %s", token)
		}
	})

	t.Run("CLI fallback", func(t *testing.T) {
		// Mock config path to return empty config or no config
		tmpDir, err := os.MkdirTemp("", "ghspector-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		os.Setenv("XDG_CONFIG_HOME", tmpDir)
		defer os.Unsetenv("XDG_CONFIG_HOME")
		os.Setenv("APPDATA", tmpDir)
		defer os.Unsetenv("APPDATA")

		token, _, err := ResolveTokenWithCliGetter(mockCliSuccess)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "cli_token_123" {
			t.Errorf("expected cli_token_123, got %s", token)
		}
	})

	t.Run("All fail returns ErrUnauthenticated", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "ghspector-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "nonexistent"))
		defer os.Unsetenv("XDG_CONFIG_HOME")
		os.Setenv("APPDATA", filepath.Join(tmpDir, "nonexistent"))
		defer os.Unsetenv("APPDATA")

		_, _, err = ResolveTokenWithCliGetter(mockCliFail)
		if !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("expected ErrUnauthenticated, got %v", err)
		}
	})
}
