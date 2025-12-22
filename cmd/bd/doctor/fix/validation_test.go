package fix

import (
	"testing"
)

// TestFixFunctions_RequireBeadsDir verifies all fix functions properly validate
// that a .beads directory exists before attempting fixes.
// This replaces 10+ individual "missing .beads directory" subtests.
func TestFixFunctions_RequireBeadsDir(t *testing.T) {
	funcs := []struct {
		name string
		fn   func(string) error
	}{
		{"GitHooks", GitHooks},
		{"MergeDriver", MergeDriver},
		{"Daemon", Daemon},
		{"DBJSONLSync", DBJSONLSync},
		{"DatabaseVersion", DatabaseVersion},
		{"SchemaCompatibility", SchemaCompatibility},
		{"SyncBranchConfig", SyncBranchConfig},
		{"SyncBranchHealth", func(dir string) error { return SyncBranchHealth(dir, "beads-sync") }},
		{"UntrackedJSONL", UntrackedJSONL},
		{"MigrateTombstones", MigrateTombstones},
	}

	for _, tc := range funcs {
		t.Run(tc.name, func(t *testing.T) {
			// Use a temp directory without .beads
			dir := t.TempDir()
			err := tc.fn(dir)
			if err == nil {
				t.Errorf("%s should return error for missing .beads directory", tc.name)
			}
		})
	}
}
