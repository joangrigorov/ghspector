package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestPollingConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ghspector-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.Setenv("APPDATA", tmpDir)
	defer os.Unsetenv("APPDATA")

	cfg := &Config{
		GitHubToken: "test",
		Polling: PollingConfig{
			WorkflowsIntervalSeconds: 15,
			PRsIntervalSeconds:       25,
		},
	}

	err = SaveConfig(cfg)
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	path, err := ResolveConfigPath()
	if err != nil {
		t.Fatalf("failed to resolve config path: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Polling.WorkflowsIntervalSeconds != 15 {
		t.Errorf("expected WorkflowsIntervalSeconds 15, got %d", loaded.Polling.WorkflowsIntervalSeconds)
	}
	if loaded.Polling.PRsIntervalSeconds != 25 {
		t.Errorf("expected PRsIntervalSeconds 25, got %d", loaded.Polling.PRsIntervalSeconds)
	}
}

func TestConfigVersion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ghspector-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.Setenv("APPDATA", tmpDir)
	defer os.Unsetenv("APPDATA")

	path, err := ResolveConfigPath()
	if err != nil {
		t.Fatalf("failed to resolve config path: %v", err)
	}

	t.Run("SaveConfig stamps the current version", func(t *testing.T) {
		cfg := &Config{GitHubToken: "test"}
		if err := SaveConfig(cfg); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}
		if cfg.Version != CurrentConfigVersion {
			t.Errorf("expected saved config version %d, got %d", CurrentConfigVersion, cfg.Version)
		}

		loaded, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}
		if loaded.Version != CurrentConfigVersion {
			t.Errorf("expected loaded config version %d, got %d", CurrentConfigVersion, loaded.Version)
		}
		if loaded.GitHubToken != "test" {
			t.Errorf("expected token to survive round trip, got %q", loaded.GitHubToken)
		}
	})

	t.Run("A config without a version key is reported as legacy", func(t *testing.T) {
		legacy := []byte("github_token: legacy_token\ndefault_org: my-org\n")
		if err := os.WriteFile(path, legacy, 0600); err != nil {
			t.Fatalf("failed to write legacy config: %v", err)
		}

		loaded, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("failed to load legacy config: %v", err)
		}
		if loaded.Version != legacyConfigVersion {
			t.Errorf("expected legacy config version %d, got %d", legacyConfigVersion, loaded.Version)
		}
		if loaded.GitHubToken != "legacy_token" {
			t.Errorf("expected legacy_token, got %q", loaded.GitHubToken)
		}
		if loaded.DefaultOrg != "my-org" {
			t.Errorf("expected default_org my-org, got %q", loaded.DefaultOrg)
		}
	})
}

// TestConfigFileTokenSourceHidesThePath pins the fix for the credential source
// label: it is rendered on screen, so it must not carry the user's home
// directory.
func TestConfigFileTokenSourceHidesThePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ghspector-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")
	os.Setenv("APPDATA", tmpDir)
	defer os.Unsetenv("APPDATA")
	os.Unsetenv("GH_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")

	if saveErr := SaveConfig(&Config{GitHubToken: "config_token_abc"}); saveErr != nil {
		t.Fatalf("failed to save config: %v", saveErr)
	}

	token, cfg, err := ResolveTokenWithCliGetter(func() (string, error) {
		return "", errors.New("gh cli not logged in")
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "config_token_abc" {
		t.Fatalf("expected the config token, got %q", token)
	}
	if cfg == nil {
		t.Fatal("expected a config")
	}

	source := cfg.TokenSource
	if !strings.Contains(source, "config.yaml") {
		t.Errorf("expected the file name in the source label, got %q", source)
	}
	if strings.Contains(source, tmpDir) {
		t.Errorf("the source label leaks the config directory: %q", source)
	}
	if strings.ContainsAny(source, `/\`) {
		t.Errorf("the source label contains a path separator: %q", source)
	}
}
