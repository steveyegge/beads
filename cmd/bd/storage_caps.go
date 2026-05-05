// Capability accessors for the cmd/bd consumer surface. After be-l7t.1
// the global Store is the narrow `storage.Storage`; capability methods
// are reached through these helpers (UnwrapStore + type-assert).
//
// Two flavors:
//
//   - mustXxx helpers — for MUST capabilities (ADR be-l7t.1 §2). Every
//     supported backend implements them, so the assertion never fails by
//     construction. A failed assertion is a programming/registration bug
//     and panics with a developer-friendly message.
//
//   - dXxx helpers — for Dolt-only capabilities. They require the store to
//     be Dolt-backed; on a non-Dolt backend they FatalErrorRespectJSON with
//     a backend-aware message. Use only from DOLT-ONLY files (per ADR §4.2).
package main

import (
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
)

// ── MUST capability accessors (never fail by construction) ────────────

func mustBulk(s storage.Storage) storage.BulkIssueStore {
	c, err := storage.RequireBulkIssueStore(s)
	if err != nil {
		panic(fmt.Sprintf("storage backend missing MUST capability: %v", err))
	}
	return c
}

func mustDeps(s storage.Storage) storage.DependencyQueryStore {
	c, err := storage.RequireDependencyQueryStore(s)
	if err != nil {
		panic(fmt.Sprintf("storage backend missing MUST capability: %v", err))
	}
	return c
}

func mustAnnot(s storage.Storage) storage.AnnotationStore {
	c, err := storage.RequireAnnotationStore(s)
	if err != nil {
		panic(fmt.Sprintf("storage backend missing MUST capability: %v", err))
	}
	return c
}

func mustConfig(s storage.Storage) storage.ConfigMetadataStore {
	c, err := storage.RequireConfigMetadataStore(s)
	if err != nil {
		panic(fmt.Sprintf("storage backend missing MUST capability: %v", err))
	}
	return c
}

func mustCompaction(s storage.Storage) storage.CompactionStore {
	c, err := storage.RequireCompactionStore(s)
	if err != nil {
		panic(fmt.Sprintf("storage backend missing MUST capability: %v", err))
	}
	return c
}

func mustAdvanced(s storage.Storage) storage.AdvancedQueryStore {
	c, err := storage.RequireAdvancedQueryStore(s)
	if err != nil {
		panic(fmt.Sprintf("storage backend missing MUST capability: %v", err))
	}
	return c
}

// ── Dolt-only capability accessors (FatalErrorRespectJSON on miss) ─────

func dVC(s storage.Storage) storage.VersionControl {
	c, err := storage.RequireVersionControl(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dHistory(s storage.Storage) storage.HistoryViewer {
	c, err := storage.RequireHistoryViewer(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dRemote(s storage.Storage) storage.RemoteStore {
	c, err := storage.RequireRemoteStore(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dSync(s storage.Storage) storage.SyncStore {
	c, err := storage.RequireSyncStore(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dFederation(s storage.Storage) storage.FederationStore {
	c, err := storage.RequireFederationStore(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dPending(s storage.Storage) storage.PendingCommitter {
	c, err := storage.RequirePendingCommitter(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dFlattener(s storage.Storage) storage.Flattener {
	c, err := storage.RequireFlattener(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dCompactor(s storage.Storage) storage.Compactor {
	c, err := storage.RequireCompactor(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dGC(s storage.Storage) storage.GarbageCollector {
	c, err := storage.RequireGarbageCollector(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dBackup(s storage.Storage) storage.BackupStore {
	c, err := storage.RequireBackupStore(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

func dStoreLocator(s storage.Storage) storage.StoreLocator {
	c, err := storage.RequireStoreLocator(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

// dRawDB returns the RawDBAccessor capability or surfaces the existing
// "storage backend does not support raw DB access" message. RawDBAccessor
// is deferred for Postgres (ADR be-l7t.1 §2 Deferred row); v1 PG backends
// surface the same gate that embedded-mode bd today surfaces.
func dRawDB(s storage.Storage) storage.RawDBAccessor {
	c, err := storage.RequireRawDBAccessor(s)
	if err != nil {
		FatalErrorRespectJSON("storage backend does not support raw DB access")
	}
	return c
}

// dDolt returns the full DoltStorage interface, FatalError if the
// underlying store is not Dolt. Used by DOLT-ONLY-by-design commands
// that need the entire fat surface (VersionControl + RemoteStore + …).
func dDolt(s storage.Storage) storage.DoltStorage {
	c, err := storage.RequireDoltStorage(s)
	if err != nil {
		FatalErrorRespectJSON("%v — Dolt backend required for this command", err)
	}
	return c
}

// mustAs returns the underlying store cast to T. T should be a composite
// of capabilities that the runtime concrete store satisfies (Storage +
// MUST sub-interfaces). Panics if the assertion fails — this is a
// programming/registration error, not a runtime input error.
//
// Usage: `view := mustAs[SwarmStorage](store)`.
func mustAs[T any](s storage.Storage) T {
	v, ok := storage.UnwrapStore(s).(T)
	if !ok {
		var zero T
		panic(fmt.Sprintf("storage backend does not satisfy %T", zero))
	}
	return v
}
