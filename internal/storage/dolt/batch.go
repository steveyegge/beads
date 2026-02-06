package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// DefaultBatchSize is the maximum number of IDs per IN clause.
// Large IN clauses (e.g. 29k params) create queries that Dolt cannot execute efficiently.
const DefaultBatchSize = 500

// BatchIN executes a batched SELECT query with an IN clause, splitting the input IDs
// into chunks of batchSize to avoid oversized queries that crush Dolt CPU.
//
// queryTemplate must contain exactly one %s placeholder for the IN clause (e.g.
// "SELECT issue_id, label FROM labels WHERE issue_id IN (%s) ORDER BY issue_id").
//
// scanRow is called for each result row and must return a key and value to accumulate
// into the result map.
//
// nolint:gosec // G201: queryTemplate %s is filled with ? placeholders only
func BatchIN[K comparable, V any](
	ctx context.Context,
	db *sql.DB,
	ids []string,
	batchSize int,
	queryTemplate string,
	scanRow func(*sql.Rows) (K, V, error),
) (map[K][]V, error) {
	if len(ids) == 0 {
		return make(map[K][]V), nil
	}

	result := make(map[K][]V)
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		placeholders := make([]string, len(batch))
		args := make([]interface{}, len(batch))
		for j, id := range batch {
			placeholders[j] = "?"
			args[j] = id
		}

		// nolint:gosec // G201: placeholders contains only ? markers, actual values passed via args
		query := fmt.Sprintf(queryTemplate, strings.Join(placeholders, ","))

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			key, val, scanErr := scanRow(rows)
			if scanErr != nil {
				rows.Close()
				return nil, scanErr
			}
			result[key] = append(result[key], val)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return result, nil
}
