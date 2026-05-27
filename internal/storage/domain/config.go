package domain

import (
	"context"
	"fmt"
)

type ConfigSQLRepository interface {
	GetMetadata(ctx context.Context, key string) (string, error)
	SetMetadata(ctx context.Context, key, value string) error
	SetLocalMetadata(ctx context.Context, key, value string) error
	GetConfig(ctx context.Context, key string) (string, error)
	SetConfig(ctx context.Context, key, value string) error

	GetCustomTypes(ctx context.Context) ([]string, error)
	GetAllowedPrefixes(ctx context.Context) (string, error)
	GetAdaptiveIDConfig(ctx context.Context) (AdaptiveIDConfig, error)
}

type ConfigUseCase interface {
	VerifyInit(ctx context.Context) (VerifyResult, error)
	GetCustomTypes(ctx context.Context) ([]string, error)
	LoadCreateContext(ctx context.Context) (CreateContext, error)
}

// CreateContext bundles the read-only config inputs that bd create needs
// before inserting an issue. Returned by ConfigUseCase.LoadCreateContext in
// a single round trip to keep the proxied-server path cheap.
type CreateContext struct {
	IssuePrefix     string
	AllowedPrefixes string
	CustomTypes     []string
}

type Issue struct{}

type BatchCreateOptions struct{}

type GlobalDatabaseParams struct{}

type ImportResult struct{}

type VerifyResult struct {
	ProjectID   string
	IssuePrefix string
	Missing     []string
}

func NewConfigUseCase(cfgRepo ConfigSQLRepository) ConfigUseCase {
	return &configUseCaseImpl{cfgRepo: cfgRepo}
}

type configUseCaseImpl struct {
	cfgRepo ConfigSQLRepository
}

var _ ConfigUseCase = (*configUseCaseImpl)(nil)

func (u *configUseCaseImpl) VerifyInit(ctx context.Context) (VerifyResult, error) {
	projectID, err := u.cfgRepo.GetMetadata(ctx, "_project_id")
	if err != nil {
		return VerifyResult{}, fmt.Errorf("VerifyInit: read _project_id: %w", err)
	}
	issuePrefix, err := u.cfgRepo.GetConfig(ctx, "issue_prefix")
	if err != nil {
		return VerifyResult{}, fmt.Errorf("VerifyInit: read issue_prefix: %w", err)
	}

	var missing []string
	if projectID == "" {
		missing = append(missing, "metadata._project_id")
	}
	if issuePrefix == "" {
		missing = append(missing, "config.issue_prefix")
	}

	return VerifyResult{
		ProjectID:   projectID,
		IssuePrefix: issuePrefix,
		Missing:     missing,
	}, nil
}

func (u *configUseCaseImpl) GetCustomTypes(ctx context.Context) ([]string, error) {
	out, err := u.cfgRepo.GetCustomTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetCustomTypes: %w", err)
	}
	return out, nil
}

func (u *configUseCaseImpl) LoadCreateContext(ctx context.Context) (CreateContext, error) {
	prefix, err := u.cfgRepo.GetConfig(ctx, "issue_prefix")
	if err != nil {
		return CreateContext{}, fmt.Errorf("LoadCreateContext: read issue_prefix: %w", err)
	}
	allowed, err := u.cfgRepo.GetAllowedPrefixes(ctx)
	if err != nil {
		return CreateContext{}, fmt.Errorf("LoadCreateContext: read allowed_prefixes: %w", err)
	}
	customTypes, err := u.cfgRepo.GetCustomTypes(ctx)
	if err != nil {
		return CreateContext{}, fmt.Errorf("LoadCreateContext: read custom types: %w", err)
	}
	return CreateContext{
		IssuePrefix:     prefix,
		AllowedPrefixes: allowed,
		CustomTypes:     customTypes,
	}, nil
}
