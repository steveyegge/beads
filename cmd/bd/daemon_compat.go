package main

import "fmt"

// Backward-compatibility stubs for the removed daemon/RPC layer.
// The --no-daemon flag is kept so existing hooks and configs don't break.

var noDaemon bool // --no-daemon flag (deprecated, always direct mode now)

func init() {
	rootCmd.PersistentFlags().BoolVar(&noDaemon, "no-daemon", false, "(deprecated) All operations use direct mode")
}

// boolToFlag returns flag if condition is true, empty string otherwise.
func boolToFlag(condition bool, flag string) string {
	if condition {
		return flag
	}
	return ""
}

// truncateString truncates s to maxLen bytes.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// formatUptime formats a duration in seconds as a human-readable string.
func formatUptime(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.0fs", seconds)
	}
	if seconds < 3600 {
		m := int(seconds) / 60
		s := int(seconds) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	if seconds < 86400 {
		h := int(seconds) / 3600
		m := (int(seconds) % 3600) / 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
	d := int(seconds) / 86400
	h := (int(seconds) % 86400) / 3600
	return fmt.Sprintf("%dd %dh", d, h)
}
