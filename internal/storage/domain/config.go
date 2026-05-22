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
}

type ConfigUseCase interface {
	VerifyInit(ctx context.Context) (VerifyResult, error)
	GetCustomTypes(ctx context.Context) ([]string, error)
	LoadCreateContext(ctx context.Context) (CreateContext, error)
	SetIssuePrefix(ctx context.Context, prefix string) error
}

// CreateContext bundles the read-only config inputs that bd create needs
// before inserting an issue. Returned by ConfigUseCase.LoadCreateContext in
// a single round trip to keep the proxied-server path cheap.
type CreateContext struct {
	IssuePrefix     string
	AllowedPrefixes string
	CustomTypes     []string
	// RoutingConfig holds the well-known routing.* / contributor.* keys.
	// Only keys with non-empty values are present.
	RoutingConfig map[string]string
}

// CreateContextRoutingKeys is the fixed set of config keys LoadCreateContext
// reads into CreateContext.RoutingConfig. Exposed so the CLI can reuse the
// same key list when overlaying YAML precedence on top of the DB values.
var CreateContextRoutingKeys = []string{
	"routing.mode",
	"routing.contributor",
	"routing.default",
	"routing.maintainer",
	"contributor.auto_route",
	"contributor.planning_repo",
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

func (u *configUseCaseImpl) SetIssuePrefix(ctx context.Context, prefix string) error {
	if prefix == "" {
		return fmt.Errorf("SetIssuePrefix: prefix must not be empty")
	}
	if err := u.cfgRepo.SetConfig(ctx, "issue_prefix", prefix); err != nil {
		return fmt.Errorf("SetIssuePrefix: %w", err)
	}
	return nil
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

	routing := make(map[string]string, len(CreateContextRoutingKeys))
	for _, key := range CreateContextRoutingKeys {
		v, err := u.cfgRepo.GetConfig(ctx, key)
		if err != nil {
			return CreateContext{}, fmt.Errorf("LoadCreateContext: read %s: %w", key, err)
		}
		if v != "" {
			routing[key] = v
		}
	}

	return CreateContext{
		IssuePrefix:     prefix,
		AllowedPrefixes: allowed,
		CustomTypes:     customTypes,
		RoutingConfig:   routing,
	}, nil
}
