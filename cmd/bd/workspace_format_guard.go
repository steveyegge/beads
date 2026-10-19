package main

import (
	"errors"
	"fmt"

	"github.com/steveyegge/beads/internal/configfile"
)

// ExitUnsupportedWorkspaceFormat is the exit code for a command refused
// because its workspace declares a format this bd build cannot open: a
// metadata.json graph_mode other than "dependency". Stable so a wrapper can
// tell "this workspace needs a different bd build" apart from a generic
// failure (1) without grepping stderr.
//
// 16 because 13, 14 and 15 are taken (ExitGuardMismatch, ExitMigrationFrozen,
// ExitHomeDirRefused). Like ExitHomeDirRefused, it is not part of protocol v0
// §E3, which freezes 10/11/12/130.
const ExitUnsupportedWorkspaceFormat = 16

// unsupportedWorkspaceFormatError refuses a workspace whose metadata declares
// a data model other than the Issue store this build opens. Graph-preview
// builds persist graph_mode "link" plus graph_* binding fields beside an
// ordinary Dolt backend, so nothing else in the metadata tells this build
// that the database behind it is not an Issue store; opening it as one would
// run Issue migrations into it.
type unsupportedWorkspaceFormatError struct {
	beadsDir  string
	graphMode string
}

func (e *unsupportedWorkspaceFormatError) Error() string {
	return fmt.Sprintf("workspace %s uses format graph_mode %q, which this bd build does not support; %s",
		e.beadsDir, e.graphMode, configfile.BackendNotOpenedGuarantee)
}

func (e *unsupportedWorkspaceFormatError) hint() string {
	return fmt.Sprintf("use a bd build that supports graph_mode %q workspaces; leave graph_mode and the graph_* fields in metadata.json unchanged", e.graphMode)
}

// checkWorkspaceFormat refuses metadata whose graph_mode marker names any
// model but the default. An unknown marker is refused too: it may describe a
// format newer than this build, so it must never fall back to the Issue store.
// cfg may be nil (no metadata), which is an ordinary workspace.
func checkWorkspaceFormat(cfg *configfile.Config, beadsDir string) error {
	if mode, err := cfg.GetGraphMode(); err == nil && mode == configfile.GraphModeDependency {
		return nil
	}
	return &unsupportedWorkspaceFormatError{beadsDir: beadsDir, graphMode: cfg.GraphMode}
}

// checkDiscoveredWorkspaceFormat runs checkWorkspaceFormat on beadsDir's
// metadata as LoadForDiscovery reads it, which never rewrites the workspace.
// A path that opens or initializes a workspace without passing through command
// admission runs it first: configfile.Load would otherwise migrate a legacy
// config.json into metadata.json before the format check could refuse it.
// Metadata that does not load is left to the caller's own load to report.
func checkDiscoveredWorkspaceFormat(beadsDir string) error {
	cfg, err := configfile.LoadForDiscovery(beadsDir)
	if err != nil {
		return nil
	}
	return checkWorkspaceFormat(cfg, beadsDir)
}

func isUnsupportedWorkspaceFormat(err error) bool {
	var formatErr *unsupportedWorkspaceFormatError
	return errors.As(err, &formatErr)
}

// handleWorkspaceGuardError renders a workspace admission refusal from
// guardLegacyUpgradeWorkspace. An unsupported workspace format gets a hint and
// ExitUnsupportedWorkspaceFormat; every other refusal keeps HandleError's
// rendering and exit 1.
func handleWorkspaceGuardError(err error) error {
	if err == nil {
		return nil
	}
	var formatErr *unsupportedWorkspaceFormatError
	if !errors.As(err, &formatErr) {
		return HandleError("%v", err)
	}
	_ = HandleErrorWithHint(formatErr.Error(), formatErr.hint())
	return &exitError{Code: ExitUnsupportedWorkspaceFormat}
}
