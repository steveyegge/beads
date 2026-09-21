package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/externaldeps"
	"github.com/steveyegge/beads/internal/storage/uow"
	"github.com/steveyegge/beads/internal/telemetry"
)

// wireStorageDecorators composes the storage chain in the order the rest of
// bd expects:
//
//	caller → HookFiringStore → externaldeps.Store → InstrumentedStorage → raw DoltStorage
//
// telemetry.WrapStorage is a no-op when telemetry is disabled, so the
// instrumentation layer is only present when BD_OTEL_ENABLED=true (or a
// legacy BD_OTEL_* selector is set). The hook layer sits outside telemetry so
// storage spans measure pure DB time without hook-firing overhead. The policy
// sits directly beneath hooks so serve's one hook-layer peel retains it.
//
// Extracted from main.go's PersistentPreRunE so the chain composition is
// unit-testable — the bug this PR fixes was a missing WrapStorage call,
// and the regression class deserves test coverage.
func wireStorageDecorators(store storage.DoltStorage, hookRunner *hooks.Runner, hooksDisabled bool) storage.DoltStorage {
	if store == nil {
		return nil
	}
	applyVersionedHistoryConfig(store, versionedHistoryEnabled(context.Background(), store))
	store = telemetry.WrapStorage(store)
	store = wireExternalDependencyPolicy(store)
	if hookRunner != nil && !hooksDisabled {
		store = storage.NewHookFiringStore(store, hookRunner)
	}
	return store
}

// wireExternalDependencyPolicy applies only the read/guard policy. Routed
// stores must use this without inheriting the caller's hooks or telemetry.
//
// It ALWAYS wraps, including a registered Remote backend (design 3.6): a
// remote server that enforces this policy itself answers the decorator's
// storage.ExternalDependencyPolicyProber probe (implemented by
// httpclient.Store, keyed on the handshake's wire.CapExternalDependencies
// token) with true, so the decorator's own loadBlockingState is a cheap no-op
// for that server — but a server that does NOT advertise the capability must
// still get client-side enforcement, which requires wrapping it in the first
// place. Skipping the wrap here for every remote backend (the pre-S6-fix
// behavior) silently dropped the policy entirely against such a server; the
// capability-aware skip now lives inside the decorator, not at wrap time.
func wireExternalDependencyPolicy(store storage.DoltStorage) storage.DoltStorage {
	if store == nil {
		return nil
	}
	return externaldeps.New(
		store,
		func(project externaldeps.ProjectName) (string, bool) {
			path := config.ResolveExternalProjectPath(string(project))
			return path, path != ""
		},
		func(ctx context.Context, projectRoot string) (storage.DoltStorage, error) {
			return newReadOnlyStoreFromConfig(ctx, filepath.Join(projectRoot, ".beads"))
		},
	)
}

// waitForCommandHooks lets the hooks this command fired finish before the
// process exits, bounded by the runner's own per-hook budget.
//
// It covers BOTH plumbings, because both fire through the one runner this
// process built: the DoltStorage chain wires it into HookFiringStore above, and
// proxied mode wires the same value into the notifying provider's sinks. A
// command that fired nothing waits on an empty group and returns at once.
//
// CALLED FROM TWO PLACES, and idempotent so that costs nothing: PersistentPostRunE
// for the clean path, where it must run after the store is closed, and main()
// after ExecuteC for every path cobra skips PostRunE on — a RunE that returned
// an error, which is how a partial batch exits with one issue committed.
func waitForCommandHooks() {
	if hookRunner == nil {
		return
	}
	hookRunner.Wait(hookRunner.Timeout())
}

// wireProxiedUOWProvider composes the proxied-server provider chain:
//
//	caller → externaldeps policy → notifying provider → raw provider
//
// It is the unit-of-work twin of wireStorageDecorators, extracted from main.go
// for the same reason: the composition is only unit-testable as a function.
// The notifying layer is present only when sinks carry a hook runner;
// uow.NewNotifyingProvider returns the provider unwrapped otherwise.
func wireProxiedUOWProvider(provider uow.UnitOfWorkProvider, sinks uow.Sinks) uow.UnitOfWorkProvider {
	return wireExternalDependencyUOWProvider(uow.NewNotifyingProvider(provider, sinks))
}

func wireExternalDependencyUOWProvider(provider uow.UnitOfWorkProvider) uow.UnitOfWorkProvider {
	return externaldeps.WrapUOWProvider(
		provider,
		func(project externaldeps.ProjectName) (string, bool) {
			path := config.ResolveExternalProjectPath(string(project))
			return path, path != ""
		},
		func(ctx context.Context, projectRoot string) (storage.DoltStorage, error) {
			return newReadOnlyStoreFromConfig(ctx, filepath.Join(projectRoot, ".beads"))
		},
	)
}

// applyVersionedHistoryConfig turns dual-write issue-version history on for
// this store instance when versioned-history.enabled is set (env:
// BD_VERSIONED_HISTORY_ENABLED).
//
// It MUST run on the raw store, before wireStorageDecorators wraps anything.
// The capability is a type assertion, and none of the decorators above
// (telemetry.WrapStorage, externaldeps, HookFiringStore) forwards
// SetVersionedHistoryEnabled -- so calling this after a wrap would assert
// against the wrapper, fail silently, and leave history off while the config
// said it was on. That is the "wrong answer, not an empty one" failure this
// feature exists to avoid, so the ordering is pinned by
// TestVersionedHistoryConfigAppliesToTheRawStore.
//
// A store that does not implement the capability is not an error: proxied and
// no-db backends legitimately do not. But it is not silently ignored either --
// asking for history and not getting it is exactly the case a user must be
// told about, so it warns.
func applyVersionedHistoryConfig(store storage.DoltStorage, enabled bool) {
	if !enabled {
		return
	}
	configurer, ok := store.(storage.VersionedHistoryConfigurer)
	if !ok {
		fmt.Fprintf(os.Stderr,
			"warning: versioned-history.enabled is set, but this storage backend (%T) does not support version history; it stays off\n",
			store)
		return
	}
	configurer.SetVersionedHistoryEnabled(true)
}
