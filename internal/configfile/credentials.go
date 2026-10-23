package configfile

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultCredentialsPath returns the platform-appropriate default credentials file path.
// Linux/macOS: ~/.config/beads/credentials
// Windows: %APPDATA%\beads\credentials
func DefaultCredentialsPath() string {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "beads", "credentials")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "beads", "credentials")
}

// LookupCredentialsPassword reads a password from an INI-style credentials file,
// keyed by [host:port] section. Returns empty string if not found or on any error.
//
// File format:
//
//	[127.0.0.1:3307]
//	password=localDevPassword
//
//	[beads.company.com:3307]
//	password=teamServerPassword
//
// The file path is determined by:
//  1. BEADS_CREDENTIALS_FILE env var (if set)
//  2. Default platform path (see DefaultCredentialsPath)
func LookupCredentialsPassword(host string, port int) string {
	credFile := os.Getenv("BEADS_CREDENTIALS_FILE")
	if credFile == "" {
		credFile = DefaultCredentialsPath()
	}
	if credFile == "" {
		return ""
	}

	for _, key := range credentialsSectionKeys(host, port) {
		if pw := readPasswordFromFile(credFile, key); pw != "" {
			return pw
		}
	}
	return ""
}

// credentialsSectionKeys returns the [host:port] section names that hold the
// password for the Dolt server at host and port, most canonical first. An
// IPv6 literal host may be configured bare ("::1") or bracketed ("[::1]"),
// and both name the same server, so either spelling finds a section written
// as [[::1]:3307] (the net.JoinHostPort form) or as [::1:3307] (the form a
// bare host was looked up under before). Any other host has one key.
func credentialsSectionKeys(host string, port int) []string {
	bare := UnbracketHost(host)
	joined := net.JoinHostPort(bare, fmt.Sprint(port))
	legacy := fmt.Sprintf("%s:%d", bare, port)
	if legacy == joined {
		return []string{joined}
	}
	return []string{joined, legacy}
}

// UnbracketHost returns host without the square brackets an IPv6 literal may
// be configured in ("[::1]" is "::1"), which is the form net.JoinHostPort
// takes. Any other host is returned as it is.
func UnbracketHost(host string) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

// readPasswordFromFile parses an INI-style credentials file and returns the
// password for the given [host:port] section. Returns empty string on any error.
func readPasswordFromFile(path string, sectionKey string) string {
	f, err := os.Open(path) //nolint:gosec // path comes from env var or os.UserHomeDir, not user input
	if err != nil {
		return ""
	}
	defer f.Close()

	// Warn if file has overly permissive permissions (unix only)
	warnIfInsecurePermissions(path)

	scanner := bufio.NewScanner(f)
	inSection := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Strip inline comments
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		// Section header: [host:port]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := line[1 : len(line)-1]
			if section == sectionKey {
				inSection = true
			} else if inSection {
				// We've left our section without finding a password
				break
			}
			continue
		}

		// Key=value within our section
		if inSection {
			key, value, found := strings.Cut(line, "=")
			if found && strings.TrimSpace(key) == "password" {
				return strings.TrimSpace(value)
			}
		}
	}

	return ""
}
