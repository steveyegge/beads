//go:build embeddeddolt

package embeddeddolt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	doltembed "github.com/dolthub/driver"
)

// validIdentifier matches safe SQL identifiers (letters, digits, underscores).
// Hyphens are excluded because database names are interpolated into system
// variable identifiers (@@<db>_head_ref) where hyphens are invalid.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

const (
	commitName            = "beads"
	commitEmail           = "beads@local"
	openSQLPanicRetryWait = 100 * time.Millisecond
	openSQLPanicRetryMax  = 5 * time.Second
)

// OpenSQL opens an embedded Dolt database at dir. The returned cleanup
// function closes both the *sql.DB and the underlying connector.
func OpenSQL(ctx context.Context, dir, database, branch string) (*sql.DB, func() error, error) {
	database = strings.TrimSpace(database)
	branch = strings.TrimSpace(branch)
	if database != "" && !validIdentifier.MatchString(database) {
		return nil, nil, fmt.Errorf("invalid database name: %q", database)
	}

	dsn := buildDSN(dir, database)
	var (
		db      *sql.DB
		cleanup func() error
	)
	err := retryOpenSQLPanics(ctx, func() error {
		var err error
		db, cleanup, err = openSQLOnce(ctx, dsn, database, branch)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return db, cleanup, nil
}

type openSQLPanicError struct {
	value any
}

func (e *openSQLPanicError) Error() string {
	return fmt.Sprintf("embedded dolt panic during OpenSQL: %v", e.value)
}

func retryOpenSQLPanics(ctx context.Context, fn func() error) error {
	wait := openSQLPanicRetryWait
	for {
		err := recoverOpenSQLPanic(fn)
		if err == nil {
			return nil
		}

		var panicErr *openSQLPanicError
		if !errors.As(err, &panicErr) {
			return err
		}

		if retryErr := waitForOpenSQLRetry(ctx, wait); retryErr != nil {
			return errors.Join(err, retryErr)
		}
		wait *= 2
		if wait > openSQLPanicRetryMax {
			wait = openSQLPanicRetryMax
		}
	}
}

func recoverOpenSQLPanic(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &openSQLPanicError{value: r}
		}
	}()
	return fn()
}

func waitForOpenSQLRetry(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func openSQLOnce(ctx context.Context, dsn, database, branch string) (*sql.DB, func() error, error) {
	cfg, err := doltembed.ParseDSN(dsn)
	if err != nil {
		return nil, nil, err
	}

	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 0 // wait until ctx cancellation
	bo.MaxInterval = 5 * time.Second
	cfg.BackOff = bo

	connector, err := doltembed.NewConnector(cfg)
	if err != nil {
		return nil, nil, err
	}

	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)

	cleanup := func() error {
		dbErr := db.Close()
		connErr := connector.Close()
		// connector.Close → engine.Close → BackgroundThreads.Shutdown
		// always returns context.Canceled because Shutdown cancels its
		// own parent context then returns parentCtx.Err(). This is
		// a spurious error from a clean shutdown; filter it from each
		// result individually so real close errors are still surfaced.
		if errors.Is(dbErr, context.Canceled) {
			dbErr = nil
		}
		if errors.Is(connErr, context.Canceled) {
			connErr = nil
		}
		return errors.Join(dbErr, connErr)
	}

	if err := initializeSQLDB(ctx, db, database, branch); err != nil {
		return nil, nil, errors.Join(err, cleanup())
	}

	return db, cleanup, nil
}

func initializeSQLDB(ctx context.Context, db *sql.DB, database, branch string) error {
	return recoverOpenSQLPanic(func() error {
		if err := db.PingContext(ctx); err != nil {
			return err
		}
		if database == "" {
			return nil
		}
		if _, err := db.ExecContext(ctx, "USE `"+database+"`"); err != nil {
			return err
		}
		if branch == "" {
			return nil
		}
		_, err := db.ExecContext(ctx, fmt.Sprintf("SET @@%s_head_ref = %s", database, sqlStringLiteral(branch)))
		return err
	})
}

func buildDSN(dir, database string) string {
	v := url.Values{}
	v.Set(doltembed.CommitNameParam, commitName)
	v.Set(doltembed.CommitEmailParam, commitEmail)
	v.Set(doltembed.MultiStatementsParam, "true")
	if strings.TrimSpace(database) != "" {
		v.Set(doltembed.DatabaseParam, database)
	}
	u := url.URL{Scheme: "file", Path: encodeDir(dir), RawQuery: v.Encode()}
	return u.String()
}

func encodeDir(dir string) string {
	if os.PathSeparator == '\\' {
		return strings.ReplaceAll(dir, `\`, `/`)
	}
	return dir
}

func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(strings.TrimSpace(s), "'", "''") + "'"
}
