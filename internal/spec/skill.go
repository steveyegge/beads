package spec

import "time"

// SkillLayer represents where a skill is found (claude, codex-superpowers, codex-local)
type SkillLayer string

const (
	SkillLayerClaude             SkillLayer = "claude"
	SkillLayerCodexSuperpowers   SkillLayer = "codex-superpowers"
	SkillLayerCodexLocal         SkillLayer = "codex-local"
)

// ScannedSkill represents a skill file discovered on disk
type ScannedSkill struct {
	SkillID     string
	Layer       SkillLayer
	Path        string
	Title       string
	Description string
	Version     string
	SHA256      string
	Mtime       time.Time
}

// SkillLayerEntry represents a skill in a specific layer
type SkillLayerEntry struct {
	Layer       SkillLayer
	Path        string
	Version     string
	SHA256      string
	Mtime       time.Time
	LastScannedAt time.Time
}

// SkillRegistryEntry represents a stored skill record (tracks all layers)
type SkillRegistryEntry struct {
	SkillID              string
	Title                string
	Description          string
	Layers               []SkillLayerEntry
	Mismatch             bool
	MismatchReason       string
	ReferencedBySpecs    []string
	DiscoveredAt         time.Time
	LastScannedAt        time.Time
	LastMismatchDetectedAt *time.Time
}

// SkillMismatch represents a detected version mismatch
type SkillMismatch struct {
	SkillID         string
	Reason          string
	AffectedLayers  []SkillLayer
	Versions        map[SkillLayer]string
	Severity        string // "HIGH", "MEDIUM", "LOW"
	AffectedSpecs   []string
	RecommendedFix  string
}

// SkillScanResult summarizes a skill scan
type SkillScanResult struct {
	Scanned              int                   `json:"scanned"`
	Added                int                   `json:"added"`
	Updated              int                   `json:"updated"`
	Unchanged            int                   `json:"unchanged"`
	Mismatches           int                   `json:"mismatches"`
	MismatchedSkillIDs   []string              `json:"mismatched_skill_ids,omitempty"`
	DiscoveredMismatches []SkillMismatch       `json:"discovered_mismatches,omitempty"`
}

// SkillSpecLink represents a skill referenced by a spec
type SkillSpecLink struct {
	SkillID       string
	SpecID        string
	RequiredVersion string // optional: e.g., "1.2+"
	Context       string   // where in the spec is it mentioned
}
