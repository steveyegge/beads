package daemonrunner

import (
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	cfg := Config{
		Interval:   5 * time.Second,
		AutoCommit: true,
		AutoPush:   false,
		Global:     false,
	}

	if cfg.Interval != 5*time.Second {
		t.Errorf("Expected Interval 5s, got %v", cfg.Interval)
	}
	if !cfg.AutoCommit {
		t.Error("Expected AutoCommit to be true")
	}
	if cfg.AutoPush {
		t.Error("Expected AutoPush to be false")
	}
	if cfg.Global {
		t.Error("Expected Global to be false")
	}
}

func TestConfigLocalDaemon(t *testing.T) {
	cfg := Config{
		Global:        false,
		WorkspacePath: "/tmp/test-workspace",
		BeadsDir:      "/tmp/test-workspace/.beads",
		DBPath:        "/tmp/test-workspace/.beads/beads.db",
		SocketPath:    "/tmp/test-workspace/.beads/bd.sock",
		LogFile:       "/tmp/test-workspace/.beads/daemon.log",
		PIDFile:       "/tmp/test-workspace/.beads/daemon.pid",
	}

	if cfg.Global {
		t.Error("Expected local daemon (Global=false)")
	}
	if cfg.WorkspacePath == "" {
		t.Error("Expected WorkspacePath to be set for local daemon")
	}
	if cfg.DBPath == "" {
		t.Error("Expected DBPath to be set for local daemon")
	}
}

func TestConfigGlobalDaemon(t *testing.T) {
	cfg := Config{
		Global:     true,
		BeadsDir:   "/home/user/.beads",
		SocketPath: "/home/user/.beads/global.sock",
		LogFile:    "/home/user/.beads/global-daemon.log",
		PIDFile:    "/home/user/.beads/global-daemon.pid",
	}

	if !cfg.Global {
		t.Error("Expected global daemon (Global=true)")
	}
	if cfg.WorkspacePath != "" {
		t.Error("Expected WorkspacePath to be empty for global daemon")
	}
	if cfg.DBPath != "" {
		t.Error("Expected DBPath to be empty for global daemon")
	}
}

func TestConfigSyncBehavior(t *testing.T) {
	tests := []struct {
		name       string
		autoCommit bool
		autoPush   bool
	}{
		{"no sync", false, false},
		{"commit only", true, false},
		{"commit and push", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				AutoCommit: tt.autoCommit,
				AutoPush:   tt.autoPush,
			}

			if cfg.AutoCommit != tt.autoCommit {
				t.Errorf("Expected AutoCommit=%v, got %v", tt.autoCommit, cfg.AutoCommit)
			}
			if cfg.AutoPush != tt.autoPush {
				t.Errorf("Expected AutoPush=%v, got %v", tt.autoPush, cfg.AutoPush)
			}
		})
	}
}
