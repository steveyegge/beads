package main

import (
	"context"
	"fmt"
	"os"
)

// maybeAutoPush pushes to the configured remote after a successful auto-commit.
//
// Semantics:
//   - Only applies when dolt auto-push is "on" AND the active store is versioned (Dolt).
//   - Requires a remote named "origin" to be configured (skips silently otherwise).
//   - Push failures are warnings only (stderr) — a failed push must never block local work.
func maybeAutoPush(ctx context.Context) {
	mode, err := getDoltAutoPushMode()
	if err != nil || mode != doltAutoPushOn {
		return
	}

	st := getStore()
	if st == nil {
		return
	}

	hasRemote, err := st.HasRemote(ctx, "origin")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dolt auto-push: failed to check remote: %v\n", err)
		return
	}
	if !hasRemote {
		return
	}

	if err := st.Push(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dolt auto-push failed: %v\n", err)
	}
}
