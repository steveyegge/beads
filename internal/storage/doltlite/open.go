//go:build cgo

package doltlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// validIdentifier matches safe SQL identifiers (letters, digits, underscores).
// Hyphens are excluded because database names are interpolated into system
// variable identifiers (@@<db>_head_ref) where hyphens are invalid.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

const (
	commitName         = "beads"
	commitEmail        = "beads@local"
	defaultBusyTimeout = 10000
	driverName         = "sqlite3_doltlite"
)

func init() {
	sql.Register(driverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("UUID", newUUID, true)
		},
	})
}

// OpenSQL opens an doltlite database at dir. The returned cleanup
// function closes the *sql.DB.
func OpenSQL(ctx context.Context, dir, database, branch string) (*sql.DB, func() error, error) {
	dbPath, err := buildDSN(dir, database)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open(driverName, dbPath)
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

	if branch = strings.TrimSpace(branch); branch != "" {
		if _, err := db.ExecContext(ctx, "SELECT dolt_checkout(?)", branch); err != nil {
			closeErr := cleanup()
			if closeErr != nil {
				return nil, nil, fmt.Errorf("%w; close: %v", err, closeErr)
			}
			return nil, nil, fmt.Errorf("doltlite: checkout branch %s: %w", branch, err)
		}
	}

	return db, cleanup, nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}

func buildDSN(dir, database string) (string, error) {
	if strings.TrimSpace(database) != "" {
		if !validIdentifier.MatchString(database) {
			return "", fmt.Errorf("doltlite: invalid database name: %q", database)
		}
	} else {
		database = "beads"
	}
	filename := database + ".db"
	path := filepath.Join(dir, filename)
	if os.PathSeparator == '\\' {
		path = strings.ReplaceAll(path, `\`, `/`)
	}
	return fmt.Sprintf("%s?_busy_timeout=%d", path, defaultBusyTimeout), nil
}
