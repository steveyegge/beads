package spec

import "time"

// ScannedSpec represents a spec file discovered on disk.
type ScannedSpec struct {
	SpecID string
	Path   string
	Title  string
	SHA256 string
	Mtime  time.Time
}

// SpecRegistryEntry represents a stored spec record.
type SpecRegistryEntry struct {
	SpecID        string
	Path          string
	Title         string
	SHA256        string
	Mtime         time.Time
	DiscoveredAt  time.Time
	LastScannedAt time.Time
	MissingAt     *time.Time
}

// SpecRegistryCount includes bead counts for a spec.
type SpecRegistryCount struct {
	Spec             SpecRegistryEntry
	BeadCount        int
	ChangedBeadCount int
}

// SpecScanResult summarizes a scan.
type SpecScanResult struct {
	Scanned        int      `json:"scanned"`
	Added          int      `json:"added"`
	Updated        int      `json:"updated"`
	Unchanged      int      `json:"unchanged"`
	Missing        int      `json:"missing"`
	MarkedBeads    int      `json:"marked_beads"`
	ChangedSpecIDs []string `json:"changed_spec_ids,omitempty"`
}
