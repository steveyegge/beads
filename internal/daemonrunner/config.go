package daemonrunner

import "time"

// Config holds all configuration for the daemon
type Config struct {
	// Sync behavior
	Interval   time.Duration
	AutoCommit bool
	AutoPush   bool

	// Scope
	Global bool

	// Paths
	LogFile   string
	PIDFile   string
	DBPath    string // Local daemon only
	BeadsDir  string // Local daemon: .beads dir, Global daemon: ~/.beads

	// RPC
	SocketPath string

	// Workspace
	WorkspacePath string // Only for local daemon: parent of .beads directory
}
