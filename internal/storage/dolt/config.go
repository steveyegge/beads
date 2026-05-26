package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// SetConfig sets a configuration value
func (s *DoltStore) SetConfig(ctx context.Context, key, value string) error {
	if err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
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
	}); err != nil {
		return err
	}

	// Invalidate caches for keys that affect cached data. The all-config
	// cache is also invalidated on every SetConfig because keys outside the
	// switch list (routing.*, contributor.*, custom.*) are read via
	// GetAllConfig from the routing module.
	s.cacheMu.Lock()
	switch key {
	case "status.custom":
		s.customStatusCached = false
		s.customStatusCache = nil
		s.customStatusDetailedCache = nil
	case "types.custom":
		s.customTypeCached = false
		s.customTypeCache = nil
	case "types.infra":
		s.infraTypeCached = false
		s.infraTypeCache = nil
	}
	s.allConfigCached = false
	s.allConfigCache = nil
	s.cacheMu.Unlock()

	return nil
}

// GetConfig retrieves a configuration value. Honors the per-store cache
// populated by preloadSessionCaches so write commands (which typically
// look up half a dozen config keys in their PreRun + PostRun hooks) do
// not pay a round trip per key once the cache is warm.
func (s *DoltStore) GetConfig(ctx context.Context, key string) (string, error) {
	s.cacheMu.Lock()
	if s.allConfigCached {
		v := s.allConfigCache[key]
		s.cacheMu.Unlock()
		return v, nil
	}
	s.cacheMu.Unlock()

	var value string
	err := s.withReadConn(ctx, func(q issueops.SQLQuerier) error {
		var err error
		value, err = issueops.GetConfigInTx(ctx, q, key)
		return err
	})
	return value, err
}

// GetAllConfig retrieves all configuration values. Honors the per-store
// cache populated by preloadSessionCaches so the routing module and other
// repeat callers do not incur a round trip after store-open.
func (s *DoltStore) GetAllConfig(ctx context.Context) (map[string]string, error) {
	s.cacheMu.Lock()
	if s.allConfigCached {
		// Copy so callers can mutate without poisoning the cache.
		out := make(map[string]string, len(s.allConfigCache))
		for k, v := range s.allConfigCache {
			out[k] = v
		}
		s.cacheMu.Unlock()
		return out, nil
	}
	s.cacheMu.Unlock()

	var result map[string]string
	err := s.withReadConn(ctx, func(q issueops.SQLQuerier) error {
		var err error
		result, err = issueops.GetAllConfigInTx(ctx, q)
		return err
	})
	return result, err
}

// DeleteConfig removes a configuration value
func (s *DoltStore) DeleteConfig(ctx context.Context, key string) error {
	err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.DeleteConfigInTx(ctx, tx, key)
	})
	if err == nil {
		s.cacheMu.Lock()
		s.allConfigCached = false
		s.allConfigCache = nil
		s.cacheMu.Unlock()
	}
	return err
}

// preloadSessionCaches warms the per-store caches that bd commands would
// otherwise populate via separate round trips on every invocation. Best
// effort: any failure leaves the caches cold and the lazy loaders take
// over. Designed to be called once, from newServerMode, on a freshly
// established connection — adds at most one round trip up front in exchange
// for eliminating ~4-6 round trips spread across the rest of the command.
func (s *DoltStore) preloadSessionCaches(ctx context.Context) {
	_ = s.withReadConn(ctx, func(q issueops.SQLQuerier) error {
		rows, err := q.QueryContext(ctx,
			"SELECT `key`, value FROM config; "+
				"SELECT 1 FROM wisps LIMIT 1")
		if err != nil {
			return nil
		}
		defer rows.Close()

		// --- result set 1: config table ---
		all := make(map[string]string)
		for rows.Next() {
			var k, v sql.NullString
			if scanErr := rows.Scan(&k, &v); scanErr == nil && k.Valid {
				all[k.String] = v.String
			}
		}

		// --- result set 2: wisps probe (may not exist on old schemas) ---
		wispsEmpty := true
		if rows.NextResultSet() {
			if rows.Next() {
				wispsEmpty = false
			}
		}

		s.cacheMu.Lock()
		s.allConfigCache = all
		s.allConfigCached = true

		// Derive custom-* caches from config string values (the resolver's
		// fallback path; identical to the normalized-table source because
		// SyncCustomStatusesTable keeps them in sync on every SetConfig).
		if v := all["status.custom"]; v != "" {
			if parsed, perr := types.ParseCustomStatusConfig(v); perr == nil {
				s.customStatusDetailedCache = parsed
				s.customStatusCache = types.CustomStatusNames(parsed)
			}
		}
		s.customStatusCached = true

		if v := all["types.custom"]; v != "" {
			s.customTypeCache = parseCommaListFromConfig(v)
		}
		s.customTypeCached = true

		var infraList []string
		if v := all["types.infra"]; v != "" {
			infraList = parseCommaListFromConfig(v)
		} else {
			infraList = storage.DefaultInfraTypes()
		}
		m := make(map[string]bool, len(infraList))
		for _, t := range infraList {
			if t = strings.TrimSpace(t); t != "" {
				m[t] = true
			}
		}
		s.infraTypeCache = m
		s.infraTypeCached = true

		s.wispsEmpty = wispsEmpty
		s.wispsStateKnown = true
		s.cacheMu.Unlock()
		return nil
	})
}

func parseCommaListFromConfig(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SetMetadata sets a metadata value
func (s *DoltStore) SetMetadata(ctx context.Context, key, value string) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.SetMetadataInTx(ctx, tx, key, value)
	})
}

// GetMetadata retrieves a metadata value
func (s *DoltStore) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.withReadConn(ctx, func(q issueops.SQLQuerier) error {
		var err error
		value, err = issueops.GetMetadataInTx(ctx, q, key)
		return err
	})
	return value, err
}

// SetLocalMetadata sets a value in the dolt-ignored local_metadata table.
// Used for clone-local state that should not generate merge conflicts.
func (s *DoltStore) SetLocalMetadata(ctx context.Context, key, value string) error {
	return s.withRetryTx(ctx, func(tx *sql.Tx) error {
		return issueops.SetLocalMetadataInTx(ctx, tx, key, value)
	})
}

// GetLocalMetadata retrieves a value from the dolt-ignored local_metadata table.
// Returns ("", nil) if the key does not exist.
func (s *DoltStore) GetLocalMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.withReadConn(ctx, func(q issueops.SQLQuerier) error {
		var err error
		value, err = issueops.GetLocalMetadataInTx(ctx, q, key)
		return err
	})
	return value, err
}

func (s *DoltStore) loadCustomConfigCache(ctx context.Context) {
	s.cacheMu.Lock()
	if s.customStatusCached && s.customTypeCached {
		s.cacheMu.Unlock()
		return
	}
	s.cacheMu.Unlock()

	var statuses []types.CustomStatus
	var customTypes []string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var resolveErr error
		statuses, customTypes, resolveErr = issueops.ResolveCustomConfigInTx(ctx, tx)
		return resolveErr
	})
	if err != nil {
		log.Printf("warning: failed to resolve custom config: %v", err)
		if yamlStatuses := config.GetCustomStatusesFromYAML(); len(yamlStatuses) > 0 {
			statuses = issueops.ParseStatusFallback(yamlStatuses)
		}
		if yamlTypes := config.GetCustomTypesFromYAML(); len(yamlTypes) > 0 {
			customTypes = yamlTypes
		}
	}

	s.cacheMu.Lock()
	if !s.customStatusCached {
		s.customStatusDetailedCache = statuses
		s.customStatusCache = types.CustomStatusNames(statuses)
		s.customStatusCached = true
	}
	if !s.customTypeCached {
		s.customTypeCache = customTypes
		s.customTypeCached = true
	}
	s.cacheMu.Unlock()
}

func (s *DoltStore) GetCustomStatuses(ctx context.Context) ([]string, error) {
	s.loadCustomConfigCache(ctx)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return s.customStatusCache, nil
}

func (s *DoltStore) GetCustomStatusesDetailed(ctx context.Context) ([]types.CustomStatus, error) {
	s.loadCustomConfigCache(ctx)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return s.customStatusDetailedCache, nil
}

// GetCustomTypes returns custom issue type values from config.
// If the database doesn't have custom types configured, falls back to config.yaml.
// Returns an empty slice if no custom types are configured.
// Results are cached per DoltStore lifetime and invalidated when SetConfig
// updates the "types.custom" key.
func (s *DoltStore) GetCustomTypes(ctx context.Context) ([]string, error) {
	s.loadCustomConfigCache(ctx)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return s.customTypeCache, nil
}

// GetInfraTypes returns infrastructure type names from config.
// Infrastructure types are routed to the wisps table to keep the versioned
// issues table clean. Defaults to ["agent", "rig", "role", "message"] if
// no custom configuration exists.
// Falls back: DB config "types.infra" → config.yaml types.infra → defaults.
// Results are cached per DoltStore lifetime and invalidated when SetConfig
// updates the "types.infra" key.
func (s *DoltStore) GetInfraTypes(ctx context.Context) map[string]bool {
	s.cacheMu.Lock()
	if s.infraTypeCached {
		result := s.infraTypeCache
		s.cacheMu.Unlock()
		return result
	}
	s.cacheMu.Unlock()

	var result map[string]bool
	if err := s.withReadTx(ctx, func(tx *sql.Tx) error {
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

	s.cacheMu.Lock()
	s.infraTypeCache = result
	s.infraTypeCached = true
	s.cacheMu.Unlock()

	return result
}
