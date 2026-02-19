package main

import (
	"strings"

	"github.com/spf13/cobra"
)

type lifecycleTransitionAssessment struct {
	Pass        bool
	Result      string
	ExitCode    int
	From        string
	To          string
	AllowedNext []string
	Message     string
	Remediation string
}

func assessLifecycleStateTransition(from, to string) lifecycleTransitionAssessment {
	trimmedFrom := strings.TrimSpace(from)
	trimmedTo := strings.TrimSpace(to)

	if trimmedFrom == "" && trimmedTo == "" {
		return lifecycleTransitionAssessment{
			Pass:     true,
			Result:   "pass",
			ExitCode: 0,
		}
	}

	if trimmedFrom == "" || trimmedTo == "" {
		return lifecycleTransitionAssessment{
			Pass:        false,
			Result:      "invalid_input",
			ExitCode:    1,
			From:        strings.ToUpper(trimmedFrom),
			To:          strings.ToUpper(trimmedTo),
			AllowedNext: []string{},
			Message:     "--state-from and --state-to must be provided together",
			Remediation: "provide both flags or omit both flags",
		}
	}

	valid, normalizedFrom, normalizedTo, allowedNext := validateSessionStateTransition(trimmedFrom, trimmedTo)
	if !valid {
		return lifecycleTransitionAssessment{
			Pass:        false,
			Result:      "policy_violation",
			ExitCode:    exitCodePolicyViolation,
			From:        normalizedFrom,
			To:          normalizedTo,
			AllowedNext: allowedNext,
			Message:     "invalid session-state transition for lifecycle command",
			Remediation: "set --state-to to one of allowed_next values for --state-from",
		}
	}

	return lifecycleTransitionAssessment{
		Pass:        true,
		Result:      "pass",
		ExitCode:    0,
		From:        normalizedFrom,
		To:          normalizedTo,
		AllowedNext: allowedNext,
	}
}

func lifecycleCommandName(cmd *cobra.Command) string {
	if cmd == nil {
		return "command"
	}
	path := strings.TrimSpace(cmd.CommandPath())
	path = strings.TrimPrefix(path, "bd ")
	if path == "" {
		return "command"
	}
	return path
}

func enforceLifecycleStateTransitionGuard(cmd *cobra.Command, from, to string) bool {
	assessment := assessLifecycleStateTransition(from, to)
	if assessment.Pass {
		return true
	}

	details := map[string]interface{}{
		"from":         assessment.From,
		"to":           assessment.To,
		"allowed_next": assessment.AllowedNext,
		"message":      assessment.Message,
		"remediation":  assessment.Remediation,
	}
	finishEnvelope(commandEnvelope{
		OK:      false,
		Command: lifecycleCommandName(cmd),
		Result:  assessment.Result,
		Details: details,
		Events:  []string{"state_transition_checked"},
	}, assessment.ExitCode)
	return false
}
