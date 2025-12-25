// Package formula provides control flow operators for step transformation.
//
// Control flow operators (gt-8tmz.4) enable:
//   - loop: Repeat a body of steps (fixed count or conditional)
//   - branch: Fork-join parallel execution patterns
//   - gate: Conditional waits before steps proceed
//
// These operators are applied during formula cooking to transform
// the step graph before creating the proto bead.
package formula

import (
	"fmt"
)

// ApplyLoops expands loop bodies in a formula's steps.
// Fixed-count loops expand the body N times with indexed step IDs.
// Conditional loops expand once and add a "loop:until" label for runtime evaluation.
// Returns a new steps slice with loops expanded.
func ApplyLoops(steps []*Step) ([]*Step, error) {
	result := make([]*Step, 0, len(steps))

	for _, step := range steps {
		if step.Loop == nil {
			// No loop - recursively process children
			clone := cloneStep(step)
			if len(step.Children) > 0 {
				children, err := ApplyLoops(step.Children)
				if err != nil {
					return nil, err
				}
				clone.Children = children
			}
			result = append(result, clone)
			continue
		}

		// Validate loop spec
		if err := validateLoopSpec(step.Loop, step.ID); err != nil {
			return nil, err
		}

		// Expand the loop
		expanded, err := expandLoop(step)
		if err != nil {
			return nil, err
		}
		result = append(result, expanded...)
	}

	return result, nil
}

// validateLoopSpec checks that a loop spec is valid.
func validateLoopSpec(loop *LoopSpec, stepID string) error {
	if len(loop.Body) == 0 {
		return fmt.Errorf("loop %q: body is required", stepID)
	}

	if loop.Count > 0 && loop.Until != "" {
		return fmt.Errorf("loop %q: cannot have both count and until", stepID)
	}

	if loop.Count == 0 && loop.Until == "" {
		return fmt.Errorf("loop %q: either count or until is required", stepID)
	}

	if loop.Until != "" && loop.Max == 0 {
		return fmt.Errorf("loop %q: max is required when until is set", stepID)
	}

	if loop.Count < 0 {
		return fmt.Errorf("loop %q: count must be positive", stepID)
	}

	if loop.Max < 0 {
		return fmt.Errorf("loop %q: max must be positive", stepID)
	}

	return nil
}

// expandLoop expands a loop step into its constituent steps.
func expandLoop(step *Step) ([]*Step, error) {
	var result []*Step

	if step.Loop.Count > 0 {
		// Fixed-count loop: expand body N times
		for i := 1; i <= step.Loop.Count; i++ {
			iterSteps, err := expandLoopIteration(step, i)
			if err != nil {
				return nil, err
			}
			result = append(result, iterSteps...)
		}

		// Chain iterations: each iteration depends on previous
		if len(step.Loop.Body) > 0 && step.Loop.Count > 1 {
			result = chainLoopIterations(result, step.Loop.Body, step.Loop.Count)
		}
	} else {
		// Conditional loop: expand once with loop metadata
		// The runtime executor will re-run until condition is met or max reached
		iterSteps, err := expandLoopIteration(step, 1)
		if err != nil {
			return nil, err
		}

		// Add loop metadata to first step for runtime evaluation
		if len(iterSteps) > 0 {
			firstStep := iterSteps[0]
			// Add labels for runtime loop control
			firstStep.Labels = append(firstStep.Labels, fmt.Sprintf("loop:until:%s", step.Loop.Until))
			firstStep.Labels = append(firstStep.Labels, fmt.Sprintf("loop:max:%d", step.Loop.Max))
		}

		result = iterSteps
	}

	return result, nil
}

// expandLoopIteration expands a single iteration of a loop.
// The iteration index is used to generate unique step IDs.
func expandLoopIteration(step *Step, iteration int) ([]*Step, error) {
	result := make([]*Step, 0, len(step.Loop.Body))

	for _, bodyStep := range step.Loop.Body {
		// Create unique ID for this iteration
		iterID := fmt.Sprintf("%s.iter%d.%s", step.ID, iteration, bodyStep.ID)

		clone := &Step{
			ID:          iterID,
			Title:       bodyStep.Title,
			Description: bodyStep.Description,
			Type:        bodyStep.Type,
			Priority:    bodyStep.Priority,
			Assignee:    bodyStep.Assignee,
			Condition:   bodyStep.Condition,
			WaitsFor:    bodyStep.WaitsFor,
		}

		// Clone labels
		if len(bodyStep.Labels) > 0 {
			clone.Labels = make([]string, len(bodyStep.Labels))
			copy(clone.Labels, bodyStep.Labels)
		}

		// Clone dependencies - prefix with iteration context
		if len(bodyStep.DependsOn) > 0 {
			clone.DependsOn = make([]string, len(bodyStep.DependsOn))
			for i, dep := range bodyStep.DependsOn {
				clone.DependsOn[i] = fmt.Sprintf("%s.iter%d.%s", step.ID, iteration, dep)
			}
		}

		if len(bodyStep.Needs) > 0 {
			clone.Needs = make([]string, len(bodyStep.Needs))
			for i, need := range bodyStep.Needs {
				clone.Needs[i] = fmt.Sprintf("%s.iter%d.%s", step.ID, iteration, need)
			}
		}

		// Recursively handle children
		if len(bodyStep.Children) > 0 {
			children := make([]*Step, 0, len(bodyStep.Children))
			for _, child := range bodyStep.Children {
				childClone := cloneStepDeep(child)
				childClone.ID = fmt.Sprintf("%s.iter%d.%s", step.ID, iteration, child.ID)
				children = append(children, childClone)
			}
			clone.Children = children
		}

		result = append(result, clone)
	}

	return result, nil
}

// chainLoopIterations adds dependencies between loop iterations.
// Each iteration's first step depends on the previous iteration's last step.
func chainLoopIterations(steps []*Step, body []*Step, count int) []*Step {
	if len(body) == 0 || count < 2 {
		return steps
	}

	stepsPerIter := len(body)

	for iter := 2; iter <= count; iter++ {
		// First step of this iteration
		firstIdx := (iter - 1) * stepsPerIter
		// Last step of previous iteration
		lastStep := steps[(iter-2)*stepsPerIter+stepsPerIter-1]

		if firstIdx < len(steps) {
			steps[firstIdx].Needs = appendUnique(steps[firstIdx].Needs, lastStep.ID)
		}
	}

	return steps
}

// ApplyBranches wires fork-join dependency patterns.
// For each branch rule:
//   - All branch steps depend on the 'from' step
//   - The 'join' step depends on all branch steps
//
// Returns the modified steps slice (steps are modified in place for dependencies).
func ApplyBranches(steps []*Step, compose *ComposeRules) ([]*Step, error) {
	if compose == nil || len(compose.Branch) == 0 {
		return steps, nil
	}

	// Build step map for quick lookup
	stepMap := buildStepMap(steps)

	for _, branch := range compose.Branch {
		// Validate the branch rule
		if branch.From == "" {
			return nil, fmt.Errorf("branch: from is required")
		}
		if len(branch.Steps) == 0 {
			return nil, fmt.Errorf("branch: steps is required")
		}
		if branch.Join == "" {
			return nil, fmt.Errorf("branch: join is required")
		}

		// Verify all steps exist
		if _, ok := stepMap[branch.From]; !ok {
			return nil, fmt.Errorf("branch: from step %q not found", branch.From)
		}
		if _, ok := stepMap[branch.Join]; !ok {
			return nil, fmt.Errorf("branch: join step %q not found", branch.Join)
		}
		for _, stepID := range branch.Steps {
			if _, ok := stepMap[stepID]; !ok {
				return nil, fmt.Errorf("branch: parallel step %q not found", stepID)
			}
		}

		// Add dependencies: branch steps depend on 'from'
		for _, stepID := range branch.Steps {
			step := stepMap[stepID]
			step.Needs = appendUnique(step.Needs, branch.From)
		}

		// Add dependencies: 'join' depends on all branch steps
		joinStep := stepMap[branch.Join]
		for _, stepID := range branch.Steps {
			joinStep.Needs = appendUnique(joinStep.Needs, stepID)
		}
	}

	return steps, nil
}

// ApplyGates adds gate conditions to steps.
// For each gate rule:
//   - The target step gets a "gate:condition" label
//   - At runtime, the patrol executor evaluates the condition
//
// Returns the modified steps slice.
func ApplyGates(steps []*Step, compose *ComposeRules) ([]*Step, error) {
	if compose == nil || len(compose.Gate) == 0 {
		return steps, nil
	}

	// Build step map for quick lookup
	stepMap := buildStepMap(steps)

	for _, gate := range compose.Gate {
		// Validate the gate rule
		if gate.Before == "" {
			return nil, fmt.Errorf("gate: before is required")
		}
		if gate.Condition == "" {
			return nil, fmt.Errorf("gate: condition is required")
		}

		// Validate the condition syntax
		_, err := ParseCondition(gate.Condition)
		if err != nil {
			return nil, fmt.Errorf("gate: invalid condition %q: %w", gate.Condition, err)
		}

		// Find the target step
		step, ok := stepMap[gate.Before]
		if !ok {
			return nil, fmt.Errorf("gate: target step %q not found", gate.Before)
		}

		// Add gate label for runtime evaluation
		gateLabel := fmt.Sprintf("gate:condition:%s", gate.Condition)
		step.Labels = appendUnique(step.Labels, gateLabel)
	}

	return steps, nil
}

// ApplyControlFlow applies all control flow operators in the correct order:
// 1. Loops (expand iterations)
// 2. Branches (wire fork-join dependencies)
// 3. Gates (add condition labels)
func ApplyControlFlow(steps []*Step, compose *ComposeRules) ([]*Step, error) {
	var err error

	// Apply loops first (expands steps)
	steps, err = ApplyLoops(steps)
	if err != nil {
		return nil, fmt.Errorf("applying loops: %w", err)
	}

	// Apply branches (wires dependencies)
	steps, err = ApplyBranches(steps, compose)
	if err != nil {
		return nil, fmt.Errorf("applying branches: %w", err)
	}

	// Apply gates (adds labels)
	steps, err = ApplyGates(steps, compose)
	if err != nil {
		return nil, fmt.Errorf("applying gates: %w", err)
	}

	return steps, nil
}

// cloneStepDeep creates a deep copy of a step including children.
func cloneStepDeep(s *Step) *Step {
	clone := cloneStep(s)

	if len(s.Children) > 0 {
		clone.Children = make([]*Step, len(s.Children))
		for i, child := range s.Children {
			clone.Children[i] = cloneStepDeep(child)
		}
	}

	return clone
}
