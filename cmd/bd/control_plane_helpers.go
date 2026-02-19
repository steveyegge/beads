package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

const (
	exitCodePolicyViolation = 3
	exitCodePartialState    = 4
)

var successCloseVerbs = map[string]struct{}{
	"added":        {},
	"implemented":  {},
	"refactored":   {},
	"updated":      {},
	"removed":      {},
	"migrated":     {},
	"configured":   {},
	"extracted":    {},
	"replaced":     {},
	"consolidated": {},
}

type commandEnvelope struct {
	OK              bool                   `json:"ok"`
	Command         string                 `json:"command"`
	Result          string                 `json:"result"`
	IssueID         string                 `json:"issue_id"`
	Details         map[string]interface{} `json:"details"`
	RecoveryCommand string                 `json:"recovery_command"`
	Events          []string               `json:"events"`
}

func emitEnvelope(payload commandEnvelope) {
	if payload.Details == nil {
		payload.Details = map[string]interface{}{}
	}
	if payload.Events == nil {
		payload.Events = []string{}
	}

	if jsonOutput {
		outputJSON(payload)
		return
	}

	status := "ok"
	if !payload.OK {
		status = "error"
	}
	fmt.Printf("[%s] %s -> %s\n", status, payload.Command, payload.Result)
	if payload.IssueID != "" {
		fmt.Printf("issue: %s\n", payload.IssueID)
	}
	if len(payload.Details) > 0 {
		fmt.Println("details:")
		keys := make([]string, 0, len(payload.Details))
		for k := range payload.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  - %s: %v\n", k, payload.Details[k])
		}
	}
	if payload.RecoveryCommand != "" {
		fmt.Printf("recovery: %s\n", payload.RecoveryCommand)
	}
	if len(payload.Events) > 0 {
		fmt.Println("events:")
		for _, event := range payload.Events {
			fmt.Printf("  - %s\n", event)
		}
	}
}

func finishEnvelope(payload commandEnvelope, exitCode int) {
	emitEnvelope(payload)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func appendNotesLine(existing, line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return line
	}
	return strings.TrimRight(existing, "\n") + "\n" + line
}

func lintCloseReason(reason string, allowFailureReason bool) error {
	stripped := strings.TrimSpace(reason)
	if stripped == "" {
		return fmt.Errorf("close reason is required")
	}

	lower := strings.ToLower(stripped)
	if strings.HasPrefix(lower, "failed:") {
		if allowFailureReason {
			return nil
		}
		return fmt.Errorf("failure close reasons require --allow-failure-reason")
	}

	firstToken := strings.ToLower(strings.FieldsFunc(stripped, func(r rune) bool {
		return r == ':' || r == ' ' || r == '\t'
	})[0])
	if _, ok := successCloseVerbs[firstToken]; !ok {
		return fmt.Errorf("unsafe success verb %q", firstToken)
	}

	for _, keyword := range types.FailureCloseKeywords {
		if strings.Contains(lower, keyword) {
			return fmt.Errorf("unsafe close reason contains keyword %q", keyword)
		}
	}
	return nil
}

func strictControlExplicitIDsEnabled(flag bool) bool {
	if flag {
		return true
	}
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("BD_STRICT_CONTROL")))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
