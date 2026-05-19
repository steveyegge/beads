package uow

import (
	"context"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/storage/domain/db"
)

type UnitOfWork interface {
	Close(ctx context.Context)
	Commit(ctx context.Context, message string) error

	ConfigUseCase() domain.ConfigUseCase
	DoltRemoteUseCase() domain.DoltRemoteUseCase
	BootstrapUseCase() domain.BootstrapUseCase
}

type UnitOfWorkProvider interface {
	NewUOW(ctx context.Context) (UnitOfWork, error)
	Close(ctx context.Context) error
}

func NewUOW(ctx context.Context, p TxProvider) (UnitOfWork, error) {
	tx, err := p.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	return &baseUOW{tx: tx}, nil
}

type baseUOW struct {
	tx Tx

	configUseCase    domain.ConfigUseCase
	remoteUseCase    domain.DoltRemoteUseCase
	bootstrapUseCase domain.BootstrapUseCase
}

func (u *baseUOW) Commit(ctx context.Context, message string) error {
	return u.tx.Commit(ctx, message)
}

func (u *baseUOW) Close(ctx context.Context) {
	u.tx.RollbackUnlessCommitted(ctx)
}

func (u *baseUOW) ConfigUseCase() domain.ConfigUseCase {
	if u.configUseCase == nil {
		u.configUseCase = domain.NewConfigUseCase(db.NewConfigSQLRepository(u.tx.Runner()))
	}
	return u.configUseCase
}

func (u *baseUOW) DoltRemoteUseCase() domain.DoltRemoteUseCase {
	if u.remoteUseCase == nil {
		u.remoteUseCase = domain.NewDoltRemoteUseCase(db.NewRemoteSQLRepository(u.tx.Runner()))
	}
	return u.remoteUseCase
}

func (u *baseUOW) BootstrapUseCase() domain.BootstrapUseCase {
	if u.bootstrapUseCase == nil {
		u.bootstrapUseCase = domain.NewBootstrapUseCase(
			db.NewConfigSQLRepository(u.tx.Runner()),
			u.DoltRemoteUseCase(),
		)
	}
	return u.bootstrapUseCase
}
