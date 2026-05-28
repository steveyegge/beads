// Package storage defines the interface for issue storage backends.
package storage

// OrphanHandling specifies how to handle issues with missing parent references.
type OrphanHandling string

const (
	// OrphanStrict fails import on missing parent (safest)
	OrphanStrict OrphanHandling = "strict"
	// OrphanResurrect auto-resurrects missing parents from database history
	OrphanResurrect OrphanHandling = "resurrect"
	// OrphanSkip skips orphaned issues with warning
	OrphanSkip OrphanHandling = "skip"
	// OrphanAllow imports orphans without validation (default, works around bugs)
	OrphanAllow OrphanHandling = "allow"
)

// BatchCreateOptions contains options for batch issue creation.
// This is a backend-agnostic type that can be used by any storage implementation.
type BatchCreateOptions struct {
	// OrphanHandling specifies how to handle issues with missing parent references
	OrphanHandling OrphanHandling
	// SkipPrefixValidation skips prefix validation for existing IDs (used during import)
	SkipPrefixValidation bool
	// ConflictSkip makes batch creation insert-if-new instead of UPSERT: an
	// issue whose ID already exists is left untouched rather than overwritten.
	// Used only by the auto-import upgrade-recovery fallback (GH#3955), so
	// that if the emptiness guard in maybeAutoImportJSONL ever regresses
	// again (cf. PR #3630), auto-import degrades to a harmless no-op instead
	// of clobbering live rows. Explicit `bd import` keeps UPSERT semantics.
	ConflictSkip bool
	// SkipDependencyValidationErrors skips dependency validation failures that
	// legacy imports tolerated, such as cycles or self-dependencies.
	SkipDependencyValidationErrors bool
	// OnSkippedDependency records dependency edges skipped during batch create.
	OnSkippedDependency func(issueID, dependsOnID, reason string)
}
