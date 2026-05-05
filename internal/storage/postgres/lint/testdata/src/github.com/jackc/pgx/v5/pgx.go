// Package pgx is a stub used by the analyzer's testdata fixture. The real
// pgx is too large to vendor under GOPATH-style testdata; the analyzer only
// inspects the import path string, so a stub with matching names is enough.
package pgx

import "context"

// Conn is the minimal stub the testdata exercise needs.
type Conn struct{}

// Exec is the stub method exercised by testdata/src/bad/bad.go.
func (*Conn) Exec(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }
