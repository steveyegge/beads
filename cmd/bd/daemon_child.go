//go:build unix

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
	bdRPC "github.com/steveyegge/beads/internal/storage/rpc"
)

const (
	bddLockFile = "bdd.lock"
	bddPIDFile  = "bdd.pid"
	bddLogFile  = "bdd.log"

	// bddExTempFail is EX_TEMPFAIL (75): bdd.lock already held by another daemon.
	bddExTempFail = 75
)

var (
	daemonChildRoot        string
	daemonChildIdleTimeout time.Duration
	daemonChildMaxLifetime time.Duration
)

var daemonChildCmd = &cobra.Command{
	Use:    "daemon-child",
	Hidden: true,
	Short:  "Internal: run as the bdd daemon child process",
	Long: `daemon-child is the long-lived RPC server that backs bd's optional
daemon mode. It is spawned by bd via fork+exec and is not intended to
be invoked directly by users.`,

	// Skip root PersistentPreRun/PostRun: no store init, no telemetry spans.
	PersistentPreRun:  func(_ *cobra.Command, _ []string) {},
	PersistentPostRun: func(_ *cobra.Command, _ []string) {},

	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDaemonChild(cmd.Context(), daemonChildRoot, daemonChildIdleTimeout, daemonChildMaxLifetime)
	},
}

func runDaemonChild(ctx context.Context, beadsDir string, idleTimeout, maxLifetime time.Duration) error {
	// Step 1: acquire bdd.lock. Exit 75 (EX_TEMPFAIL) if already held.
	lock, err := util.TryLock(filepath.Join(beadsDir, bddLockFile))
	if err != nil {
		if lockfile.IsLocked(err) {
			os.Exit(bddExTempFail)
		}
		return fmt.Errorf("bdd: acquire lock: %w", err)
	}
	defer lock.Unlock()

	// Step 2: open storage directly (bypasses daemon probe to avoid recursion).
	store, err := newDoltStoreFromConfig(ctx, beadsDir)
	if err != nil {
		return fmt.Errorf("bdd: open store: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Step 3: readiness probe.
	if _, err := store.GetStatistics(ctx); err != nil {
		return fmt.Errorf("bdd: readiness probe: %w", err)
	}

	// Step 4: listen on Unix socket, restrict to owner only.
	sockAddr := sockPath(beadsDir)
	_ = os.Remove(sockAddr) // remove stale socket from a crashed prior run
	ln, err := net.Listen("unix", sockAddr)
	if err != nil {
		return fmt.Errorf("bdd: listen on %s: %w", sockAddr, err)
	}
	if err := os.Chmod(sockAddr, 0o600); err != nil { //nolint:gosec // G302: restricting socket, not loosening permissions
		_ = ln.Close()
		return fmt.Errorf("bdd: chmod %s: %w", sockAddr, err)
	}

	// Step 5: write bdd.pid atomically.
	now := time.Now()
	if err := pidfile.Write(beadsDir, bddPIDFile, pidfile.PidFile{
		Pid:        os.Getpid(),
		SocketPath: sockAddr,
		Version:    Version,
		StartedAt:  &now,
	}); err != nil {
		_ = ln.Close()
		return fmt.Errorf("bdd: write pidfile: %w", err)
	}
	// Remove sock + pid on any exit path (bdd.log is intentionally left intact).
	defer func() {
		_ = pidfile.Remove(beadsDir, bddPIDFile)
		_ = os.Remove(sockAddr)
	}()

	// Step 6: root context with cancellation.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Step 7: apply max lifetime ceiling.
	if maxLifetime > 0 {
		var lifetimeCancel context.CancelFunc
		ctx, lifetimeCancel = context.WithTimeout(ctx, maxLifetime)
		defer lifetimeCancel()
	}

	// Step 8: signal handler — cancel ctx on SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Step 9: load config for the RPC server (iter session limits, etc.).
	cfg, _ := configfile.Load(beadsDir)

	// Step 10: run the RPC accept loop; blocks until ctx is done.
	return bdRPC.ServeListener(ctx, ln, store, cfg, idleTimeout)
}

func init() {
	daemonChildCmd.Flags().StringVar(&daemonChildRoot, "root", "", "beads directory (required)")
	daemonChildCmd.Flags().DurationVar(&daemonChildIdleTimeout, "idle-timeout", 5*time.Minute,
		"shut down after this long with no active iterator sessions (0 disables)")
	daemonChildCmd.Flags().DurationVar(&daemonChildMaxLifetime, "max-lifetime", time.Hour,
		"hard lifetime ceiling; daemon exits after this long regardless of activity (0 disables)")
	_ = daemonChildCmd.MarkFlagRequired("root")
	rootCmd.AddCommand(daemonChildCmd)
}
