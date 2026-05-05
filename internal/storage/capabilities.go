// Package storage — capabilities.go
//
// Capability gates and helpers for backends that implement only a subset
// of the full DoltStorage surface. See docs/AGENTS.md "Storage Boundary"
// and ADR be-l7t.1 §4 for the design rationale: cmd/bd consumers bind to
// the narrow Storage interface and reach optional capabilities through
// these helpers, so a backend that does not implement a Dolt-specific
// capability (e.g., a future Postgres backend) returns a typed error
// instead of breaking compilation.
package storage

import "fmt"

// ErrCapabilityMissing is returned when a Storage does not implement an
// optional capability. The Capability field is a human-readable name.
type ErrCapabilityMissing struct {
	Capability string
}

func (e ErrCapabilityMissing) Error() string {
	return fmt.Sprintf("storage backend does not support: %s", e.Capability)
}

// MustCapabilities returns the canonical names of capability sub-interfaces
// that every supported backend implements. Test fixtures iterate this list
// to assert no future driver silently omits one.
func MustCapabilities() []string {
	return []string{
		"BulkIssueStore",
		"DependencyQueryStore",
		"AnnotationStore",
		"ConfigMetadataStore",
		"CompactionStore",
		"AdvancedQueryStore",
		"LifecycleManager",
	}
}

// DoltOnlyCapabilities returns the canonical names of capabilities tied
// to Dolt's storage-versioning model. Postgres (and any other backend
// without a commit graph) intentionally does not implement these.
func DoltOnlyCapabilities() []string {
	return []string{
		"VersionControl",
		"HistoryViewer",
		"RemoteStore",
		"SyncStore",
		"FederationStore",
		"PendingCommitter",
		"Flattener",
		"Compactor",
		"GarbageCollector",
		"BackupStore",
		"StoreLocator",
	}
}

// HasCapability reports whether s (or its inner store, if s is a
// HookFiringStore) implements the named capability.
func HasCapability(s Storage, name string) bool {
	inner := UnwrapStore(s)
	switch name {
	case "BulkIssueStore":
		_, ok := inner.(BulkIssueStore)
		return ok
	case "DependencyQueryStore":
		_, ok := inner.(DependencyQueryStore)
		return ok
	case "AnnotationStore":
		_, ok := inner.(AnnotationStore)
		return ok
	case "ConfigMetadataStore":
		_, ok := inner.(ConfigMetadataStore)
		return ok
	case "CompactionStore":
		_, ok := inner.(CompactionStore)
		return ok
	case "AdvancedQueryStore":
		_, ok := inner.(AdvancedQueryStore)
		return ok
	case "LifecycleManager":
		_, ok := inner.(LifecycleManager)
		return ok
	case "VersionControl":
		_, ok := inner.(VersionControl)
		return ok
	case "HistoryViewer":
		_, ok := inner.(HistoryViewer)
		return ok
	case "RemoteStore":
		_, ok := inner.(RemoteStore)
		return ok
	case "SyncStore":
		_, ok := inner.(SyncStore)
		return ok
	case "FederationStore":
		_, ok := inner.(FederationStore)
		return ok
	case "PendingCommitter":
		_, ok := inner.(PendingCommitter)
		return ok
	case "Flattener":
		_, ok := inner.(Flattener)
		return ok
	case "Compactor":
		_, ok := inner.(Compactor)
		return ok
	case "GarbageCollector":
		_, ok := inner.(GarbageCollector)
		return ok
	case "BackupStore":
		_, ok := inner.(BackupStore)
		return ok
	case "StoreLocator":
		_, ok := inner.(StoreLocator)
		return ok
	case "RawDBAccessor":
		_, ok := inner.(RawDBAccessor)
		return ok
	}
	return false
}

// RequireDoltStorage returns the underlying store as DoltStorage if s
// (or its inner store, if s is a HookFiringStore) satisfies it. Use from
// cmd/bd commands that intrinsically require the full Dolt capability set
// (bd dolt, bd vc, bd branch, bd flatten, etc.).
func RequireDoltStorage(s Storage) (DoltStorage, error) {
	if d, ok := UnwrapStore(s).(DoltStorage); ok {
		return d, nil
	}
	return nil, ErrCapabilityMissing{Capability: "DoltStorage"}
}

// RequireVersionControl returns the VersionControl capability or a typed
// error if the backend does not implement it.
func RequireVersionControl(s Storage) (VersionControl, error) {
	if c, ok := UnwrapStore(s).(VersionControl); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "VersionControl"}
}

// RequireHistoryViewer returns the HistoryViewer capability or a typed
// error if the backend does not implement it.
func RequireHistoryViewer(s Storage) (HistoryViewer, error) {
	if c, ok := UnwrapStore(s).(HistoryViewer); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "HistoryViewer"}
}

// RequireRemoteStore returns the RemoteStore capability or a typed error
// if the backend does not implement it.
func RequireRemoteStore(s Storage) (RemoteStore, error) {
	if c, ok := UnwrapStore(s).(RemoteStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "RemoteStore"}
}

// RequireSyncStore returns the SyncStore capability or a typed error
// if the backend does not implement it.
func RequireSyncStore(s Storage) (SyncStore, error) {
	if c, ok := UnwrapStore(s).(SyncStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "SyncStore"}
}

// RequireFederationStore returns the FederationStore capability or a typed
// error if the backend does not implement it.
func RequireFederationStore(s Storage) (FederationStore, error) {
	if c, ok := UnwrapStore(s).(FederationStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "FederationStore"}
}

// RequirePendingCommitter returns the PendingCommitter capability or a
// typed error if the backend does not implement it.
func RequirePendingCommitter(s Storage) (PendingCommitter, error) {
	if c, ok := UnwrapStore(s).(PendingCommitter); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "PendingCommitter"}
}

// RequireFlattener returns the Flattener capability or a typed error
// if the backend does not implement it.
func RequireFlattener(s Storage) (Flattener, error) {
	if c, ok := UnwrapStore(s).(Flattener); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "Flattener"}
}

// RequireCompactor returns the Compactor capability or a typed error
// if the backend does not implement it.
func RequireCompactor(s Storage) (Compactor, error) {
	if c, ok := UnwrapStore(s).(Compactor); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "Compactor"}
}

// RequireGarbageCollector returns the GarbageCollector capability or a
// typed error if the backend does not implement it.
func RequireGarbageCollector(s Storage) (GarbageCollector, error) {
	if c, ok := UnwrapStore(s).(GarbageCollector); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "GarbageCollector"}
}

// RequireBackupStore returns the BackupStore capability or a typed error
// if the backend does not implement it.
func RequireBackupStore(s Storage) (BackupStore, error) {
	if c, ok := UnwrapStore(s).(BackupStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "BackupStore"}
}

// RequireStoreLocator returns the StoreLocator capability or a typed error
// if the backend does not implement it.
func RequireStoreLocator(s Storage) (StoreLocator, error) {
	if c, ok := UnwrapStore(s).(StoreLocator); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "StoreLocator"}
}

// RequireRawDBAccessor returns the RawDBAccessor capability or a typed
// error if the backend does not implement it. RawDBAccessor is a deferred
// capability for the Postgres backend (see ADR be-l7t.1 §2): the Dolt
// backend implements it; future backends may.
func RequireRawDBAccessor(s Storage) (RawDBAccessor, error) {
	if c, ok := UnwrapStore(s).(RawDBAccessor); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "RawDBAccessor"}
}

// RequireBulkIssueStore returns the BulkIssueStore capability. BulkIssueStore
// is a MUST capability — every backend implements it — but the helper
// exists for symmetry with the Dolt-only Require<X> helpers and for tests
// that exercise unusual store wrappers.
func RequireBulkIssueStore(s Storage) (BulkIssueStore, error) {
	if c, ok := UnwrapStore(s).(BulkIssueStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "BulkIssueStore"}
}

// RequireDependencyQueryStore returns the DependencyQueryStore capability.
// MUST capability; see RequireBulkIssueStore for rationale.
func RequireDependencyQueryStore(s Storage) (DependencyQueryStore, error) {
	if c, ok := UnwrapStore(s).(DependencyQueryStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "DependencyQueryStore"}
}

// RequireAnnotationStore returns the AnnotationStore capability. MUST.
func RequireAnnotationStore(s Storage) (AnnotationStore, error) {
	if c, ok := UnwrapStore(s).(AnnotationStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "AnnotationStore"}
}

// RequireConfigMetadataStore returns the ConfigMetadataStore capability. MUST.
func RequireConfigMetadataStore(s Storage) (ConfigMetadataStore, error) {
	if c, ok := UnwrapStore(s).(ConfigMetadataStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "ConfigMetadataStore"}
}

// RequireCompactionStore returns the CompactionStore capability. MUST.
func RequireCompactionStore(s Storage) (CompactionStore, error) {
	if c, ok := UnwrapStore(s).(CompactionStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "CompactionStore"}
}

// RequireAdvancedQueryStore returns the AdvancedQueryStore capability. MUST.
func RequireAdvancedQueryStore(s Storage) (AdvancedQueryStore, error) {
	if c, ok := UnwrapStore(s).(AdvancedQueryStore); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "AdvancedQueryStore"}
}

// RequireLifecycleManager returns the LifecycleManager capability. MUST.
func RequireLifecycleManager(s Storage) (LifecycleManager, error) {
	if c, ok := UnwrapStore(s).(LifecycleManager); ok {
		return c, nil
	}
	return nil, ErrCapabilityMissing{Capability: "LifecycleManager"}
}
