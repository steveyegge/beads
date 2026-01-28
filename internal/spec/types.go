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
	Lifecycle     string
	CompletedAt   *time.Time
	Summary       string
	SummaryTokens int
	ArchivedAt    *time.Time
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

// SpecScanEvent records a scan of a spec file.
type SpecScanEvent struct {
	SpecID    string
	ScannedAt time.Time
	SHA256    string
	Changed   bool
}

// SpecRiskEntry summarizes drift risk signals for a spec.
type SpecRiskEntry struct {
	SpecID        string
	Title         string
	ChangeCount   int
	LastChangedAt *time.Time
	OpenIssues    int
}
