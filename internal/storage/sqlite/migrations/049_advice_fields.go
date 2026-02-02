package migrations

import (
	"database/sql"
)

// MigrateAdviceFields is a no-op migration.
//
// Originally added advice_target_rig, advice_target_role, advice_target_agent columns
// for hierarchical agent advice targeting. These columns were removed because
// advice targeting now uses subscriptions instead (see advice_subscription_fields migration).
//
// Existing databases may still have these columns but they are unused.
// New databases will not have these columns created.
func MigrateAdviceFields(db *sql.DB) error {
	// No-op: advice targeting columns removed in favor of subscription-based filtering.
	// See bd-hhbu epic for details.
	return nil
}
