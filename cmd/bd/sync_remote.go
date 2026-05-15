package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/doltremote"
)

// resolveSyncRemote returns the effective sync remote URL.
// Resolution order:
//  1. sync.remote (primary — any Dolt-compatible remote URL)
//  2. sync.git-remote (deprecated fallback)
//  3. "" (not configured)
func resolveSyncRemote() string {
	if v := config.GetString("sync.remote"); v != "" {
		return v
	}
	return config.GetString("sync.git-remote")
}

// resolveSyncRemoteFromDir is like resolveSyncRemote but reads from a
// specific beads directory's config.yaml. Used by context_cmd, doctor,
// and other paths that operate on a resolved beads dir rather than CWD.
func resolveSyncRemoteFromDir(beadsDir string) string {
	if v := config.GetStringFromDir(beadsDir, "sync.remote"); v != "" {
		return v
	}
	return config.GetStringFromDir(beadsDir, "sync.git-remote")
}

// commitBeadsConfig stages .beads/config.yaml and commits it.
// Silently no-ops if the file is clean or the commit fails (e.g. hooks,
// nothing to commit). Used by bd dolt remote add/remove to keep the
// working tree clean after persisting sync.remote.
func commitBeadsConfig(msg string) {
	addCmd := exec.Command("git", "add", ".beads/config.yaml")
	if err := addCmd.Run(); err != nil {
		return
	}
	commitCmd := exec.Command("git", "commit", "-m", msg) //nolint:gosec // G702: msg is from internal callers only, not user input
	if out, err := commitCmd.CombinedOutput(); err != nil {
		// "nothing to commit" is normal if the file was already staged
		if !strings.Contains(string(out), "nothing to commit") {
			fmt.Fprintf(os.Stderr, "Warning: failed to commit config change: %v\n", err)
		}
	}
}

func commitBeadsConfigForActiveRepo(ctx context.Context, msg string) {
	rc, err := beads.GetRepoContext()
	if err != nil {
		return
	}
	addCmd := rc.GitCmd(ctx, "add", ".beads/config.yaml")
	if err := addCmd.Run(); err != nil {
		return
	}
	commitCmd := rc.GitCmd(ctx, "commit", "-m", msg)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "nothing to commit") {
			fmt.Fprintf(os.Stderr, "Warning: failed to commit config change: %v\n", err)
		}
	}
}

// normalizeRemoteURL converts a remote URL to a Dolt-compatible format.
// Dolt-native URLs (dolthub://, file://, aws://, gs://, git+...) are
// returned as-is. Git URLs (https://, ssh://, git@...) are converted
// via gitURLToDoltRemote. Unknown schemes are returned as-is and let
// dolt clone decide.
func normalizeRemoteURL(url string) string {
	return doltremote.Normalize(url)
}
