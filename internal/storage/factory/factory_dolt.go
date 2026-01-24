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
		// Only bootstrap in embedded mode - server mode has database on server
		if !opts.ServerMode {
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

		// Auto-start dolt sql-server if server mode is enabled but server isn't running
		if opts.ServerMode {
			if err := dolt.EnsureServerRunning(ctx, path, opts.ServerHost, opts.ServerPort); err != nil {
				return nil, fmt.Errorf("failed to ensure dolt server is running: %w", err)
			}
		}

		return dolt.New(ctx, &dolt.Config{
			Path:       path,
			Database:   opts.Database,
			ReadOnly:   opts.ReadOnly,
			ServerMode: opts.ServerMode,
			ServerHost: opts.ServerHost,
			ServerPort: opts.ServerPort,
			ServerUser: opts.ServerUser,
		})
	})
}
