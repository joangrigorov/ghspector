package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
			IssuesIntervalSeconds:    35,
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
	if loaded.Polling.IssuesIntervalSeconds != 35 {
		t.Errorf("expected IssuesIntervalSeconds 35, got %d", loaded.Polling.IssuesIntervalSeconds)
	}
}

// TestPollingIntervals pins the resolution rule spec §5.4 records: the per-tab
// keys win, the legacy single key covers all three when they are absent, and the
// defaults cover the rest. A non-positive value means "unset" rather than "off",
// which is how the previous version read the file.
func TestPollingIntervals(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want PollingIntervals
	}{
		{
			"defaults",
			Config{},
			PollingIntervals{
				Workflows: DefaultWorkflowsIntervalSeconds * time.Second,
				PRs:       DefaultPRsIntervalSeconds * time.Second,
				Issues:    DefaultIssuesIntervalSeconds * time.Second,
			},
		},
		{
			"the legacy key sets all three",
			Config{PollingIntervalSeconds: 7},
			PollingIntervals{7 * time.Second, 7 * time.Second, 7 * time.Second},
		},
		{
			"a per-tab key wins over the legacy one",
			Config{PollingIntervalSeconds: 7, Polling: PollingConfig{IssuesIntervalSeconds: 45}},
			PollingIntervals{7 * time.Second, 7 * time.Second, 45 * time.Second},
		},
		{
			"each tab can be retimed on its own",
			Config{Polling: PollingConfig{WorkflowsIntervalSeconds: 3, PRsIntervalSeconds: 4, IssuesIntervalSeconds: 5}},
			PollingIntervals{3 * time.Second, 4 * time.Second, 5 * time.Second},
		},
		{
			"a zero or negative value is unset, not off",
			Config{Polling: PollingConfig{WorkflowsIntervalSeconds: -1, IssuesIntervalSeconds: 0}},
			PollingIntervals{
				Workflows: DefaultWorkflowsIntervalSeconds * time.Second,
				PRs:       DefaultPRsIntervalSeconds * time.Second,
				Issues:    DefaultIssuesIntervalSeconds * time.Second,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.PollingIntervals(); got != tc.want {
				t.Errorf("PollingIntervals() = %+v, want %+v", got, tc.want)
			}
		})
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

// TestSetDefaultMergeMethod covers the one write the TUI performs: it must
// change the merge method and nothing else, because re-encoding the file would
// drop keys this build does not know about.
func TestSetDefaultMergeMethod(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)

	path, err := ResolveConfigPath()
	if err != nil {
		t.Fatalf("failed to resolve config path: %v", err)
	}

	t.Run("creates the file when there is none", func(t *testing.T) {
		if err := SetDefaultMergeMethod("rebase"); err != nil {
			t.Fatalf("SetDefaultMergeMethod: %v", err)
		}
		loaded, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("failed to load the created config: %v", err)
		}
		if loaded.DefaultMergeMethod != "rebase" {
			t.Errorf("default_merge_method = %q, want rebase", loaded.DefaultMergeMethod)
		}
		if loaded.Version != CurrentConfigVersion {
			t.Errorf("version = %d, want %d", loaded.Version, CurrentConfigVersion)
		}
	})

	t.Run("preserves the other keys and the token", func(t *testing.T) {
		file := "github_token: keep_me\n" +
			"default_org: my-org\n" +
			"polling_interval_seconds: 7\n" +
			"future_key: future_value\n"
		if err := os.WriteFile(path, []byte(file), 0600); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		if err := SetDefaultMergeMethod("merge"); err != nil {
			t.Fatalf("SetDefaultMergeMethod: %v", err)
		}

		loaded, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}
		if loaded.GitHubToken != "keep_me" {
			t.Errorf("github_token = %q, want it untouched", loaded.GitHubToken)
		}
		if loaded.DefaultOrg != "my-org" {
			t.Errorf("default_org = %q, want it untouched", loaded.DefaultOrg)
		}
		if loaded.PollingIntervalSeconds != 7 {
			t.Errorf("polling_interval_seconds = %d, want it untouched", loaded.PollingIntervalSeconds)
		}
		if loaded.DefaultMergeMethod != "merge" {
			t.Errorf("default_merge_method = %q, want merge", loaded.DefaultMergeMethod)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read config: %v", err)
		}
		if !strings.Contains(string(data), "future_key") {
			t.Errorf("a key this build does not know was dropped: %s", data)
		}
	})

	t.Run("rejects an unknown method", func(t *testing.T) {
		if err := SetDefaultMergeMethod("fast-forward"); err == nil {
			t.Fatal("SetDefaultMergeMethod accepted an unknown method")
		}
	})
}

// TestConfigFileSurface covers what §6.2 of the spec promises about the file
// itself: the modes it is created with, the warning a too-open file produces,
// and the keys that are left out rather than written empty.
func TestConfigFileSurface(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes and the permission warning are unix-only")
	}
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", tmpDir)

	path, err := ResolveConfigPath()
	if err != nil {
		t.Fatalf("failed to resolve config path: %v", err)
	}
	if err := SaveConfig(&Config{GitHubToken: "tok"}); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	t.Run("the file is 0600 and its directory is 0700", func(t *testing.T) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat the config: %v", err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Errorf("config mode = %o, want 600", got)
		}
		dir, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatalf("failed to stat the config directory: %v", err)
		}
		if got := dir.Mode().Perm(); got != 0700 {
			t.Errorf("config directory mode = %o, want 700", got)
		}
	})

	t.Run("a token-only config omits the optional keys", func(t *testing.T) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read the config: %v", err)
		}
		for _, key := range []string{"default_org", "default_account", "polling", "default_merge_method"} {
			if strings.Contains(string(data), key) {
				t.Errorf("%s is written even though it is empty:\n%s", key, data)
			}
		}
	})

	t.Run("a group-readable file warns on stderr and a 0600 file does not", func(t *testing.T) {
		var out strings.Builder
		restore := warnOut
		warnOut = &out
		defer func() { warnOut = restore }()

		if err := os.Chmod(path, 0644); err != nil {
			t.Fatalf("failed to loosen the config mode: %v", err)
		}
		if _, err := LoadConfig(path); err != nil {
			t.Fatalf("failed to load the config: %v", err)
		}
		if !strings.Contains(out.String(), "permissions are too open") {
			t.Errorf("a 0644 config produced no warning, got %q", out.String())
		}

		out.Reset()
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatalf("failed to tighten the config mode: %v", err)
		}
		if _, err := LoadConfig(path); err != nil {
			t.Fatalf("failed to load the config: %v", err)
		}
		if out.String() != "" {
			t.Errorf("a 0600 config warned anyway: %q", out.String())
		}
	})
}

func TestMergeMethodCycling(t *testing.T) {
	if got := ResolveMergeMethod(""); got != "squash" {
		t.Errorf("ResolveMergeMethod(\"\") = %q, want squash", got)
	}
	if got := ResolveMergeMethod("squash"); got != "squash" {
		t.Errorf("ResolveMergeMethod(squash) = %q, want squash", got)
	}
	if got := ResolveMergeMethod("nonsense"); got != "squash" {
		t.Errorf("ResolveMergeMethod(nonsense) = %q, want squash", got)
	}

	cases := map[string]string{"squash": "merge", "merge": "rebase", "rebase": "squash", "": "squash"}
	for from, want := range cases {
		if got := NextMergeMethod(from); got != want {
			t.Errorf("NextMergeMethod(%q) = %q, want %q", from, got, want)
		}
	}
}
