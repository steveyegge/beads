package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
)

var (
	webPort int
	webHost string
	webDev  bool
	webOpen bool
)

var webCmd = &cobra.Command{
	Use:     "web",
	GroupID: "views",
	Short:   "Open interactive web dashboard",
	Long: `Launch an interactive web dashboard for visualizing issues.

Three views are available:
  Kanban   Columns grouped by status (open, in_progress, blocked, deferred)
  Table    Sortable, filterable table of all issues
  Graph    Dependency DAG with layered layout

The dashboard connects to the running daemon for real-time updates via SSE.
Changes made via 'bd create', 'bd update', etc. appear automatically.

Requires the daemon to be running (bd daemon start).`,
	Run: func(cmd *cobra.Command, args []string) {
		runWeb()
	},
}

func init() {
	webCmd.Flags().IntVar(&webPort, "port", 8080, "Port for web server")
	webCmd.Flags().StringVar(&webHost, "host", "localhost", "Host to bind to")
	webCmd.Flags().BoolVar(&webDev, "dev", false, "Serve web files from disk (development mode)")
	webCmd.Flags().BoolVar(&webOpen, "open", true, "Auto-open browser")
	rootCmd.AddCommand(webCmd)
}

func runWeb() {
	if daemonClient == nil {
		fmt.Fprintf(os.Stderr, "Error: bd web requires the daemon to be running\n\n")
		fmt.Fprintf(os.Stderr, "Start the daemon first:\n\n")
		fmt.Fprintf(os.Stderr, "  bd daemon start\n\n")
		fmt.Fprintf(os.Stderr, "Then run:\n\n")
		fmt.Fprintf(os.Stderr, "  bd web\n")
		os.Exit(1)
	}

	// open read-only direct storage for graph queries
	// (daemon RPC doesn't expose dependency graph methods)
	var graphStore storage.Storage
	if dbPath != "" {
		var err error
		graphStore, err = factory.NewFromConfig(rootCtx, filepath.Dir(dbPath))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: graph view unavailable (failed to open database: %v)\n", err)
		} else {
			defer func() { _ = graphStore.Close() }()
		}
	}

	addr := fmt.Sprintf("%s:%d", webHost, webPort)
	url := fmt.Sprintf("http://%s", addr)

	mux := buildWebMux(daemonClient, graphStore, webDev)

	fmt.Printf("bd web dashboard starting on %s\n", url)
	fmt.Printf("press ctrl+c to stop\n\n")

	// open browser
	if webOpen {
		openBrowser(url)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// graceful shutdown on context cancellation
	go func() {
		<-rootCtx.Done()
		_ = server.Close()
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// openBrowser opens the given URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
