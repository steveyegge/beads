package main

import (
	"fmt"
	"path/filepath"

	"github.com/steveyegge/beads/internal/doltserver"
)

// externallyManagedFreshStartRefusal decides whether `bd dolt start` may run
// when dolt.auto-start is off and the workspace's data dir holds no Dolt
// database (bd-7pkvvd / PR #7729).
//
// In such a workspace the server is managed outside bd, and `bd dolt start`
// does not reconnect to it: doltserver.Start runs dolt init on the empty data
// dir, launches a second, unsupervised sql-server, and publishes that port —
// after which every bd command resolves to an empty phantom database that
// shadows the real one. The refusal must fire before anything is written, so
// this predicate is read-only by construction: HasDoltDatabase never writes,
// DoltDirPath never creates directories (unlike ResolveDoltDir, whose
// shared-server branch mkdirs), and ManagesLiveServerOnPort is the read-only
// ownership probe (IsRunning repairs as it goes and can stop a live server
// whose port is unknown — it must not run on a mere classification path).
//
// autoStartDisabled is injected rather than read from
// doltserver.IsAutoStartDisabled() so tests are hermetic.
func externallyManagedFreshStartRefusal(serverDir string, autoStartDisabled, force bool) error {
	if force || !autoStartDisabled {
		return nil
	}
	doltDir := doltserver.DoltDirPath(serverDir)
	if doltserver.HasDoltDatabase(doltDir) {
		// A start over an existing database adopts or restarts it; that is
		// not a fresh mint.
		return nil
	}
	if port := doltserver.ReadPortFile(serverDir); port > 0 && doltserver.ManagesLiveServerOnPort(serverDir, port) {
		// bd already tracks a live server for this workspace (its state
		// files name a port the PID still serves); the data dir may simply
		// live elsewhere via dolt_data_dir.
		return nil
	}
	return fmt.Errorf(`refusing to start a new, empty Dolt database at %s

dolt.auto-start is off, so this workspace's server is managed outside bd, and
bd dolt start does not reconnect to it. With no database at %s, this start
would run dolt init on an empty directory, launch an unsupervised sql-server,
and publish its port in %s — and every later bd command would then use that
empty database instead of the real one.

If you really want a new, empty database here, rerun with --force. Otherwise,
have whoever manages the server start it over its own data directory.`,
		doltDir, doltDir, filepath.Join(serverDir, doltserver.PortFileName))
}