package domain

import (
	"context"
	"fmt"
)

type DoltRemoteUseCase interface {
	CreateRemote(ctx context.Context, name, url string) error
	UpdateRemote(ctx context.Context, name, url string) error
	DeleteRemote(ctx context.Context, name string) error
	ListRemotes(ctx context.Context) ([]Remote, error)
}

type Remote struct {
	Name string
	URL  string
}

type RemoteSQLRepository interface {
	AddRemote(ctx context.Context, name, url string) error
	RemoveRemote(ctx context.Context, name string) error
	ListRemotes(ctx context.Context) ([]Remote, error)
}

func NewDoltRemoteUseCase(remoteRepo RemoteSQLRepository) DoltRemoteUseCase {
	return &doltRemoteUseCaseImpl{remoteRepo: remoteRepo}
}

type doltRemoteUseCaseImpl struct {
	remoteRepo RemoteSQLRepository
}

var _ DoltRemoteUseCase = (*doltRemoteUseCaseImpl)(nil)

func (u *doltRemoteUseCaseImpl) CreateRemote(ctx context.Context, name, url string) error {
	if name == "" {
		return fmt.Errorf("CreateRemote: name must not be empty")
	}
	if url == "" {
		return fmt.Errorf("CreateRemote: url must not be empty")
	}
	if err := u.remoteRepo.AddRemote(ctx, name, url); err != nil {
		return fmt.Errorf("CreateRemote %s: %w", name, err)
	}
	return nil
}

func (u *doltRemoteUseCaseImpl) UpdateRemote(ctx context.Context, name, url string) error {
	if name == "" {
		return fmt.Errorf("UpdateRemote: name must not be empty")
	}
	if url == "" {
		return fmt.Errorf("UpdateRemote: url must not be empty")
	}
	if err := u.remoteRepo.RemoveRemote(ctx, name); err != nil {
		return fmt.Errorf("UpdateRemote %s: remove: %w", name, err)
	}
	if err := u.remoteRepo.AddRemote(ctx, name, url); err != nil {
		return fmt.Errorf("UpdateRemote %s: add: %w", name, err)
	}
	return nil
}

func (u *doltRemoteUseCaseImpl) DeleteRemote(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("DeleteRemote: name must not be empty")
	}
	if err := u.remoteRepo.RemoveRemote(ctx, name); err != nil {
		return fmt.Errorf("DeleteRemote %s: %w", name, err)
	}
	return nil
}

func (u *doltRemoteUseCaseImpl) ListRemotes(ctx context.Context) ([]Remote, error) {
	remotes, err := u.remoteRepo.ListRemotes(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListRemotes: %w", err)
	}
	return remotes, nil
}
