package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/types"
)

// SetConfig writes a config key.
func (s *PostgresStore) SetConfig(ctx context.Context, key, value string) error {
	return setKV(ctx, s.pool, "config", key, value)
}

// GetConfig reads a config key. Empty key → empty value, no error.
func (s *PostgresStore) GetConfig(ctx context.Context, key string) (string, error) {
	return getKV(ctx, s.pool, "config", key)
}

// GetAllConfig returns the config table as a map.
func (s *PostgresStore) GetAllConfig(ctx context.Context) (map[string]string, error) {
	return readAllKV(ctx, s.pool, "config")
}

// DeleteConfig removes a config key. ConfigMetadataStore method.
func (s *PostgresStore) DeleteConfig(ctx context.Context, key string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM config WHERE key = $1`, key); err != nil {
		return wrapErr("delete config", err)
	}
	return nil
}

// SetMetadata writes a metadata key (clone-shared).
func (s *PostgresStore) SetMetadata(ctx context.Context, key, value string) error {
	return setKV(ctx, s.pool, "metadata", key, value)
}

// GetMetadata reads a metadata key.
func (s *PostgresStore) GetMetadata(ctx context.Context, key string) (string, error) {
	return getKV(ctx, s.pool, "metadata", key)
}

// SetLocalMetadata writes a clone-local metadata key. PG has the table even
// though local_metadata is conceptually clone-local; we honor the call so
// callers don't have to special-case PG.
func (s *PostgresStore) SetLocalMetadata(ctx context.Context, key, value string) error {
	return setKV(ctx, s.pool, "local_metadata", key, value)
}

// GetLocalMetadata reads a clone-local metadata key.
func (s *PostgresStore) GetLocalMetadata(ctx context.Context, key string) (string, error) {
	return getKV(ctx, s.pool, "local_metadata", key)
}

// kvTables is an allowlist for setKV/getKV so the table name interpolation
// stays bounded.
var kvTables = map[string]bool{
	"config":         true,
	"metadata":       true,
	"local_metadata": true,
}

func setKV(ctx context.Context, c pgxConn, table, key, value string) error {
	if !kvTables[table] {
		return errors.New("postgres: setKV: unknown KV table")
	}
	stmt := `INSERT INTO ` + table + ` (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`
	if _, err := c.Exec(ctx, stmt, key, value); err != nil {
		return wrapErr("set "+table, err)
	}
	return nil
}

func getKV(ctx context.Context, c pgxConn, table, key string) (string, error) {
	if !kvTables[table] {
		return "", errors.New("postgres: getKV: unknown KV table")
	}
	q := `SELECT value FROM ` + table + ` WHERE key = $1`
	var v string
	if err := c.QueryRow(ctx, q, key).Scan(&v); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", wrapErr("get "+table, err)
	}
	return v, nil
}

func readAllKV(ctx context.Context, c pgxConn, table string) (map[string]string, error) {
	if !kvTables[table] {
		return nil, errors.New("postgres: readAllKV: unknown KV table")
	}
	rows, err := c.Query(ctx, `SELECT key, value FROM `+table)
	if err != nil {
		return nil, wrapErr("read "+table, err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, wrapErr("scan "+table, err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// GetCustomStatuses returns custom status names.
func (s *PostgresStore) GetCustomStatuses(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM custom_statuses ORDER BY name`)
	if err != nil {
		return nil, wrapErr("get custom statuses", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, wrapErr("scan custom statuses", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetCustomStatusesDetailed returns name + category for each custom status.
func (s *PostgresStore) GetCustomStatusesDetailed(ctx context.Context) ([]types.CustomStatus, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, category FROM custom_statuses ORDER BY name`)
	if err != nil {
		return nil, wrapErr("get custom statuses (detailed)", err)
	}
	defer rows.Close()
	var out []types.CustomStatus
	for rows.Next() {
		var c types.CustomStatus
		var cat string
		if err := rows.Scan(&c.Name, &cat); err != nil {
			return nil, wrapErr("scan custom statuses (detailed)", err)
		}
		c.Category = types.StatusCategory(cat)
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCustomTypes returns custom type names.
func (s *PostgresStore) GetCustomTypes(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM custom_types ORDER BY name`)
	if err != nil {
		return nil, wrapErr("get custom types", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, wrapErr("scan custom types", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetInfraTypes returns the static infrastructure-type allowlist. The PG
// backend has no dynamic source for these, so we return the same hard-coded
// set as the Dolt backend.
func (s *PostgresStore) GetInfraTypes(ctx context.Context) map[string]bool {
	return map[string]bool{
		"agent":         true,
		"mol":           true,
		"patrol":        true,
		"merge-request": true,
		"gate":          true,
	}
}

// IsInfraTypeCtx reports whether t is an infra type for routing decisions.
func (s *PostgresStore) IsInfraTypeCtx(ctx context.Context, t types.IssueType) bool {
	return s.GetInfraTypes(ctx)[string(t)]
}
