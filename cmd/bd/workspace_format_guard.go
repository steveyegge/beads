package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
)

// ExitUnsupportedWorkspaceFormat is the exit code for a command refused
// because its workspace declares a format this bd build cannot open: a
// metadata.json graph_mode other than "dependency", or a graph workspace
// format marker (graphFormatMarkerFile). Stable so a wrapper can
// tell "this workspace needs a different bd build" apart from a generic
// failure (1) without grepping stderr.
//
// 16 because 13, 14 and 15 are taken (ExitGuardMismatch, ExitMigrationFrozen,
// ExitHomeDirRefused). Like ExitHomeDirRefused, it is not part of protocol v0
// §E3, which freezes 10/11/12/130.
const ExitUnsupportedWorkspaceFormat = 16

// graphFormatMarkerFile is the format sentinel graph-preview builds write in
// .beads before metadata.json, so that a graph workspace whose metadata was
// never written, was lost, or was rewritten without graph_mode by a bd that
// does not know the field still declares its format.
const graphFormatMarkerFile = "graph-preview-format"

// unsupportedWorkspaceFormatError refuses a workspace whose metadata or format
// marker declares a data model other than the Issue store this build opens.
// Graph-preview builds persist graph_mode "link" plus graph_* binding fields
// beside an ordinary Dolt backend, and the graphFormatMarkerFile beside them,
// so nothing else tells this build that the database behind it is not an
// Issue store; opening it as one would run Issue migrations into it.
type unsupportedWorkspaceFormatError struct {
	beadsDir  string
	graphMode string
	// formatMarker reports that graphFormatMarkerFile, not graph_mode,
	// declared the format.
	formatMarker bool
}

func (e *unsupportedWorkspaceFormatError) Error() string {
	if e.formatMarker {
		return fmt.Sprintf("workspace %s has a %s marker, which this bd build does not support; %s",
			e.beadsDir, graphFormatMarkerFile, configfile.BackendNotOpenedGuarantee)
	}
	return fmt.Sprintf("workspace %s uses format graph_mode %q, which this bd build does not support; %s",
		e.beadsDir, e.graphMode, configfile.BackendNotOpenedGuarantee)
}

func (e *unsupportedWorkspaceFormatError) hint() string {
	if e.formatMarker {
		return fmt.Sprintf("restore metadata.json (graph_mode and the graph_* fields) from version control or a backup, then use a bd build that supports graph workspaces; do not remove .beads/%s", graphFormatMarkerFile)
	}
	return fmt.Sprintf("use a bd build that supports graph_mode %q workspaces; leave graph_mode and the graph_* fields in metadata.json unchanged", e.graphMode)
}

// checkWorkspaceFormat refuses metadata whose graph_mode marker names any
// model but the default. An unknown marker is refused too: it may describe a
// format newer than this build, so it must never fall back to the Issue store.
// It also refuses a beadsDir holding the graph format marker file, whatever
// the metadata says and whatever the file holds. cfg may be nil (no metadata);
// beadsDir may be "" (no workspace directory, so no marker to find).
func checkWorkspaceFormat(cfg *configfile.Config, beadsDir string) error {
	if mode, err := cfg.GetGraphMode(); err != nil || mode != configfile.GraphModeDependency {
		return &unsupportedWorkspaceFormatError{beadsDir: beadsDir, graphMode: cfg.GraphMode}
	}
	if hasGraphFormatMarker(beadsDir) {
		return &unsupportedWorkspaceFormatError{beadsDir: beadsDir, formatMarker: true}
	}
	return nil
}

// loadAndCheckWorkspaceFormat reads beadsDir's discovery metadata without
// migrating it and refuses a workspace in a format this build cannot open
// (checkWorkspaceFormat). cfg is nil when beadsDir has no metadata. Metadata
// that cannot be read beside the graph format marker is refused as that
// format too: the marker alone declares it.
func loadAndCheckWorkspaceFormat(beadsDir string) (*configfile.Config, error) {
	cfg, err := configfile.LoadForDiscovery(beadsDir)
	if err != nil {
		if hasGraphFormatMarker(beadsDir) {
			return nil, &unsupportedWorkspaceFormatError{beadsDir: beadsDir, formatMarker: true}
		}
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if err := checkWorkspaceFormat(cfg, beadsDir); err != nil {
		return nil, err
	}
	return cfg, nil
}

// hasGraphFormatMarker reports whether anything exists at beadsDir's graph
// format marker path. Callers read beadsDir's metadata first, so a directory
// bd cannot search has already failed there.
func hasGraphFormatMarker(beadsDir string) bool {
	if beadsDir == "" {
		return false
	}
	_, err := os.Lstat(filepath.Join(beadsDir, graphFormatMarkerFile))
	return err == nil
}

// checkDiscoveredWorkspaceFormat runs checkWorkspaceFormat on beadsDir's
// metadata as LoadForDiscovery reads it, which never rewrites the workspace.
// A path that opens or initializes a workspace without passing through command
// admission runs it first: configfile.Load would otherwise migrate a legacy
// config.json into metadata.json before the format check could refuse it.
// Metadata that does not load is left to the caller's own load to report,
// unless the graph format marker sits beside it (loadAndCheckWorkspaceFormat).
func checkDiscoveredWorkspaceFormat(beadsDir string) error {
	if _, err := loadAndCheckWorkspaceFormat(beadsDir); isUnsupportedWorkspaceFormat(err) {
		return err
	}
	return nil
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
