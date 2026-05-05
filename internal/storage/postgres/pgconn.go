package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgxConn is the small subset of pgx that both *pgxpool.Pool and pgx.Tx
// satisfy. Storage methods take a pgxConn so the same SQL helpers serve
// pool-rooted reads/writes and in-transaction operations.
type pgxConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// nullString returns sql.NullString backed by s; an empty string maps to NULL.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullStringPtr maps a *string to sql.NullString; nil maps to NULL.
func nullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// nullInt maps a *int to sql.NullInt32; nil maps to NULL.
func nullInt(i *int) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*i), Valid: true}
}

// nullIntVal maps an int to sql.NullInt32; zero maps to NULL.
func nullIntVal(i int) sql.NullInt32 {
	if i == 0 {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(i), Valid: true}
}

// jsonbMetadata returns a JSONB-safe representation. Empty input becomes
// an empty object so the column never carries a NULL value.
func jsonbMetadata(m []byte) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	if !json.Valid(m) {
		return []byte("{}")
	}
	return m
}
