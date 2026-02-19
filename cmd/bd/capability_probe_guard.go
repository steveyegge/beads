package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func validateUnknownHelpSubcommandProbe(root *cobra.Command, argv []string) error {
	if len(argv) < 3 {
		return nil
	}
	if !containsHelpFlag(argv) {
		return nil
	}

	commandIdx := firstNonFlagArg(argv)
	if commandIdx < 0 || commandIdx+1 >= len(argv) {
		return nil
	}
	parentName := strings.TrimSpace(argv[commandIdx])
	childName := strings.TrimSpace(argv[commandIdx+1])
	if parentName == "" || childName == "" || strings.HasPrefix(childName, "-") || childName == "help" {
		return nil
	}

	parent := findImmediateSubcommand(root, parentName)
	if parent == nil || len(parent.Commands()) == 0 {
		return nil
	}
	if hasChildSubcommand(parent, childName) {
		return nil
	}
	return fmt.Errorf("unknown subcommand %q for %s", childName, parent.CommandPath())
}

func containsHelpFlag(argv []string) bool {
	for _, arg := range argv {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "--help" || trimmed == "-h" {
			return true
		}
	}
	return false
}

func firstNonFlagArg(argv []string) int {
	for i, arg := range argv {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return i
	}
	return -1
}

func findImmediateSubcommand(root *cobra.Command, name string) *cobra.Command {
	for _, sub := range root.Commands() {
		if commandMatches(sub, name) {
			return sub
		}
	}
	return nil
}

func commandMatches(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}
	if cmd.Name() == name {
		return true
	}
	for _, alias := range cmd.Aliases {
		if alias == name {
			return true
		}
	}
	return false
}

func hasChildSubcommand(parent *cobra.Command, name string) bool {
	for _, child := range parent.Commands() {
		if commandMatches(child, name) {
			return true
		}
	}
	return false
}
