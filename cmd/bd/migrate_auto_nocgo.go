//go:build !cgo

package main

// autoMigrateSQLiteToDolt uses the sqlite3 CLI shim for non-CGO builds.
// This enables automatic SQLite→Dolt migration without requiring the
// ncruces/go-sqlite3 CGO driver.
func autoMigrateSQLiteToDolt() {
	shimMigrateSQLiteToDolt()
}
