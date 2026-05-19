package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/git"
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/storage/fs"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

type initProxiedServerInput struct {
	prefix            string
	database          string
	roleFlag          string
	initRemote        string
	initRemoteChanged bool
	destroyToken      string
	serverConfigPath  string
	serverLogPath     string
	serverRootPath    string
	quiet             bool
	stealth           bool
	skipHooks         bool
	skipAgents        bool
	reinitLocal       bool
	contributor       bool
	team              bool
	fromJSONL         bool
	nonInteractive    bool
}

func runInitProxiedServer(cmd *cobra.Command, ctx context.Context, in initProxiedServerInput) {
	if in.fromJSONL {
		FatalError("--from-jsonl is not supported with --proxied-server")
	}
	if in.contributor {
		FatalError("--contributor is not supported with --proxied-server")
	}
	if in.team {
		FatalError("--team is not supported with --proxied-server")
	}

	if err := config.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize config: %v\n", err)
	}

	if err := checkExistingBeadsData(in.prefix); err != nil {
		FatalError("%v", err)
	}

	fsProvider := fs.NewFileSystemProvider(newFileSystemAdapters())
	fsUseCase := fsProvider.BeadsDirFSUseCase()

	if in.stealth {
		if err := fsUseCase.SetupStealthMode(ctx, !in.quiet); err != nil {
			FatalError("setting up stealth mode: %v", err)
		}
		in.skipHooks = true
	}

	prefix := resolveInitPrefix(in.prefix)

	cwd, err := os.Getwd()
	if err != nil {
		FatalError("failed to get current directory: %v", err)
	}

	beadsDirResolution := fsUseCase.ResolveBeadsDir(ctx)
	beadsDir, hasExplicitBeadsDir := beadsDirResolution.BeadsDir, beadsDirResolution.HasExplicit
	if strings.Contains(filepath.Clean(cwd), string(filepath.Separator)+".beads"+string(filepath.Separator)) ||
		strings.HasSuffix(filepath.Clean(cwd), string(filepath.Separator)+".beads") {
		fmt.Fprintf(os.Stderr, "Error: cannot initialize bd inside a .beads directory\n")
		fmt.Fprintf(os.Stderr, "Current directory: %s\n", cwd)
		os.Exit(1)
	}

	beadsDirAbs, err := filepath.Abs(beadsDir)
	if err != nil {
		beadsDirAbs = filepath.Clean(beadsDir)
	}

	cwdAbs, _ := filepath.Abs(cwd)
	beadsDirIsLocal := strings.HasPrefix(beadsDirAbs, filepath.Clean(cwdAbs)+string(filepath.Separator)) ||
		filepath.Clean(beadsDirAbs) == filepath.Clean(cwdAbs)
	useLocalBeads := !hasExplicitBeadsDir || beadsDirIsLocal

	if !isGitRepo() && !hasExplicitBeadsDir {
		gitInitCmd := exec.Command("git", "init")
		if output, err := gitInitCmd.CombinedOutput(); err != nil {
			FatalError("failed to initialize git repository: %v\n%s", err, output)
		}
		git.ResetCaches()
		if !in.quiet {
			fmt.Printf("  %s Initialized git repository\n", ui.RenderPass("✓"))
		}
	}

	dbName := resolveProxiedDatabaseName(beadsDir, prefix, in.database)
	projectID := resolveProjectID(beadsDir)

	metadataBody, err := composeProxiedServerMetadataJSON(proxiedMetadataInputs{
		dbName:           dbName,
		projectID:        projectID,
		serverConfigPath: in.serverConfigPath,
		serverLogPath:    in.serverLogPath,
		serverRootPath:   in.serverRootPath,
	})
	if err != nil {
		FatalError("composing metadata.json: %v", err)
	}
	configYAMLBody := renderInitConfigYAML("", false)

	fsParams := domain.InitializeBeadsDirParams{
		BeadsDir:         beadsDir,
		MetadataJSONBody: metadataBody,
		ConfigYAMLBody:   configYAMLBody,
		SetNoCOW:         true,
	}
	if useLocalBeads && beadsDirIsLocal {
		fsParams.RepoRoot = cwd
	}
	if useLocalBeads {
		fsParams.LocalVersion = Version
	}

	fsResult, err := fsUseCase.InitializeBeadsDir(ctx, fsParams)
	if err != nil {
		FatalError("initializing .beads directory: %v", err)
	}
	if fsResult.NoCOWErr != nil && !in.quiet {
		fmt.Fprintf(os.Stderr, "Warning: failed to set FS_NOCOW_FL on %s: %v\n", beadsDir, fsResult.NoCOWErr)
	}
	if fsResult.LocalVersionErr != nil && !in.quiet {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize version tracking: %v\n", fsResult.LocalVersionErr)
	}

	uowProvider, err := newProxiedServerUOWProvider(ctx, beadsDir)
	if err != nil {
		FatalError("failed to open uow provider: %v", err)
	}

	uw, err := uowProvider.NewUOW(ctx)
	if err != nil {
		FatalError("failed to open unit of work: %v", err)
	}
	defer uw.Close(ctx)

	bootstrapParams := domain.BootstrapProjectParams{
		Prefix:         prefix,
		ProjectID:      projectID,
		BdVersion:      Version,
		LastImportTime: time.Now(),
	}

	if repoID, err := beads.ComputeRepoID(); err == nil {
		bootstrapParams.RepoID = repoID
	} else if !in.quiet {
		fmt.Fprintf(os.Stderr, "Warning: could not compute repository ID: %v\n", err)
	}
	if cloneID, err := beads.GetCloneID(); err == nil {
		bootstrapParams.CloneID = cloneID
	} else if !in.quiet {
		fmt.Fprintf(os.Stderr, "Warning: could not compute clone ID: %v\n", err)
	}
	if remoteURL := resolveProxiedInitRemoteURL(in); remoteURL != "" {
		bootstrapParams.RemoteName = "origin"
		bootstrapParams.RemoteURL = remoteURL
	}

	if _, err := uw.BootstrapUseCase().BootstrapProject(ctx, bootstrapParams); err != nil {
		FatalError("bootstrap project: %v", err)
	}

	if err := uw.Commit(ctx, "bd init"); err != nil {
		FatalError("commit init: %v", err)
	}

	runInitProxiedServerTail(cmd, ctx, in, runInitTailContext{
		beadsDir:      beadsDir,
		prefix:        prefix,
		dbName:        dbName,
		useLocalBeads: useLocalBeads,
		remoteURL:     bootstrapParams.RemoteURL,
		fsUseCase:     fsUseCase,
	})
}

func resolveInitPrefix(flagPrefix string) string {
	prefix := flagPrefix
	if prefix == "" {
		prefix = config.GetString("issue-prefix")
	}
	if prefix == "" {
		cwd, err := os.Getwd()
		if err != nil {
			FatalError("failed to get current directory: %v", err)
		}
		prefix = filepath.Base(cwd)
	}
	prefix = strings.TrimLeft(prefix, ".")
	prefix = strings.TrimRight(prefix, "-")
	prefix = strings.ReplaceAll(prefix, ".", "_")
	if len(prefix) > 0 && !((prefix[0] >= 'a' && prefix[0] <= 'z') || (prefix[0] >= 'A' && prefix[0] <= 'Z') || prefix[0] == '_') {
		prefix = "bd_" + prefix
	}
	return prefix
}

func resolveBeadsDirForInit() (beadsDir string, hasExplicit bool) {
	if envBeadsDir := os.Getenv("BEADS_DIR"); envBeadsDir != "" {
		return utils.CanonicalizePath(envBeadsDir), true
	}
	beadsDir = beads.GetWorktreeFallbackBeadsDir()
	if beadsDir == "" {
		beadsDir = beads.FollowRedirect(filepath.Join(".", ".beads"))
	}
	return beadsDir, false
}

func resolveProxiedDatabaseName(beadsDir, prefix, dbFlag string) string {
	if dbFlag != "" {
		return dbFlag
	}
	if cfg, _ := configfile.Load(beadsDir); cfg != nil && cfg.DoltDatabase != "" {
		return cfg.DoltDatabase
	}
	if prefix != "" {
		return strings.ReplaceAll(prefix, "-", "_")
	}
	return configfile.DefaultDoltDatabase
}

func resolveProjectID(beadsDir string) string {
	if cfg, _ := configfile.Load(beadsDir); cfg != nil && cfg.ProjectID != "" {
		return cfg.ProjectID
	}
	return configfile.GenerateProjectID()
}

func resolveProxiedInitRemoteURL(in initProxiedServerInput) string {
	url, source := resolveInitConfiguredSyncRemote(in.initRemote, in.initRemoteChanged, resolveSyncRemote)
	if url != "" {
		return url
	}
	if source != initSyncRemoteNone {
		return ""
	}
	if !in.stealth && isGitRepo() && !isBareGitRepo() {
		if originURL, err := gitOriginGetURL(); err == nil && originURL != "" {
			return normalizeRemoteURL(originURL)
		}
	}
	return ""
}

type proxiedMetadataInputs struct {
	dbName           string
	projectID        string
	serverConfigPath string
	serverLogPath    string
	serverRootPath   string
}

func composeProxiedServerMetadataJSON(in proxiedMetadataInputs) ([]byte, error) {
	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendDolt
	cfg.Database = "dolt"
	cfg.DoltDatabase = in.dbName
	cfg.DoltMode = configfile.DoltModeProxiedServer
	cfg.ProjectID = in.projectID

	cfg.DoltProxiedServerConfig = in.serverConfigPath
	cfg.DoltProxiedServerLog = in.serverLogPath
	cfg.DoltProxiedServerRootPath = in.serverRootPath

	if filepath.IsAbs(cfg.DoltDataDir) {
		cfg.DoltDataDir = ""
	}
	if filepath.IsAbs(cfg.DoltProxiedServerConfig) {
		cfg.DoltProxiedServerConfig = ""
	}
	if filepath.IsAbs(cfg.DoltProxiedServerLog) {
		cfg.DoltProxiedServerLog = ""
	}
	if filepath.IsAbs(cfg.DoltProxiedServerRootPath) {
		cfg.DoltProxiedServerRootPath = ""
	}

	return json.MarshalIndent(cfg, "", "  ")
}

type runInitTailContext struct {
	beadsDir      string
	prefix        string
	dbName        string
	useLocalBeads bool
	remoteURL     string
	fsUseCase     domain.BeadsDirFSUseCase
}

func runInitProxiedServerTail(cmd *cobra.Command, ctx context.Context, in initProxiedServerInput, t runInitTailContext) {
	if isGitRepo() {
		role := in.roleFlag
		if role == "" {
			role = "maintainer"
		}
		if _, hasRole := getBeadsRole(); !hasRole {
			if err := setBeadsRole(role); err != nil && !in.quiet {
				fmt.Fprintf(os.Stderr, "Warning: failed to set beads.role: %v\n", err)
			}
		} else if in.roleFlag != "" {
			if err := setBeadsRole(role); err != nil && !in.quiet {
				fmt.Fprintf(os.Stderr, "Warning: failed to set beads.role: %v\n", err)
			}
		}
	}

	setupExclude, _ := cmd.Flags().GetBool("setup-exclude")
	if setupExclude {
		if err := t.fsUseCase.SetupForkExclude(ctx, !in.quiet); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to configure git exclude: %v\n", err)
		}
	} else if !in.stealth && isGitRepo() {
		if isFork, upstreamURL := detectForkSetup(); isFork {
			if in.nonInteractive {
				if err := t.fsUseCase.SetupForkExclude(ctx, !in.quiet); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to configure git exclude: %v\n", err)
				}
			} else {
				shouldExclude, err := promptForkExclude(upstreamURL, in.quiet)
				if err != nil && isCanceled(err) {
					fmt.Fprintln(os.Stderr, "Setup canceled.")
					exitCanceled()
				}
				if shouldExclude {
					if err := t.fsUseCase.SetupForkExclude(ctx, !in.quiet); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to configure git exclude: %v\n", err)
					}
				}
			}
		}
	}

	if !in.skipHooks && (!hooksInstalled() || hooksNeedUpdate()) {
		if hooksInstalled() && !in.quiet {
			fmt.Printf("  Updating hooks to version %s...\n", Version)
		}
		isJJ := git.IsJujutsuRepo()
		isColocated := git.IsColocatedJJGit()
		switch {
		case isJJ && !isColocated:
			if !in.quiet {
				printJJAliasInstructions()
			}
		case isColocated:
			if err := t.fsUseCase.InstallJJHooks(ctx); err != nil && !in.quiet {
				fmt.Fprintf(os.Stderr, "\n%s Failed to install jj hooks: %v\n", ui.RenderWarn("⚠"), err)
			} else if !in.quiet {
				fmt.Printf("  Hooks installed (jujutsu mode - no staging)\n")
			}
		default:
			if isGitRepo() {
				hooksParams := domain.HooksInstallParams{
					HookNames:  managedHookNames,
					BeadsHooks: true,
				}
				if err := t.fsUseCase.InstallGitHooks(ctx, hooksParams); err != nil && !in.quiet {
					fmt.Fprintf(os.Stderr, "\n%s Failed to install git hooks to .beads/hooks/: %v\n", ui.RenderWarn("⚠"), err)
				} else if !in.quiet {
					fmt.Printf("  Hooks installed to: .beads/hooks/\n")
				}
			}
		}
	}

	if !in.stealth && !in.skipAgents {
		agentsTemplate, _ := cmd.Flags().GetString("agents-template")
		agentsProfileStr, _ := cmd.Flags().GetString("agents-profile")
		agentsFile, _ := cmd.Flags().GetString("agents-file")
		if agentsFile != "" {
			if err := config.ValidateAgentsFile(agentsFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid --agents-file: %v\n", err)
				return
			}
			if err := t.fsUseCase.SetYAMLConfig(ctx, "agents.file", agentsFile); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to persist agents.file to config: %v\n", err)
			}
		}
		resolvedAgentsFile := agentsFile
		if resolvedAgentsFile == "" {
			resolvedAgentsFile = config.SafeAgentsFile()
		}
		if isBareGitRepo() {
			if !in.quiet {
				fmt.Printf("  Skipping %s generation in bare repository\n", resolvedAgentsFile)
			}
		} else {
			_ = t.fsUseCase.AddAgentsInstructions(ctx, domain.AgentsFileParams{
				File:         resolvedAgentsFile,
				Verbose:      !in.quiet,
				TemplatePath: agentsTemplate,
				Profile:      agentsProfileStr,
				HasRemote:    t.remoteURL != "",
			})
		}
	}

	if !in.stealth && !in.skipAgents && !isBareGitRepo() {
		if err := t.fsUseCase.InstallClaudeProject(ctx, in.stealth); err != nil && !in.quiet {
			fmt.Fprintf(os.Stderr, "Warning: failed to setup Claude hooks: %v\n", err)
		}
	}

	if !in.stealth && isGitRepo() && t.useLocalBeads {
		gitAddCmd := exec.Command("git", "add", ".beads/")
		if _, addErr := gitAddCmd.CombinedOutput(); addErr == nil {
			agentsFileToStage := config.SafeAgentsFile()
			if _, statErr := os.Stat(agentsFileToStage); statErr == nil {
				_ = exec.Command("git", "add", agentsFileToStage).Run()
			}
			if _, statErr := os.Stat(filepath.Join(".claude", "settings.json")); statErr == nil {
				_ = exec.Command("git", "add", filepath.Join(".claude", "settings.json")).Run()
			}
			if _, statErr := os.Stat("CLAUDE.md"); statErr == nil {
				_ = exec.Command("git", "add", "CLAUDE.md").Run()
			}
			if _, statErr := os.Stat(".gitignore"); statErr == nil {
				_ = exec.Command("git", "add", ".gitignore").Run()
			}
			commitCmd := exec.Command("git", "commit", "--no-verify", "-m", "bd init: initialize beads issue tracking")
			if commitOut, commitErr := commitCmd.CombinedOutput(); commitErr != nil {
				if !in.quiet && !strings.Contains(string(commitOut), "nothing to commit") {
					fmt.Fprintf(os.Stderr, "Warning: failed to commit beads files: %v\n", commitErr)
				}
			} else if !in.quiet {
				fmt.Printf("  %s Committed beads files to git\n", ui.RenderPass("✓"))
			}
		}
	}

	if isGitRepo() && !in.quiet {
		if gitHasAnyRemotes() && !gitHasUpstream() {
			fmt.Fprintf(os.Stderr, "\n%s Git upstream not configured\n", ui.RenderWarn("⚠"))
			fmt.Fprintf(os.Stderr, "  For sync workflows, set your upstream with:\n")
			fmt.Fprintf(os.Stderr, "  %s\n\n", ui.RenderAccent("git remote add upstream <repo-url>"))
		}
		if !in.stealth && !in.initRemoteChanged && t.remoteURL == "" {
			printInitNoDoltRemoteWarning()
		}
	}

	if in.quiet {
		return
	}
	fmt.Printf("\n%s bd initialized successfully!\n\n", ui.RenderPass("✓"))
	fmt.Printf("  Backend: %s\n", ui.RenderAccent(configfile.BackendDolt))
	fmt.Printf("  Mode: %s\n", ui.RenderAccent("proxied-server"))
	fmt.Printf("  Database: %s\n", ui.RenderAccent(t.dbName))
	fmt.Printf("  Issue prefix: %s\n", ui.RenderAccent(t.prefix))
	fmt.Printf("  Issues will be named: %s\n\n", ui.RenderAccent(t.prefix+"-<hash> (e.g., "+t.prefix+"-a3f2dd)"))
	fmt.Printf("Run %s to get started.\n\n", ui.RenderAccent("bd quickstart"))
}
