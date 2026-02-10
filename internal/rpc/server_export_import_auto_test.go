package rpc

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/autoimport"
	"github.com/steveyegge/beads/internal/storage"
)

func TestCheckAndAutoImportIfStale_SerializesConcurrentImports(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	dbPath := server.storage.Path()
	beadsDir := filepath.Dir(dbPath)
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	if err := os.WriteFile(jsonlPath, []byte("{}\n"), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	old := time.Now().Add(-10 * time.Minute).Format(time.RFC3339Nano)
	if err := server.storage.SetMetadata(ctx, "last_import_time", old); err != nil {
		t.Fatalf("set last_import_time: %v", err)
	}

	orig := autoImportIfNewerFn
	defer func() { autoImportIfNewerFn = orig }()

	var calls atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	autoImportIfNewerFn = func(
		_ context.Context,
		_ storage.Storage,
		_ string,
		_ autoimport.Notifier,
		_ autoimport.ImportFunc,
		_ func(bool),
	) error {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return nil
	}

	req := &Request{Operation: OpList}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = server.checkAndAutoImportIfStale(req)
		}()
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("import did not start")
	}
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("auto-import calls = %d, want 1 (single-flight serialization)", got)
	}
}
