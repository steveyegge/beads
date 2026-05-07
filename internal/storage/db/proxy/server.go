package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cenkalti/backoff/v4"
	"golang.org/x/sync/errgroup"

	"github.com/steveyegge/beads/internal/storage/db/server"
)

type ProxyOpts struct {
	RootDir     string
	Port        int
	IdleTimeout time.Duration
	Server      server.DatabaseServer
	// Stats is optional. When non-nil, the proxy records per-event counters
	// against it; tests use Snapshot() to assert. Production code should
	// leave this nil.
	Stats *Stats
}

type proxyServer struct {
	rootDir     string
	port        int
	idleTimeout time.Duration
	server      server.DatabaseServer
	stats       *Stats

	listener    net.Listener
	activeConns atomic.Int64
	conns       errgroup.Group
}

const (
	serverReadyTimeout     = 30 * time.Second
	readyPingTimeout       = 2 * time.Second
	readyInitialBackoff    = 50 * time.Millisecond
	readyMaxBackoff        = 1 * time.Second
	idleWatcherMinInterval = 1 * time.Second
	backendStopTimeout     = 10 * time.Second
	tcpKeepAlivePeriod     = 30 * time.Second
)

var errIdleTimeout = errors.New("idle timeout reached")

func NewProxyServer(opts ProxyOpts) *proxyServer {
	return &proxyServer{
		rootDir:     opts.RootDir,
		port:        opts.Port,
		idleTimeout: opts.IdleTimeout,
		server:      opts.Server,
		stats:       opts.Stats,
	}
}

func (p *proxyServer) Start(parentCtx context.Context) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Install signal handlers BEFORE Listen. Without this, Go's default
	// SIGTERM action terminates the process during the startup window
	// (Listen, pidfile write, backend Start, readiness wait), bypassing all
	// deferred cleanup including RemoveDatabaseProxyPidFile.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	var sigReceived atomic.Bool
	go func() {
		select {
		case <-ctx.Done():
		case <-sigCh:
			sigReceived.Store(true)
			p.stats.IncSignalReceived()
			cancel()
		}
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", p.port)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	p.listener = ln
	defer func() { _ = ln.Close() }()
	p.stats.IncStart()

	p.stats.IncBackendStart()
	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("start database server: %w", err)
	}

	if err := waitForServerReady(ctx, p.server, serverReadyTimeout); err != nil {
		p.stats.IncBackendStop()
		_ = stopBackendBounded(p.server)
		return fmt.Errorf("database server not ready: %w", err)
	}

	if err := WriteDatabaseProxyPidFile(p.rootDir, PidFile{Pid: os.Getpid(), Port: p.port}); err != nil {
		p.stats.IncBackendStop()
		_ = stopBackendBounded(p.server)
		return fmt.Errorf("write pid file: %w", err)
	}
	defer func() { _ = RemoveDatabaseProxyPidFile(p.rootDir) }()

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		<-gctx.Done()
		_ = p.listener.Close()
		return nil
	})
	g.Go(func() error { return p.idleWatcher(gctx) })
	g.Go(func() error { return p.acceptLoop(gctx) })

	runErr := g.Wait()
	_ = p.conns.Wait()
	p.stats.IncBackendStop()
	if stopErr := stopBackendBounded(p.server); stopErr != nil && runErr == nil {
		runErr = fmt.Errorf("stop database server: %w", stopErr)
	}
	if errors.Is(runErr, errIdleTimeout) || sigReceived.Load() {
		return nil
	}
	return runErr
}

// stopBackendBounded calls server.Stop with a fresh, time-bounded context.
// We use Background here (not the parent ctx) because by the time Stop
// runs, the parent ctx is typically already canceled — a backend that
// honors ctx would bail immediately and skip its cleanup. The bound
// protects against backends that ignore ctx entirely.
func stopBackendBounded(s server.DatabaseServer) error {
	ctx, cancel := context.WithTimeout(context.Background(), backendStopTimeout)
	defer cancel()
	return s.Stop(ctx)
}

func (p *proxyServer) idleWatcher(ctx context.Context) error {
	if p.idleTimeout <= 0 {
		<-ctx.Done()
		return nil
	}
	interval := p.idleTimeout / 4
	if interval < idleWatcherMinInterval {
		interval = idleWatcherMinInterval
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var idleSince time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if p.activeConns.Load() > 0 {
				idleSince = time.Time{}
				continue
			}
			if idleSince.IsZero() {
				idleSince = time.Now()
				continue
			}
			if time.Since(idleSince) >= p.idleTimeout {
				p.stats.IncIdleTimeout()
				return errIdleTimeout
			}
		}
	}
}

func (p *proxyServer) acceptLoop(ctx context.Context) error {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			// Surface non-shutdown accept errors to the errgroup so the
			// proxy fails fast instead of busy-looping. Specific errors that
			// warrant retry (e.g. transient EMFILE under load) can be added
			// here as the need arises.
			p.stats.IncAcceptError()
			return fmt.Errorf("accept: %w", err)
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetKeepAlive(true)
			_ = tc.SetKeepAlivePeriod(tcpKeepAlivePeriod)
		}
		p.stats.IncAccept()
		p.conns.Go(func() error {
			return p.handleConn(ctx, conn)
		})
	}
}

func (p *proxyServer) handleConn(ctx context.Context, client net.Conn) error {
	p.activeConns.Add(1)
	defer p.activeConns.Add(-1)

	p.stats.IncBackendDialAttempt()
	backend, err := p.server.Dial(ctx)
	if err != nil {
		p.stats.IncBackendDialError()
		_ = client.Close()
		return err
	}
	p.stats.IncBackendDialSuccess()
	p.stats.IncHandledConn()

	done := make(chan struct{})
	var doneOnce sync.Once
	finish := func() { doneOnce.Do(func() { close(done) }) }

	var g errgroup.Group
	g.Go(func() error {
		select {
		case <-ctx.Done():
			_ = client.Close()
			_ = backend.Close()
		case <-done:
		}
		return nil
	})
	g.Go(func() error {
		defer finish()
		defer func() { _ = backend.Close() }()
		defer func() { _ = client.Close() }()
		n, err := io.Copy(backend, client)
		p.stats.AddBytesClientToBackend(n)
		return err
	})
	g.Go(func() error {
		defer finish()
		defer func() { _ = backend.Close() }()
		defer func() { _ = client.Close() }()
		n, err := io.Copy(client, backend)
		p.stats.AddBytesBackendToClient(n)
		return err
	})
	return g.Wait()
}

func waitForServerReady(ctx context.Context, s server.DatabaseServer, timeout time.Duration) error {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = readyInitialBackoff
	bo.MaxInterval = readyMaxBackoff
	bo.MaxElapsedTime = timeout

	return backoff.Retry(func() error {
		if !s.Running(ctx) {
			return errors.New("database server not running")
		}
		pingCtx, cancel := context.WithTimeout(ctx, readyPingTimeout)
		defer cancel()
		return s.Ping(pingCtx)
	}, backoff.WithContext(bo, ctx))
}
