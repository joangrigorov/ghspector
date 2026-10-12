package main

import (
	"strings"
	"testing"

	"ghspector/internal/auth"
)

// TestVersionFlagPrintsTheBuildStampsAndExits pins spec §6.1: the flag prints
// the three stamps and returns a zero exit code. run returns before it reads a
// token or opens a terminal, so this needs neither.
func TestVersionFlagPrintsTheBuildStampsAndExits(t *testing.T) {
	for _, arg := range []string{"-v", "--v", "-version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var out strings.Builder
			if code := run([]string{arg}, &out); code != 0 {
				t.Errorf("run(%s) = %d, want 0", arg, code)
			}
			got := out.String()
			for _, want := range []string{"ghspector " + version, "commit: " + commit, "build date: " + date} {
				if !strings.Contains(got, want) {
					t.Errorf("run(%s) printed %q, missing %q", arg, got, want)
				}
			}
		})
	}
}

// TestFlagOverridesPickOneContext pins the other half of §6.1: -org and -user
// are the same choice, so each clears the other rather than leaving a config
// that names both.
func TestFlagOverridesPickOneContext(t *testing.T) {
	t.Run("no flag leaves the configured defaults", func(t *testing.T) {
		cfg := &auth.Config{DefaultOrg: "acme", DefaultAccount: "octocat"}
		applyFlagOverrides(cfg, "", "")
		if cfg.DefaultOrg != "acme" || cfg.DefaultAccount != "octocat" {
			t.Errorf("defaults changed: org=%q account=%q", cfg.DefaultOrg, cfg.DefaultAccount)
		}
	})

	t.Run("-org clears the account", func(t *testing.T) {
		cfg := &auth.Config{DefaultAccount: "octocat"}
		applyFlagOverrides(cfg, "acme", "")
		if cfg.DefaultOrg != "acme" || cfg.DefaultAccount != "" {
			t.Errorf("org=%q account=%q, want org=acme account empty", cfg.DefaultOrg, cfg.DefaultAccount)
		}
	})

	t.Run("-user clears the org", func(t *testing.T) {
		cfg := &auth.Config{DefaultOrg: "acme"}
		applyFlagOverrides(cfg, "", "octocat")
		if cfg.DefaultAccount != "octocat" || cfg.DefaultOrg != "" {
			t.Errorf("org=%q account=%q, want org empty account=octocat", cfg.DefaultOrg, cfg.DefaultAccount)
		}
	})

	t.Run("-org wins when both are given", func(t *testing.T) {
		cfg := &auth.Config{}
		applyFlagOverrides(cfg, "acme", "octocat")
		if cfg.DefaultOrg != "acme" || cfg.DefaultAccount != "" {
			t.Errorf("org=%q account=%q, want org=acme account empty", cfg.DefaultOrg, cfg.DefaultAccount)
		}
	})
}

// TestUnknownFlagFailsWithoutStartingTheProgram keeps a typo from being read as
// "no flags", which would open the TUI on the wrong context.
func TestUnknownFlagFailsWithoutStartingTheProgram(t *testing.T) {
	var out strings.Builder
	if code := run([]string{"-nope"}, &out); code == 0 {
		t.Errorf("run(-nope) = 0, want a failure exit code")
	}
}
