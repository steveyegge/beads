//go:build cgo

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
)

// `bd init --external` declares that the Dolt server is managed outside bd, so
// init must never start one. These tests own one boundary risk that no lower
// seam can show: init builds its dolt config by hand, so the flag reaches the
// auto-start decision only if init carries it there. They run the real bd
// binary against a dolt shim that records every launch, which makes the
// outcome observable without a real server and leaves nothing running.

// externalInitFixture is one fresh project plus a dolt shim for `bd init`
// attempts. The shim sits first on PATH, records every invocation and fails.
type externalInitFixture struct {
	t        *testing.T
	repoDir  string
	home     string
	shimDir  string
	launches string
}

type externalInitResult struct {
	output   string // combined stdout and stderr
	failed   bool   // bd exited non-zero
	launches string // dolt invocations the shim saw, one per line
}

func newExternalInitFixture(t *testing.T) *externalInitFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the dolt shim is a POSIX shell script")
	}
	f := &externalInitFixture{t: t, repoDir: t.TempDir(), home: t.TempDir(), shimDir: t.TempDir()}
	f.launches = filepath.Join(f.shimDir, "invocations")
	shim := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$EXTERNAL_INIT_SHIM_LOG\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(f.shimDir, "dolt"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, f.repoDir)
	return f
}

// run executes bd in the project. autoStart is the value of
// BEADS_DOLT_AUTO_START, or "" to leave it unset: an unpinned environment is
// what most callers run in, so it is the one that matters here.
func (f *externalInitFixture) run(autoStart string, args ...string) externalInitResult {
	f.t.Helper()
	env := append(envWithoutBeadsStorageSettings(),
		"HOME="+f.home,
		"XDG_CONFIG_HOME="+filepath.Join(f.home, ".config"),
		"BEADS_TEST_IGNORE_REPO_CONFIG=1",
		"BD_DISABLE_METRICS=1",
		"PATH="+f.shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"EXTERNAL_INIT_SHIM_LOG="+f.launches,
	)
	if autoStart != "" {
		env = append(env, "BEADS_DOLT_AUTO_START="+autoStart)
	}
	out, err := runExternalServerBD(f.t, f.repoDir, env, args...)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		f.t.Fatalf("bd %s did not run: %v\n%s", strings.Join(args, " "), err, out)
	}
	launches, _ := os.ReadFile(f.launches)
	return externalInitResult{output: out, failed: err != nil, launches: strings.TrimSpace(string(launches))}
}

// externalEndpoint is the server an --external init is pointed at.
type externalEndpoint struct {
	flags   []string // --server-host/--server-port, or --server-socket
	network string   // "tcp" or "unix"
	addr    string   // host:port or the socket path, as bd reports it
}

func tcpEndpoint(port int) externalEndpoint {
	return externalEndpoint{
		flags:   []string{"--server-host", "127.0.0.1", "--server-port", strconv.Itoa(port)},
		network: "tcp",
		addr:    net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
	}
}

func unixEndpoint(path string) externalEndpoint {
	return externalEndpoint{flags: []string{"--server-socket", path}, network: "unix", addr: path}
}

// externalInitSocketPath returns an unused unix socket path. Socket paths are
// capped near 104 bytes and t.TempDir() nests too deeply under some runners.
func externalInitSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bdx")
	if err != nil {
		t.Fatal(err)
	}
	if len(filepath.Join(dir, "dolt.sock")) > 100 {
		_ = os.RemoveAll(dir)
		if dir, err = os.MkdirTemp("/tmp", "bdx"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "dolt.sock")
}

// serveUnixProxy listens on a fresh unix socket and forwards each connection
// to target, so bd sees a reachable server at a socket path. The returned
// func reports how many connections bd made through it.
func serveUnixProxy(t *testing.T, target string) (string, func() int) {
	t.Helper()
	path := externalInitSocketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on %s: %v", path, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var mu sync.Mutex
	accepted := 0
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			accepted++
			mu.Unlock()
			go func() {
				defer client.Close()
				upstream, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer upstream.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
				go func() { _, _ = io.Copy(client, upstream); done <- struct{}{} }()
				<-done // either side closing ends the session; the deferred closes release the other copy
			}()
		}
	}()
	return path, func() int { mu.Lock(); defer mu.Unlock(); return accepted }
}

// requireNoLaunch fails when bd tried to run dolt at all: an externally
// managed server is never started by bd, and the shim is how a start shows.
func requireNoLaunch(t *testing.T, res externalInitResult) {
	t.Helper()
	if res.launches != "" {
		t.Errorf("bd launched dolt although the server is externally managed (dolt %s):\n%s",
			strings.ReplaceAll(res.launches, "\n", "; dolt "), res.output)
	}
}

// requireExternalUnreachableMessage checks the one wording an --external init
// gives for an endpoint it cannot reach: it names the endpoint, says the server
// is declared external and that bd did not start one, and never advises
// `bd dolt start`, which would start the server --external says bd must not.
func requireExternalUnreachableMessage(t *testing.T, output, endpoint string) {
	t.Helper()
	for _, want := range []string{endpoint, "externally managed", "did not start"} {
		if !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "bd dolt start") {
		t.Errorf("output advises `bd dolt start`:\n%s", output)
	}
}

func requireNothingListening(t *testing.T, network, addr string) {
	t.Helper()
	conn, err := net.DialTimeout(network, addr, time.Second)
	if err == nil {
		_ = conn.Close()
		t.Errorf("something accepts connections on %s %s after init", network, addr)
	}
}

// requireDoltRootEmpty fails when .beads/dolt holds anything: init leaves at
// most an empty marker directory for an external server, so any entry is a
// server root a local dolt created.
func requireDoltRootEmpty(t *testing.T, repoDir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoDir, ".beads", "dolt"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf(".beads/dolt holds %v, which init did not put there", names)
	}
}

// TestInitServerExternalDeadPortStartsNoServer is the end-to-end boundary for
// --external: a dead endpoint must fail loudly instead of making bd launch a
// dolt of its own on the port the caller named.
func TestInitServerExternalDeadPortStartsNoServer(t *testing.T) {
	f := newExternalInitFixture(t)
	ep := tcpEndpoint(freeLoopbackPort(t))

	res := f.run("", append(append([]string{"init", "--quiet", "--server", "--external"}, ep.flags...),
		"--prefix", "x", "--skip-hooks", "--skip-agents")...)

	if !res.failed {
		t.Errorf("bd init --server --external succeeded against a dead endpoint %s:\n%s", ep.addr, res.output)
	}
	requireNoLaunch(t, res)
	requireExternalUnreachableMessage(t, res.output, ep.addr)
	requireNothingListening(t, ep.network, ep.addr)
	requireDoltRootEmpty(t, f.repoDir)
}

// TestInitServerExternalNoPortStartsNoServer is the --external boundary that
// rule 2c (#5934) cannot see: the argv and the environment name no port, so
// nothing but the flag declares the server external. A server init in that
// shape launches a repo-local dolt by design (see
// TestInitServerWithoutNamedPortStillStartsServer), so with --external it must
// fail instead, and say that bd did not start one. There is no endpoint to
// name, so the wording is checked without one: the child inherits
// BEADS_TEST_MODE, under which the store dials port 1 rather than leave an
// unnamed port at 0, so the port-0 wording itself is pinned by
// TestExternalServerUnreachableMessage.
func TestInitServerExternalNoPortStartsNoServer(t *testing.T) {
	f := newExternalInitFixture(t)

	res := f.run("", "init", "--quiet", "--server", "--external", "--prefix", "x", "--skip-hooks", "--skip-agents")

	if !res.failed {
		t.Errorf("bd init --server --external succeeded with no server named:\n%s", res.output)
	}
	requireNoLaunch(t, res)
	requireDoltRootEmpty(t, f.repoDir)
	for _, want := range []string{"externally managed", "did not start"} {
		if !strings.Contains(res.output, want) {
			t.Errorf("output does not contain %q:\n%s", want, res.output)
		}
	}
	if strings.Contains(res.output, "bd dolt start") {
		t.Errorf("output advises `bd dolt start`:\n%s", res.output)
	}
}

// TestInitSharedServerExternalDeadPortStillFails pins the sibling route that
// already honors --external: the same dead endpoint under --shared-server
// exits non-zero without launching dolt. Without it the test above could pass
// because every init fails here, not because --server honors the flag.
func TestInitSharedServerExternalDeadPortStillFails(t *testing.T) {
	f := newExternalInitFixture(t)
	ep := tcpEndpoint(freeLoopbackPort(t))

	res := f.run("", append(append([]string{"init", "--quiet", "--shared-server", "--external"}, ep.flags...),
		"--prefix", "x", "--skip-hooks", "--skip-agents")...)

	if !res.failed {
		t.Errorf("bd init --shared-server --external succeeded against a dead endpoint %s:\n%s", ep.addr, res.output)
	}
	requireNoLaunch(t, res)
}

// TestInitServerWithoutNamedPortStillStartsServer pins the other half of the
// contract: a --server init that names no port is where bd starts a repo-local
// dolt by design, so the no-launch assertions above cannot pass merely because
// nothing can ever launch one. It names no port, in argv or in the environment
// (envWithoutBeadsStorageSettings drops every BEADS_DOLT_* variable): a named
// port is declared external by rule 2c (#5934) with or without --external, so
// it would launch nothing here either way.
func TestInitServerWithoutNamedPortStillStartsServer(t *testing.T) {
	f := newExternalInitFixture(t)

	res := f.run("", "init", "--quiet", "--server", "--prefix", "x", "--skip-hooks", "--skip-agents")

	if res.launches == "" {
		t.Errorf("bd init --server naming no port never reached dolt, so auto-start regressed:\n%s", res.output)
	}
}

// TestInitExternalUnreachableMessage covers the wording across the routes an
// --external init can take. Every one of them gives the same message, whether
// auto-start is pinned off by the environment, the server is a shared one, or
// the endpoint is a socket.
func TestInitExternalUnreachableMessage(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		autoStart string
		endpoint  func(t *testing.T) externalEndpoint
	}{
		{"server host:port with auto-start pinned off", "--server", "0",
			func(t *testing.T) externalEndpoint { return tcpEndpoint(freeLoopbackPort(t)) }},
		{"shared-server host:port", "--shared-server", "",
			func(t *testing.T) externalEndpoint { return tcpEndpoint(freeLoopbackPort(t)) }},
		{"server socket", "--server", "",
			func(t *testing.T) externalEndpoint { return unixEndpoint(externalInitSocketPath(t)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newExternalInitFixture(t)
			ep := tc.endpoint(t)

			res := f.run(tc.autoStart, append(append([]string{"init", "--quiet", tc.mode, "--external"}, ep.flags...),
				"--prefix", "x", "--skip-hooks", "--skip-agents")...)

			if !res.failed {
				t.Fatalf("bd init succeeded against a dead endpoint %s:\n%s", ep.addr, res.output)
			}
			requireExternalUnreachableMessage(t, res.output, ep.addr)
		})
	}
}

// TestInitExternalProvisionerShape runs init with the argv a provisioner of an
// externally managed server uses, in both endpoint forms, with auto-start
// unset and pinned off, against a server that is up and one that is not. Every
// cell is a control: a port named with --server-port is already declared
// external by rule 2c (#5934), and a socket is never auto-started, so none of
// them launched dolt before init honored --external either. They pin that the
// provisioner shape keeps launching nothing, keeps failing against a dead
// endpoint, and still records the endpoint it was given. The reachable cells
// need the Dolt test server and skip without it.
func TestInitExternalProvisionerShape(t *testing.T) {
	for _, form := range []string{"host-port", "socket"} {
		for _, autoStart := range []string{"unset", "0"} {
			for _, reachable := range []bool{false, true} {
				state := "unreachable"
				if reachable {
					state = "reachable"
				}
				t.Run(fmt.Sprintf("%s/auto-start=%s/%s", form, autoStart, state), func(t *testing.T) {
					runProvisionerCell(t, form, autoStart, reachable)
				})
			}
		}
	}
}

func runProvisionerCell(t *testing.T, form, autoStart string, reachable bool) {
	t.Helper()
	if reachable {
		skipIfNoDolt(t)
	}
	f := newExternalInitFixture(t)

	var ep externalEndpoint
	var socketConnections func() int
	switch {
	case form == "host-port" && reachable:
		ep = tcpEndpoint(testDoltServerPort)
	case form == "host-port":
		ep = tcpEndpoint(freeLoopbackPort(t))
	case reachable:
		var sock string
		sock, socketConnections = serveUnixProxy(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(testDoltServerPort)))
		ep = unixEndpoint(sock)
	default:
		ep = unixEndpoint(externalInitSocketPath(t))
	}

	database := "provisioned"
	if reachable {
		database = uniqueTestDBName(t)
		t.Cleanup(func() { dropTestDatabase(database, testDoltServerPort) })
	}

	args := append([]string{"init", "--init-if-missing", "--quiet", "--server", "--external"}, ep.flags...)
	args = append(args, "-p", "prov", "--skip-hooks", "--skip-agents", "--database", database, f.repoDir)
	pin := ""
	if autoStart == "0" {
		pin = "0"
	}

	res := f.run(pin, args...)

	requireNoLaunch(t, res)
	if !reachable {
		if !res.failed {
			t.Errorf("bd init succeeded against an unreachable endpoint %s:\n%s", ep.addr, res.output)
		}
		requireNothingListening(t, ep.network, ep.addr)
		return
	}
	if res.failed {
		t.Fatalf("bd init against the reachable server %s failed:\n%s", ep.addr, res.output)
	}
	requireDoltRootEmpty(t, f.repoDir)
	data, err := os.ReadFile(filepath.Join(f.repoDir, ".beads", "metadata.json"))
	if err != nil {
		t.Fatalf("expected metadata.json after init: %v", err)
	}
	var meta configfile.Config
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse metadata.json: %v\n%s", err, data)
	}
	if form == "socket" {
		if meta.DoltServerSocket != ep.addr {
			t.Errorf("metadata.json dolt_server_socket = %q, want the socket init was given, %q", meta.DoltServerSocket, ep.addr)
		}
		if socketConnections() == 0 {
			t.Errorf("bd never connected through the socket %s", ep.addr)
		}
		return
	}
	if meta.DoltServerHost != "127.0.0.1" || meta.DoltServerPort != testDoltServerPort {
		t.Errorf("metadata.json names %s:%d, want the endpoint init was given, %s", meta.DoltServerHost, meta.DoltServerPort, ep.addr)
	}
}
