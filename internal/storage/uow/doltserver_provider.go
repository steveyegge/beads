package uow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff/v4"
	_ "github.com/go-sql-driver/mysql"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
	db "github.com/steveyegge/beads/internal/storage/domain/db"
	"github.com/steveyegge/beads/internal/storage/schema"
)

const defaultBranch = "main"

type doltServerProvider struct {
	defaultBranch string
	db            *sql.DB
	autoMigrate   bool
}

var (
	_ UnitOfWorkProvider = (*doltServerProvider)(nil)
	_ TxProvider         = (*doltServerProvider)(nil)
)

func NewDoltServerUOWProvider(
	ctx context.Context,
	serverRootDir string,
	database string,
	serverLogFilePath string,
	serverConfigFilePath string,
	backend proxy.Backend,
	rootUser string,
	rootPassword string,
	doltBinExec string,
	autoMigrate bool,
) (UnitOfWorkProvider, error) {
	if database == "" {
		return nil, fmt.Errorf("uow: database name must not be empty (caller should default to %q)", "beads")
	}
	if err := backend.Validate(); err != nil {
		return nil, fmt.Errorf("uow: backend: %w", err)
	}
	if rootUser == "" {
		return nil, fmt.Errorf("uow: rootUser must not be empty")
	}
	if doltBinExec == "" {
		return nil, fmt.Errorf("uow: doltBinExec must not be empty")
	}

	absServerRootDir, err := filepath.Abs(serverRootDir)
	if err != nil {
		return nil, fmt.Errorf("uow: resolving server root dir: %w", err)
	}
	absDoltBinExec, err := filepath.Abs(doltBinExec)
	if err != nil {
		return nil, fmt.Errorf("uow: resolving dolt bin exec: %w", err)
	}

	if err := os.MkdirAll(absServerRootDir, config.BeadsDirPerm); err != nil {
		return nil, fmt.Errorf("uow: creating server root directory: %w", err)
	}

	ep, err := getDatabaseProxyEndpoint(absServerRootDir, backend, serverConfigFilePath, serverLogFilePath, absDoltBinExec)
	if err != nil {
		return nil, fmt.Errorf("uow: get proxy endpoint: %w", err)
	}

	initDB, err := openDB(ctx, buildDSN(ep, "", rootUser, rootPassword))
	if err != nil {
		return nil, err
	}

	initProvider := &doltServerProvider{
		defaultBranch: defaultBranch,
		db:            initDB,
		autoMigrate:   autoMigrate,
	}

	if err := initProvider.initSchema(ctx, database); err != nil {
		_ = initDB.Close()
		return nil, fmt.Errorf("uow: init schema: %w", err)
	}

	if err := initDB.Close(); err != nil {
		return nil, fmt.Errorf("uow: close init db: %w", err)
	}

	dbConn, err := openDB(ctx, buildDSN(ep, database, rootUser, rootPassword))
	if err != nil {
		return nil, err
	}

	return &doltServerProvider{
		defaultBranch: defaultBranch,
		db:            dbConn,
	}, nil
}

func (p *doltServerProvider) NewUOW(ctx context.Context) (UnitOfWork, error) {
	return NewUOW(ctx, p)
}

func (p *doltServerProvider) NewTx(ctx context.Context) (Tx, error) {
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("uow: pin connection: %w", err)
	}
	return &doltServerTx{
		conn:         conn,
		vc:           db.NewDoltVersionControlSQLRepository(conn),
		targetBranch: p.defaultBranch,
	}, nil
}

func getDatabaseProxyEndpoint(serverRootDir string, backend proxy.Backend, configFilePath, logFilePath, doltBinPath string) (proxy.Endpoint, error) {
	return proxy.GetCreateDatabaseProxyServerEndpoint(serverRootDir, proxy.OpenOpts{
		Backend:        backend,
		ConfigFilePath: configFilePath,
		LogFilePath:    logFilePath,
		DoltBinPath:    doltBinPath,
		IdleTimeout:    30 * time.Second,
	})
}

func buildDSN(ep proxy.Endpoint, database, user, password string) string {
	return util.DoltServerDSN{
		Host:     ep.Host,
		Port:     ep.Port,
		User:     user,
		Password: password,
		Database: database,
	}.String()
}

func openDB(ctx context.Context, dsn string) (*sql.DB, error) {
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("uow: open db: %w", err)
	}
	if err := conn.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("uow: ping db: %w", err), conn.Close())
	}
	return conn, nil
}

func (p *doltServerProvider) initSchema(ctx context.Context, database string) error {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 25 * time.Millisecond
	bo.MaxElapsedTime = 5 * time.Second
	return backoff.Retry(func() error {
		conn, err := p.db.Conn(ctx)
		if err != nil {
			if isSerializationError(err) {
				return fmt.Errorf("uow: pin connection: %w", err)
			}
			return backoff.Permanent(fmt.Errorf("uow: pin connection: %w", err))
		}
		defer conn.Close()

		ddl := db.NewDDLSQLRepository(conn)
		if err := ddl.CreateDatabaseIfNotExists(ctx, database); err != nil {
			return backoff.Permanent(fmt.Errorf("uow: creating database: %w", err))
		}
		if err := ddl.UseDatabase(ctx, database); err != nil {
			return backoff.Permanent(fmt.Errorf("uow: switching to database: %w", err))
		}

		if p.autoMigrate {
			if _, err := schema.MigrateUp(ctx, conn); err != nil {
				if isSerializationError(err) {
					return fmt.Errorf("uow: migrate: %w", err)
				}
				return backoff.Permanent(fmt.Errorf("uow: migrate: %w", err))
			}
		} else {
			pending, err := schema.PendingMigrationCount(ctx, conn)
			if err != nil {
				if isSerializationError(err) {
					return fmt.Errorf("uow: schema version check: %w", err)
				}
				return backoff.Permanent(fmt.Errorf("uow: schema version check: %w", err))
			}
			if pending > 0 {
				latestVersion := schema.LatestVersion()
				currentVersion, err := schema.CurrentVersion(ctx, conn)
				if err != nil {
					return backoff.Permanent(fmt.Errorf("uow: reading current version: %w", err))
				}
				return backoff.Permanent(fmt.Errorf("beads schema is at version %d, binary requires %d — run: bd migrate schema", currentVersion, latestVersion))
			}
		}
		return nil
	}, backoff.WithContext(bo, ctx))
}
