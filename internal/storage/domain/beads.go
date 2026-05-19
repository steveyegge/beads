package domain

import (
	"context"
	"fmt"
	"path/filepath"
)

type BeadsDirFSRepository interface {
	CreateBeadsDir(ctx context.Context, beadsDir string) error
	BeadsDirExists(ctx context.Context, beadsDir string) (bool, error)
	WriteBeadsGitignore(ctx context.Context, beadsDir string) error
	BeadsGitignoreExists(ctx context.Context, beadsDir string) (bool, error)
	WriteProjectGitignore(ctx context.Context, repoRoot string) error
	ProjectGitignoreExists(ctx context.Context, repoRoot string) (bool, error)
	WriteInteractionsLog(ctx context.Context, beadsDir string) error
	WriteReadme(ctx context.Context, beadsDir string) error
	WriteMetadataJSON(ctx context.Context, beadsDir string, content []byte) error
	ReadMetadataJSON(ctx context.Context, beadsDir string) ([]byte, error)
	WriteConfigYAML(ctx context.Context, beadsDir string, content []byte) error
	ReadConfigYAML(ctx context.Context, beadsDir string) ([]byte, error)
}

type BeadsDirFSUseCase interface {
	ResolveBeadsDir(ctx context.Context) BeadsDirResolution
	InitializeBeadsDir(ctx context.Context, params InitializeBeadsDirParams) (InitializeBeadsDirResult, error)
	SetupForkExclude(ctx context.Context, verbose bool) error
	SetupStealthMode(ctx context.Context, verbose bool) error
	InstallGitHooks(ctx context.Context, params HooksInstallParams) error
	InstallJJHooks(ctx context.Context) error
	AddAgentsInstructions(ctx context.Context, params AgentsFileParams) error
	InstallClaudeProject(ctx context.Context, stealth bool) error
	SetYAMLConfig(ctx context.Context, key, value string) error
}

type BeadsDirResolution struct {
	BeadsDir    string
	HasExplicit bool
}

type InitializeBeadsDirParams struct {
	BeadsDir         string
	RepoRoot         string
	MetadataJSONBody []byte
	ConfigYAMLBody   []byte

	// SetNoCOW, when true, applies FS_NOCOW_FL to BeadsDir after creation.
	// Best-effort: failures surface in InitializeBeadsDirResult.NoCOWErr,
	// not the returned error.
	SetNoCOW bool

	// LocalVersion, when non-empty, writes BeadsDir/.local_version with
	// the given value. Best-effort: failures surface in
	// InitializeBeadsDirResult.LocalVersionErr, not the returned error.
	LocalVersion string
}

type InitializeBeadsDirResult struct {
	NoCOWErr        error
	LocalVersionErr error
}

type HooksInstallParams struct {
	HookNames  []string
	Force      bool
	Shared     bool
	Chain      bool
	BeadsHooks bool
}

type AgentsFileParams struct {
	File         string
	Verbose      bool
	TemplatePath string
	Profile      string
	HasRemote    bool
}

// BeadsDirFSAdapters wires the use case's non-trivial side effects to the
// CLI-side helper implementations. Each adapter is optional at the type
// level; methods that depend on a nil adapter return a configuration error.
type BeadsDirFSAdapters struct {
	ResolveBeadsDir       func() BeadsDirResolution
	ApplyNoCOW            func(path string) error
	WriteLocalVersion     func(path, version string) error
	SetupForkExclude      func(verbose bool) error
	SetupStealthMode      func(verbose bool) error
	InstallGitHooks       func(params HooksInstallParams) error
	InstallJJHooks        func() error
	AddAgentsInstructions func(params AgentsFileParams)
	InstallClaudeProject  func(stealth bool) error
	SetYAMLConfig         func(key, value string) error
}

func NewBeadsDirFSUseCase(fsRepo BeadsDirFSRepository, adapters BeadsDirFSAdapters) BeadsDirFSUseCase {
	return &beadsDirFSUseCaseImpl{fsRepo: fsRepo, adapters: adapters}
}

type beadsDirFSUseCaseImpl struct {
	fsRepo   BeadsDirFSRepository
	adapters BeadsDirFSAdapters
}

var _ BeadsDirFSUseCase = (*beadsDirFSUseCaseImpl)(nil)

func (u *beadsDirFSUseCaseImpl) ResolveBeadsDir(ctx context.Context) BeadsDirResolution {
	if u.adapters.ResolveBeadsDir == nil {
		return BeadsDirResolution{}
	}
	return u.adapters.ResolveBeadsDir()
}

func (u *beadsDirFSUseCaseImpl) InitializeBeadsDir(ctx context.Context, params InitializeBeadsDirParams) (InitializeBeadsDirResult, error) {
	if params.BeadsDir == "" {
		return InitializeBeadsDirResult{}, fmt.Errorf("InitializeBeadsDir: BeadsDir must not be empty")
	}

	if err := u.fsRepo.CreateBeadsDir(ctx, params.BeadsDir); err != nil {
		return InitializeBeadsDirResult{}, err
	}
	if err := u.fsRepo.WriteBeadsGitignore(ctx, params.BeadsDir); err != nil {
		return InitializeBeadsDirResult{}, err
	}
	if len(params.MetadataJSONBody) > 0 {
		if err := u.fsRepo.WriteMetadataJSON(ctx, params.BeadsDir, params.MetadataJSONBody); err != nil {
			return InitializeBeadsDirResult{}, err
		}
	}
	if len(params.ConfigYAMLBody) > 0 {
		if err := u.fsRepo.WriteConfigYAML(ctx, params.BeadsDir, params.ConfigYAMLBody); err != nil {
			return InitializeBeadsDirResult{}, err
		}
	}
	if err := u.fsRepo.WriteInteractionsLog(ctx, params.BeadsDir); err != nil {
		return InitializeBeadsDirResult{}, err
	}
	if err := u.fsRepo.WriteReadme(ctx, params.BeadsDir); err != nil {
		return InitializeBeadsDirResult{}, err
	}
	if params.RepoRoot != "" {
		if err := u.fsRepo.WriteProjectGitignore(ctx, params.RepoRoot); err != nil {
			return InitializeBeadsDirResult{}, err
		}
	}

	var result InitializeBeadsDirResult
	if params.SetNoCOW && u.adapters.ApplyNoCOW != nil {
		result.NoCOWErr = u.adapters.ApplyNoCOW(params.BeadsDir)
	}
	if params.LocalVersion != "" && u.adapters.WriteLocalVersion != nil {
		result.LocalVersionErr = u.adapters.WriteLocalVersion(
			filepath.Join(params.BeadsDir, ".local_version"),
			params.LocalVersion,
		)
	}
	return result, nil
}

func (u *beadsDirFSUseCaseImpl) SetupForkExclude(ctx context.Context, verbose bool) error {
	if u.adapters.SetupForkExclude == nil {
		return fmt.Errorf("SetupForkExclude: adapter not configured")
	}
	return u.adapters.SetupForkExclude(verbose)
}

func (u *beadsDirFSUseCaseImpl) SetupStealthMode(ctx context.Context, verbose bool) error {
	if u.adapters.SetupStealthMode == nil {
		return fmt.Errorf("SetupStealthMode: adapter not configured")
	}
	return u.adapters.SetupStealthMode(verbose)
}

func (u *beadsDirFSUseCaseImpl) InstallGitHooks(ctx context.Context, params HooksInstallParams) error {
	if u.adapters.InstallGitHooks == nil {
		return fmt.Errorf("InstallGitHooks: adapter not configured")
	}
	return u.adapters.InstallGitHooks(params)
}

func (u *beadsDirFSUseCaseImpl) InstallJJHooks(ctx context.Context) error {
	if u.adapters.InstallJJHooks == nil {
		return fmt.Errorf("InstallJJHooks: adapter not configured")
	}
	return u.adapters.InstallJJHooks()
}

func (u *beadsDirFSUseCaseImpl) AddAgentsInstructions(ctx context.Context, params AgentsFileParams) error {
	if u.adapters.AddAgentsInstructions == nil {
		return fmt.Errorf("AddAgentsInstructions: adapter not configured")
	}
	u.adapters.AddAgentsInstructions(params)
	return nil
}

func (u *beadsDirFSUseCaseImpl) InstallClaudeProject(ctx context.Context, stealth bool) error {
	if u.adapters.InstallClaudeProject == nil {
		return fmt.Errorf("InstallClaudeProject: adapter not configured")
	}
	return u.adapters.InstallClaudeProject(stealth)
}

func (u *beadsDirFSUseCaseImpl) SetYAMLConfig(ctx context.Context, key, value string) error {
	if u.adapters.SetYAMLConfig == nil {
		return fmt.Errorf("SetYAMLConfig: adapter not configured")
	}
	return u.adapters.SetYAMLConfig(key, value)
}
