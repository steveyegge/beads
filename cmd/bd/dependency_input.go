package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// dependencySpec is a parsed dependency input in either "type:id" or "id" form.
// Bare IDs default to "blocks".
type dependencySpec struct {
	Raw         string
	DependsOnID string
	Type        types.DependencyType
}

var knownDependencyTypes = []types.DependencyType{
	types.DepBlocks,
	types.DepParentChild,
	types.DepConditionalBlocks,
	types.DepWaitsFor,
	types.DepRelated,
	types.DepDiscoveredFrom,
	types.DepRepliesTo,
	types.DepRelatesTo,
	types.DepDuplicates,
	types.DepSupersedes,
	types.DepAuthoredBy,
	types.DepAssignedTo,
	types.DepApprovedBy,
	types.DepAttests,
	types.DepTracks,
	types.DepUntil,
	types.DepCausedBy,
	types.DepValidates,
	types.DepDelegatedFrom,
}

var commonBlockingTypeAliases = map[string]types.DependencyType{
	"needs":      types.DepBlocks,
	"depends-on": types.DepBlocks,
	"depends_on": types.DepBlocks,
	"blocked-by": types.DepBlocks,
	"blocked_by": types.DepBlocks,
}

func knownDependencyTypeList() string {
	names := make([]string, 0, len(knownDependencyTypes))
	for _, depType := range knownDependencyTypes {
		names = append(names, string(depType))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func parseDependencyTypeStrict(raw string) (types.DependencyType, error) {
	depType := types.DependencyType(strings.ToLower(strings.TrimSpace(raw)))
	if !depType.IsValid() {
		return "", fmt.Errorf("invalid dependency type %q", raw)
	}
	if depType.IsWellKnown() {
		return depType, nil
	}

	if canonical, ok := commonBlockingTypeAliases[string(depType)]; ok {
		return "", fmt.Errorf("unknown dependency type %q: use %q for blocking dependencies", raw, canonical)
	}

	return "", fmt.Errorf(
		"unknown dependency type %q: custom types are non-blocking in ready-work calculations and are disallowed; valid types: %s",
		raw,
		knownDependencyTypeList(),
	)
}

func parseDependencySpec(spec string) (dependencySpec, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return dependencySpec{}, fmt.Errorf("dependency cannot be empty")
	}

	// External references are valid bare dependency targets and default to "blocks".
	if strings.HasPrefix(spec, "external:") {
		if err := validateExternalRef(spec); err != nil {
			return dependencySpec{}, fmt.Errorf("invalid external dependency %q: %w", spec, err)
		}
		return dependencySpec{
			Raw:         spec,
			DependsOnID: spec,
			Type:        types.DepBlocks,
		}, nil
	}

	if !strings.Contains(spec, ":") {
		return dependencySpec{
			Raw:         spec,
			DependsOnID: spec,
			Type:        types.DepBlocks,
		}, nil
	}

	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return dependencySpec{}, fmt.Errorf("invalid dependency format %q, expected \"type:id\" or \"id\"", spec)
	}

	depTypeRaw := strings.TrimSpace(parts[0])
	dependsOnID := strings.TrimSpace(parts[1])
	if dependsOnID == "" {
		return dependencySpec{}, fmt.Errorf("invalid dependency format %q: missing target ID", spec)
	}

	depType, err := parseDependencyTypeStrict(depTypeRaw)
	if err != nil {
		return dependencySpec{}, err
	}

	// Validate external reference format if the target is an external ref.
	if strings.HasPrefix(dependsOnID, "external:") {
		if err := validateExternalRef(dependsOnID); err != nil {
			return dependencySpec{}, fmt.Errorf("invalid external dependency target %q: %w", dependsOnID, err)
		}
	}

	return dependencySpec{
		Raw:         spec,
		DependsOnID: dependsOnID,
		Type:        depType,
	}, nil
}

// parseDependencySpecs parses a slice of dependency spec strings, returning
// all valid specs or an error on the first invalid one. This is fail-fast:
// callers should validate before creating issues so that no issue is persisted
// with partially-valid dependencies. Previously, invalid deps were silently
// skipped with warnings; callers now abort or skip the entire operation instead.
func parseDependencySpecs(specs []string) ([]dependencySpec, error) {
	parsed := make([]dependencySpec, 0, len(specs))
	for i, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}

		dep, err := parseDependencySpec(spec)
		if err != nil {
			return nil, fmt.Errorf("dependency %d (%q): %w", i+1, spec, err)
		}
		parsed = append(parsed, dep)
	}

	return parsed, nil
}
