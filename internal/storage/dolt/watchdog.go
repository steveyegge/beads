package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

const (
	// defaultQueryTimeout is the maximum time a single query can run before
	// the watchdog fires KILL QUERY. This protects against Dolt queries that
	// can hang for 25-47 minutes when the query planner encounters pathological
	// cases (large IN clauses, complex JOINs, etc.).
	//
	// go-sql-driver/mysql does NOT send KILL QUERY on context cancel (GitHub
	// issue #863) - it only closes the client-side connection. The server-side
	// query continues running and consuming resources. This watchdog is the
	// ONLY way to actually stop a stuck query on Dolt.
	defaultQueryTimeout = 30 * time.Second

	// killQueryTimeout is how long we wait for the KILL QUERY command itself.
	killQueryTimeout = 5 * time.Second
)

// rowScanner is the interface needed by scanIssueRow and similar per-row
// scan functions. Both *sql.Rows and *Rows satisfy this via their Scan method.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// rowIterator is the interface needed by functions that iterate over result
// sets (e.g. scanDependencyRows). Both *sql.Rows and *Rows satisfy this.
type rowIterator interface {
	rowScanner
	Next() bool
	Err() error
}

// Rows wraps *sql.Rows with optional KILL QUERY watchdog cleanup.
// In server mode, closing Rows also cancels the watchdog goroutine and
// returns the dedicated connection to the pool. In embedded mode, Rows
// is a thin wrapper with no extra behavior.
type Rows struct {
	*sql.Rows
	conn   *sql.Conn        // Non-nil in watchdog mode (server mode)
	cancel context.CancelFunc // Non-nil in watchdog mode
	done   chan struct{}      // Non-nil in watchdog mode
}

// Close releases the underlying rows, stops the watchdog, and returns
// the connection to the pool.
func (r *Rows) Close() error {
	err := r.Rows.Close()
	if r.done != nil {
		close(r.done)
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.conn != nil {
		r.conn.Close()
	}
	return err
}

// queryContext executes a query with KILL QUERY watchdog in server mode.
//
// In server mode:
//  1. Acquires a dedicated connection from the pool
//  2. Gets the MySQL CONNECTION_ID() for that connection
//  3. Runs the query with a timeout context
//  4. Spawns a watchdog goroutine that fires KILL QUERY if the timeout expires
//  5. Returns *Rows whose Close() method cleans up the watchdog
//
// In embedded mode, delegates directly to db.QueryContext (KILL QUERY is not
// possible with a single-connection embedded engine).
func (s *DoltStore) queryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	if !s.serverMode || s.queryTimeout <= 0 {
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return &Rows{Rows: rows}, nil
	}

	// Acquire a dedicated connection for this query
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}

	// Get the MySQL connection ID so we can KILL QUERY if needed
	var connID int64
	if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&connID); err != nil {
		conn.Close()
		return nil, fmt.Errorf("get connection ID for watchdog: %w", err)
	}

	// Create a timeout context for the query
	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)

	// Start the KILL QUERY watchdog goroutine
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			// Query completed or rows closed - nothing to do
		case <-queryCtx.Done():
			// Context expired (timeout or parent cancel) - kill the server-side query.
			// Only fire KILL if this was a deadline exceeded (not a normal cancel from Close).
			if queryCtx.Err() == context.DeadlineExceeded {
				killCtx, killCancel := context.WithTimeout(context.Background(), killQueryTimeout)
				defer killCancel()
				if _, killErr := s.db.ExecContext(killCtx, fmt.Sprintf("KILL QUERY %d", connID)); killErr != nil {
					fmt.Fprintf(os.Stderr, "watchdog: KILL QUERY %d failed: %v\n", connID, killErr)
				} else {
					fmt.Fprintf(os.Stderr, "watchdog: KILL QUERY %d fired (query timeout %v exceeded)\n", connID, s.queryTimeout)
				}
			}
		}
	}()

	rows, err := conn.QueryContext(queryCtx, query, args...)
	if err != nil {
		close(done)
		cancel()
		conn.Close()
		return nil, err
	}

	return &Rows{Rows: rows, conn: conn, cancel: cancel, done: done}, nil
}

// execContext executes a statement with KILL QUERY watchdog in server mode.
//
// Unlike queryContext, exec completes synchronously so cleanup happens
// before returning (no wrapper type needed).
func (s *DoltStore) execContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if !s.serverMode || s.queryTimeout <= 0 {
		return s.db.ExecContext(ctx, query, args...)
	}

	// Acquire a dedicated connection
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Get CONNECTION_ID
	var connID int64
	if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&connID); err != nil {
		return nil, fmt.Errorf("get connection ID for watchdog: %w", err)
	}

	// Timeout context
	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	// Start watchdog
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-queryCtx.Done():
			if queryCtx.Err() == context.DeadlineExceeded {
				killCtx, killCancel := context.WithTimeout(context.Background(), killQueryTimeout)
				defer killCancel()
				if _, killErr := s.db.ExecContext(killCtx, fmt.Sprintf("KILL QUERY %d", connID)); killErr != nil {
					fmt.Fprintf(os.Stderr, "watchdog: KILL QUERY %d failed: %v\n", connID, killErr)
				} else {
					fmt.Fprintf(os.Stderr, "watchdog: KILL QUERY %d fired (exec timeout %v exceeded)\n", connID, s.queryTimeout)
				}
			}
		}
	}()

	result, err := conn.ExecContext(queryCtx, query, args...)
	close(done)
	return result, err
}

// parseQueryTimeout reads the query timeout from the environment, falling back
// to the provided default. Returns 0 to disable the watchdog.
func parseQueryTimeout(defaultTimeout time.Duration) time.Duration {
	if env := os.Getenv("BEADS_QUERY_TIMEOUT"); env != "" {
		if d, err := time.ParseDuration(env); err == nil && d > 0 {
			return d
		}
	}
	return defaultTimeout
}
