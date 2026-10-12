package ui

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// OpenBrowser opens a URL in the user's default browser.
//
// Only http and https are accepted. Every call site passes an API-supplied
// HTMLURL or a URL this program assembled, so the check is not covering a known
// bug; it closes the boundary. Without it a malformed or hostile response could
// hand an arbitrary scheme to the OS handler, and those handlers execute
// registered scheme commands rather than treating the string as data.
func OpenBrowser(raw string) error {
	if err := ValidateBrowserURL(raw); err != nil {
		return err
	}
	return openWithSystemHandler(raw)
}

// ValidateBrowserURL reports whether a URL is safe to hand to the system
// opener.
func ValidateBrowserURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		if u.Scheme == "" {
			return errors.New("refusing to open a URL with no scheme")
		}
		return fmt.Errorf("refusing to open a URL with scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("refusing to open a URL with no host")
	}
	return nil
}

// openWithSystemHandler launches the platform opener without waiting for it.
func openWithSystemHandler(raw string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	case "darwin":
		cmd = exec.Command("open", raw)
	default: // linux, freebsd, openbsd, netbsd
		cmd = exec.Command("xdg-open", raw)
	}
	return cmd.Start()
}
