package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// helpAllFlag is the --all flag for the help command
var helpAllFlag bool

// registerHelpAllFlag adds the --all flag to Cobra's auto-generated help command.
// Must be called after rootCmd.InitDefaultHelpCmd() has run (i.e., after first Execute
// or explicit init). We hook it in init() after all subcommands are registered.
func registerHelpAllFlag() {
	// Cobra lazily creates the help command. We need to find it.
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "help" {
			cmd.Flags().BoolVar(&helpAllFlag, "all", false, "Show help for all commands in a single document")

			// Wrap the existing Run to check --all first
			originalRun := cmd.Run
			cmd.Run = func(cmd *cobra.Command, args []string) {
				if helpAllFlag {
					writeAllHelp(os.Stdout, rootCmd)
					return
				}
				if originalRun != nil {
					originalRun(cmd, args)
				}
			}
			return
		}
	}
}

// writeAllHelp writes a complete markdown reference for all commands,
// generated from the live Cobra command tree.
func writeAllHelp(w io.Writer, root *cobra.Command) {
	if err := writeAllHelpInternal(w, root); err != nil {
		if err2 := writef(os.Stderr, "Error writing help: %v\n", err); err2 != nil {
			return
		}
	}
}

func writeAllHelpInternal(w io.Writer, root *cobra.Command) error {
	if err := writef(w, "# bd — Complete Command Reference\n\n"); err != nil {
		return err
	}
	if err := writef(w, "Generated from `bd help --all` (bd version %s)\n\n", Version); err != nil {
		return err
	}

	// Collect commands grouped by their GroupID
	type group struct {
		title    string
		commands []*cobra.Command
	}

	// Build ordered group list from root's groups
	groups := root.Groups()
	groupMap := make(map[string]*group, len(groups))
	var orderedGroups []*group
	for _, g := range groups {
		grp := &group{title: g.Title}
		groupMap[g.ID] = grp
		orderedGroups = append(orderedGroups, grp)
	}

	// Ungrouped commands (if any)
	var ungrouped []*group

	for _, cmd := range root.Commands() {
		if !cmd.IsAvailableCommand() && cmd.Name() != "help" {
			continue
		}
		if gid := cmd.GroupID; gid != "" {
			if grp, ok := groupMap[gid]; ok {
				grp.commands = append(grp.commands, cmd)
			}
		} else {
			// Ungrouped
			if len(ungrouped) == 0 {
				ungrouped = append(ungrouped, &group{title: "Other Commands:"})
			}
			ungrouped[0].commands = append(ungrouped[0].commands, cmd)
		}
	}

	// Table of contents
	if err := writef(w, "## Table of Contents\n\n"); err != nil {
		return err
	}
	allGroups := append(orderedGroups, ungrouped...)
	for _, grp := range allGroups {
		if len(grp.commands) == 0 {
			continue
		}
		if err := writef(w, "### %s\n\n", grp.title); err != nil {
			return err
		}
		for _, cmd := range grp.commands {
			anchor := "bd-" + strings.ReplaceAll(cmd.Name(), "-", "-")
			if err := writef(w, "- [bd %s](#%s) — %s\n", cmd.Name(), anchor, cmd.Short); err != nil {
				return err
			}
			// Include subcommands in TOC
			for _, sub := range cmd.Commands() {
				if !sub.IsAvailableCommand() {
					continue
				}
				subAnchor := "bd-" + cmd.Name() + "-" + strings.ReplaceAll(sub.Name(), "-", "-")
				if err := writef(w, "  - [bd %s %s](#%s) — %s\n", cmd.Name(), sub.Name(), subAnchor, sub.Short); err != nil {
					return err
				}
			}
		}
		if err := writef(w, "\n"); err != nil {
			return err
		}
	}

	// Global flags (once)
	if err := writef(w, "---\n\n## Global Flags\n\n"); err != nil {
		return err
	}
	if err := writef(w, "These flags apply to all commands:\n\n"); err != nil {
		return err
	}
	if err := writef(w, "```\n"); err != nil {
		return err
	}
	if err := writef(w, "%s", root.PersistentFlags().FlagUsages()); err != nil {
		return err
	}
	if err := writef(w, "```\n\n"); err != nil {
		return err
	}

	// Command details
	if err := writef(w, "---\n\n"); err != nil {
		return err
	}
	for _, grp := range allGroups {
		if len(grp.commands) == 0 {
			continue
		}
		if err := writef(w, "## %s\n\n", grp.title); err != nil {
			return err
		}
		for _, cmd := range grp.commands {
			if err := writeCommandHelp(w, cmd, "bd", 3); err != nil {
				return err
			}
		}
	}

	return nil
}

// writeCommandHelp writes markdown help for a single command and its subcommands.
func writeCommandHelp(w io.Writer, cmd *cobra.Command, parentPath string, depth int) error {
	fullPath := parentPath + " " + cmd.Name()
	heading := strings.Repeat("#", depth)

	if err := writef(w, "%s %s\n\n", heading, fullPath); err != nil {
		return err
	}

	// Description
	if cmd.Long != "" {
		if err := writef(w, "%s\n\n", cmd.Long); err != nil {
			return err
		}
	} else if cmd.Short != "" {
		if err := writef(w, "%s\n\n", cmd.Short); err != nil {
			return err
		}
	}

	// Usage
	if err := writef(w, "```\n%s\n```\n\n", strings.TrimRight(cmd.UseLine(), " ")); err != nil {
		return err
	}

	// Aliases
	if len(cmd.Aliases) > 0 {
		if err := writef(w, "**Aliases:** %s\n\n", strings.Join(cmd.Aliases, ", ")); err != nil {
			return err
		}
	}

	// Examples
	if cmd.Example != "" {
		if err := writef(w, "**Examples:**\n\n```bash\n%s\n```\n\n", cmd.Example); err != nil {
			return err
		}
	}

	// Local flags (not inherited/global)
	localFlags := cmd.NonInheritedFlags()
	if localFlags.HasFlags() {
		if err := writef(w, "**Flags:**\n\n```\n%s```\n\n", localFlags.FlagUsages()); err != nil {
			return err
		}
	}

	// Subcommands
	subCmds := cmd.Commands()
	hasVisibleSubs := false
	for _, sub := range subCmds {
		if sub.IsAvailableCommand() {
			hasVisibleSubs = true
			break
		}
	}

	if hasVisibleSubs {
		for _, sub := range subCmds {
			if !sub.IsAvailableCommand() {
				continue
			}
			if err := writeCommandHelp(w, sub, fullPath, depth+1); err != nil {
				return err
			}
		}
	}

	return nil
}

func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}
