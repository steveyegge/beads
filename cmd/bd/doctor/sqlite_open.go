package doctor

import "fmt"

// sqliteConnString builds a SQLite connection string with optional read-only mode.
func sqliteConnString(path string, readOnly bool) string {
	if readOnly {
		return fmt.Sprintf("file:%s?mode=ro", path)
	}
	return fmt.Sprintf("file:%s", path)
}
