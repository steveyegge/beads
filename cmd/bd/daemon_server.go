package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/steveyegge/beads/internal/daemon"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
)

// startRPCServer initializes and starts the RPC server
func startRPCServer(ctx context.Context, socketPath string, store storage.Storage, workspacePath string, dbPath string, authToken, httpAddr string, log daemonLogger) (*rpc.Server, chan error, error) {
	// Sync daemon version with CLI version
	rpc.ServerVersion = Version

	// Create wisp store for ephemeral issues.
	// Use Redis when BD_REDIS_URL is configured (enables multi-replica sharing),
	// otherwise fall back to in-memory store.
	var wispStore daemon.WispStore
	if redisURL := os.Getenv("BD_REDIS_URL"); redisURL != "" {
		var redisOpts []daemon.RedisWispOption
		if ns := os.Getenv("BD_REDIS_NAMESPACE"); ns != "" {
			redisOpts = append(redisOpts, daemon.WithNamespace(ns))
		}
		if ttlStr := os.Getenv("BD_REDIS_WISP_TTL"); ttlStr != "" {
			if ttl, err := time.ParseDuration(ttlStr); err == nil {
				redisOpts = append(redisOpts, daemon.WithTTL(ttl))
			} else {
				log.Warn("invalid BD_REDIS_WISP_TTL, using default", "value", ttlStr, "error", err)
			}
		}
		var err error
		wispStore, err = daemon.NewRedisWispStore(redisURL, redisOpts...)
		if err != nil {
			log.Warn("Redis unavailable, falling back to in-memory wisp store", "error", err)
			wispStore = daemon.NewWispStore()
		} else {
			log.Info("using Redis wisp store", "url", redactRedisURL(redisURL))
		}
	} else {
		wispStore = daemon.NewWispStore()
		log.Info("using in-memory wisp store")
	}

	server := rpc.NewServerWithWispStore(socketPath, store, wispStore, workspacePath, dbPath)

	// Configure auth token if provided
	if authToken != "" {
		server.SetAuthToken(authToken)
	}

	// Configure HTTP listener if address provided (Connect-RPC style API)
	if httpAddr != "" {
		server.SetHTTPAddr(httpAddr)
	}

	serverErrChan := make(chan error, 1)

	go func() {
		log.Info("starting RPC server", "socket", socketPath)
		if err := server.Start(ctx); err != nil {
			log.Error("RPC server error", "error", err)
			serverErrChan <- err
		}
	}()

	select {
	case err := <-serverErrChan:
		log.Error("RPC server failed to start", "error", err)
		return nil, nil, err
	case <-server.WaitReady():
		log.Info("RPC server ready (socket listening)")
	case <-time.After(5 * time.Second):
		log.Warn("server didn't signal ready after 5 seconds (may still be starting)")
	}

	return server, serverErrChan, nil
}

// checkParentProcessAlive checks if the parent process is still running.
// Returns true if parent is alive, false if it died.
// Returns true if parent PID is 0 or 1 (not tracked, or adopted by init).
func checkParentProcessAlive(parentPID int) bool {
	if parentPID == 0 {
		// Parent PID not tracked (older lock files)
		return true
	}
	
	if parentPID == 1 {
		// Adopted by init/launchd - this is normal for detached daemons on macOS/Linux
		// Don't treat this as parent death
		return true
	}
	
	// Check if parent process is running
	return isProcessRunning(parentPID)
}

// runEventLoop runs the daemon event loop (polling mode)
func runEventLoop(ctx context.Context, cancel context.CancelFunc, ticker *time.Ticker, doSync func(), server *rpc.Server, serverErrChan chan error, parentPID int, log daemonLogger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, daemonSignals...)
	defer signal.Stop(sigChan)

	// Parent process check (every 10 seconds)
	parentCheckTicker := time.NewTicker(10 * time.Second)
	defer parentCheckTicker.Stop()

	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			doSync()
		case <-parentCheckTicker.C:
			// Check if parent process is still alive
			if !checkParentProcessAlive(parentPID) {
				log.Info("parent process died, shutting down daemon", "parent_pid", parentPID)
				log.Info("final sync before shutdown")
				doSync()
				cancel()
				if err := server.Stop(); err != nil {
					log.Error("stopping server", "error", err)
				}
				return
			}
		case sig := <-sigChan:
			if isReloadSignal(sig) {
				log.Info("received reload signal, ignoring (daemon continues running)")
				continue
			}
			log.Info("received signal, shutting down gracefully", "signal", sig)
			log.Info("final sync before shutdown")
			doSync()
			cancel()
			if err := server.Stop(); err != nil {
				log.Error("stopping RPC server", "error", err)
			}
			return
		case <-ctx.Done():
			log.Info("context canceled, shutting down")
			log.Info("final sync before shutdown")
			doSync()
			if err := server.Stop(); err != nil {
				log.Error("stopping RPC server", "error", err)
			}
			return
		case err := <-serverErrChan:
			log.Error("RPC server failed", "error", err)
			cancel()
			if err := server.Stop(); err != nil {
				log.Error("stopping RPC server", "error", err)
			}
			return
		}
	}
}

// redactRedisURL replaces the password portion of a Redis URL for safe logging.
func redactRedisURL(rawURL string) string {
	// Redis URLs: redis://[:password@]host:port/db
	// Simple approach: mask anything between :// and @
	const prefix = "://"
	idx := len("redis") // position after scheme
	start := idx + len(prefix)
	if start >= len(rawURL) {
		return rawURL
	}
	rest := rawURL[start:]
	atIdx := -1
	for i, c := range rest {
		if c == '@' {
			atIdx = i
			break
		}
	}
	if atIdx < 0 {
		return rawURL // no credentials
	}
	return rawURL[:start] + "***@" + rest[atIdx+1:]
}
