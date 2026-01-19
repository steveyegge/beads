//go:build cgo

package factory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
)

func init() {
	RegisterBackend(configfile.BackendDolt, func(ctx context.Context, path string, opts Options) (storage.Storage, error) {
		// Check if bootstrap is needed (JSONL exists but Dolt doesn't)
		// Path is the dolt subdirectory, parent is .beads directory
		beadsDir := filepath.Dir(path)

		bootstrapped, result, err := dolt.Bootstrap(ctx, dolt.BootstrapConfig{
			BeadsDir:    beadsDir,
			DoltPath:    path,
			LockTimeout: opts.LockTimeout,
		})
		if err != nil {
			return nil, fmt.Errorf("bootstrap failed: %w", err)
		}

		if bootstrapped && result != nil {
			// Report bootstrap results
			fmt.Fprintf(os.Stderr, "Bootstrapping Dolt from JSONL...\n")
			if len(result.ParseErrors) > 0 {
				fmt.Fprintf(os.Stderr, "  Skipped %d malformed lines (see above for details)\n", len(result.ParseErrors))
			}
			fmt.Fprintf(os.Stderr, "  Imported %d issues", result.IssuesImported)
			if result.IssuesSkipped > 0 {
				fmt.Fprintf(os.Stderr, ", skipped %d duplicates", result.IssuesSkipped)
			}
			fmt.Fprintf(os.Stderr, "\n  Dolt database ready\n")
		}

		// Calculate retry configuration from LockTimeout
		// Default: 30 retries with 100ms initial delay = ~6 seconds
		lockRetries := 30
		lockRetryDelay := 100 * time.Millisecond
		if opts.LockTimeout > 0 {
			// Convert timeout to number of retries with exponential backoff
			// Formula: timeout ≈ initialDelay * (2^retries - 1)
			// For simplicity, assume ~200ms per retry on average
			lockRetries = int(opts.LockTimeout.Milliseconds() / 200)
			if lockRetries < 1 {
				lockRetries = 1
			}
			if lockRetries > 100 {
				lockRetries = 100 // Cap at 100 retries to avoid excessive waits
			}
		}

		return dolt.New(ctx, &dolt.Config{
			Path:           path,
			ReadOnly:       opts.ReadOnly,
			LockRetries:    lockRetries,
			LockRetryDelay: lockRetryDelay,
		})
	})
}
