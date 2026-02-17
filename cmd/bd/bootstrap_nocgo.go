//go:build !cgo

package main

import (
	"context"

	"github.com/steveyegge/beads/internal/storage/dolt"
)

func bootstrapEmbeddedDolt(_ context.Context, _ string, _ *dolt.Config) error {
	return nil
}
