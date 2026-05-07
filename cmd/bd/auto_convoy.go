package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// crewActorRegex matches actor strings of the form "<rig>/crew/<name>".
// Used by the auto-convoy hook to detect when a crew member created a bead
// so a tracking convoy can be auto-attached for Mayor visibility.
//
// Examples that match:   whatsapp_automation/crew/digo, lexbh/crew/mila
// Examples that don't:   mayor, whatsapp_automation/polecats/foo, daemon
var crewActorRegex = regexp.MustCompile(`^[^/]+/crew/[^/]+$`)

// isCrewActor reports whether the given actor string represents a crew member.
func isCrewActor(actor string) bool {
	return crewActorRegex.MatchString(actor)
}

// shortIDLen is the length of the random suffix appended to auto-convoy IDs
// (matching the gastown convention of `hq-cv-<5lower>`).
const shortIDLen = 5

// generateConvoyShortID returns a 5-char lowercase base32 random suffix.
func generateConvoyShortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000"
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))[:shortIDLen]
}

// autoConvoyDescription is the canonical convoy description used by both bd's
// auto-convoy hook and gastown's createAutoConvoy. The substring "tracking <id>"
// is what gastown's findConvoyByDescription matches on, so keep it stable.
func autoConvoyDescription(beadID string) string {
	return fmt.Sprintf("Auto-created convoy tracking %s", beadID)
}

// maybeAutoConvoy creates a tracking convoy in HQ for crew-created beads.
// It is a no-op outside the crew context so non-crew workflows are unaffected.
//
// Skip conditions (in order):
//  1. --no-convoy flag set
//  2. --dry-run was used (caller already returned, but defensive)
//  3. BD_AUTO_CONVOY=off in env (test/script escape hatch)
//  4. Actor doesn't match `<rig>/crew/<name>`
//  5. Issue is ephemeral (wisp) — short-lived, no point convoying
//  6. Issue type is itself a meta type (convoy/molecule/event/message) —
//     prevents recursive auto-convoying of the convoy we just created
//  7. No HQ town beads dir found by walking up from cwd
//
// On any failure during convoy creation a warning is printed but the bead
// itself remains intact — convoy is auxiliary, not essential.
func maybeAutoConvoy(cmd *cobra.Command, issue *types.Issue) {
	if issue == nil || issue.ID == "" {
		return
	}
	if noConvoy, _ := cmd.Flags().GetBool("no-convoy"); noConvoy {
		return
	}
	if strings.EqualFold(os.Getenv("BD_AUTO_CONVOY"), "off") {
		return
	}
	if !isCrewActor(getActor()) {
		return
	}
	if issue.Ephemeral {
		return
	}
	switch string(issue.IssueType) {
	case "convoy", "molecule", "event", "message", "agent", "role", "rig", "slot", "queue", "merge-request", "gate":
		return
	}

	hqBeadsDir, err := findHQBeadsDir()
	if err != nil {
		debug.Logf("auto-convoy: skipping (no town beads dir found): %v", err)
		return
	}

	convoyID := fmt.Sprintf("hq-cv-%s", generateConvoyShortID())
	convoyTitle := fmt.Sprintf("Work (crew): %s", issue.Title)

	if err := createAutoConvoyBead(rootCtx, hqBeadsDir, convoyID, convoyTitle, issue.ID, getActor()); err != nil {
		WarnError("auto-convoy: failed to create convoy %s: %v", convoyID, err)
		return
	}

	// Best-effort tracks dep. Cross-rig dep validation may fail when the bead
	// lives in a non-HQ rig DB; gastown's ConvoyManager + findConvoyByDescription
	// both have description-pattern fallbacks, so a missing dep is non-fatal.
	if err := addAutoConvoyTracksDep(filepath.Dir(hqBeadsDir), convoyID, issue.ID); err != nil {
		debug.Logf("auto-convoy: tracks dep failed (best-effort, description pattern still works): %v", err)
	}

	silent, _ := cmd.Flags().GetBool("silent")
	if !jsonOutput && !silent && !debug.IsQuiet() {
		fmt.Printf("  %s Auto-convoy: %s (crew tracking)\n", ui.RenderInfoIcon(), convoyID)
	}
}

// createAutoConvoyBead writes the convoy issue directly into the HQ beads DB
// via the storage API. Using the storage API (rather than shelling out to bd)
// keeps this testable and avoids re-entering bd's command flow.
func createAutoConvoyBead(ctx context.Context, hqBeadsDir, convoyID, convoyTitle, trackedBeadID, actor string) (retErr error) {
	hqStore, err := dolt.NewFromConfig(ctx, hqBeadsDir)
	if err != nil {
		return fmt.Errorf("opening HQ store: %w", err)
	}
	defer func() {
		if cerr := hqStore.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("closing HQ store: %w", cerr)
		}
	}()

	convoy := &types.Issue{
		ID:          convoyID,
		Title:       convoyTitle,
		Description: autoConvoyDescription(trackedBeadID),
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.IssueType("convoy"),
		CreatedBy:   actor,
		Owner:       actor,
		Assignee:    actor,
	}
	if err := hqStore.CreateIssue(ctx, convoy, actor); err != nil {
		return fmt.Errorf("creating convoy issue: %w", err)
	}

	commitMsg := fmt.Sprintf("bd: auto-convoy %s tracks %s", convoyID, trackedBeadID)
	if err := hqStore.Commit(ctx, commitMsg); err != nil && !isDoltNothingToCommit(err) {
		return fmt.Errorf("committing convoy: %w", err)
	}
	return nil
}

// addAutoConvoyTracksDep shells out to `bd dep add` from the town root so the
// command's normal cross-DB routing handles the rig-prefixed bead correctly.
// Returns an error which the caller treats as non-fatal.
func addAutoConvoyTracksDep(townRoot, convoyID, beadID string) error {
	bdPath := bdSelfExecPath()
	depCmd := exec.Command(bdPath, "dep", "add", convoyID, beadID, "--type=tracks")
	depCmd.Dir = townRoot

	env := os.Environ()
	// Strip BEADS_DIR so dep add uses the working-directory routing.
	filtered := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "BEADS_DIR=") {
			filtered = append(filtered, kv)
		}
	}
	filtered = append(filtered, "BD_DOLT_AUTO_COMMIT=on")
	depCmd.Env = filtered

	if out, err := depCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// bdSelfExecPath returns the absolute path of the running bd binary so the
// auto-convoy hook re-invokes the same build (important for tests where the
// system PATH may resolve to a different bd).
func bdSelfExecPath() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return "bd"
}

// findHQBeadsDir walks up from the current working directory looking for a
// `.beads/routes.jsonl` — the canonical marker of the orchestrator's town-level
// beads database (HQ). Returns an error if no such directory is found.
//
// This intentionally only matches town/HQ-level setups: in a flat single-repo
// install there is no routes.jsonl, so the auto-convoy hook stays a no-op,
// which is the desired behavior outside of Gas Town.
func findHQBeadsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		beadsDir := filepath.Join(dir, ".beads")
		routesFile := filepath.Join(beadsDir, "routes.jsonl")
		if _, err := os.Stat(routesFile); err == nil {
			return beadsDir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .beads/routes.jsonl found in any parent directory")
		}
		dir = parent
	}
}
