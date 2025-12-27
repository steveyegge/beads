package migrations

import (
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

func MigrateContentHashColumn(db *sql.DB) error {
	var colName string
	err := db.QueryRow(`
		SELECT name FROM pragma_table_info('issues')
		WHERE name = 'content_hash'
	`).Scan(&colName)

	if err == sql.ErrNoRows {
		_, err := db.Exec(`ALTER TABLE issues ADD COLUMN content_hash TEXT`)
		if err != nil {
			return fmt.Errorf("failed to add content_hash column: %w", err)
		}

		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_content_hash ON issues(content_hash)`)
		if err != nil {
			return fmt.Errorf("failed to create content_hash index: %w", err)
		}

		rows, err := db.Query(`
			SELECT id, title, description, design, acceptance_criteria, notes,
			       status, priority, issue_type, assignee, external_ref
			FROM issues
		`)
		if err != nil {
			return fmt.Errorf("failed to query existing issues: %w", err)
		}
		defer rows.Close()

		updates := make(map[string]string)
		for rows.Next() {
			var issue types.Issue
			var assignee sql.NullString
			var externalRef sql.NullString
			err := rows.Scan(
				&issue.ID, &issue.Title, &issue.Description, &issue.Design,
				&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
				&issue.Priority, &issue.IssueType, &assignee, &externalRef,
			)
			if err != nil {
				return fmt.Errorf("failed to scan issue: %w", err)
			}
			if assignee.Valid {
				issue.Assignee = assignee.String
			}
			if externalRef.Valid {
				issue.ExternalRef = &externalRef.String
			}

			updates[issue.ID] = issue.ComputeContentHash()
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("error iterating issues: %w", err)
		}

		// Use SAVEPOINT for atomicity (we're already inside an EXCLUSIVE transaction from RunMigrations)
		// SQLite doesn't support nested transactions but SAVEPOINTs work inside transactions
		_, err = db.Exec(`SAVEPOINT content_hash_migration`)
		if err != nil {
			return fmt.Errorf("failed to create savepoint: %w", err)
		}
		savepointReleased := false
		defer func() {
			if !savepointReleased {
				_, _ = db.Exec(`ROLLBACK TO SAVEPOINT content_hash_migration`)
			}
		}()

		stmt, err := db.Prepare(`UPDATE issues SET content_hash = ? WHERE id = ?`)
		if err != nil {
			return fmt.Errorf("failed to prepare update statement: %w", err)
		}
		defer stmt.Close()

		for id, hash := range updates {
			if _, err := stmt.Exec(hash, id); err != nil {
				return fmt.Errorf("failed to update content_hash for issue %s: %w", id, err)
			}
		}

		// Release savepoint (commits the changes within the outer transaction)
		_, err = db.Exec(`RELEASE SAVEPOINT content_hash_migration`)
		if err != nil {
			return fmt.Errorf("failed to release savepoint: %w", err)
		}
		savepointReleased = true

		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to check content_hash column: %w", err)
	}

	return nil
}
