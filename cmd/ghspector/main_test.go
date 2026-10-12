package main

import (
	"strings"
	"testing"

	"github.com/joangrigorov/ghspector/internal/auth"
	"github.com/joangrigorov/ghspector/internal/store"
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

// TestDefaultOwnerPicksTheConfiguredContext pins how the config's default
// becomes the context the session starts in: an organization wins, an account
// is a user, and nothing configured means the authenticated user.
func TestDefaultOwnerPicksTheConfiguredContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  auth.Config
		want *store.OwnerRef
	}{
		{"nothing configured", auth.Config{}, nil},
		{"an organization", auth.Config{DefaultOrg: "acme"}, &store.OwnerRef{Login: "acme", IsOrg: true}},
		{"an account", auth.Config{DefaultAccount: "octocat"}, &store.OwnerRef{Login: "octocat"}},
		{"both, as a hand-edited file can", auth.Config{DefaultOrg: "acme", DefaultAccount: "octocat"}, &store.OwnerRef{Login: "acme", IsOrg: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultOwner(&tc.cfg)
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("defaultOwner() = %v, want %v", got, tc.want)
			}
			if got != nil && *got != *tc.want {
				t.Errorf("defaultOwner() = %+v, want %+v", *got, *tc.want)
			}
		})
	}
}

// TestNoCredentialsPrintsInstructionsAndExits pins the last clause of §2.1: when
// every source fails, the program says how to authenticate and returns 1 instead
// of falling through to a terminal that cannot fetch anything.
func TestNoCredentialsPrintsInstructionsAndExits(t *testing.T) {
	instructions := 0
	restore := resolveToken
	resolveToken = func() (string, *auth.Config, error) {
		return "", nil, auth.ErrUnauthenticated
	}
	t.Cleanup(func() { resolveToken = restore })
	restorePrint := printAuthInstructions
	printAuthInstructions = func() { instructions++ }
	t.Cleanup(func() { printAuthInstructions = restorePrint })

	var out strings.Builder
	if code := run(nil, &out); code != 1 {
		t.Errorf("run() with no credentials = %d, want 1", code)
	}
	if instructions != 1 {
		t.Errorf("instructions printed %d times, want 1", instructions)
	}
	if out.Len() != 0 {
		t.Errorf("run() wrote %q to stdout, want nothing", out.String())
	}
}
