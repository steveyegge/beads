package dolt

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/schema"
)

// DoltDriver implements storage.Driver by embedding a *DoltStore.
// All storage.Storage methods are satisfied via the embedded DoltStore.
// The driver-specific lifecycle methods (Open, Ping, SchemaVersion, etc.)
// are implemented directly on DoltDriver.
//
// Registration: init() registers the "dolt" driver opener with storage.RegisterDriver.
// Production callers use storage.OpenDriver(ctx, "dolt", cfg) to obtain a Driver.
type DoltDriver struct {
	*DoltStore // embeds all storage.Storage methods
}

var _ storage.Driver = (*DoltDriver)(nil)

// Name returns "dolt".
func (d *DoltDriver) Name() string { return "dolt" }

// Capabilities returns the full Dolt capability set.
func (d *DoltDriver) Capabilities() storage.CapabilitySet { return storage.DoltCapabilities }

// Open initializes the DoltStore from cfg.BeadsDir.
// After Open returns nil, all Storage methods are safe to call.
func (d *DoltDriver) Open(ctx context.Context, cfg storage.DriverConfig) error {
	store, err := NewFromConfig(ctx, cfg.BeadsDir)
	if err != nil {
		return fmt.Errorf("dolt driver open: %w", err)
	}
	d.DoltStore = store
	return nil
}

// Ping executes a lightweight SELECT 1 to confirm the server is reachable.
func (d *DoltDriver) Ping(ctx context.Context) error {
	var dummy int
	return d.DB().QueryRowContext(ctx, "SELECT 1").Scan(&dummy)
}

// SchemaVersion returns the highest applied schema migration version.
func (d *DoltDriver) SchemaVersion(ctx context.Context) (int, error) {
	var ver int
	err := d.DB().QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	).Scan(&ver)
	if err != nil {
		return 0, fmt.Errorf("dolt driver schema version: %w", err)
	}
	return ver, nil
}

// InitSchema applies all pending schema migrations on an empty database.
func (d *DoltDriver) InitSchema(ctx context.Context) error {
	conn, err := d.DB().Conn(ctx)
	if err != nil {
		return fmt.Errorf("dolt driver init schema: %w", err)
	}
	defer conn.Close()
	_, err = schema.MigrateUp(ctx, conn)
	return err
}

// MigrateSchema runs migrations up to and including targetVersion.
// It is a no-op when the current schema version is already >= targetVersion.
func (d *DoltDriver) MigrateSchema(ctx context.Context, targetVersion int) error {
	current, err := d.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if current >= targetVersion {
		return nil
	}
	conn, err := d.DB().Conn(ctx)
	if err != nil {
		return fmt.Errorf("dolt driver migrate schema: %w", err)
	}
	defer conn.Close()
	_, err = schema.MigrateUp(ctx, conn)
	return err
}

func init() {
	storage.RegisterDriver("dolt", func() (storage.Driver, error) {
		return &DoltDriver{}, nil
	})
}
