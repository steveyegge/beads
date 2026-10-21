package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// openMigrationPlanningStore opens the personal planning workspace's store for
// the copy phase below.
//
// A planning repo that has a .beads directory but no metadata.json is first
// initialized the way a routed bd create initializes it (ensureBeadsDirForPath,
// with the project store supplying the prefix). Contributor setup leaves exactly
// that shape: runContributorWizard creates an empty .beads, and
// autoConfigureForkContributor a database with no metadata.json and no
// issue_prefix. migrate-personal's own error text sends users to those setups,
// so their repos must be migratable into. A repo that already has a
// metadata.json (a clone not yet bootstrapped, say) is left as it is.
//
// Any other metadata-less .beads is initialized the same way, as a routed
// bd create would initialize it, with two exceptions that are refused before
// anything is written (refuseUninitializablePlanningRepo): a redirect stub,
// whose new database would land inside the stub where bd run in the planning
// repo never looks, and any workspace bd's own command admission refuses there
// (guardLegacyUpgradeWorkspace), such as a SQLite-era repo, whose data a fresh
// database beside it would hide.
//
// The planning repo is the contributor auto-route target, so it then opens the
// way a write auto-routed there does (openAutoRoutedStore): its own
// metadata.json picks the backend, the mode and the database, and the database
// is pinned against a launching workspace's BEADS_DOLT_SERVER_DATABASE
// (autoRouteTargetDatabase, openRoutedTargetStore). A metadata.json that names
// no dolt_database, or an embedded planning workspace with no database, is
// refused, not given one. A bare dolt.New here used to dial a SQL server for
// every planning workspace, under the default database name.
//
// The store it returns is WRITTEN THROUGH: the migration creates every issue in
// the planning workspace and then deletes them from the project. Those are
// ordinary bead mutations in another workspace, so they belong in that
// workspace's events journal if it has one enabled — and the setting is read
// from there, not from the workspace bd was launched in. The same goes for its
// issue-version history, which the store's own settings row decides. The
// routed-write factory (newExistingDoltStoreFromConfig) activates the planning
// workspace's events journal and issue-version history on the store it opens.
func openMigrationPlanningStore(ctx context.Context, planningBeadsDir string, projectStore storage.DoltStorage) (storage.DoltStorage, error) {
	if err := refuseUninitializablePlanningRepo(planningBeadsDir); err != nil {
		return nil, err
	}
	if err := ensureBeadsDirForPath(ctx, filepath.Dir(planningBeadsDir), projectStore, true); err != nil {
		return nil, fmt.Errorf("initialize planning repo: %w", err)
	}
	database, err := autoRouteTargetDatabase(planningBeadsDir, true)
	if err != nil {
		return nil, err
	}
	return openRoutedTargetStore(ctx, planningBeadsDir, database, true)
}

// refuseUninitializablePlanningRepo refuses a planning .beads with no
// metadata.json that openMigrationPlanningStore must not initialize: a redirect
// stub, and a workspace guardLegacyUpgradeWorkspace refuses. Every other
// metadata-less .beads is left to ensureBeadsDirForPath. It writes nothing. A
// .beads that has a metadata.json is left to the open, which refuses what it
// cannot use.
func refuseUninitializablePlanningRepo(planningBeadsDir string) error {
	if _, err := os.Stat(filepath.Join(planningBeadsDir, "metadata.json")); err == nil {
		return nil
	}
	if target := beads.FollowRedirect(planningBeadsDir); utils.CanonicalizePath(target) != utils.CanonicalizePath(planningBeadsDir) {
		return fmt.Errorf("planning repo %s redirects to %s; set routing.contributor to that repository instead", filepath.Dir(planningBeadsDir), filepath.Dir(target))
	}
	return guardLegacyUpgradeWorkspace(planningBeadsDir)
}

// refuseSelfPlanningRepo refuses a planning repo that is the project workspace
// itself (routing.contributor set to the project's path, or to ".", the value
// routing.maintainer and routing.default default to). The migration copies each
// issue into the planning store and then deletes it from the project; when both
// are the same database the copy is a no-op and the delete removes the only
// copy. The planning .beads directory (after any redirect) is compared with the
// project's, and the two metadata.json project_id values when both are set.
func refuseSelfPlanningRepo(planningPath, planningBeadsDir, projectBeadsDir string) error {
	if projectBeadsDir == "" {
		return nil
	}
	planning := utils.CanonicalizePath(beads.FollowRedirect(planningBeadsDir))
	project := utils.CanonicalizePath(beads.FollowRedirect(projectBeadsDir))
	same := planning == project
	if !same {
		// LoadForDiscovery, not Load: Load migrates a legacy config.json to
		// metadata.json, and this check runs before the confirmation prompt
		// and before any format admission, so it must not rewrite either
		// workspace.
		planningCfg, perr := configfile.LoadForDiscovery(planning)
		projectCfg, qerr := configfile.LoadForDiscovery(project)
		same = perr == nil && qerr == nil && planningCfg != nil && projectCfg != nil &&
			planningCfg.ProjectID != "" && planningCfg.ProjectID == projectCfg.ProjectID
	}
	if same {
		return fmt.Errorf("planning repo %s is this project's own workspace; set routing.contributor to a separate planning repository (e.g. ~/.beads-planning)", planningPath)
	}
	return nil
}

var migratePersonalCmd = &cobra.Command{
	Use:   "migrate-personal",
	Short: "Move personal planning issues from the project database to your planning repo",
	Long: `Identify issues you created in the project database and move them to your
personal planning repository (~/.beads-planning by default).

This is a one-time migration for contributors who created personal planning
issues before contributor routing was configured.

The command:
  1. Finds all issues in the project database created by your git identity
  2. Shows you the list and asks for confirmation
  3. Moves them to the planning repo configured in routing.contributor

EXAMPLES:
  bd migrate-personal        # Interactive: show list and prompt
  bd migrate-personal -y     # Non-interactive: skip confirmation`,
	GroupID: "setup",
	RunE:    runMigratePersonal,
}

var migratePersonalYes bool

func init() {
	migratePersonalCmd.Flags().BoolVarP(&migratePersonalYes, "yes", "y", false, "Skip confirmation prompt")
	rootCmd.AddCommand(migratePersonalCmd)
}

func runMigratePersonal(cmd *cobra.Command, args []string) error {
	if usesProxiedServer() {
		return HandleErrorRespectJSON("migrate-personal is not supported in proxied-server mode")
	}
	ctx := rootCtx

	identity := getActorWithGit()

	// Find personal issues in project DB
	filter := types.IssueFilter{Limit: 0}
	allIssues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		return fmt.Errorf("failed to search issues: %w", err)
	}

	var personal []*types.Issue
	for _, issue := range allIssues {
		if issue.CreatedBy == identity {
			personal = append(personal, issue)
		}
	}

	if len(personal) == 0 {
		fmt.Printf("Nothing to migrate — no issues in the project database were created by %s\n", ui.RenderAccent(identity))
		return nil
	}

	// Show list
	fmt.Printf("\n%s personal issues created by %s found in project database:\n\n",
		ui.RenderBold(fmt.Sprintf("%d", len(personal))), ui.RenderAccent(identity))
	for _, issue := range personal {
		fmt.Printf("  %s  %s\n", ui.RenderAccent(issue.ID), issue.Title)
	}
	fmt.Println()

	// Get planning repo path
	planningRaw := getRoutingConfigValue(ctx, store, "routing.contributor")
	if planningRaw == "" {
		// Backward compat
		planningRaw = getRoutingConfigValue(ctx, store, "contributor.planning_repo")
	}
	if planningRaw == "" {
		return fmt.Errorf("no planning repository configured; run 'bd init --contributor' or 'bd init' in a fork repo first")
	}
	planningPath := routing.ExpandPath(planningRaw)

	// Validate planning DB path before any destructive operation
	planningBeadsDir := filepath.Join(planningPath, ".beads")
	if _, err := os.Stat(planningBeadsDir); os.IsNotExist(err) {
		return fmt.Errorf("planning repo at %s has no .beads directory; initialize it first with 'bd init'", planningPath)
	}
	if err := refuseSelfPlanningRepo(planningPath, planningBeadsDir, resolveCommandBeadsDir(dbPath)); err != nil {
		return err
	}

	fmt.Printf("Move to planning repo: %s\n\n", ui.RenderAccent(planningPath))

	// Prompt for confirmation
	if !migratePersonalYes {
		fmt.Printf("Move %d issues? [Y/n]: ", len(personal))
		reader := bufio.NewReader(os.Stdin)
		response, err := readLineWithContext(ctx, reader, os.Stdin)
		if err != nil {
			if isCanceled(err) {
				fmt.Fprintln(os.Stderr, "Aborted.")
				return nil
			}
			response = ""
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response == "n" || response == "no" {
			fmt.Println("Aborted — no changes made.")
			return nil
		}
	}

	// Open planning store (writable). This is a SECOND workspace's store, and
	// the copy below creates and deletes real beads in it, so it takes the
	// planning workspace's own events-journal setting — not the launching
	// workspace's. See openMigrationPlanningStore.
	planningStore, err := openMigrationPlanningStore(ctx, planningBeadsDir, store)
	if err != nil {
		if isUnsupportedWorkspaceFormat(err) {
			// Same rendering as bd's own admission refusal: hint and exit 16.
			return handleWorkspaceGuardError(err)
		}
		return fmt.Errorf("failed to open planning store at %s: %w", planningPath, err)
	}
	defer func() { _ = planningStore.Close() }()

	// Phase 0: refuse when any issue ID is already in the planning DB. A create
	// can upsert onto an existing row, and the rollback below would then delete
	// a row this migration did not create; if the planning store were somehow
	// the project's own database (refuseSelfPlanningRepo is the first guard),
	// phase 2 would delete the only copy.
	for _, issue := range personal {
		_, err := planningStore.GetIssue(ctx, issue.ID)
		switch {
		case err == nil:
			if issue.SourceRepo != "" && issue.SourceRepo != "." {
				// A mirror hydrated by bd repo sync, not a personal issue.
				return fmt.Errorf("issue %s already exists in the planning repo %s; the project copy was imported from %s by 'bd repo sync' (remove those copies with 'bd repo remove %s', then rerun); nothing was moved", issue.ID, planningPath, issue.SourceRepo, issue.SourceRepo)
			}
			return fmt.Errorf("issue %s already exists in the planning repo %s; nothing was moved", issue.ID, planningPath)
		case !errors.Is(err, storage.ErrNotFound):
			return fmt.Errorf("failed to check the planning DB for %s: %w", issue.ID, err)
		}
	}

	// Phase 1: Copy everything to the planning DB before touching the project
	// DB. The copy is all-or-error — any failed step rolls back the planning-DB
	// rows we created and aborts without deleting anything from the project DB,
	// so a partial copy can never be mistaken for success and lose data
	// (maphew review, be-e2nb).
	//
	// Phase 1a: create every issue row first, so same-prefix dependencies
	// between two migrated issues resolve in phase 1b regardless of copy order.
	var created []string
	rollback := func() {
		if len(created) > 0 {
			_, _ = planningStore.DeleteIssues(ctx, created, false, false, false)
		}
	}
	for _, issue := range personal {
		if err := planningStore.CreateIssue(ctx, issue, identity); err != nil {
			rollback()
			return fmt.Errorf("failed to copy issue %s to planning DB: %w", issue.ID, err)
		}
		created = append(created, issue.ID)
	}

	// Phase 1b: copy labels, dependency records, and comments for every issue.
	for _, issue := range personal {
		if err := copyIssueRelations(ctx, store, planningStore, issue, identity); err != nil {
			rollback()
			return fmt.Errorf("failed to copy %s to planning DB: %w", issue.ID, err)
		}
	}

	// Phase 2: Delete all from project DB in a single transaction. The planning
	// copy is complete at this point. If the delete fails, the planning repo
	// already holds the good copy and the project DB is untouched, so we tell
	// the user to resolve the duplicate rather than silently losing data.
	ids := make([]string, len(personal))
	for i, issue := range personal {
		ids[i] = issue.ID
	}
	if _, err := store.DeleteIssues(ctx, ids, false, false, false); err != nil {
		return fmt.Errorf("issues copied to planning repo %s but deleting them from the project DB failed; resolve the duplicate manually: %w", planningPath, err)
	}

	fmt.Printf("\n%s Moved %d %s to %s\n",
		ui.RenderPass("✓"),
		len(personal),
		pluralIssue(len(personal)),
		ui.RenderAccent(planningPath))
	return nil
}

// copyIssueRelations copies the labels, dependency records, and comments for a
// single issue from src to dst. The issue row itself must already exist in dst
// (created in phase 1a of runMigratePersonal). Every step — including the reads
// from src — is fatal on error so the caller can abort the migration before any
// source data is deleted; a partial copy must never be mistaken for success
// (maphew review, be-e2nb).
func copyIssueRelations(ctx context.Context, src, dst storage.DoltStorage, issue *types.Issue, actor string) error {
	labels, err := src.GetLabels(ctx, issue.ID)
	if err != nil {
		return fmt.Errorf("read labels for %s: %w", issue.ID, err)
	}
	for _, label := range labels {
		if err := dst.AddLabel(ctx, issue.ID, label, actor); err != nil {
			return fmt.Errorf("copy label %q for %s: %w", label, issue.ID, err)
		}
	}

	deps, err := src.GetDependencyRecords(ctx, issue.ID)
	if err != nil {
		return fmt.Errorf("read dependencies for %s: %w", issue.ID, err)
	}
	for _, dep := range deps {
		if err := dst.AddDependency(ctx, dep, actor); err != nil {
			return fmt.Errorf("copy dependency %s→%s: %w", dep.IssueID, dep.DependsOnID, err)
		}
	}

	comments, err := src.GetIssueComments(ctx, issue.ID)
	if err != nil {
		return fmt.Errorf("read comments for %s: %w", issue.ID, err)
	}
	for _, c := range comments {
		// ImportIssueComment writes a structured comment row preserving the
		// original author and created_at. AddComment would instead record an
		// event-style entry that GetIssueComments never returns, silently
		// dropping the comment from the migrated issue (maphew review, be-e2nb).
		if _, err := dst.ImportIssueComment(ctx, issue.ID, c.Author, c.Text, c.CreatedAt); err != nil {
			return fmt.Errorf("copy comment for %s: %w", issue.ID, err)
		}
	}

	return nil
}

func pluralIssue(n int) string {
	if n == 1 {
		return "issue"
	}
	return "issues"
}
