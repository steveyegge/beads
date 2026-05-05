package teststub_test

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/teststub"
)

// TestExtensibility is the FR-10 check from be-l7t.2: a third-party-style
// package outside internal/storage can register a driver and dispatch to
// it via storage.Open without any edit to the storage package.
func TestExtensibility(t *testing.T) {
	cfg := storage.ConnectionConfig{BeadsDir: "/tmp/teststub", Database: "x", ReadOnly: true, DSN: "n/a"}
	got, err := storage.Open(context.Background(), teststub.Name, cfg)
	if err != nil {
		t.Fatalf("storage.Open(teststub) returned error: %v", err)
	}
	if got == nil {
		t.Fatal("storage.Open(teststub) returned nil Storage")
	}
	stub, ok := got.(*teststub.Stub)
	if !ok {
		t.Fatalf("storage.Open(teststub) returned %T, want *teststub.Stub", got)
	}
	if stub.Config != cfg {
		t.Errorf("Stub.Config = %+v, want %+v", stub.Config, cfg)
	}

	found := false
	for _, b := range storage.RegisteredBackends() {
		if b == teststub.Name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("teststub not in RegisteredBackends() = %v", storage.RegisteredBackends())
	}
}
