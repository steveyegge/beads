package main

import "strings"

// sanitizeDBName replaces hyphens and dots with underscores for
// SQL-idiomatic embedded Dolt database names (GH#2142, GH#3231).
// Lives in a non-cgo file because cmd/bd/init.go calls it; keeping the
// helper alongside the cgo-only Dolt store factory left the package
// untyped under CGO_ENABLED=0 (the lint mode used by .githooks/pre-commit).
func sanitizeDBName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}
