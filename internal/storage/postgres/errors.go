package postgres

import (
	"errors"
	"fmt"
)

// ErrSchemaOutOfDate is returned by Open when ReadOnly=true and the database
// schema is older than the embedded migrations.
var ErrSchemaOutOfDate = errors.New("postgres: schema out of date (read-only mode cannot run migrations)")

// ErrUnsupportedVersion is returned by Open when the connected server is older
// than PG14.
type ErrUnsupportedVersion struct {
	ServerVersionNum int
}

func (e ErrUnsupportedVersion) Error() string {
	maj := e.ServerVersionNum / 10000
	min := e.ServerVersionNum % 10000 / 100
	patch := e.ServerVersionNum % 100
	return fmt.Sprintf("postgres: server version %d.%d.%d is below the required PG14 minimum",
		maj, min, patch)
}

// ErrUnknownDSNParam is returned when a DSN query parameter is not in the
// driver's allowlist.
type ErrUnknownDSNParam struct {
	Name string
}

func (e ErrUnknownDSNParam) Error() string {
	return fmt.Sprintf("postgres: unknown DSN query parameter %q (see docs for allowed pgxpool tunings)", e.Name)
}

// errNotImplemented is the canonical sentinel for methods that exist to satisfy
// compile-time interface assertions but whose implementation has not landed
// yet. See bead be-6fk.3 — v1 ships only the mayor-scope command path; the
// remaining surface returns this error so the build stays green and follow-up
// beads can flesh out the rest.
var errNotImplemented = errors.New("postgres: method not yet implemented in v1 (see bead be-6fk.3)")

func notImplemented(name string) error {
	return fmt.Errorf("postgres: %s: %w", name, errNotImplemented)
}
