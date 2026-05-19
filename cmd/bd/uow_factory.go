package main

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
	"github.com/steveyegge/beads/internal/storage/uow"
)

func newProxiedServerUOWProvider(ctx context.Context, beadsDir string) (uow.UnitOfWorkProvider, error) {
	if beadsDir == "" {
		return nil, fmt.Errorf("newProxiedServerUOWProvider: beadsDir must be set")
	}

	doltBin, err := exec.LookPath("dolt")
	if err != nil {
		return nil, fmt.Errorf("newProxiedServerUOWProvider: dolt is not installed (not found in PATH); install from https://docs.dolthub.com/introduction/installation: %w", err)
	}

	persisted, _ := configfile.Load(beadsDir)
	database := configfile.DefaultDoltDatabase
	if persisted != nil {
		database = persisted.GetDoltDatabase()
	}

	rootPath := resolveProxiedServerRootPath(beadsDir, persisted)
	if err := validateProxiedServerRootPath(rootPath); err != nil {
		return nil, err
	}

	configPath, err := ensureProxiedServerConfig(beadsDir, persisted)
	if err != nil {
		return nil, err
	}

	logPath, isCustomLog := resolveProxiedServerLogPath(beadsDir, persisted)
	if isCustomLog {
		if err := validateProxiedServerLogPath(logPath); err != nil {
			return nil, err
		}
	}

	return uow.NewDoltServerUOWProvider(
		ctx,
		rootPath,
		database,
		logPath,
		configPath,
		proxy.BackendLocalServer,
		"root",
		"", // proxy is loopback-only, no auth
		doltBin,
	)
}
