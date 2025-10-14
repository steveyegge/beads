module bd-example-extension-go

go 1.25.2

require github.com/steveyegge/beads v0.0.0-00010101000000-000000000000

require github.com/mattn/go-sqlite3 v1.14.32 // indirect

// Removable when beads is published
replace github.com/steveyegge/beads => ../..
