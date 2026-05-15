package storage

import (
	"errors"
	"fmt"
)

// ErrCapabilityNotSupported is returned when a driver does not support a requested capability.
var ErrCapabilityNotSupported = errors.New("capability not supported")

// Capability is an opaque string identifying a backend feature.
type Capability string

const (
	CapCRUD           Capability = "crud"
	CapSchemaInit     Capability = "schema-init"
	CapSchemaMigrate  Capability = "schema-migrate"
	CapArchiveJSONL   Capability = "archive-jsonl"
	CapRowExport      Capability = "row-export"
	CapRowImport      Capability = "row-import"
	CapVersionControl Capability = "version-control"
	CapSync           Capability = "sync"
	CapPush           Capability = "push"
	CapPull           Capability = "pull"
)

// CapabilitySet is an immutable set of Capability values.
type CapabilitySet struct {
	caps map[Capability]struct{}
}

// NewCapabilitySet constructs an immutable CapabilitySet from the given capabilities.
func NewCapabilitySet(caps ...Capability) CapabilitySet {
	m := make(map[Capability]struct{}, len(caps))
	for _, c := range caps {
		m[c] = struct{}{}
	}
	return CapabilitySet{caps: m}
}

// Has reports whether c is present in the set.
func (s CapabilitySet) Has(c Capability) bool {
	_, ok := s.caps[c]
	return ok
}

// Require returns nil if c is in the set; otherwise returns ErrCapabilityNotSupported
// with an actionable message naming both the capability and the driver.
func (s CapabilitySet) Require(c Capability, driverName string) error {
	if s.Has(c) {
		return nil
	}
	return fmt.Errorf("%w: capability %q not supported by driver %q", ErrCapabilityNotSupported, c, driverName)
}

// DoltCapabilities is the full capability set for the Dolt backend.
var DoltCapabilities = NewCapabilitySet(
	CapCRUD,
	CapSchemaInit,
	CapSchemaMigrate,
	CapArchiveJSONL,
	CapRowExport,
	CapRowImport,
	CapVersionControl,
	CapSync,
	CapPush,
	CapPull,
)

// PostgresCapabilities is the capability set for the Postgres backend.
var PostgresCapabilities = NewCapabilitySet(
	CapCRUD,
	CapSchemaInit,
	CapSchemaMigrate,
	CapRowExport,
	CapRowImport,
)
