//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

func (s *DoltliteStore) SetConfig(ctx context.Context, key, value string) error {
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		if err := issueops.SetConfigInTx(ctx, tx, key, value); err != nil {
			return err
		}
		// Sync normalized tables when config keys change
		switch key {
		case "status.custom":
			if err := issueops.SyncCustomStatusesTable(ctx, tx, value); err != nil {
				return fmt.Errorf("syncing custom_statuses table: %w", err)
			}
		case "types.custom":
			if err := issueops.SyncCustomTypesTable(ctx, tx, value); err != nil {
				return fmt.Errorf("syncing custom_types table: %w", err)
			}
		}
		return nil
	})
}

func (s *DoltliteStore) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		value, err = issueops.GetConfigInTx(ctx, tx, key)
		return err
	})
	return value, err
}

func (s *DoltliteStore) GetAllConfig(ctx context.Context) (map[string]string, error) {
	var result map[string]string
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetAllConfigInTx(ctx, tx)
		return err
	})
	return result, err
}

func (s *DoltliteStore) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		value, err = issueops.GetMetadataInTx(ctx, tx, key)
		return err
	})
	return value, err
}

func (s *DoltliteStore) SetMetadata(ctx context.Context, key, value string) error {
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		return issueops.SetMetadataInTx(ctx, tx, key, value)
	})
}

func (s *DoltliteStore) SetLocalMetadata(ctx context.Context, key, value string) error {
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		return issueops.SetLocalMetadataInTx(ctx, tx, key, value)
	})
}

func (s *DoltliteStore) GetLocalMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		value, err = issueops.GetLocalMetadataInTx(ctx, tx, key)
		return err
	})
	return value, err
}

// GetInfraTypes returns the set of infrastructure types that should be routed
// to the wisps table. Reads from DB config "types.infra", falls back to YAML,
// then to hardcoded defaults (agent, rig, role, message).
func (s *DoltliteStore) GetInfraTypes(ctx context.Context) map[string]bool {
	var result map[string]bool
	if err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		result = issueops.ResolveInfraTypesInTx(ctx, tx)
		return nil
	}); err != nil || result == nil {
		// DB unavailable — fall back to YAML then defaults.
		var typeList []string
		if yamlTypes := config.GetInfraTypesFromYAML(); len(yamlTypes) > 0 {
			typeList = yamlTypes
		} else {
			typeList = storage.DefaultInfraTypes()
		}
		result = make(map[string]bool, len(typeList))
		for _, t := range typeList {
			result[t] = true
		}
	}
	return result
}

// IsInfraTypeCtx returns true if the issue type is an infrastructure type.
func (s *DoltliteStore) IsInfraTypeCtx(ctx context.Context, t types.IssueType) bool {
	return s.GetInfraTypes(ctx)[string(t)]
}
