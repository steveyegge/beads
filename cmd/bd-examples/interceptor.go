package main

import (
	"fmt"
	"strings"
)

// Interceptor provides dry-run command interception for bash scripts
type Interceptor struct {
	// Commands that modify state and should be intercepted
	interceptCommands map[string][]string

	// Mock responses for intercepted commands
	mockResponses map[string]string
}

// NewInterceptor creates a new dry-run interceptor
func NewInterceptor() *Interceptor {
	return &Interceptor{
		interceptCommands: map[string][]string{
			"bd": {"update", "close", "create", "dep", "sync"},
		},
		mockResponses: map[string]string{
			"bd create": `{"id": "bd-dry1", "title": "DRY-RUN: Created issue"}`,
			"bd update": `{"id": "bd-xxx", "status": "updated"}`,
			"bd close":  `{"id": "bd-xxx", "status": "closed"}`,
			"bd dep":    `{"dependency": "added"}`,
			"bd sync":   `Synced (dry-run)`,
		},
	}
}

// WrapScript generates a bash wrapper that intercepts state-modifying commands
func (i *Interceptor) WrapScript(scriptContent string) string {
	var wrapper strings.Builder

	wrapper.WriteString("#!/usr/bin/env bash\n")
	wrapper.WriteString("# Dry-run wrapper - intercepts state-modifying commands\n\n")

	// Create wrapper functions for each command
	for cmd, subcommands := range i.interceptCommands {
		wrapper.WriteString(i.generateWrapper(cmd, subcommands))
		wrapper.WriteString("\n")
	}

	// Export wrapper functions
	for cmd := range i.interceptCommands {
		wrapper.WriteString(fmt.Sprintf("export -f %s\n", cmd))
	}
	wrapper.WriteString("\n")

	// Add the original script content
	wrapper.WriteString("# Original script follows:\n")
	wrapper.WriteString(scriptContent)

	return wrapper.String()
}

// generateWrapper generates a bash function wrapper for a command
func (i *Interceptor) generateWrapper(cmd string, subcommands []string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s() {\n", cmd))
	sb.WriteString("    local subcmd=\"$1\"\n")
	sb.WriteString("    case \"$subcmd\" in\n")

	for _, sub := range subcommands {
		mockResp := i.mockResponses[cmd+" "+sub]
		if mockResp == "" {
			mockResp = "OK"
		}
		sb.WriteString(fmt.Sprintf("        %s)\n", sub))
		sb.WriteString(fmt.Sprintf("            echo \"[DRY-RUN] Would run: %s $@\" >&2\n", cmd))
		// For JSON output, emit a mock response
		sb.WriteString(fmt.Sprintf("            echo '%s'\n", mockResp))
		sb.WriteString("            return 0\n")
		sb.WriteString("            ;;\n")
	}

	sb.WriteString("        *)\n")
	sb.WriteString(fmt.Sprintf("            command %s \"$@\"\n", cmd))
	sb.WriteString("            ;;\n")
	sb.WriteString("    esac\n")
	sb.WriteString("}\n")

	return sb.String()
}

// BashWrapper returns a string that can be prepended to script execution
// to intercept commands. This version uses eval rather than sourcing.
func (i *Interceptor) BashWrapper() string {
	var wrapper strings.Builder

	wrapper.WriteString("# Dry-run interceptor functions\n")

	for cmd, subcommands := range i.interceptCommands {
		wrapper.WriteString(i.generateWrapper(cmd, subcommands))
		wrapper.WriteString("\n")
	}

	for cmd := range i.interceptCommands {
		wrapper.WriteString(fmt.Sprintf("export -f %s\n", cmd))
	}

	return wrapper.String()
}

// DryRunPrefix returns a bash prefix that intercepts bd commands
// This can be prepended to a script command line
func DryRunPrefix() string {
	// Simpler inline version for direct execution
	return `
bd() {
    case "$1" in
        update|close|create|dep|sync)
            echo "[DRY-RUN] Would run: bd $@" >&2
            case "$1" in
                create) echo '{"id": "bd-dry1"}';;
                *) echo '{}';;
            esac
            return 0
            ;;
        *)
            command bd "$@"
            ;;
    esac
}
export -f bd
`
}
