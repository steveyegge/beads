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

	IssueUseCase() domain.IssueUseCase
	DependencyUseCase() domain.DependencyUseCase
	LabelUseCase() domain.LabelUseCase
	CustomTypesUseCase() domain.CustomTypesUseCase

	DependencyQueryUseCase() domain.DependencyQueryUseCase
	LabelQueryUseCase() domain.LabelQueryUseCase
	CommentQueryUseCase() domain.CommentQueryUseCase
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

	issueUseCase       domain.IssueUseCase
	dependencyUseCase  domain.DependencyUseCase
	labelUseCase       domain.LabelUseCase
	customTypesUseCase domain.CustomTypesUseCase

	dependencyQueryUseCase domain.DependencyQueryUseCase
	labelQueryUseCase      domain.LabelQueryUseCase
	commentQueryUseCase    domain.CommentQueryUseCase
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

func (u *baseUOW) IssueUseCase() domain.IssueUseCase {
	if u.issueUseCase == nil {
		runner := u.tx.Runner()
		u.issueUseCase = domain.NewIssueUseCase(
			db.NewIssueSQLRepository(runner),
			db.NewDependencySQLRepository(runner),
			db.NewLabelSQLRepository(runner),
			db.NewChildCounterSQLRepository(runner),
			db.NewCommentSQLRepository(runner),
			db.NewConfigSQLRepository(runner),
		)
	}
	return u.issueUseCase
}

func (u *baseUOW) DependencyUseCase() domain.DependencyUseCase {
	if u.dependencyUseCase == nil {
		runner := u.tx.Runner()
		u.dependencyUseCase = domain.NewDependencyUseCase(
			db.NewDependencySQLRepository(runner),
			db.NewIssueSQLRepository(runner),
		)
	}
	return u.dependencyUseCase
}

func (u *baseUOW) LabelUseCase() domain.LabelUseCase {
	if u.labelUseCase == nil {
		u.labelUseCase = domain.NewLabelUseCase(db.NewLabelSQLRepository(u.tx.Runner()))
	}
	return u.labelUseCase
}

func (u *baseUOW) CustomTypesUseCase() domain.CustomTypesUseCase {
	if u.customTypesUseCase == nil {
		u.customTypesUseCase = domain.NewCustomTypesUseCase(db.NewConfigSQLRepository(u.tx.Runner()))
	}
	return u.customTypesUseCase
}

func (u *baseUOW) DependencyQueryUseCase() domain.DependencyQueryUseCase {
	if u.dependencyQueryUseCase == nil {
		u.dependencyQueryUseCase = domain.NewDependencyQueryUseCase(db.NewDependencySQLRepository(u.tx.Runner()))
	}
	return u.dependencyQueryUseCase
}

func (u *baseUOW) LabelQueryUseCase() domain.LabelQueryUseCase {
	if u.labelQueryUseCase == nil {
		u.labelQueryUseCase = domain.NewLabelQueryUseCase(db.NewLabelSQLRepository(u.tx.Runner()))
	}
	return u.labelQueryUseCase
}

func (u *baseUOW) CommentQueryUseCase() domain.CommentQueryUseCase {
	if u.commentQueryUseCase == nil {
		u.commentQueryUseCase = domain.NewCommentQueryUseCase(db.NewCommentSQLRepository(u.tx.Runner()))
	}
	return u.commentQueryUseCase
}
