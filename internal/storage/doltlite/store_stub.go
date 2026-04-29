//go:build !cgo

package doltlite

import (
	"context"
	"errors"
)

// DoltliteStore is a stub for builds without CGO.
type DoltliteStore struct {
	dataDir  string
	database string
	branch   string
}

// Option configures optional behavior for New (stub: no-op).
type Option func(*struct{})

// WithLock is a no-op in non-CGO builds.
func WithLock(_ Unlocker) Option {
	return func(*struct{}) {}
}

// New returns an error when CGO is not enabled.
func New(_ context.Context, _, _, _ string, _ ...Option) (*DoltliteStore, error) {
	return nil, errors.New("doltlite: requires CGO (build with CGO_ENABLED=1)")
}
