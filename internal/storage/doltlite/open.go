//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
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
	commitName         = "beads"
	commitEmail        = "beads@local"
	defaultBusyTimeout = 10000
)

// OpenSQL opens an doltlite database at dir. The returned cleanup
// function closes the *sql.DB.
func OpenSQL(ctx context.Context, dir, database, branch string) (*sql.DB, func() error, error) {
	dbPath, err := buildBranchDSN(dir, database, branch)
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
	return buildBranchDSN(dir, database, "")
}

func buildBranchDSN(dir, database, branch string) (string, error) {
	if strings.TrimSpace(database) != "" {
		if !validIdentifier.MatchString(database) {
			return "", fmt.Errorf("doltlite: invalid database name: %q", database)
		}
	} else {
		database = "beads"
	}
	filename := database + ".db"
	if strings.TrimSpace(branch) != "" {
		filename = fmt.Sprintf("%s__%s.db", database, url.QueryEscape(branch))
	}
	path := filepath.Join(dir, filename)
	if os.PathSeparator == '\\' {
		path = strings.ReplaceAll(path, `\`, `/`)
	}
	return fmt.Sprintf("%s?_busy_timeout=%d", path, defaultBusyTimeout), nil
}

func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(strings.TrimSpace(s), "'", "''") + "'"
}
