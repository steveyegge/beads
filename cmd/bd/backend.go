package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/storage/doltutil"
	"github.com/steveyegge/beads/internal/ui"
)

// BackendStatusResult is the stable JSON shape for bd backend status.
// All fields are always present; empty string / false / [] are used when not applicable.
type BackendStatusResult struct {
	Backend string `json:"backend"`
	Mode    string `json:"mode"`
	Target  string `json:"target"`
	User    string `json:"user,omitempty"`
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
	Error   string `json:"error"`
}

var backendCmd = &cobra.Command{
	Use:     "backend",
	GroupID: "setup",
	Short:   "Manage and inspect the active beads backend.",
	Long: `Manage and inspect the active beads backend.

Available Commands:
  status    Report the active backend type and health

See 'bd dolt' for Dolt-specific operations.
For backend-agnostic health reporting, see 'bd backend status'.`,
}

var backendStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report the active backend type and health",
	Long: `Report the active storage backend type, connection target, and health.

Exit codes:
  0   Backend healthy (or not configured for live probe)
  1   Backend unhealthy or unresolvable`,
	Run: func(cmd *cobra.Command, args []string) {
		if selected := selectedNoDBBeadsDir(cmd); selected != "" {
			prepareSelectedNoDBContext(selected)
		}

		beadsDir := beads.FindBeadsDir()
		bi := configfile.ResolveBackendInfo(beadsDir)

		res := probeBackend(bi, beadsDir)

		exitCode := 0
		if !res.Healthy {
			exitCode = 1
		}

		if jsonOutput {
			outputJSON(res)
		} else {
			printBackendStatusText(res)
		}

		if exitCode != 0 {
			os.Exit(exitCode)
		}
	},
}

// probeBackend performs the health probe for the given backend and returns the status result.
func probeBackend(bi configfile.BackendInfo, beadsDir string) BackendStatusResult {
	res := BackendStatusResult{
		Backend: bi.Backend,
		Mode:    bi.Mode,
	}

	switch bi.Backend {
	case "unknown", "":
		if bi.ParseError != "" {
			res.Error = bi.ParseError
		} else {
			res.Error = "metadata.json malformed or unreadable"
		}
		return res

	case "unconfigured":
		res.Error = "no backend configured — run 'bd init' to initialize"
		return res

	default: // dolt
		return probeDolt(res, bi, beadsDir)
	}
}

func probeDolt(res BackendStatusResult, bi configfile.BackendInfo, beadsDir string) BackendStatusResult {
	switch bi.Mode {
	case "embedded":
		return probeDoltEmbedded(res, bi, beadsDir)
	default: // server (local or external)
		return probeDoltServer(res, bi, beadsDir)
	}
}

func probeDoltEmbedded(res BackendStatusResult, bi configfile.BackendInfo, beadsDir string) BackendStatusResult {
	dataDir := bi.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(beadsDir, "embeddeddolt")
	}
	res.Target = dataDir

	if _, err := os.Stat(dataDir); err == nil {
		res.Healthy = true
	} else {
		res.Error = fmt.Sprintf("data directory not found: %s", dataDir)
	}
	return res
}

func probeDoltServer(res BackendStatusResult, bi configfile.BackendInfo, beadsDir string) BackendStatusResult {
	host := bi.Host
	if host == "" {
		host = "127.0.0.1"
	}

	// Prefer runtime port from port file over static config.
	port := bi.Port
	if beadsDir != "" {
		if runtimePort := doltserver.DefaultConfig(beadsDir).Port; runtimePort > 0 {
			port = runtimePort
		}
	}
	if port == 0 {
		port = 3306
	}

	database := bi.Database
	user := bi.User
	res.Target = fmt.Sprintf("%s:%d/%s", host, port, database)
	res.User = user

	// Load password from config file.
	var password string
	if cfg, err := configfile.Load(beadsDir); err == nil && cfg != nil {
		password = cfg.GetDoltServerPasswordForPort(port)
	}

	dsnStr := doltutil.ServerDSN{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		Timeout:  5 * time.Second,
	}.String()

	db, openErr := sql.Open("mysql", dsnStr)
	if openErr != nil {
		res.Error = fmt.Sprintf("connect: %v", openErr)
		return res
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if pingErr := db.PingContext(ctx); pingErr != nil {
		res.Error = fmt.Sprintf("connect: %v", pingErr)
		return res
	}

	res.Healthy = true
	_ = db.QueryRowContext(ctx, "SELECT @@version").Scan(&res.Version)
	return res
}

func printBackendStatusText(res BackendStatusResult) {
	healthIcon := ui.RenderPass("✓")
	healthWord := "yes"
	if !res.Healthy {
		healthIcon = ui.FailStyle.Render("●")
		healthWord = "no"
	}

	switch res.Backend {
	case "unknown", "":
		fmt.Printf("backend  %s  unknown  (%s)\n", ui.FailStyle.Render("●"), res.Error)
		return

	case "unconfigured":
		fmt.Printf("backend  %s  unconfigured  (%s)\n", ui.FailStyle.Render("●"), res.Error)
		return

	default: // dolt
		modeLabel := res.Mode
		if modeLabel == "" {
			modeLabel = "embedded"
		}
		fmt.Printf("backend  %s  (%s)\n", ui.AccentStyle.Render("dolt"), modeLabel)
		fmt.Printf("  mode     %s\n", modeLabel)
		if res.Mode == "embedded" && res.Target != "" {
			fmt.Printf("  data     %s\n", res.Target)
		} else if res.Target != "" {
			fmt.Printf("  target   %s\n", res.Target)
		}
	}

	if res.Healthy {
		fmt.Printf("  healthy  %s  %s\n", healthIcon, healthWord)
	} else {
		fmt.Printf("  healthy  %s  %s  (%s)\n", healthIcon, healthWord, res.Error)
	}
	if res.Version != "" {
		fmt.Printf("  version  %s\n", res.Version)
	}

}

func init() {
	backendCmd.AddCommand(backendStatusCmd)
	rootCmd.AddCommand(backendCmd)
}
