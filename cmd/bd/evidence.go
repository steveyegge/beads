package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	evidenceIssueID   string
	evidenceEnvID     string
	evidenceArtifact  string
	evidenceTimestamp string
)

var evidenceCmd = &cobra.Command{
	Use:     "evidence",
	GroupID: "issues",
	Short:   "Structured evidence tuple utilities",
}

var evidenceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Append a canonical EvidenceTuple record to issue notes",
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("evidence add")

		issueID := strings.TrimSpace(evidenceIssueID)
		envID := strings.TrimSpace(evidenceEnvID)
		artifactID := strings.TrimSpace(evidenceArtifact)
		tsRaw := strings.TrimSpace(evidenceTimestamp)
		if tsRaw == "" {
			tsRaw = time.Now().UTC().Format(time.RFC3339)
		}

		if issueID == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "evidence add",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--issue is required"},
				Events:  []string{"evidence_add_failed"},
			}, 1)
			return
		}
		if envID == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "evidence add",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--env-id is required"},
				Events:  []string{"evidence_add_failed"},
			}, 1)
			return
		}
		if artifactID == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "evidence add",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--artifact-id is required"},
				Events:  []string{"evidence_add_failed"},
			}, 1)
			return
		}

		parsedTS, err := parseEvidenceTimestamp(tsRaw)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "evidence add",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"evidence_add_failed"},
			}, 1)
			return
		}

		line, err := canonicalEvidenceTupleLine(evidenceTuple{
			TS:         parsedTS.Format(time.RFC3339),
			EnvID:      envID,
			ArtifactID: artifactID,
		})
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "evidence add",
				Result:  "policy_violation",
				Details: map[string]interface{}{"message": err.Error()},
				Events:  []string{"evidence_add_failed"},
			}, exitCodePolicyViolation)
			return
		}

		ctx := rootCtx
		issueResult, err := resolveAndGetIssueWithRouting(ctx, store, issueID)
		if err != nil {
			if issueResult != nil {
				issueResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "evidence add",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve issue %q: %v", issueID, err)},
				Events:  []string{"evidence_add_failed"},
			}, 1)
			return
		}
		if issueResult == nil || issueResult.Issue == nil {
			if issueResult != nil {
				issueResult.Close()
			}
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "evidence add",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("issue %q not found", issueID)},
				Events:  []string{"evidence_add_failed"},
			}, 1)
			return
		}
		defer issueResult.Close()

		notes := appendNotesLine(issueResult.Issue.Notes, line)
		if err := issueResult.Store.UpdateIssue(ctx, issueResult.ResolvedID, map[string]interface{}{"notes": notes}, actor); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "evidence add",
				Result:  "system_error",
				IssueID: issueResult.ResolvedID,
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to append evidence tuple: %v", err)},
				Events:  []string{"evidence_add_failed"},
			}, 1)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "evidence add",
			Result:  "evidence_added",
			IssueID: issueResult.ResolvedID,
			Details: map[string]interface{}{
				"tuple": map[string]string{
					"ts":          parsedTS.Format(time.RFC3339),
					"env_id":      envID,
					"artifact_id": artifactID,
				},
			},
			Events: []string{"evidence_tuple_added"},
		}, 0)
	},
}

func init() {
	evidenceAddCmd.Flags().StringVar(&evidenceIssueID, "issue", "", "Issue ID to append evidence to")
	evidenceAddCmd.Flags().StringVar(&evidenceEnvID, "env-id", "", "Environment identifier for evidence tuple")
	evidenceAddCmd.Flags().StringVar(&evidenceArtifact, "artifact-id", "", "Artifact identifier for evidence tuple")
	evidenceAddCmd.Flags().StringVar(&evidenceTimestamp, "ts", "", "Evidence timestamp in RFC3339 (defaults to now UTC)")
	evidenceAddCmd.ValidArgsFunction = noCompletions

	evidenceCmd.AddCommand(evidenceAddCmd)
	rootCmd.AddCommand(evidenceCmd)
}
