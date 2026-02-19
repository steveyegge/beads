//go:build !cgo

package fix

// sqliteConnString returns an empty string in non-CGO builds where SQLite is unavailable.
func sqliteConnString(_ string, _ bool) string {
	return ""
}
