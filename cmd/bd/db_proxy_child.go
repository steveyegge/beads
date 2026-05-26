package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
	"github.com/steveyegge/beads/internal/storage/dbproxy/server"
)

var (
	dbProxyChildRoot        string
	dbProxyChildPort        int
	dbProxyChildIdleTimeout time.Duration
	dbProxyChildBackend     string
	dbProxyChildConfig      string
	dbProxyChildLogPath     string
	dbProxyChildDoltBin     string
)

var dbProxyChildCmd = &cobra.Command{
	Use:    "db-proxy-child",
	Hidden: true,
	Short:  "Internal: run as the database proxy child process",
	Long: `db-proxy-child runs the long-lived per-rootDir TCP proxy that fronts a
DatabaseServer. It is spawned by the parent bd process via fork+exec and is
not intended to be invoked directly by users.`,

	// Skip the root PersistentPreRun/PostRun. Those initialize the bd issue
	// store, telemetry spans, Dolt auto-commit tracking, etc. — none of which
	// apply to a long-running proxy daemon with its own lifecycle.
	PersistentPreRun:  func(cmd *cobra.Command, args []string) {},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {},

	RunE: func(cmd *cobra.Command, _ []string) error {
		backend := proxy.Backend(dbProxyChildBackend)
		if err := backend.Validate(); err != nil {
			return err
		}

		srv, err := newDatabaseServer(backend, dbProxyChildRoot, dbProxyChildConfig, dbProxyChildLogPath, dbProxyChildDoltBin)
		if err != nil {
			return err
		}

		p := proxy.NewProxyServer(proxy.ProxyOpts{
			RootDir:     dbProxyChildRoot,
			Port:        dbProxyChildPort,
			IdleTimeout: dbProxyChildIdleTimeout,
			Server:      srv,
		})
		if err := p.ListenAndServe(cmd.Context()); err != nil {
			if errors.Is(err, proxy.ErrLockHeld) {
				os.Exit(proxy.LockHeldExitCode)
			}
			return err
		}
		return nil
	},
}

func newDatabaseServer(backend proxy.Backend, rootDir, configPath, logPath, doltBin string) (server.DatabaseServer, error) {
	switch backend {
	case proxy.BackendLocalServer:
		return server.NewDoltServer(doltBin, rootDir, configPath, logPath, 0)
	case proxy.BackendExternal, proxy.BackendLocalSharedServer:
		return nil, fmt.Errorf("backend %q: not yet implemented", backend)
	}
	return nil, fmt.Errorf("unknown backend %q", backend)
}

func init() {
	dbProxyChildCmd.Flags().StringVar(&dbProxyChildRoot, "root", "", "root directory holding proxy.lock, proxy.pid, proxy.log")
	dbProxyChildCmd.Flags().IntVar(&dbProxyChildPort, "port", 0, "port to listen on")
	dbProxyChildCmd.Flags().DurationVar(&dbProxyChildIdleTimeout, "idle-timeout", 30*time.Second, "idle timeout before shutdown (0 disables)")
	dbProxyChildCmd.Flags().StringVar(&dbProxyChildBackend, "backend", "",
		"backend kind: "+strings.Join(proxy.KnownBackendNames(), " | "))
	dbProxyChildCmd.Flags().StringVar(&dbProxyChildConfig, "config", "", "path to backend server config (e.g. dolt sql-server YAML)")
	dbProxyChildCmd.Flags().StringVar(&dbProxyChildLogPath, "logpath", "", "path the backend server should write its stdout/stderr to")
	dbProxyChildCmd.Flags().StringVar(&dbProxyChildDoltBin, "dolt-bin", "", "path to the dolt executable")
	_ = dbProxyChildCmd.MarkFlagRequired("root")
	_ = dbProxyChildCmd.MarkFlagRequired("port")
	_ = dbProxyChildCmd.MarkFlagRequired("backend")
	rootCmd.AddCommand(dbProxyChildCmd)
}
