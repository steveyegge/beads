package rpc

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/testutil/teststore"
)

// setupAdminTestServer creates a test server for admin RPC tests.
func setupAdminTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	dbPath := filepath.Join(beadsDir, "beads.db")

	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	store := teststore.New(t)

	server := NewServer(filepath.Join(beadsDir, "daemon.sock"), store, tmpDir, dbPath)
	return server, tmpDir
}

func TestHandleAdminGC_NoDoltDir(t *testing.T) {
	server, _ := setupAdminTestServer(t)

	args := AdminGCArgs{}
	argsJSON, _ := json.Marshal(args)

	resp := server.handleAdminGC(&Request{
		Operation: OpAdminGC,
		Args:      argsJSON,
	})

	if resp.Success {
		t.Fatal("expected failure when dolt directory does not exist")
	}
	if resp.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestHandleAdminGC_DryRun(t *testing.T) {
	server, tmpDir := setupAdminTestServer(t)

	// Create a fake dolt directory with some data
	doltDir := filepath.Join(tmpDir, ".beads", "dolt")
	if err := os.MkdirAll(doltDir, 0755); err != nil {
		t.Fatalf("failed to create dolt dir: %v", err)
	}
	// Write a dummy file to give the directory a measurable size
	if err := os.WriteFile(filepath.Join(doltDir, "data"), make([]byte, 1024), 0644); err != nil {
		t.Fatalf("failed to write dummy data: %v", err)
	}

	args := AdminGCArgs{DryRun: true}
	argsJSON, _ := json.Marshal(args)

	resp := server.handleAdminGC(&Request{
		Operation: OpAdminGC,
		Args:      argsJSON,
	})

	if !resp.Success {
		t.Fatalf("dry run failed: %s", resp.Error)
	}

	var result AdminGCResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if result.DoltPath != doltDir {
		t.Errorf("expected DoltPath=%s, got %s", doltDir, result.DoltPath)
	}
	if result.BytesBefore < 1024 {
		t.Errorf("expected BytesBefore >= 1024, got %d", result.BytesBefore)
	}
	if result.SpaceFreed != 0 {
		t.Errorf("expected SpaceFreed=0 for dry run, got %d", result.SpaceFreed)
	}
}

func TestHandleAdminGC_RealGC(t *testing.T) {
	// Skip if dolt is not installed
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed, skipping real GC test")
	}

	server, tmpDir := setupAdminTestServer(t)

	// Initialize a real dolt repo
	doltDir := filepath.Join(tmpDir, ".beads", "dolt")
	if err := os.MkdirAll(doltDir, 0755); err != nil {
		t.Fatalf("failed to create dolt dir: %v", err)
	}
	// Configure dolt user for this temp directory
	cfgCmd := exec.Command("dolt", "config", "--global", "--add", "user.name", "test")
	cfgCmd.Dir = doltDir
	cfgCmd.Env = append(os.Environ(), "DOLT_ROOT_PATH="+tmpDir)
	cfgCmd.CombinedOutput() // ignore errors if already set

	cfgCmd2 := exec.Command("dolt", "config", "--global", "--add", "user.email", "test@test.com")
	cfgCmd2.Dir = doltDir
	cfgCmd2.Env = append(os.Environ(), "DOLT_ROOT_PATH="+tmpDir)
	cfgCmd2.CombinedOutput()

	cmd := exec.Command("dolt", "init")
	cmd.Dir = doltDir
	cmd.Env = append(os.Environ(), "DOLT_ROOT_PATH="+tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dolt init failed: %v\n%s", err, string(out))
	}

	args := AdminGCArgs{}
	argsJSON, _ := json.Marshal(args)

	resp := server.handleAdminGC(&Request{
		Operation: OpAdminGC,
		Args:      argsJSON,
	})

	if !resp.Success {
		t.Fatalf("gc failed: %s", resp.Error)
	}

	var result AdminGCResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result.DoltPath != doltDir {
		t.Errorf("expected DoltPath=%s, got %s", doltDir, result.DoltPath)
	}
	if result.DryRun {
		t.Error("expected DryRun=false for real GC")
	}
	if result.ElapsedMs < 0 {
		t.Errorf("expected non-negative ElapsedMs, got %d", result.ElapsedMs)
	}
}

func TestHandleAdminGC_InvalidArgs(t *testing.T) {
	server, _ := setupAdminTestServer(t)

	resp := server.handleAdminGC(&Request{
		Operation: OpAdminGC,
		Args:      json.RawMessage(`{invalid`),
	})

	if resp.Success {
		t.Fatal("expected failure for invalid args")
	}
}

func TestAdminGetDirSize(t *testing.T) {
	tmpDir := t.TempDir()

	// Write known data
	if err := os.WriteFile(filepath.Join(tmpDir, "file1"), make([]byte, 500), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "file2"), make([]byte, 300), 0644); err != nil {
		t.Fatal(err)
	}

	size, err := adminGetDirSize(tmpDir)
	if err != nil {
		t.Fatalf("adminGetDirSize failed: %v", err)
	}
	if size != 800 {
		t.Errorf("expected size=800, got %d", size)
	}
}
