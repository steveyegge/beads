package rpc

import (
	"context"
	"fmt"
	"net"
	"net/rpc"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
)

// ServeListener registers a daemonServer backed by store, then accepts
// connections from ln until ctx is canceled or ln is closed. If idleTimeout
// is > 0, the server cancels itself after that long with no active iterator
// sessions. iterMgr is drained synchronously before returning.
//
// The caller must close store after ServeListener returns.
func ServeListener(ctx context.Context, ln net.Listener, store storage.Storage, cfg *configfile.Config, idleTimeout time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	srv := newDaemonServer(ctx, store, cfg)
	// stop() must be called AFTER the accept loop exits (iter_shared.go §stop).
	defer srv.iterMgr.stop()

	rpcSrv := rpc.NewServer()
	if err := rpcSrv.RegisterName("daemonServer", srv); err != nil {
		return fmt.Errorf("rpc: register daemonServer: %w", err)
	}

	// Close listener when context is done so Accept() unblocks.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	if idleTimeout > 0 {
		go daemonIdleWatcher(ctx, cancel, srv.iterMgr, idleTimeout)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("rpc: accept: %w", err)
			}
		}
		go rpcSrv.ServeConn(conn)
	}
}

// daemonIdleWatcher cancels the daemon context after idleTimeout elapses with
// no active iterator sessions. It polls at idleTimeout/4, but never faster
// than once per second.
func daemonIdleWatcher(ctx context.Context, cancel context.CancelFunc, mgr *iterSessionManager, idleTimeout time.Duration) {
	interval := idleTimeout / 4
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastActive := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if mgr.sessionsActive.Load() > 0 {
				lastActive = time.Now()
			} else if time.Since(lastActive) >= idleTimeout {
				cancel()
				return
			}
		}
	}
}
