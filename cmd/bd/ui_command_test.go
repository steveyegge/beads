package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// concurrentBuffer is a threadsafe bytes buffer used to capture command output.
type concurrentBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *concurrentBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *concurrentBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestUICommandBindsAndServesHealth(t *testing.T) {
	workspace, dbFile := makeUITestWorkspace(t)
	stopDaemon := startTestDaemon(t, workspace, dbFile)
	t.Cleanup(stopDaemon)

	oldNoDaemon := noDaemon
	noDaemon = false
	defer func() { noDaemon = oldNoDaemon }()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Fatalf("restore wd: %v", chdirErr)
		}
	}()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}

	oldDBPath := dbPath
	dbPath = dbFile
	defer func() { dbPath = oldDBPath }()

	rootCmd.SetArgs([]string{"ui", "--listen", "127.0.0.1:0", "--no-open"})

	var stdout, stderr concurrentBuffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	defer func() {
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
		rootCmd.SetArgs(nil)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- rootCmd.ExecuteContext(ctx)
	}()

	urlRegex := regexp.MustCompile(`http://[^\s]+`)

	var baseURL string
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()

	for {
		if match := urlRegex.FindString(stdout.String()); match != "" {
			baseURL = strings.TrimSpace(match)
			break
		}

		select {
		case <-time.After(20 * time.Millisecond):
			continue
		case <-waitCtx.Done():
			cancel()
			err := <-errCh
			t.Fatalf("timeout waiting for server (stdout=%q stderr=%q err=%v)", stdout.String(), stderr.String(), err)
		}
	}

	resp, err := http.Get(fmt.Sprintf("%s/healthz", baseURL))
	if err != nil {
		cancel()
		t.Fatalf("health request failed: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("expected 200 health, got %d", resp.StatusCode)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("ui command returned error: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ui command did not exit after cancel")
	}
}
