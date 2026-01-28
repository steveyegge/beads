package spec

import (
	"context"
	"time"
)

// SpecRegistryStore defines the storage surface for Shadow Ledger.
type SpecRegistryStore interface {
	UpsertSpecRegistry(ctx context.Context, specs []SpecRegistryEntry) error
	ListSpecRegistry(ctx context.Context) ([]SpecRegistryEntry, error)
	GetSpecRegistry(ctx context.Context, specID string) (*SpecRegistryEntry, error)
	ListSpecRegistryWithCounts(ctx context.Context) ([]SpecRegistryCount, error)
	MarkSpecsMissing(ctx context.Context, specIDs []string, missingAt time.Time) error
	ClearSpecsMissing(ctx context.Context, specIDs []string) error
	MarkSpecChangedBySpecIDs(ctx context.Context, specIDs []string, changedAt time.Time) (int, error)
}
