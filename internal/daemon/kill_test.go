//go:build unix

package daemon

import (
	"os"
	"testing"

	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
)

func TestKillNonExistentDir(t *testing.T) {
	if err := Kill("/tmp/nonexistent-bdd-kill-xyz-qrs"); err != nil {
		t.Fatalf("non-existent dir: %v", err)
	}
}

func TestKillNoPidFile(t *testing.T) {
	tmp := t.TempDir()
	if err := Kill(tmp); err != nil {
		t.Fatalf("no pid file: %v", err)
	}
}

func TestKillStalePidFile(t *testing.T) {
	tmp := t.TempDir()
	// Write a pid file with a PID that is certainly not running.
	if err := pidfile.Write(tmp, "bdd.pid", pidfile.PidFile{Pid: 99999999}); err != nil {
		t.Fatal(err)
	}
	if err := Kill(tmp); err != nil {
		t.Fatalf("stale pid: %v", err)
	}
	if _, err := os.Stat(tmp + "/bdd.pid"); !os.IsNotExist(err) {
		t.Error("bdd.pid should be removed after stale kill")
	}
}

func TestKillIdempotent(t *testing.T) {
	tmp := t.TempDir()
	// Two calls on the same empty dir should both return nil.
	if err := Kill(tmp); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := Kill(tmp); err != nil {
		t.Fatalf("second call: %v", err)
	}
}
