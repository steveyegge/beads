package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/ui"
)

// isFreshCloneError checks if the error is due to a fresh clone scenario
// where the database exists but is missing required config (like issue_prefix).
// This happens when someone clones a repo with beads but needs to initialize.
func isFreshCloneError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for the specific migration invariant error pattern
	return strings.Contains(errStr, "post-migration validation failed") &&
		strings.Contains(errStr, "required config key missing: issue_prefix")
}

// handleFreshCloneError displays a helpful message when a fresh clone is detected
// and returns true if the error was handled (so caller should exit).
// If not a fresh clone error, returns false and does nothing.
func handleFreshCloneError(err error) bool {
	if !isFreshCloneError(err) {
		return false
	}

	fmt.Fprintf(os.Stderr, "Error: Database not initialized\n\n")
	fmt.Fprintf(os.Stderr, "This appears to be a fresh clone or the database needs initialization.\n")
	fmt.Fprintf(os.Stderr, "\nTo initialize a new database, run:\n")
	fmt.Fprintf(os.Stderr, "  bd init --prefix <your-prefix>\n\n")
	fmt.Fprintf(os.Stderr, "For more information: bd init --help\n")
	return true
}

// isDatabaseNotFoundError checks if the error indicates the database
// doesn't exist on the Dolt server. This happens when switching git branches
// where the local dolt database hasn't been created yet.
func isDatabaseNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "database") &&
		strings.Contains(errStr, "not found") &&
		strings.Contains(errStr, "dolt server")
}

// hasBackupFiles checks if there are backup JSONL files in .beads/backup/
// that could be restored. Returns the backup directory path and issue count
// if backups exist, or empty string and 0 if not.
func hasBackupFiles() (backupPath string, issueCount int) {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		beadsDir = ".beads"
	}
	return detectBackupFiles(beadsDir)
}

// handleDatabaseNotFoundError checks for backup files when the database is not found
// and offers to restore from backup. Returns true if the error was handled.
func handleDatabaseNotFoundError(err error) bool {
	if !isDatabaseNotFoundError(err) {
		return false
	}

	backupDir, issueCount := hasBackupFiles()
	if backupDir == "" || issueCount == 0 {
		// No backups available - show standard error
		return false
	}

	// Backups exist - offer to restore
	fmt.Fprintf(os.Stderr, "%s Database not found, but backup files exist\n\n",
		ui.RenderWarn("!"))
	fmt.Fprintf(os.Stderr, "  Backup location: %s\n", backupDir)
	fmt.Fprintf(os.Stderr, "  Issues in backup: %d\n\n", issueCount)
	fmt.Fprintf(os.Stderr, "This can happen when switching git branches where the dolt database\n")
	fmt.Fprintf(os.Stderr, "hasn't been created yet, but backup files exist in git.\n\n")

	// Offer restore options
	fmt.Fprintf(os.Stderr, "Options:\n")
	fmt.Fprintf(os.Stderr, "  1) Restore from backup:  bd init && bd backup restore\n")
	fmt.Fprintf(os.Stderr, "  2) Start fresh:          bd init --prefix <prefix>\n\n")

	// Check if running interactively (stdin is a terminal)
	if isInteractiveTerminal() {
		fmt.Fprintf(os.Stderr, "Would you like to restore from backup? [Y/n]: ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "" || response == "y" || response == "yes" {
			fmt.Fprintf(os.Stderr, "\nRestoring from backup...\n\n")
			return attemptAutoRestore(backupDir)
		}
	}

	return true
}

// isInteractiveTerminal checks if stdin is connected to a terminal.
func isInteractiveTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// attemptAutoRestore tries to initialize the database and restore from backup.
// Returns true on success (caller should exit), false if restore failed.
func attemptAutoRestore(backupDir string) bool {
	// We need to run bd init first, then bd backup restore
	// Since we're in the error handling path, we can't easily invoke the full
	// command flow. Instead, provide clear instructions.
	fmt.Fprintf(os.Stderr, "Run the following commands to restore:\n\n")
	fmt.Fprintf(os.Stderr, "  bd init\n")
	fmt.Fprintf(os.Stderr, "  bd backup restore %s\n\n", backupDir)
	return true
}

// isWispOperation returns true if the command operates on ephemeral wisps.
// Wisp operations auto-bypass the daemon because wisps are local-only.
// Detects:
//   - mol wisp subcommands (create, list, gc, or direct proto invocation)
//   - mol burn (only operates on wisps)
//   - mol squash (condenses wisps to digests)
//   - Commands with ephemeral issue IDs in args (bd-*-wisp-*, wisp-*, or legacy eph-*)
func isWispOperation(cmd *cobra.Command, args []string) bool {
	cmdName := cmd.Name()

	// Check command hierarchy for wisp subcommands
	// bd mol wisp → parent is "mol", cmd is "wisp"
	// bd mol wisp create → parent is "wisp", cmd is "create"
	if cmd.Parent() != nil {
		parentName := cmd.Parent().Name()
		// Direct wisp command or subcommands under wisp
		if parentName == "wisp" || cmdName == "wisp" {
			return true
		}
		// mol burn and mol squash are wisp-only operations
		if parentName == "mol" && (cmdName == "burn" || cmdName == "squash") {
			return true
		}
	}

	// Check for ephemeral issue IDs in arguments
	// Ephemeral IDs have "wisp" segment: bd-wisp-xxx, gt-wisp-xxx, wisp-xxx
	// Also detect legacy "eph" prefix for backwards compatibility
	for _, arg := range args {
		// Skip flags
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// Check for ephemeral prefix patterns (wisp-* or legacy eph-*)
		if strings.Contains(arg, "-wisp-") || strings.HasPrefix(arg, "wisp-") ||
			strings.Contains(arg, "-eph-") || strings.HasPrefix(arg, "eph-") {
			return true
		}
	}

	return false
}
