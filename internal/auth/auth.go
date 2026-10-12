package auth

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// PollingConfig holds configuration for background polling intervals.
type PollingConfig struct {
	WorkflowsIntervalSeconds int `yaml:"workflows_interval_seconds,omitempty"`
	PRsIntervalSeconds       int `yaml:"prs_interval_seconds,omitempty"`
}

// Default poll intervals, in seconds, used when the config file does not set
// them.
const (
	DefaultWorkflowsIntervalSeconds = 5
	DefaultPRsIntervalSeconds       = 10
)

// PollingIntervals is the resolved refresh cadence of the two polled lists.
type PollingIntervals struct {
	Workflows time.Duration
	PRs       time.Duration
}

// PollingIntervals resolves the refresh cadence from the configured values.
//
// The per-tab keys win, the legacy single key sets both when they are absent,
// and the defaults cover the rest. A non-positive value means "unset" rather
// than "off", which is how the previous version read the file: polling could
// only be retimed, never disabled.
func (c Config) PollingIntervals() PollingIntervals {
	out := PollingIntervals{
		Workflows: DefaultWorkflowsIntervalSeconds * time.Second,
		PRs:       DefaultPRsIntervalSeconds * time.Second,
	}
	if c.PollingIntervalSeconds > 0 {
		legacy := time.Duration(c.PollingIntervalSeconds) * time.Second
		out.Workflows, out.PRs = legacy, legacy
	}
	if c.Polling.WorkflowsIntervalSeconds > 0 {
		out.Workflows = time.Duration(c.Polling.WorkflowsIntervalSeconds) * time.Second
	}
	if c.Polling.PRsIntervalSeconds > 0 {
		out.PRs = time.Duration(c.Polling.PRsIntervalSeconds) * time.Second
	}
	return out
}

// CurrentConfigVersion is the configuration schema version written by this build.
const CurrentConfigVersion = 2

// legacyConfigVersion is the version reported for config files written before
// the version key existed.
const legacyConfigVersion = 1

// Config holds the configuration options.
type Config struct {
	GitHubToken            string        `yaml:"github_token"`
	DefaultOrg             string        `yaml:"default_org,omitempty"`
	DefaultAccount         string        `yaml:"default_account,omitempty"`
	PollingIntervalSeconds int           `yaml:"polling_interval_seconds,omitempty"`
	Polling                PollingConfig `yaml:"polling,omitempty"`
	DefaultMergeMethod     string        `yaml:"default_merge_method,omitempty"`
	Version                int           `yaml:"version,omitempty"`
	TokenSource            string        `yaml:"-"`
}

// ErrUnauthenticated is returned when no GitHub token is found.
var ErrUnauthenticated = errors.New("github token not found")

// ResolveConfigPath returns the path to config.yaml depending on user OS.
func ResolveConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "ghspector", "config.yaml"), nil
}

// LoadConfig reads the config file from disk.
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Check file permissions on non-Windows platforms.
	if runtime.GOOS != "windows" {
		info, err := file.Stat()
		if err == nil {
			// Warn if permissions are too open (e.g. group/other write or read access)
			if info.Mode().Perm()&0077 != 0 {
				fmt.Fprintln(os.Stderr, "Warning: Config file permissions are too open. Please restrict access: chmod 600 config.yaml")
			}
		}
	}

	var cfg Config
	dec := yaml.NewDecoder(file)
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Version == 0 {
		cfg.Version = legacyConfigVersion
	}
	return &cfg, nil
}

// SaveConfig writes the config to config.yaml with restricted permissions.
func SaveConfig(cfg *Config) error {
	path, err := ResolveConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return err
	}

	// The file now carries the current schema, so stamp it before writing.
	cfg.Version = CurrentConfigVersion

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// ResolveToken retrieves a GitHub token using the fallback strategy.
func ResolveToken() (string, *Config, error) {
	return ResolveTokenWithCliGetter(getGhCliToken)
}

// ResolveTokenWithCliGetter retrieves a GitHub token using the fallback strategy, with a custom CLI getter for testing.
func ResolveTokenWithCliGetter(cliGetter func() (string, error)) (string, *Config, error) {
	// 1. Env variables
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token, &Config{GitHubToken: token, TokenSource: "GH_TOKEN environment variable"}, nil
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, &Config{GitHubToken: token, TokenSource: "GITHUB_TOKEN environment variable"}, nil
	}

	// 2. Config File
	var loadedCfg *Config
	if path, err := ResolveConfigPath(); err == nil {
		if cfg, err := LoadConfig(path); err == nil && cfg.GitHubToken != "" {
			// Report the file name only. The full path contains the user's home
			// directory, and this label is rendered on screen.
			cfg.TokenSource = "config file: " + filepath.Base(path)
			return cfg.GitHubToken, cfg, nil
		}
		loadedCfg = &Config{}
	}

	// 3. GitHub CLI fallback
	if token, err := cliGetter(); err == nil && token != "" {
		if loadedCfg == nil {
			loadedCfg = &Config{}
		}
		loadedCfg.GitHubToken = token
		loadedCfg.TokenSource = "GitHub CLI (gh)"
		return token, loadedCfg, nil
	}

	return "", nil, ErrUnauthenticated
}

// getGhCliToken runs `gh auth token` and returns the output.
func getGhCliToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	var stdout strings.Builder
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	token := strings.TrimSpace(stdout.String())
	if token == "" {
		return "", errors.New("empty token returned from gh cli")
	}
	return token, nil
}

// PrintAuthInstructions prints how to authenticate if none found.
func PrintAuthInstructions() {
	fmt.Fprintln(os.Stderr, "Error: GitHub token not found.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "To authenticate ghspector, please perform one of the following steps:")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "1. Authenticate via GitHub CLI (recommended):")
	fmt.Fprintln(os.Stderr, "   $ gh auth login --scopes \"repo,workflow,read:org\"")
	fmt.Fprintln(os.Stderr, "   If you are already logged in but need write permissions (for merging PRs or approving runs), run:")
	fmt.Fprintln(os.Stderr, "   $ gh auth refresh -s repo -s workflow")
	fmt.Fprintln(os.Stderr, "   (ghspector will automatically pick up your credentials)")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "2. Set the GH_TOKEN environment variable:")
	fmt.Fprintln(os.Stderr, "   $ export GH_TOKEN=your_personal_access_token")
	fmt.Fprintln(os.Stderr, "   Note: To merge pull requests and approve runs, the token must have the 'repo' and 'workflow' scopes.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "3. Create a configuration file at ~/.config/ghspector/config.yaml:")
	fmt.Fprintln(os.Stderr, "   github_token: \"your_personal_access_token\"")
}
