module bd-example-extension-go

go 1.21

require (
	github.com/mattn/go-sqlite3 v1.14.32
	github.com/steveyegge/beads v0.0.0-00010101000000-000000000000
)

// For local development - remove when beads is published
replace github.com/steveyegge/beads => ../..
