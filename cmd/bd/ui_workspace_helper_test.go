package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestUISessionManagerOpenFailsWhenNoDaemonFlag(t *testing.T) {
	manager := &uiSessionManager{}

	oldNoDaemon := noDaemon
	noDaemon = true
	defer func() { noDaemon = oldNoDaemon }()

	_, err := manager.Open(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "requires the Beads daemon") {
		t.Fatalf("expected daemon-required error, got %v", err)
	}
}

func TestUISessionManagerOpenFailsWithoutDaemonWhenAutoStartDisabled(t *testing.T) {
	manager := &uiSessionManager{}

	_, dbFile := makeUITestWorkspace(t)
	t.Setenv("BEADS_AUTO_START_DAEMON", "0")

	oldDBPath := dbPath
	dbPath = dbFile
	defer func() { dbPath = oldDBPath }()

	oldNoDaemon := noDaemon
	noDaemon = false
	defer func() { noDaemon = oldNoDaemon }()

	_, err := manager.Open(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "bd daemon") {
		t.Fatalf("expected missing-daemon error, got %v", err)
	}
}

func TestUISessionManagerOpenPopulatesMetadata(t *testing.T) {
	manager := &uiSessionManager{}

	workspace, dbFile := makeUITestWorkspace(t)
	stopDaemon := startTestDaemon(t, workspace, dbFile)
	defer stopDaemon()

	oldDBPath := dbPath
	dbPath = dbFile
	defer func() { dbPath = oldDBPath }()

	oldNoDaemon := noDaemon
	noDaemon = false
	defer func() { noDaemon = oldNoDaemon }()

	session, err := manager.Open(context.Background(), io.Discard)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}

	meta := session.Metadata
	if meta.SocketPath == "" {
		t.Fatal("expected socket path to be populated")
	}
	if meta.DatabasePath == "" {
		t.Fatal("expected database path to be populated")
	}
	if meta.WorkspacePath == "" {
		t.Fatal("expected workspace path to be populated")
	}
	if meta.AutoStartAttempted {
		t.Fatal("auto-start should not be attempted when daemon already running")
	}

	stored := manager.Metadata()
	if stored.SocketPath != meta.SocketPath {
		t.Fatalf("metadata not stored: got %q want %q", stored.SocketPath, meta.SocketPath)
	}
}
