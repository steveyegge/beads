package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// autoPushVersionWarned ensures the Dolt version warning is shown at most once per session.
var autoPushVersionWarned bool

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

	// One-time version check: warn if Dolt is too old for fast git remote push.
	if !autoPushVersionWarned {
		autoPushVersionWarned = true
		if ver, verErr := st.Version(ctx); verErr == nil {
			if compareDoltVersion(ver, "1.82.4") < 0 {
				fmt.Fprintf(os.Stderr, "Warning: Dolt %s detected; upgrade to 1.82.4+ for fast git remote push (70s → 8s)\n", ver)
			}
		}
	}

	if err := st.Push(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dolt auto-push failed: %v\n", err)
	}
}

// compareDoltVersion compares two semver-style version strings (e.g., "1.82.4").
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Non-numeric or malformed versions compare as 0.0.0.
func compareDoltVersion(a, b string) int {
	pa := parseSemver(a)
	pb := parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// parseSemver extracts [major, minor, patch] from a version string.
// Tolerates leading "v" and trailing suffixes (e.g., "v1.82.4-rc1").
func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		// Strip anything after a hyphen (pre-release suffix)
		p := parts[i]
		if idx := strings.IndexByte(p, '-'); idx >= 0 {
			p = p[:idx]
		}
		n, err := strconv.Atoi(p)
		if err == nil {
			result[i] = n
		}
	}
	return result
}
