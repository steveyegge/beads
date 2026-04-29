//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// validIdentifier matches safe SQL identifiers (letters, digits, underscores).
// Hyphens are excluded because database names are interpolated into system
// variable identifiers (@@<db>_head_ref) where hyphens are invalid.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

const (
	commitName  = "beads"
	commitEmail = "beads@local"
)

// OpenSQL opens an doltlite database at dir. The returned cleanup
// function closes the *sql.DB.
func OpenSQL(ctx context.Context, dir, database, branch string) (*sql.DB, func() error, error) {
	dbPath, err := buildDSN(dir, database)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)

	cleanup := func() error {
		return db.Close()
	}

	if err := db.PingContext(ctx); err != nil {
		closeErr := cleanup()
		if closeErr != nil {
			return nil, nil, fmt.Errorf("%w; close: %v", err, closeErr)
		}
		return nil, nil, err
	}

	return db, cleanup, nil
}

func buildDSN(dir, database string) (string, error) {
	if strings.TrimSpace(database) != "" {
		if !validIdentifier.MatchString(database) {
			return "", fmt.Errorf("doltlite: invalid database name: %q", database)
		}
	} else {
		database = "beads"
	}
	path := filepath.Join(dir, database+".db")
	if os.PathSeparator == '\\' {
		path = strings.ReplaceAll(path, `\`, `/`)
	}
	return path, nil
}

func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(strings.TrimSpace(s), "'", "''") + "'"
}
