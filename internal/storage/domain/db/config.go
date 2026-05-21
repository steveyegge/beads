package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/domain"
)

func NewConfigSQLRepository(runner Runner) domain.ConfigSQLRepository {
	return &configSQLRepositoryImpl{runner: runner}
}

type configSQLRepositoryImpl struct {
	runner Runner
}

var _ domain.ConfigSQLRepository = (*configSQLRepositoryImpl)(nil)

func (r *configSQLRepositoryImpl) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := r.runner.QueryRowContext(ctx, "SELECT value FROM metadata WHERE `key` = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("db: GetMetadata %s: %w", key, err)
	}
	return value, nil
}

func (r *configSQLRepositoryImpl) SetMetadata(ctx context.Context, key, value string) error {
	if _, err := r.runner.ExecContext(ctx, "REPLACE INTO metadata (`key`, value) VALUES (?, ?)", key, value); err != nil {
		return fmt.Errorf("db: SetMetadata %s: %w", key, err)
	}
	return nil
}

func (r *configSQLRepositoryImpl) SetLocalMetadata(ctx context.Context, key, value string) error {
	if _, err := r.runner.ExecContext(ctx, "REPLACE INTO local_metadata (`key`, value) VALUES (?, ?)", key, value); err != nil {
		return fmt.Errorf("db: SetLocalMetadata %s: %w", key, err)
	}
	return nil
}

func (r *configSQLRepositoryImpl) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := r.runner.QueryRowContext(ctx, "SELECT value FROM config WHERE `key` = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("db: GetConfig %s: %w", key, err)
	}
	return value, nil
}

func (r *configSQLRepositoryImpl) SetConfig(ctx context.Context, key, value string) error {
	if key == "issue_prefix" {
		value = strings.TrimSuffix(value, "-")
	}
	if _, err := r.runner.ExecContext(ctx, "REPLACE INTO config (`key`, value) VALUES (?, ?)", key, value); err != nil {
		return fmt.Errorf("db: SetConfig %s: %w", key, err)
	}
	return nil
}

func (r *configSQLRepositoryImpl) GetCustomTypes(ctx context.Context) ([]string, error) {
	return nil, errors.New("db: GetCustomTypes: not implemented")
}

func (r *configSQLRepositoryImpl) GetAllowedPrefixes(ctx context.Context) (string, error) {
	return "", errors.New("db: GetAllowedPrefixes: not implemented")
}
