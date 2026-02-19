//go:build !cgo

package doctor

// sqliteConnString returns an empty string in non-CGO builds where SQLite is unavailable.
func sqliteConnString(_ string, _ bool) string {
	return ""
}
