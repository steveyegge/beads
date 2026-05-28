package doltutil

import "strings"

// ShellQuote returns s wrapped in single quotes with any embedded single
// quotes escaped, making it safe to interpolate into a shell command string.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// IsSSHURL returns true if the URL uses SSH transport.
// Matches git+ssh://, ssh://, and git@host: patterns.
func IsSSHURL(url string) bool {
	return strings.HasPrefix(url, "git+ssh://") ||
		strings.HasPrefix(url, "ssh://") ||
		strings.HasPrefix(url, "git@")
}

// IsGitProtocolURL returns true if the URL uses the git wire protocol.
// This includes SSH transports (git+ssh://, ssh://, git@host:) and
// git-over-HTTPS (git+https://) and plain git:// protocol.
func IsGitProtocolURL(url string) bool {
	return IsSSHURL(url) ||
		strings.HasPrefix(url, "git+https://") ||
		strings.HasPrefix(url, "git+http://") ||
		strings.HasPrefix(url, "git://")
}
