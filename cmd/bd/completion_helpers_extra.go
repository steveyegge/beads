package main

import "github.com/spf13/cobra"

// noCompletions disables shell completion and file completion for commands
// that operate only via flags.
func noCompletions(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}
