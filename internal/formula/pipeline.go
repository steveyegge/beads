package formula

import (
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
)

// LoadAndResolve parses a formula file and applies all transformations.
// It first tries to load by name from the formula registry (.beads/formulas/),
// and falls back to parsing as a file path if that fails.
func LoadAndResolve(formulaPath string, searchPaths []string) (*Formula, error) {
	parser := NewParser(searchPaths...)
	return loadAndResolveWithParser(formulaPath, parser)
}

// LoadAndResolveWithStorage parses a formula and applies all transformations,
// using a database storage backend for formula resolution. The parser checks
// the database first for formulas stored as issues, then falls back to
// filesystem search paths.
func LoadAndResolveWithStorage(formulaPath string, searchPaths []string, store storage.Storage) (*Formula, error) {
	parser := NewParserWithStorage(store, searchPaths...)
	return loadAndResolveWithParser(formulaPath, parser)
}

// loadAndResolveWithParser implements the full formula resolution pipeline
// using the provided parser (which may or may not have a storage backend).
func loadAndResolveWithParser(formulaPath string, parser *Parser) (*Formula, error) {
	// Try to load by name first (from .beads/formulas/ registry or DB)
	f, err := parser.LoadByName(formulaPath)
	if err != nil {
		// Fall back to parsing as a file path
		f, err = parser.ParseFile(formulaPath)
		if err != nil {
			return nil, fmt.Errorf("parsing formula: %w", err)
		}
	}

	// Resolve inheritance
	resolved, err := parser.Resolve(f)
	if err != nil {
		return nil, fmt.Errorf("resolving formula: %w", err)
	}

	// Apply control flow operators - loops, branches, gates
	controlFlowSteps, err := ApplyControlFlow(resolved.Steps, resolved.Compose)
	if err != nil {
		return nil, fmt.Errorf("applying control flow: %w", err)
	}
	resolved.Steps = controlFlowSteps

	// Apply advice transformations
	if len(resolved.Advice) > 0 {
		resolved.Steps = ApplyAdvice(resolved.Steps, resolved.Advice)
	}

	// Apply inline step expansions
	inlineExpandedSteps, err := ApplyInlineExpansions(resolved.Steps, parser)
	if err != nil {
		return nil, fmt.Errorf("applying inline expansions: %w", err)
	}
	resolved.Steps = inlineExpandedSteps

	// Apply expansion operators
	if resolved.Compose != nil && (len(resolved.Compose.Expand) > 0 || len(resolved.Compose.Map) > 0) {
		expandedSteps, err := ApplyExpansions(resolved.Steps, resolved.Compose, parser)
		if err != nil {
			return nil, fmt.Errorf("applying expansions: %w", err)
		}
		resolved.Steps = expandedSteps
	}

	// Apply aspects from compose.aspects
	if resolved.Compose != nil && len(resolved.Compose.Aspects) > 0 {
		for _, aspectName := range resolved.Compose.Aspects {
			aspectFormula, err := parser.LoadByName(aspectName)
			if err != nil {
				return nil, fmt.Errorf("loading aspect %q: %w", aspectName, err)
			}
			if aspectFormula.Type != TypeAspect {
				return nil, fmt.Errorf("%q is not an aspect formula (type=%s)", aspectName, aspectFormula.Type)
			}
			if len(aspectFormula.Advice) > 0 {
				resolved.Steps = ApplyAdvice(resolved.Steps, aspectFormula.Advice)
			}
		}
	}

	return resolved, nil
}

// ResolveAndCook loads a formula by name, resolves it, applies all transformations,
// and returns an in-memory TemplateSubgraph ready for instantiation.
// This is the main entry point for ephemeral proto cooking.
func ResolveAndCook(formulaName string, searchPaths []string) (*TemplateSubgraph, error) {
	return ResolveAndCookWithVars(formulaName, searchPaths, nil)
}

// ResolveAndCookWithVars loads a formula and optionally filters steps by condition.
// If conditionVars is provided, steps with conditions that evaluate to false are excluded.
// Pass nil for conditionVars to include all steps (condition filtering skipped).
func ResolveAndCookWithVars(formulaName string, searchPaths []string, conditionVars map[string]string) (*TemplateSubgraph, error) {
	return resolveAndCookWithParser(formulaName, NewParser(searchPaths...), conditionVars)
}

// ResolveAndCookWithStorage loads a formula from DB or filesystem, applies all
// transformations, and returns an in-memory TemplateSubgraph. The storage backend
// is checked first before falling back to filesystem search paths.
func ResolveAndCookWithStorage(formulaName string, searchPaths []string, store storage.Storage, conditionVars map[string]string) (*TemplateSubgraph, error) {
	return resolveAndCookWithParser(formulaName, NewParserWithStorage(store, searchPaths...), conditionVars)
}

// resolveAndCookWithParser implements the full resolve-and-cook pipeline.
func resolveAndCookWithParser(formulaName string, parser *Parser, conditionVars map[string]string) (*TemplateSubgraph, error) {

	// Load formula by name
	f, err := parser.LoadByName(formulaName)
	if err != nil {
		return nil, fmt.Errorf("loading formula %q: %w", formulaName, err)
	}

	// Resolve inheritance
	resolved, err := parser.Resolve(f)
	if err != nil {
		return nil, fmt.Errorf("resolving formula %q: %w", formulaName, err)
	}

	// Apply control flow operators - loops, branches, gates
	controlFlowSteps, err := ApplyControlFlow(resolved.Steps, resolved.Compose)
	if err != nil {
		return nil, fmt.Errorf("applying control flow to %q: %w", formulaName, err)
	}
	resolved.Steps = controlFlowSteps

	// Apply advice transformations
	if len(resolved.Advice) > 0 {
		resolved.Steps = ApplyAdvice(resolved.Steps, resolved.Advice)
	}

	// Apply inline step expansions
	inlineExpandedSteps, err := ApplyInlineExpansions(resolved.Steps, parser)
	if err != nil {
		return nil, fmt.Errorf("applying inline expansions to %q: %w", formulaName, err)
	}
	resolved.Steps = inlineExpandedSteps

	// Apply expansion operators
	if resolved.Compose != nil && (len(resolved.Compose.Expand) > 0 || len(resolved.Compose.Map) > 0) {
		expandedSteps, err := ApplyExpansions(resolved.Steps, resolved.Compose, parser)
		if err != nil {
			return nil, fmt.Errorf("applying expansions to %q: %w", formulaName, err)
		}
		resolved.Steps = expandedSteps
	}

	// Apply aspects from compose.aspects
	if resolved.Compose != nil && len(resolved.Compose.Aspects) > 0 {
		for _, aspectName := range resolved.Compose.Aspects {
			aspectFormula, err := parser.LoadByName(aspectName)
			if err != nil {
				return nil, fmt.Errorf("loading aspect %q: %w", aspectName, err)
			}
			if aspectFormula.Type != TypeAspect {
				return nil, fmt.Errorf("%q is not an aspect formula (type=%s)", aspectName, aspectFormula.Type)
			}
			if len(aspectFormula.Advice) > 0 {
				resolved.Steps = ApplyAdvice(resolved.Steps, aspectFormula.Advice)
			}
		}
	}

	// Apply step condition filtering if vars provided
	// This filters out steps whose conditions evaluate to false
	if conditionVars != nil {
		// Merge with formula defaults for complete evaluation
		mergedVars := make(map[string]string)
		for name, def := range resolved.Vars {
			if def != nil && def.Default != "" {
				mergedVars[name] = def.Default
			}
		}
		for k, v := range conditionVars {
			mergedVars[k] = v
		}

		filteredSteps, err := FilterStepsByCondition(resolved.Steps, mergedVars)
		if err != nil {
			return nil, fmt.Errorf("filtering steps by condition: %w", err)
		}
		resolved.Steps = filteredSteps
	}

	// Cook to in-memory subgraph, including variable definitions for default handling
	return CookToSubgraphWithVars(resolved, resolved.Formula, resolved.Vars)
}
