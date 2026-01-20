//go:build cgo

package factory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
)

func init() {
	RegisterBackend(configfile.BackendDolt, func(ctx context.Context, path string, opts Options) (storage.Storage, error) {
		// Skip bootstrap for read-only mode - we don't want to create/modify anything
		// Read-only commands (list, show, ready, etc.) should never trigger bootstrap
		if !opts.ReadOnly {
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
		}

		// Server mode is enabled by default for Dolt to avoid lock contention (bd-f4f78a)
		// Can be disabled with BEADS_DOLT_SERVER_MODE=0
		serverMode := opts.ServerMode
		if os.Getenv("BEADS_DOLT_SERVER_MODE") != "0" && os.Getenv("BEADS_DOLT_SERVER_MODE") != "false" {
			serverMode = true
		}

		return dolt.New(ctx, &dolt.Config{
			Path:        path,
			ReadOnly:    opts.ReadOnly,
			IdleTimeout: opts.IdleTimeout,
			// Server mode options (bd-f4f78a)
			ServerMode: serverMode,
			ServerHost: opts.ServerHost,
			ServerPort: opts.ServerPort,
			ServerUser: opts.ServerUser,
			ServerPass: opts.ServerPass,
		})
	})
}
